package cellnreview

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	api "github.com/sympozium-ai/sympozium/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestRunDispatcherRecoversAndCancelsWithoutFreshApproval(t *testing.T) {
	f, m, _ := managedFixture(t)
	ctx := context.Background()
	candidate, err := f.l.BuildExecution(ctx, *f.f, *f.a, f.artifacts)
	if err != nil {
		t.Fatal(err)
	}
	var identity struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(candidate.Request, &identity); err != nil {
		t.Fatal(err)
	}
	var gets, cancels, posts atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		phase := "Running"
		switch {
		case r.Method == "GET" && r.URL.Path == "/v1/executions/"+identity.ID:
			gets.Add(1)
		case r.Method == "POST" && r.URL.Path == "/v1/executions/"+identity.ID+"/cancel":
			cancels.Add(1)
			phase = "Cancelling"
		default:
			posts.Add(1)
			t.Error("recovery tried to prewarm or submit")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(RouterExecution{RequestID: identity.ID, Phase: phase})
	}))
	defer server.Close()
	dir := t.TempDir()
	ca, routerToken := filepath.Join(dir, "ca.pem"), filepath.Join(dir, "router-token")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(routerToken, []byte("public-recovery-router-token"), 0600); err != nil {
		t.Fatal(err)
	}
	router, err := NewRouterClient(server.URL, routerToken, ca)
	if err != nil {
		t.Fatal(err)
	}
	defer router.CloseIdleConnections()
	endpoint, _, tokenPath := serveTestIssuer(t, m)
	issuer, err := NewIssuerClient(IssuerClientOptions{URL: endpoint, TokenFile: tokenPath, CAFile: filepath.Join(filepath.Dir(tokenPath), "cert.pem"), Route: &DispatchRoute{RouterURL: server.URL, Backend: "http://host-a:8787"}})
	if err != nil {
		t.Fatal(err)
	}
	defer issuer.CloseIdleConnections()
	key := types.NamespacedName{Namespace: f.f.Run.Namespace, Name: f.f.Run.Name}
	seed := &IssuerRequest{APIVersion: "sympozium.ai/celln-issuer-request-v1", Frozen: *f.f, Approval: *f.a, Artifacts: f.artifacts}
	if _, err := issuer.IssueForRun(ctx, f.c, f.c, key, f.l, seed); err != nil {
		t.Fatal(err)
	}
	if _, err := issuer.FreezeIssuedDispatch(ctx, f.c, f.c, key, f.l); err != nil {
		t.Fatal(err)
	}
	d, err := NewRunDispatcher(f.c, f.c, map[types.NamespacedName]RunDispatchBinding{{Namespace: f.f.Snapshot.Agent.Namespace, Name: f.f.Snapshot.Agent.Name}: {Issuer: issuer, Router: router, Loader: f.l}})
	if err != nil {
		t.Fatal(err)
	}
	var policy corev1.ConfigMap
	if err := f.c.Get(ctx, f.l.Source, &policy); err != nil {
		t.Fatal(err)
	}
	if err := f.c.Delete(ctx, &policy); err != nil {
		t.Fatal(err)
	}
	if record, err := d.ReconcilePending(ctx, key, nil); err != nil || record.Phase != "Running" {
		t.Fatalf("lost acceptance was not recoverable after withdrawal: %v", err)
	}
	if record, err := d.Lookup(ctx, key); err != nil || record.Phase != "Running" {
		t.Fatalf("lookup required fresh approval: %v", err)
	}
	if record, err := d.Cancel(ctx, key); err != nil || record.Phase != "Cancelling" {
		t.Fatalf("cancel required fresh approval: %v", err)
	}
	if gets.Load() != 2 || cancels.Load() != 1 || posts.Load() != 0 {
		t.Fatal("recovery replayed work")
	}
	var current api.AgentRun
	if err := f.c.Get(ctx, key, &current); err != nil {
		t.Fatal(err)
	}
	current.Status.CellnActionID = "substituted"
	if err := f.c.Status().Update(ctx, &current); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Lookup(ctx, key); err == nil {
		t.Fatal("substituted journal accepted")
	}
	if gets.Load() != 2 {
		t.Fatal("invalid journal reached network")
	}
}
