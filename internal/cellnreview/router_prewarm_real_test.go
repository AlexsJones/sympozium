package cellnreview

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Only called by the explicit real-KVM catalogue fixture. All sockets bind
// loopback, all processes are owned/joined by this test, and no model is called.
func proveRouterPrewarm(t *testing.T, ctx context.Context, o ComposeOptions, issued json.RawMessage) {
	t.Helper()
	dir := t.TempDir()
	backendToken, routerToken := filepath.Join(dir, "backend-token"), filepath.Join(dir, "router-token")
	for path, token := range map[string]string{backendToken: "public-real-prewarm-backend-token", routerToken: "public-real-prewarm-router-token"} {
		if err := os.WriteFile(path, []byte(token), 0600); err != nil {
			t.Fatal(err)
		}
	}
	backendAddr := prewarmTestAddress(t)
	backend := "http://" + backendAddr
	startPrewarmProcess(t, o.Binary, backendAddr, "--root", o.PolicyRoot, "dispatcher", "--listen", backendAddr, "--token-file", backendToken,
		"--node-name", "catalogue-prewarm-proof", "--mote-store", filepath.Join(o.PolicyRoot, "motes"), "--tool-store", filepath.Join(o.PolicyRoot, "tools"))
	routerAddr := prewarmTestAddress(t)
	ownership := filepath.Join(dir, "ownership")
	startPrewarmProcess(t, o.Binary, routerAddr, "route", "--listen", routerAddr, "--backends", backend, "--token-file", backendToken, "--client-token-file", routerToken, "--ownership-dir", ownership)
	target, err := url.Parse("http://" + routerAddr)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = &http.Transport{Proxy: nil}
	t.Cleanup(func() { proxy.Transport.(*http.Transport).CloseIdleConnections() })
	tls := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v1/artifacts/prewarm" {
			t.Error("prewarm client attempted a different operation")
			http.Error(w, "prewarm only", http.StatusForbidden)
			return
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(tls.Close)
	ca := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: tls.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	client, err := NewRouterClient(tls.URL, routerToken, ca)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.CloseIdleConnections)
	route := DispatchRoute{RouterURL: tls.URL, Backend: backend}
	first, err := client.Prewarm(ctx, route, issued)
	if err != nil {
		t.Fatal(err)
	}
	second, err := client.Prewarm(ctx, route, issued)
	if err != nil {
		t.Fatal(err)
	}
	if first.Node != "catalogue-prewarm-proof" || first.ProcessEpoch != second.ProcessEpoch || first.RequestHash != second.RequestHash || first.Verification.Challenge == second.Verification.Challenge {
		t.Fatal("prewarm did not bind the same process/request and fresh sealed verification")
	}
	for _, journal := range []string{ownership, filepath.Join(o.PolicyRoot, "execution-journal")} {
		records, err := filepath.Glob(filepath.Join(journal, "*.json"))
		if err != nil || len(records) != 0 {
			t.Fatalf("prewarm created execution records in %s", journal)
		}
	}
	t.Logf("PASS verified TLS client -> TLS terminator -> actual pinned Celln router -> actual KVM dispatcher prewarm, twice; node=%s epoch=%s request=%s closure=%s; no execution ownership, no task submission, no model calls; Kubernetes=fake", first.Node, first.ProcessEpoch, first.RequestHash, first.Verification.Closure)
}

func prewarmTestAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}

func startPrewarmProcess(t *testing.T, binary, address string, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	log, err := os.CreateTemp(t.TempDir(), "process-*.log")
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		cancel()
		_ = log.Close()
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("owned Celln process did not exit")
		}
		_ = log.Close()
	})
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			done <- err // leave completion available for cleanup
			data, _ := os.ReadFile(log.Name())
			t.Fatalf("Celln process exited: %v: %s", err, data)
		default:
		}
		conn, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	data, _ := os.ReadFile(log.Name())
	t.Fatal(fmt.Sprintf("Celln listener did not start: %s", data))
}
