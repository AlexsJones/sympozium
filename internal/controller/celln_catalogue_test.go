package controller

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	api "github.com/sympozium-ai/sympozium/api/v1alpha1"
	"github.com/sympozium-ai/sympozium/internal/cellnreview"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type catalogueFixture struct {
	record                  *cellnreview.RouterExecution
	pending, lookup, cancel int
	beforePending           func()
}

func (f *catalogueFixture) ReconcilePending(context.Context, types.NamespacedName, func(context.Context, *api.AgentRun) error) (*cellnreview.RouterExecution, error) {
	f.pending++
	if f.beforePending != nil {
		f.beforePending()
	}
	return f.record, nil
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
	if _, err := r.reconcilePending(ctx, logr.Discard(), run); err != nil {
		t.Fatal(err)
	}
	var stored api.AgentRun
	if err := r.Get(ctx, client.ObjectKeyFromObject(run), &stored); err != nil {
		t.Fatal(err)
	}
	if stored.Status.Phase != api.AgentRunPhaseRunning || stored.Status.StartedAt == nil || stored.Status.CellnActionID != "celln-catalogue-derived-id" {
		t.Fatal("catalogue identity or phase was lost")
	}
	if _, err := r.reconcileRunningCelln(ctx, logr.Discard(), &stored); err != nil {
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
