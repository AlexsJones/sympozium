package cellnreview

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/zeebo/blake3"
)

func TestRouterPrewarmVerifiesExactObservationOverTLS(t *testing.T) {
	for _, mode := range []string{"valid", "hash", "closure", "node", "epoch", "missing-executed", "executes", "tool-executes", "not-dissolved", "ready", "trailing", "oversized", "redirect"} {
		t.Run(mode, func(t *testing.T) {
			f := provisionFixture(t)
			candidate, err := f.l.BuildExecution(context.Background(), *f.f, *f.a, f.artifacts)
			if err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != "POST" || r.URL.Path != "/v1/artifacts/prewarm" || r.Header.Get("Authorization") != "Bearer public-router-client-token-24" || r.Header.Get("X-Celln-Backend") != "http://host-a:8787" {
					t.Error("wrong method, path, credential or pin")
				}
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					return
				}
				var request map[string]json.RawMessage
				if json.Unmarshal(body, &request) != nil {
					t.Error("invalid prewarm request")
				}
				for _, key := range []string{"harness", "forge", "inputs"} {
					if _, ok := request[key]; ok {
						t.Errorf("prewarm retained %s", key)
					}
				}
				var capabilities map[string]json.RawMessage
				var invocation map[string]json.RawMessage
				_ = json.Unmarshal(request["capabilities"], &capabilities)
				_ = json.Unmarshal(request["invocation"], &invocation)
				if string(capabilities["egress"]) != "[]" || string(invocation["args"]) != "[]" || string(capabilities["workspace"]) != `"none"` {
					t.Error("prewarm retained executable/data authority")
				}
				h := blake3.Sum256(body)
				hash := "blake3:" + strings.Repeat("a", 64)
				verification := map[string]any{"apiVersion": "celln.dev/sealed-members-verification-v1", "scope": "sealed-member-identities-only", "mote": f.artifacts.Mote.Hash, "closure": f.artifacts.Closure.Hash, "publisher": strings.Repeat("a", 64), "toolfs": hash, "kernel": hash, "initrd": hash, "memberCount": 3, "requestHash": hash, "challenge": hash, "memberIntegrity": "verified-in-sealed-cell", "toolExecution": false, "cellDissolved": true, "conformance": "not_checked", "artifactReadiness": "not_checked"}
				report := map[string]any{"apiVersion": "celln.dev/artifact-prewarm-v1", "node": "node-a", "processEpoch": hash, "requestHash": "blake3:" + hex.EncodeToString(h[:]), "verification": verification, "warmState": "present-at-observation", "validity": "observation-only", "executionAuthorized": false, "conformance": "not_checked", "artifactReadiness": "not_checked"}
				switch mode {
				case "hash":
					report["requestHash"] = hash
				case "closure":
					verification["closure"] = "blake3:" + strings.Repeat("f", 64)
				case "node":
					report["node"] = ""
				case "epoch":
					report["processEpoch"] = ""
				case "missing-executed":
					delete(report, "executionAuthorized")
				case "executes":
					report["executionAuthorized"] = true
				case "tool-executes":
					verification["toolExecution"] = true
				case "not-dissolved":
					verification["cellDissolved"] = false
				case "ready":
					report["artifactReadiness"] = "ready"
				case "redirect":
					w.Header().Set("Location", "/v1/executions")
					w.WriteHeader(http.StatusTemporaryRedirect)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if mode == "oversized" {
					_, _ = io.WriteString(w, strings.Repeat(" ", 65537))
					return
				}
				_ = json.NewEncoder(w).Encode(report)
				if mode == "trailing" {
					_, _ = io.WriteString(w, "{}")
				}
			}))
			defer server.Close()
			dir := t.TempDir()
			ca, token := filepath.Join(dir, "ca.pem"), filepath.Join(dir, "token")
			if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(token, []byte("public-router-client-token-24"), 0600); err != nil {
				t.Fatal(err)
			}
			client, err := NewRouterClient(server.URL, token, ca)
			if err != nil {
				t.Fatal(err)
			}
			defer client.CloseIdleConnections()
			report, err := client.Prewarm(context.Background(), DispatchRoute{RouterURL: server.URL, Backend: "http://host-a:8787"}, candidate.Request)
			if mode == "valid" {
				if err != nil || report == nil {
					t.Fatalf("valid response refused: %v", err)
				}
			} else if err == nil || report != nil {
				t.Fatal("invalid observation accepted")
			}
			if calls.Load() != 1 {
				t.Fatalf("prewarm retried or followed redirect: %d", calls.Load())
			}
			if report, err := client.Prewarm(context.Background(), DispatchRoute{RouterURL: "https://different-router.example", Backend: "http://host-a:8787"}, candidate.Request); err == nil || report != nil || calls.Load() != 1 {
				t.Fatal("retargeted router was contacted")
			}
		})
	}
}

func TestPrewarmRequestHashUsesBlake3(t *testing.T) {
	h := blake3.Sum256(nil)
	if hex.EncodeToString(h[:]) != "af1349b9f5f9a1a6a0404dea36dcc9499bcb25c9adc112b7cc9a93cae41f3262" {
		t.Fatal("wrong request hash algorithm")
	}
}
