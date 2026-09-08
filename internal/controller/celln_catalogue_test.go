package controller

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	api "github.com/sympozium-ai/sympozium/api/v1alpha1"
	"github.com/sympozium-ai/sympozium/internal/cellnreview"
	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type catalogueFixture struct {
	record                  *cellnreview.RouterExecution
	pending, lookup, cancel int
	beforePending           func()
	pendingErr              error
}

func TestUnissuedCatalogueRunWaitsWithoutLegacyExecution(t *testing.T) {
	for _, configured := range []bool{false, true} {
		ctx := context.Background()
		run := newTestCellnRun(t, "catalogue-wait", "catalogue-wait-uid")
		run.Spec.Celln = nil
		run.Spec.CellnSelection = &api.CellnCatalogueSelection{ToolRefs: []api.CellnCatalogueToolRef{}}
		r := newAgentRunTestReconciler(t, run)
		r.APIReader = r.Client
		f := &catalogueFixture{}
		if configured {
			r.CatalogueDispatcher = f
		}
		request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(run)}
		for i := 0; i < 2; i++ {
			result, err := r.Reconcile(ctx, request)
			if err != nil || result.RequeueAfter == 0 {
				t.Fatalf("did not wait: %v %v", result, err)
			}
		}
		var stored api.AgentRun
		if err := r.Get(ctx, request.NamespacedName, &stored); err != nil {
			t.Fatal(err)
		}
		condition := meta.FindStatusCondition(stored.Status.Conditions, "CellnIssuanceCommitted")
		want := "DispatcherNotConfigured"
		if configured {
			want = "AwaitingIssuance"
		}
		if condition == nil || condition.Reason != want || condition.Status != "False" || stored.Status.CellnActionID != "" || stored.Status.StartedAt != nil || f.pending != 0 {
			t.Fatal("unissued selection was dispatched or readiness implied")
		}
		var jobs batchv1.JobList
		if err := r.List(ctx, &jobs); err != nil || len(jobs.Items) != 0 {
			t.Fatal("unissued selection created a Job")
		}
	}
}

func (f *catalogueFixture) ReconcilePending(context.Context, types.NamespacedName, func(context.Context, *api.AgentRun) error) (*cellnreview.RouterExecution, error) {
	f.pending++
	if f.beforePending != nil {
		f.beforePending()
	}
	return f.record, f.pendingErr
}

type failedCatalogueProvisioner struct{ catalogueFixture }

func (f *failedCatalogueProvisioner) EnsureIssued(context.Context, types.NamespacedName, func(context.Context, *api.AgentRun) error) error {
	return fmt.Errorf("private issuer token/secret detail")
}

func TestCatalogueProgressReportsSafeRetryAndDoesNotChangeTerminal(t *testing.T) {
	ctx := context.Background()
	run := newTestCellnRun(t, "progress", "progress-uid")
	run.Spec.Celln = nil
	run.Spec.CellnSelection = &api.CellnCatalogueSelection{ToolRefs: []api.CellnCatalogueToolRef{}}
	r := newAgentRunTestReconciler(t, run)
	r.APIReader = r.Client
	r.CatalogueDispatcher = &failedCatalogueProvisioner{}
	if _, err := r.awaitCatalogueIssuance(ctx, logr.Discard(), run); err == nil {
		t.Fatal("issuance error swallowed")
	}
	var got api.AgentRun
	if err := r.Get(ctx, client.ObjectKeyFromObject(run), &got); err != nil {
		t.Fatal(err)
	}
	c := meta.FindStatusCondition(got.Status.Conditions, "CellnIssuanceCommitted")
	if c == nil || c.Reason != "IssuanceNeedsAttention" || strings.Contains(c.Message, "secret") || got.Status.CellnActionID != "" {
		t.Fatalf("unsafe or missing progress: %+v", c)
	}
	r.CatalogueDispatcher = &catalogueFixture{pendingErr: fmt.Errorf("ambiguous POST secret")}
	if _, err := r.reconcilePendingCatalogue(ctx, logr.Discard(), run); err == nil {
		t.Fatal("dispatch error swallowed")
	}
	if err := r.Get(ctx, client.ObjectKeyFromObject(run), &got); err != nil {
		t.Fatal(err)
	}
	c = meta.FindStatusCondition(got.Status.Conditions, "CellnExecutionObserved")
	if c == nil || c.Status != "Unknown" || strings.Contains(c.Message, "secret") || !strings.Contains(c.Message, "Do not resubmit") {
		t.Fatalf("ambiguous outcome misreported: %+v", c)
	}
	rv := got.ResourceVersion
	if _, err := r.reconcilePendingCatalogue(ctx, logr.Discard(), run); err == nil {
		t.Fatal("dispatch error swallowed")
	}
	if err := r.Get(ctx, client.ObjectKeyFromObject(run), &got); err != nil {
		t.Fatal(err)
	}
	if got.ResourceVersion != rv {
		t.Fatal("identical observation churned status")
	}
	got.Status.Phase = api.AgentRunPhaseSucceeded
	if err := r.Status().Update(ctx, &got); err != nil {
		t.Fatal(err)
	}
	if err := r.catalogueProgress(ctx, run, "CellnExecutionObserved", "False", "Stale", "stale"); err != nil {
		t.Fatal(err)
	}
	if err := r.Get(ctx, client.ObjectKeyFromObject(run), &got); err != nil {
		t.Fatal(err)
	}
	if meta.FindStatusCondition(got.Status.Conditions, "CellnExecutionObserved").Reason == "Stale" {
		t.Fatal("terminal state changed")
	}
}
func (f *catalogueFixture) Lookup(context.Context, types.NamespacedName) (*cellnreview.RouterExecution, error) {
	f.lookup++
	return f.record, nil
}
func (f *catalogueFixture) Cancel(context.Context, types.NamespacedName) (*cellnreview.RouterExecution, error) {
	f.cancel++
	return f.record, nil
}

func TestCatalogueControllerRecoversWithoutOCIOrLegacyID(t *testing.T) {
	ctx := context.Background()
	run := newTestCellnRun(t, "catalogue-recovery", "catalogue-uid")
	run.Status.CellnIssuance = &api.CellnIssuanceStatus{Phase: "Issued"}
	run.Status.CellnActionID = "celln-catalogue-derived-id"
	run.Status.CellnRequest = `{"id":"celln-catalogue-derived-id"}`
	r := newAgentRunTestReconciler(t, run)
	r.APIReader = r.Client
	f := &catalogueFixture{record: &cellnreview.RouterExecution{RequestID: run.Status.CellnActionID, Phase: "Running"}}
	r.CatalogueDispatcher = f
	// There is deliberately no Agent/OCI runtime in this fixture. Observing
	// already-issued work must not require those mutable prerequisites.
	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(run)}
	if _, err := r.Reconcile(ctx, request); err != nil {
		t.Fatal(err)
	}
	var stored api.AgentRun
	if err := r.Get(ctx, client.ObjectKeyFromObject(run), &stored); err != nil {
		t.Fatal(err)
	}
	if stored.Status.Phase != api.AgentRunPhaseRunning || stored.Status.StartedAt == nil || stored.Status.CellnActionID != "celln-catalogue-derived-id" {
		t.Fatal("catalogue identity or phase was lost")
	}
	if !slices.Contains(stored.Finalizers, agentRunFinalizer) {
		t.Fatal("full reconciliation did not retain cleanup finalizer")
	}
	var jobs batchv1.JobList
	if err := r.List(ctx, &jobs); err != nil || len(jobs.Items) != 0 {
		t.Fatal("catalogue recovery created a Job")
	}
	if _, err := r.Reconcile(ctx, request); err != nil {
		t.Fatal(err)
	}
	f.record.Phase = "Cancelling"
	if done, err := r.cancelCelln(ctx, &stored); err != nil || done {
		t.Fatalf("cancellation implied teardown: %v", err)
	}
	f.record.Phase = "Cancelled"
	if done, err := r.cancelCelln(ctx, &stored); err != nil || !done {
		t.Fatalf("terminal cancellation refused catalogue ID: %v", err)
	}
	if f.pending != 1 || f.lookup != 1 || f.cancel != 2 {
		t.Fatal("wrong catalogue lifecycle calls")
	}
}

func TestCatalogueFullDeletionWaitsForTerminalCancellation(t *testing.T) {
	ctx := context.Background()
	run := newTestCellnRun(t, "catalogue-delete", "delete-uid")
	run.Finalizers = []string{agentRunFinalizer}
	run.Status.Phase = api.AgentRunPhaseRunning
	run.Status.CellnIssuance = &api.CellnIssuanceStatus{Phase: "Issued"}
	run.Status.CellnActionID = "catalogue-delete-id"
	run.Status.CellnRequest = `{"id":"catalogue-delete-id"}`
	r := newAgentRunTestReconciler(t, run)
	r.APIReader = r.Client
	f := &catalogueFixture{record: &cellnreview.RouterExecution{RequestID: run.Status.CellnActionID, Phase: "Cancelling"}}
	r.CatalogueDispatcher = f
	if err := r.Delete(ctx, run); err != nil {
		t.Fatal(err)
	}
	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(run)}
	result, err := r.Reconcile(ctx, request)
	if err != nil || result.RequeueAfter == 0 {
		t.Fatalf("deletion did not wait for cleanup: %v", err)
	}
	var current api.AgentRun
	if err := r.Get(ctx, request.NamespacedName, &current); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(current.Finalizers, agentRunFinalizer) {
		t.Fatal("cleanup finalizer removed before terminal cancellation")
	}
	f.record.Phase = "Cancelled"
	if _, err := r.Reconcile(ctx, request); err != nil {
		t.Fatal(err)
	}
	if err := r.Get(ctx, request.NamespacedName, &current); !apierrors.IsNotFound(err) {
		t.Fatalf("terminal cleanup did not release deletion: %v", err)
	}
	if f.cancel != 2 || f.pending != 0 || f.lookup != 0 {
		t.Fatal("deletion selected wrong operation")
	}
}

func TestCatalogueControllerCannotRegressConcurrentTerminalState(t *testing.T) {
	ctx := context.Background()
	run := newTestCellnRun(t, "terminal-race", "terminal-uid")
	run.Status.CellnIssuance = &api.CellnIssuanceStatus{Phase: "Issued"}
	run.Status.CellnActionID = "catalogue-terminal"
	run.Status.CellnRequest = `{"id":"catalogue-terminal"}`
	r := newAgentRunTestReconciler(t, run)
	r.APIReader = r.Client
	f := &catalogueFixture{record: &cellnreview.RouterExecution{RequestID: run.Status.CellnActionID, Phase: "Running"}}
	f.beforePending = func() {
		var current api.AgentRun
		if err := r.Get(ctx, client.ObjectKeyFromObject(run), &current); err != nil {
			t.Fatal(err)
		}
		current.Status.Phase = api.AgentRunPhaseSucceeded
		if err := r.Status().Update(ctx, &current); err != nil {
			t.Fatal(err)
		}
	}
	r.CatalogueDispatcher = f
	if _, err := r.reconcilePending(ctx, logr.Discard(), run); err == nil {
		t.Fatal("concurrent terminal state regressed")
	}
	var current api.AgentRun
	if err := r.Get(ctx, client.ObjectKeyFromObject(run), &current); err != nil {
		t.Fatal(err)
	}
	if current.Status.Phase != api.AgentRunPhaseSucceeded {
		t.Fatal("terminal state changed")
	}
}
