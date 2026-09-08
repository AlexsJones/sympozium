package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	api "github.com/sympozium-ai/sympozium/api/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func proveUnissuedCatalogueCancellation(t *testing.T, ctx context.Context, c client.Client, key types.NamespacedName, uid types.UID, posts *atomic.Int32, evidence string, browserDelete func()) {
	t.Helper()
	var run api.AgentRun
	ready := false
	for end := time.Now().Add(60 * time.Second); time.Now().Before(end) && ctx.Err() == nil; {
		must(t, c.Get(ctx, key, &run))
		if run.UID != uid || run.Status.CellnActionID != "" || run.Status.CellnIssuance != nil || posts.Load() != 0 {
			t.Fatal("unissued run changed identity or crossed issuance boundary")
		}
		condition := meta.FindStatusCondition(run.Status.Conditions, "CellnIssuanceCommitted")
		if run.Status.CellnOnly && slices.Contains(run.Finalizers, "sympozium.ai/agentrun-finalizer") && condition != nil && condition.Reason == "AwaitingRegisteredComposition" {
			ready = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !ready {
		t.Fatal("controller did not record boundary and wait before issuance")
	}
	writeJSON(t, filepath.Join(evidence, "unissued-agentrun.json"), run)
	// Exercise real API transition rules without persisting destructive edits.
	for _, patch := range []string{`{"status":{"cellnOnly":false}}`, `{"status":null}`} {
		copy := run.DeepCopy()
		err := c.Status().Patch(ctx, copy, client.RawPatch(types.MergePatchType, []byte(patch)), client.DryRunAll)
		if !apierrors.IsInvalid(err) || !strings.Contains(err.Error(), "boundary cannot be removed") {
			t.Fatalf("boundary removal was not refused by validation: %v", err)
		}
	}
	copy := run.DeepCopy()
	err := c.Patch(ctx, copy, client.RawPatch(types.MergePatchType, []byte(`{"spec":{"backend":"job"}}`)), client.DryRunAll)
	if !apierrors.IsInvalid(err) || !strings.Contains(err.Error(), "cannot change execution backend") {
		t.Fatalf("backend change was not refused by validation: %v", err)
	}
	started := time.Now()
	if browserDelete != nil {
		browserDelete()
	} else {
		must(t, c.Delete(ctx, &run, &client.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}}))
	}
	deleted := false
	for end := time.Now().Add(45 * time.Second); time.Now().Before(end) && ctx.Err() == nil; {
		var current api.AgentRun
		err := c.Get(ctx, key, &current)
		if apierrors.IsNotFound(err) {
			deleted = true
			break
		}
		must(t, err)
		if current.UID != uid {
			t.Fatal("deleted run was replaced")
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !deleted || posts.Load() != 0 {
		t.Fatal("unissued cancellation failed finalization or submitted execution")
	}
	var jobs batchv1.JobList
	must(t, c.List(ctx, &jobs, client.InNamespace(key.Namespace)))
	if len(jobs.Items) != 0 {
		t.Fatal("unissued cancellation created Jobs")
	}
	writeJSON(t, filepath.Join(evidence, "unissued-cancellation.json"), map[string]any{"status": "execution-checks-passed", "scope": "controller-Pod cancellation before registered issuance; not active-cell teardown; final cleanup in test-outcome.json", "browserCancellation": browserDelete != nil, "controllerPodImage": os.Getenv("CELLN_LIVE_CONTROLLER_IMAGE"), "runUID": uid, "namespace": key.Namespace, "boundaryRecorded": true, "boundaryRemovalRefused": true, "statusRemovalRefused": true, "backendChangeRefused": true, "finalizerCompleted": true, "executionPosts": posts.Load(), "jobs": len(jobs.Items), "cancelToDeletionMilliseconds": time.Since(started).Milliseconds()})
	t.Log("PASS unissued controller-Pod cancellation and real API boundary refusals")
}
