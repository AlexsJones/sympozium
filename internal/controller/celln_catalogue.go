package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"time"

	"github.com/go-logr/logr"
	api "github.com/sympozium-ai/sympozium/api/v1alpha1"
	"github.com/sympozium-ai/sympozium/internal/cellnreview"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type CatalogueDispatcher interface {
	ReconcilePending(context.Context, types.NamespacedName, func(context.Context, *api.AgentRun) error) (*cellnreview.RouterExecution, error)
	Lookup(context.Context, types.NamespacedName) (*cellnreview.RouterExecution, error)
	Cancel(context.Context, types.NamespacedName) (*cellnreview.RouterExecution, error)
}

type catalogueProvisioner interface {
	EnsureIssued(context.Context, types.NamespacedName, func(context.Context, *api.AgentRun) error) error
}

// A named catalogue request must never fall through to forge/explicit artifact
// execution while an operator or provisioning controller prepares issuance.
func (r *AgentRunReconciler) awaitCatalogueIssuance(ctx context.Context, log logr.Logger, run *api.AgentRun) (ctrl.Result, error) {
	condition := metav1.Condition{Type: "CellnIssuanceCommitted", Status: metav1.ConditionFalse, Reason: "AwaitingIssuance", Message: "Waiting for trusted catalogue issuance; no execution has been submitted", ObservedGeneration: run.Generation}
	if r.CatalogueDispatcher == nil {
		condition.Reason = "DispatcherNotConfigured"
		condition.Message = "Catalogue controller binding is not configured; no execution has been submitted"
	}
	if provisioner, ok := r.CatalogueDispatcher.(catalogueProvisioner); ok {
		if r.APIReader == nil {
			return ctrl.Result{}, fmt.Errorf("uncached reader required for automatic catalogue issuance")
		}
		err := provisioner.EnsureIssued(ctx, client.ObjectKeyFromObject(run), func(ctx context.Context, current *api.AgentRun) error { return r.catalogueAdmission(ctx, log, current) })
		if err == nil {
			var current api.AgentRun
			if err := r.APIReader.Get(ctx, client.ObjectKeyFromObject(run), &current); err != nil {
				return ctrl.Result{}, err
			}
			return r.reconcilePendingCatalogue(ctx, log, &current)
		}
		if !errors.Is(err, cellnreview.ErrNoRegisteredComposition) {
			return ctrl.Result{}, err
		}
		condition.Reason = "AwaitingRegisteredComposition"
		condition.Message = "No operator-registered admitted composition matches these source closures; no execution has been submitted"
	}
	result := ctrl.Result{RequeueAfter: 5 * time.Second}
	if existing := meta.FindStatusCondition(run.Status.Conditions, condition.Type); existing != nil && existing.Status == condition.Status && existing.Reason == condition.Reason && existing.Message == condition.Message && existing.ObservedGeneration == condition.ObservedGeneration {
		return result, nil
	}
	err := r.updateStatusWithRetry(ctx, run, func(current *api.AgentRun) {
		if current.Status.CellnIssuance != nil {
			return
		}
		meta.SetStatusCondition(&current.Status.Conditions, condition)
	})
	return result, err
}

func (r *AgentRunReconciler) catalogueAdmission(ctx context.Context, log logr.Logger, current *api.AgentRun) error {
	if os.Getenv("CELLN_HARNESS_ENABLED") != "true" {
		return fmt.Errorf("Celln Harness is disabled")
	}
	if err := r.validatePolicy(ctx, current); err != nil {
		return err
	}
	if err := validateGateHooks(current); err != nil {
		return err
	}
	return r.checkTokenBudget(ctx, log, current)
}

func catalogueRecord(record *cellnreview.RouterExecution) (executionRecord, error) {
	var out executionRecord
	if record == nil {
		return out, fmt.Errorf("missing catalogue execution record")
	}
	data, err := json.Marshal(record)
	if err != nil {
		return out, err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&out) != nil || d.Decode(new(any)) != io.EOF {
		return out, fmt.Errorf("incompatible catalogue receipt")
	}
	return out, nil
}

func (r *AgentRunReconciler) reconcilePendingCatalogue(ctx context.Context, log logr.Logger, run *api.AgentRun) (ctrl.Result, error) {
	if r.CatalogueDispatcher == nil || r.APIReader == nil {
		return ctrl.Result{}, fmt.Errorf("Celln catalogue dispatcher and uncached reader are not configured")
	}
	record, err := r.CatalogueDispatcher.ReconcilePending(ctx, client.ObjectKeyFromObject(run), func(ctx context.Context, current *api.AgentRun) error {
		return r.catalogueAdmission(ctx, log, current)
	})
	if err != nil {
		return ctrl.Result{}, err
	}
	parsed, err := catalogueRecord(record)
	if err != nil {
		return ctrl.Result{}, err
	}
	// Refresh the durable journal written by the service, never a mutated task
	// normalized through the OCI Harness path. Do not regress a concurrent terminal.
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var current api.AgentRun
		if err := r.APIReader.Get(ctx, client.ObjectKeyFromObject(run), &current); err != nil {
			return err
		}
		if current.UID != run.UID || current.DeletionTimestamp != nil || current.Status.CellnIssuance == nil || current.Status.CellnRequest == "" || current.Status.CellnActionID != parsed.RequestID || (current.Status.Phase != "" && current.Status.Phase != api.AgentRunPhasePending && current.Status.Phase != api.AgentRunPhaseRunning) {
			return fmt.Errorf("catalogue run changed during dispatch")
		}
		current.Status.Phase = api.AgentRunPhaseRunning
		meta.SetStatusCondition(&current.Status.Conditions, metav1.Condition{Type: "CellnIssuanceCommitted", Status: metav1.ConditionTrue, Reason: "Issued", Message: "Trusted catalogue issuance and exact dispatch identity are committed", ObservedGeneration: current.Generation})
		if current.Status.StartedAt == nil {
			now := metav1.Now()
			current.Status.StartedAt = &now
		}
		if err := r.Status().Update(ctx, &current); err != nil {
			return err
		}
		*run = current
		return nil
	})
	if err != nil {
		return ctrl.Result{}, err
	}
	return r.applyCellnRecord(ctx, log, run, parsed)
}

func (r *AgentRunReconciler) reconcileRunningCatalogue(ctx context.Context, log logr.Logger, run *api.AgentRun) (ctrl.Result, error) {
	if r.CatalogueDispatcher == nil {
		return ctrl.Result{}, fmt.Errorf("Celln catalogue dispatcher is not configured")
	}
	record, err := r.CatalogueDispatcher.Lookup(ctx, client.ObjectKeyFromObject(run))
	if err != nil {
		return ctrl.Result{}, err
	}
	parsed, err := catalogueRecord(record)
	if err != nil {
		return ctrl.Result{}, err
	}
	return r.applyCellnRecord(ctx, log, run, parsed)
}

func (r *AgentRunReconciler) cancelCatalogue(ctx context.Context, run *api.AgentRun) (bool, error) {
	if r.CatalogueDispatcher == nil {
		return false, fmt.Errorf("Celln catalogue dispatcher is not configured")
	}
	record, err := r.CatalogueDispatcher.Cancel(ctx, client.ObjectKeyFromObject(run))
	if err != nil {
		return false, err
	}
	parsed, err := catalogueRecord(record)
	if err != nil {
		return false, err
	}
	if parsed.RequestID != run.Status.CellnActionID {
		return false, fmt.Errorf("catalogue cancel identity mismatch")
	}
	if !slices.Contains([]string{"Succeeded", "Failed", "Refused", "Cancelled"}, parsed.Phase) {
		return false, nil
	}
	if parsed.Receipt != nil {
		if err := validateCellnReceipt(run, parsed); err != nil {
			return false, err
		}
		data, _ := json.Marshal(parsed.Receipt)
		if err := r.updateStatusWithRetry(ctx, run, func(current *api.AgentRun) { current.Status.CellnReceipt = string(data) }); err != nil {
			return false, err
		}
	} else if parsed.Phase == "Succeeded" {
		return false, fmt.Errorf("catalogue success lacks terminal receipt")
	}
	return true, nil
}
