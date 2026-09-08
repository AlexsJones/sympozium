package cellnreview

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	api "github.com/sympozium-ai/sympozium/api/v1alpha1"
	"github.com/zeebo/blake3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

func dispatchSubmissionFixture(t *testing.T, handler func(issuerFixture, http.ResponseWriter, *http.Request)) (*RunDispatcher, issuerFixture, *IssuerClient) {
	t.Helper()
	f, m, _ := managedFixture(t)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler(f, w, r) }))
	t.Cleanup(server.Close)
	dir := t.TempDir()
	ca, token := filepath.Join(dir, "ca.pem"), filepath.Join(dir, "router-token")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(token, []byte("public-submit-router-credential"), 0600); err != nil {
		t.Fatal(err)
	}
	router, err := NewRouterClient(server.URL, token, ca)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(router.CloseIdleConnections)
	endpoint, _, issuerToken := serveTestIssuer(t, m)
	issuer, err := NewIssuerClient(IssuerClientOptions{URL: endpoint, TokenFile: issuerToken, CAFile: filepath.Join(filepath.Dir(issuerToken), "cert.pem"), Route: &DispatchRoute{RouterURL: server.URL, Backend: "http://host-a:8787"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(issuer.CloseIdleConnections)
	key := types.NamespacedName{Namespace: f.f.Run.Namespace, Name: f.f.Run.Name}
	seed := &IssuerRequest{APIVersion: "sympozium.ai/celln-issuer-request-v1", Frozen: *f.f, Approval: *f.a, Artifacts: f.artifacts}
	if _, err := issuer.IssueForRun(context.Background(), f.c, f.c, key, f.l, seed); err != nil {
		t.Fatal(err)
	}
	d, err := NewRunDispatcher(f.c, f.c, map[types.NamespacedName]RunDispatchBinding{{Namespace: f.f.Snapshot.Agent.Namespace, Name: f.f.Snapshot.Agent.Name}: {Issuer: issuer, Router: router, Loader: f.l}})
	if err != nil {
		t.Fatal(err)
	}
	return d, f, issuer
}

func TestRunDispatcherSubmissionGatesAndAmbiguousRecovery(t *testing.T) {
	for _, mode := range []string{"first", "404", "503", "gate-first", "gate-after", "withdraw-after", "missing-gate", "lost-submit"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			var gets, warms, posts, gates atomic.Int32
			d, f, issuer := dispatchSubmissionFixture(t, func(f issuerFixture, w http.ResponseWriter, r *http.Request) {
				key := types.NamespacedName{Namespace: f.f.Run.Namespace, Name: f.f.Run.Name}
				var current api.AgentRun
				if err := f.c.Get(ctx, key, &current); err != nil {
					t.Error(err)
					return
				}
				if current.Status.CellnRequest == "" || current.Status.CellnActionID == "" {
					t.Error("network effect preceded durable dispatch journal")
				}
				w.Header().Set("Content-Type", "application/json")
				if r.Method == "GET" {
					gets.Add(1)
					if mode == "lost-submit" && posts.Load() == 1 {
						_ = json.NewEncoder(w).Encode(RouterExecution{RequestID: current.Status.CellnActionID, Phase: "Running"})
						return
					}
					if mode == "503" {
						w.WriteHeader(503)
					} else {
						w.WriteHeader(404)
					}
					return
				}
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					return
				}
				switch r.URL.Path {
				case "/v1/artifacts/prewarm":
					warms.Add(1)
					h := blake3.Sum256(body)
					hash := "blake3:" + strings.Repeat("a", 64)
					yes, no := true, false
					report := PrewarmObservation{APIVersion: "celln.dev/artifact-prewarm-v1", Node: "host-a", ProcessEpoch: hash, RequestHash: "blake3:" + hex.EncodeToString(h[:]), WarmState: "present-at-observation", Validity: "observation-only", ExecutionAuthorized: &no, Conformance: "not_checked", ArtifactReadiness: "not_checked", Verification: MemberObservation{APIVersion: "celln.dev/sealed-members-verification-v1", Scope: "sealed-member-identities-only", Mote: f.artifacts.Mote.Hash, Closure: f.artifacts.Closure.Hash, Publisher: strings.Repeat("a", 64), Toolfs: hash, Kernel: hash, Initrd: hash, MemberCount: 3, RequestHash: hash, Challenge: hash, MemberIntegrity: "verified-in-sealed-cell", ToolExecution: &no, CellDissolved: &yes, Conformance: "not_checked", ArtifactReadiness: "not_checked"}}
					if mode == "withdraw-after" {
						var policy corev1.ConfigMap
						if err := f.c.Get(ctx, f.l.Source, &policy); err != nil {
							t.Error(err)
						} else if err := f.c.Delete(ctx, &policy); err != nil {
							t.Error(err)
						}
					}
					_ = json.NewEncoder(w).Encode(report)
				case "/v1/executions":
					posts.Add(1)
					if string(body) != current.Status.CellnRequest || gates.Load() != 2 {
						t.Error("submission changed bytes or skipped admission recheck")
					}
					if mode == "lost-submit" {
						conn, _, err := w.(http.Hijacker).Hijack()
						if err != nil {
							t.Error(err)
						} else {
							_ = conn.Close()
						}
						return
					}
					w.WriteHeader(202)
					_ = json.NewEncoder(w).Encode(RouterExecution{RequestID: current.Status.CellnActionID, Phase: "Running"})
				default:
					t.Error("unexpected dispatch operation")
				}
			})
			key := types.NamespacedName{Namespace: f.f.Run.Namespace, Name: f.f.Run.Name}
			if mode == "404" || mode == "503" {
				if _, err := issuer.FreezeIssuedDispatch(ctx, f.c, f.c, key, f.l); err != nil {
					t.Fatal(err)
				}
			}
			admit := func(context.Context, *api.AgentRun) error {
				n := gates.Add(1)
				if mode == "gate-first" || mode == "gate-after" && n == 2 {
					return fmt.Errorf("admission denied")
				}
				return nil
			}
			if mode == "missing-gate" {
				admit = nil
			}
			record, err := d.ReconcilePending(ctx, key, admit)
			if mode == "first" || mode == "404" {
				if err != nil || record == nil || record.Phase != "Running" {
					t.Fatalf("valid submission failed: %v", err)
				}
			} else if err == nil || record != nil {
				t.Fatal("refused/ambiguous submission returned success")
			}
			wantGates, wantWarms, wantPosts := int32(2), int32(1), int32(0)
			switch mode {
			case "first", "404", "lost-submit":
				wantPosts = 1
			case "503", "missing-gate":
				wantGates, wantWarms = 0, 0
			case "gate-first":
				wantGates, wantWarms = 1, 0
			}
			if gates.Load() != wantGates || warms.Load() != wantWarms || posts.Load() != wantPosts {
				t.Fatalf("wrong transition counts: gates=%d warms=%d posts=%d", gates.Load(), warms.Load(), posts.Load())
			}
			if mode == "lost-submit" {
				if !errors.Is(err, ErrExecutionOutcomeUnknown) {
					t.Fatal("lost submission not classified as ambiguous")
				}
				if record, err := d.ReconcilePending(ctx, key, nil); err != nil || record.Phase != "Running" {
					t.Fatalf("lost submission failed recovery: %v", err)
				}
				if gets.Load() != 1 || posts.Load() != 1 || warms.Load() != 1 {
					t.Fatal("lost submission was replayed or rewarmed")
				}
			}
		})
	}
}
