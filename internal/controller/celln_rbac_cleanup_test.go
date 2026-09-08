package controller

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/go-logr/logr"
	api "github.com/sympozium-ai/sympozium/api/v1alpha1"
	"github.com/sympozium-ai/sympozium/internal/cellnreview"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type rbacReadObserver struct {
	client.Client
	lists int
}

func TestFreshCellnBoundaryIsPersistedBeforeFinalizer(t *testing.T) {
	ctx := context.Background()
	run := newTestCellnRun(t, "fresh-boundary", "fresh-boundary-uid")
	run.Status = api.AgentRunStatus{}
	run.Finalizers = nil
	r := newAgentRunTestReconciler(t, run)
	result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(run)})
	if err != nil || !result.Requeue {
		t.Fatalf("persist boundary: result=%+v err=%v", result, err)
	}
	var current api.AgentRun
	if err := r.Get(ctx, client.ObjectKeyFromObject(run), &current); err != nil {
		t.Fatal(err)
	}
	if !current.Status.CellnOnly || len(current.Finalizers) != 0 || current.Status.CellnActionID != "" {
		t.Fatal("boundary was not recorded before finalizer/execution")
	}
	// A user editing backend must not turn this cleanup exemption into Job
	// authority. Exercise the public reconciler, not just the cleanup helper.
	current.Spec.Backend = ""
	if err := r.Update(ctx, &current); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(run)}); err != nil {
		t.Fatal(err)
	}
	if err := r.Get(ctx, client.ObjectKeyFromObject(run), &current); err != nil {
		t.Fatal(err)
	}
	if current.Status.Phase != api.AgentRunPhaseFailed || current.Status.JobName != "" || !current.Status.CellnOnly {
		t.Fatal("backend mutation crossed recorded execution boundary")
	}
}

func TestUnissuedRecordedCellnDeletionCompletesWithoutClusterRBAC(t *testing.T) {
	ctx := context.Background()
	run := newTestCellnRun(t, "unissued-cleanup", "unissued-cleanup-uid")
	run.Status = api.AgentRunStatus{CellnOnly: true, Phase: api.AgentRunPhasePending}
	run.Finalizers = []string{agentRunFinalizer}
	r := newAgentRunTestReconciler(t, run)
	observer := &rbacReadObserver{Client: r.Client}
	r.Client, r.APIReader = observer, observer
	if err := r.Delete(ctx, run); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(run)}); err != nil {
		t.Fatal(err)
	}
	var current api.AgentRun
	if err := r.Get(ctx, client.ObjectKeyFromObject(run), &current); !apierrors.IsNotFound(err) {
		t.Fatalf("unissued finalizer did not complete: %v", err)
	}
	if observer.lists != 0 {
		t.Fatal("unissued recorded Celln deletion queried cluster RBAC")
	}
}

func (c *rbacReadObserver) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	switch list.(type) {
	case *rbacv1.ClusterRoleList, *rbacv1.ClusterRoleBindingList:
		c.lists++
		return fmt.Errorf("cluster RBAC read denied")
	}
	return c.Client.List(ctx, list, opts...)
}

func TestRecordedCellnCleanupDoesNotRequireJobRBAC(t *testing.T) {
	for _, kind := range []string{"celln", "unrecorded", "unissued-recorded", "recorded-job", "job", "pod", "sandbox", "claim", "deployment", "service", "post-run"} {
		t.Run(kind, func(t *testing.T) {
			run := newTestCellnRun(t, "cleanup", "cleanup-uid")
			run.Status.CellnActionID = "original"
			run.Status.CellnRequest = `{"id":"original"}`
			switch kind {
			case "unrecorded":
				run.Status.CellnActionID = ""
			case "unissued-recorded", "recorded-job":
				run.Status.CellnActionID, run.Status.CellnRequest = "", ""
				run.Status.CellnOnly = true
				if kind == "recorded-job" {
					run.Status.JobName = "legacy"
				}
			case "job":
				run.Status.JobName = "legacy"
			case "pod":
				run.Status.PodName = "legacy"
			case "sandbox":
				run.Status.SandboxName = "legacy"
			case "claim":
				run.Status.SandboxClaimName = "legacy"
			case "deployment":
				run.Status.DeploymentName = "legacy"
			case "service":
				run.Status.ServiceName = "legacy"
			case "post-run":
				run.Status.PostRunJobName = "legacy"
			}
			r := newAgentRunTestReconciler(t, run)
			observer := &rbacReadObserver{Client: r.Client}
			r.Client = observer
			r.cleanupSkillRBAC(context.Background(), logr.Discard(), run)
			want := 2
			if kind == "celln" || kind == "unissued-recorded" {
				want = 0
			}
			if observer.lists != want {
				t.Fatalf("cluster RBAC reads=%d want %d", observer.lists, want)
			}
		})
	}
}

func TestCellnTerminalFinalizerCompletesWithoutClusterRBAC(t *testing.T) {
	ctx := context.Background()
	run := newTestCellnRun(t, "terminal-cleanup", "terminal-cleanup-uid")
	run.Status.Phase = api.AgentRunPhaseSucceeded
	run.Status.CellnActionID = "original"
	run.Status.CellnRequest = `{"id":"original"}`
	run.Status.CellnIssuance = &api.CellnIssuanceStatus{Phase: "Issued"}
	run.Finalizers = []string{agentRunFinalizer}
	r := newAgentRunTestReconciler(t, run)
	observer := &rbacReadObserver{Client: r.Client}
	r.Client = observer
	r.APIReader = observer
	r.CatalogueDispatcher = &catalogueFixture{record: &cellnreview.RouterExecution{RequestID: "original", Phase: "Cancelled"}}
	if _, err := r.reconcileCompleted(ctx, logr.Discard(), run); err != nil {
		t.Fatal(err)
	}
	if observer.lists != 0 {
		t.Fatal("Celln completion queried cluster RBAC")
	}
	var current api.AgentRun
	if err := r.Get(ctx, client.ObjectKeyFromObject(run), &current); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(current.Finalizers, agentRunFinalizer) {
		t.Fatal("terminal cleanup retained finalizer")
	}
}
