package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Real router + real TLS reverse proxy. No KVM, model credentials, Kubernetes
// or execution are needed to test rejected-request response delivery.
func TestActualRouterRefusalsThroughTLSProxy(t *testing.T) {
	binary := os.Getenv("CELLN_COMPOSITION_BINARY")
	if binary == "" {
		t.Skip("explicit actual Celln binary required")
	}
	if !filepath.IsAbs(binary) {
		t.Fatal("absolute Celln binary required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var backendRequests atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backendRequests.Add(1)
		http.Error(w, "unauthorized request reached backend", http.StatusInternalServerError)
	}))
	defer backend.Close()
	dir := t.TempDir()
	clientToken, backendToken := filepath.Join(dir, "client"), filepath.Join(dir, "backend")
	must(t, os.WriteFile(clientToken, freshProofToken(t), 0600))
	foreign := freshProofToken(t)
	must(t, os.WriteFile(backendToken, foreign, 0600))
	address := freeAddress(t)
	startProcess(t, ctx, nil, binary, "route", "--listen", address, "--backends", backend.URL, "--token-file", backendToken, "--client-token-file", clientToken, "--ownership-dir", filepath.Join(dir, "ownership"))
	waitTCP(t, address)
	target, err := url.Parse("http://" + address)
	must(t, err)
	proxy := httputil.NewSingleHostReverseProxy(target)
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	proxy.Transport = transport
	server := httptest.NewTLSServer(proxy)
	defer server.Close()
	client := server.Client()
	client.Timeout = 3 * time.Second
	for round := 0; round < 16; round++ {
		for _, size := range []int{0, 2, 32768} {
			for _, token := range []string{"", string(foreign)} {
				request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/v1/executions", strings.NewReader(strings.Repeat("x", size)))
				must(t, err)
				request.Header.Set("Content-Type", "application/json")
				if token != "" {
					request.Header.Set("Authorization", "Bearer "+token)
				}
				response, err := client.Do(request)
				must(t, err)
				body, readErr := io.ReadAll(io.LimitReader(response.Body, 8192))
				response.Body.Close()
				if readErr != nil || response.StatusCode != http.StatusUnauthorized || string(body) != `{"error":"unauthorized"}` {
					t.Fatalf("incomplete refusal round=%d bytes=%d status=%d readErr=%v", round, size, response.StatusCode, readErr)
				}
			}
		}
	}
	if backendRequests.Load() != 0 {
		t.Fatal("rejected input reached backend")
	}
	t.Log("PASS 96 complete TLS-proxy refusals without retry; zero backend requests")
}
