package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	api "github.com/sympozium-ai/sympozium/api/v1alpha1"
	"github.com/sympozium-ai/sympozium/internal/cellnreview"
	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// This mode is deliberately separate from success evidence. It cancels via a
// Kubernetes deletion with the actual controller/finalizer, not a direct backend
// cancel request. No terminal-before-cancel execution is accepted as a pass.
func proveActiveCatalogueCancellation(t *testing.T, ctx context.Context, c client.Client, key types.NamespacedName, uid types.UID, root, backend, backendToken string, router *httptest.Server, routerToken, evidence string, browserDelete func()) {
	t.Helper()
	var run api.AgentRun
	var node struct {
		Node struct {
			LiveCells int `json:"live_cells"`
		} `json:"node"`
	}
	active := false
	var activeCell string
	for end := time.Now().Add(150 * time.Second); time.Now().Before(end) && ctx.Err() == nil; {
		must(t, c.Get(ctx, key, &run))
		if run.UID != uid {
			t.Fatal("cancellation target identity changed")
		}
		if run.Status.Phase == api.AgentRunPhaseSucceeded || run.Status.Phase == api.AgentRunPhaseFailed {
			t.Fatalf("run became terminal before active cancellation: %s %s", run.Status.Phase, run.Status.Error)
		}
		if run.Status.Phase == api.AgentRunPhaseRunning && run.Status.CellnActionID != "" {
			must(t, json.Unmarshal(authenticatedGet(t, ctx, &http.Client{Timeout: 5 * time.Second}, backend+"/v1/node", backendToken), &node))
			var owner cellnreview.RouterExecution
			must(t, json.Unmarshal(authenticatedGet(t, ctx, router.Client(), router.URL+"/v1/executions/"+run.Status.CellnActionID, routerToken), &owner))
			// Celln currently retains Resolving during synchronous launch_declared.
			// run_cell writes this registry entry only after constructing LinuxCell.
			// Node live_cells alone counts reservations, not necessarily live VMs.
			entries, err := os.ReadDir(filepath.Join(root, "cells"))
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				var record struct {
					ID     string `json:"id"`
					Status string `json:"status"`
				}
				raw, err := os.ReadFile(filepath.Join(root, "cells", entry.Name()))
				must(t, err)
				if json.Unmarshal(raw, &record) == nil && record.Status == "running" {
					activeCell = record.ID
				}
			}
			if node.Node.LiveCells > 0 && activeCell != "" && owner.RequestID == run.Status.CellnActionID && (owner.Phase == "Running" || owner.Phase == "Resolving") {
				active = true
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !active {
		t.Fatal("no correlated running owner and live cell observed; cancellation not proven")
	}
	writeJSON(t, filepath.Join(evidence, "before-cancel-agentrun.json"), run)
	writeJSON(t, filepath.Join(evidence, "before-cancel-node.json"), node)
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
			t.Fatal("cancellation target replaced")
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !deleted {
		t.Fatal("active run finalizer did not complete")
	}
	cancelToDeletion := time.Since(started).Milliseconds()
	var owner cellnreview.RouterExecution
	must(t, json.Unmarshal(authenticatedGet(t, ctx, router.Client(), router.URL+"/v1/executions/"+run.Status.CellnActionID, routerToken), &owner))
	if owner.RequestID != run.Status.CellnActionID || owner.Phase != "Cancelled" {
		t.Fatalf("original owner did not confirm cancellation: %+v", owner)
	}
	var receipt struct {
		CellID string `json:"cellId"`
	}
	must(t, json.Unmarshal(owner.Receipt, &receipt))
	if receipt.CellID != activeCell {
		t.Fatal("cancelled receipt does not match the observed live cell")
	}
	must(t, json.Unmarshal(authenticatedGet(t, ctx, &http.Client{Timeout: 5 * time.Second}, backend+"/v1/node", backendToken), &node))
	if node.Node.LiveCells != 0 {
		t.Fatal("cell remains live after cancellation finalizer completed")
	}
	var jobs batchv1.JobList
	must(t, c.List(ctx, &jobs, client.InNamespace(key.Namespace)))
	if len(jobs.Items) != 0 {
		t.Fatal("cancellation created workload Jobs")
	}
	var audit struct {
		Events []struct {
			Phase string `json:"phase"`
		} `json:"events"`
	}
	must(t, json.Unmarshal(authenticatedGet(t, ctx, router.Client(), router.URL+"/v1/executions/"+run.Status.CellnActionID+"/audit", routerToken), &audit))
	dissolved := false
	for _, event := range audit.Events {
		if event.Phase == "Dissolved" {
			dissolved = true
		}
	}
	if !dissolved {
		t.Fatal("cancelled owner audit lacks dissolution")
	}
	writeJSON(t, filepath.Join(evidence, "cancelled-owner.json"), owner)
	writeJSON(t, filepath.Join(evidence, "cancelled-audit.json"), audit)
	writeJSON(t, filepath.Join(evidence, "summary.json"), map[string]any{"status": "execution-checks-passed", "scope": "actual controller/issuer/router/KVM and isolated Kind deletion; final cleanup outcome is in test-outcome.json", "browserCancellation": browserDelete != nil, "controllerPodImage": os.Getenv("CELLN_LIVE_CONTROLLER_IMAGE"), "standaloneIssuer": os.Getenv("CELLN_LIVE_ISSUER_PROCESS") == "1", "namespace": key.Namespace, "runUID": uid, "action": run.Status.CellnActionID, "activeCellObserved": true, "cellID": activeCell, "ownerPhase": owner.Phase, "finalizerCompleted": true, "liveCells": node.Node.LiveCells, "jobs": len(jobs.Items), "dissolved": dissolved, "cancelToDeletionMilliseconds": cancelToDeletion})
	t.Logf("PASS active catalogue cancellation: uid=%s action=%s terminal owner retained, dissolved, zero live cells/Jobs", uid, run.Status.CellnActionID)
}
