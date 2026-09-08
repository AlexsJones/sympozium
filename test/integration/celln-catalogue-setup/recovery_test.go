package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	api "github.com/sympozium-ai/sympozium/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// The real router must accept the POST before the proxy discards its response.
// Subsequent lookups fail until the test has observed persisted uncertainty.
// No successful response is synthesized, and the request is never replayed here.
type dispatchResponseLoss struct {
	proxy    *httputil.ReverseProxy
	posts    atomic.Int32
	dropped  atomic.Bool
	released atomic.Bool
}

func newDispatchResponseLoss(proxy *httputil.ReverseProxy) *dispatchResponseLoss {
	loss := &dispatchResponseLoss{proxy: proxy}
	proxy.ModifyResponse = func(response *http.Response) error {
		if response.Request.Method == http.MethodPost && response.Request.URL.Path == "/v1/executions" && response.StatusCode >= 200 && response.StatusCode < 300 && loss.dropped.CompareAndSwap(false, true) {
			response.Body.Close()
			body := "injected loss of accepted execution response"
			response.StatusCode = http.StatusServiceUnavailable
			response.Status = "503 Service Unavailable"
			response.Body = io.NopCloser(strings.NewReader(body))
			response.ContentLength = int64(len(body))
			response.Header = http.Header{"Content-Type": []string{"text/plain"}}
		}
		return nil
	}
	return loss
}

func (loss *dispatchResponseLoss) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost && r.URL.Path == "/v1/executions" {
		loss.posts.Add(1)
	}
	if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/executions/") && loss.dropped.Load() && !loss.released.Load() {
		http.Error(w, "injected owner observation outage", http.StatusServiceUnavailable)
		return
	}
	loss.proxy.ServeHTTP(w, r)
}

func observeLostCatalogueResponse(t *testing.T, ctx context.Context, c client.Client, key types.NamespacedName, loss *dispatchResponseLoss, evidence string, beforeRestore func()) *api.AgentRun {
	t.Helper()
	// Always restore observation before the run cleanup/finalizer on failure.
	defer loss.released.Store(true)
	for end := time.Now().Add(100 * time.Second); time.Now().Before(end) && ctx.Err() == nil; {
		var run api.AgentRun
		must(t, c.Get(ctx, key, &run))
		condition := meta.FindStatusCondition(run.Status.Conditions, "CellnExecutionObserved")
		if loss.dropped.Load() && condition != nil && condition.Status == "Unknown" && condition.Reason == "ExecutionOutcomeUnconfirmed" {
			if run.Status.CellnActionID == "" || run.Status.CellnRequest == "" || loss.posts.Load() != 1 || !strings.Contains(condition.Message, "Do not resubmit") {
				t.Fatal("uncertain dispatch lacks original identity or safe recovery guidance")
			}
			writeJSON(t, filepath.Join(evidence, "lost-response-agentrun.json"), run)
			if beforeRestore != nil {
				beforeRestore()
			}
			t.Logf("accepted response lost; original request %s persisted with Unknown outcome; restoring observation", run.Status.CellnActionID)
			return &run
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("accepted response loss and persisted uncertainty were not observed")
	return nil
}

func TestResponseLossRequiresRealAcceptanceAndDoesNotReplay(t *testing.T) {
	var posts atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts.Add(1)
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()
	target, err := url.Parse(backend.URL)
	must(t, err)
	loss := newDispatchResponseLoss(httputil.NewSingleHostReverseProxy(target))
	proxy := httptest.NewServer(loss)
	defer proxy.Close()
	request := func(method, path string, want int) {
		req, err := http.NewRequest(method, proxy.URL+path, nil)
		must(t, err)
		response, err := proxy.Client().Do(req)
		must(t, err)
		response.Body.Close()
		if response.StatusCode != want {
			t.Fatalf("response status %d want %d", response.StatusCode, want)
		}
	}
	request("GET", "/v1/executions/original", 200)
	request("POST", "/v1/executions", 503)
	if posts.Load() != 1 || !loss.dropped.Load() {
		t.Fatal("failure injected before real acceptance")
	}
	request("GET", "/v1/executions/original", 503)
	loss.released.Store(true)
	request("GET", "/v1/executions/original", 200)
	if posts.Load() != 1 || loss.posts.Load() != 1 {
		t.Fatal("proxy replayed execution")
	}
}
