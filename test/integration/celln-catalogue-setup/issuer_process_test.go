package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Exercise the shipped command and strict operator configuration, not an
// in-process issuer substitute. Model credentials remain in the host mapping.
func liveIssuerProcess(t *testing.T, ctx context.Context, dir, evidence, cli, kube, binary, root, publisher, namespace string) (string, string, string, func()) {
	t.Helper()
	kube = liveIssuerKubeconfig(t, ctx, dir, evidence, kube, namespace)
	token, ca, private := issuerTLSFiles(t, dir)
	addr := freeServiceAddress(t)
	ref := func(name string) map[string]string { return map[string]string{"namespace": namespace, "name": name} }
	config := filepath.Join(dir, "issuer-service.json")
	writeJSON(t, config, map[string]any{
		"apiVersion": "sympozium.ai/celln-issuer-service-v1", "listen": addr,
		"certificateFile": ca, "privateKeyFile": private, "tokenFile": token,
		"cellnBinary": binary, "policyRoot": root, "composerPublisher": publisher,
		"profileLifetimeMs": 300000, "sweepIntervalMs": 1000,
		"bindings": []any{map[string]any{"agent": ref("agent"), "operatorGrants": ref("operator"), "runtimeGrants": ref("runtime"), "agentGrants": ref("agent"), "modelPolicy": ref("model")}},
	})
	restart := startProcess(t, ctx, nil, cli, "--kubeconfig", kube, "celln-tool", "serve-issuer", "--config", config)
	endpoint := "https://" + addr
	waitIssuerProcessReady(t, ctx, endpoint, token, ca)
	return endpoint, token, ca, func() {
		restart()
		waitIssuerProcessReady(t, ctx, endpoint, token, ca)
	}
}

func waitIssuerProcessReady(t *testing.T, ctx context.Context, endpoint, token, ca string) {
	t.Helper()
	rawCA, err := os.ReadFile(ca)
	must(t, err)
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(rawCA) {
		t.Fatal("invalid issuer CA")
	}
	transport := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: pool}}
	defer transport.CloseIdleConnections()
	httpClient := &http.Client{Transport: transport, Timeout: 2 * time.Second}
	tokenBytes, err := os.ReadFile(token)
	must(t, err)
	for end := time.Now().Add(20 * time.Second); time.Now().Before(end) && ctx.Err() == nil; {
		req, err := http.NewRequestWithContext(ctx, "GET", endpoint+"/v1/issuer/status", nil)
		must(t, err)
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(tokenBytes)))
		response, err := httpClient.Do(req)
		if err == nil {
			var status struct {
				APIVersion          string `json:"apiVersion"`
				Open                bool   `json:"provisioningGateOpen"`
				ExecutionAuthorized bool   `json:"executionAuthorized"`
			}
			err = json.NewDecoder(http.MaxBytesReader(nil, response.Body, 8192)).Decode(&status)
			response.Body.Close()
			if err == nil && response.StatusCode == http.StatusOK && status.APIVersion == "sympozium.ai/celln-issuer-status-v1" && status.Open && !status.ExecutionAuthorized {
				t.Log("actual serve-issuer command ready over verified TLS; provisioning gate only, no execution authorized")
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("actual issuer service did not open its authenticated provisioning gate")
}
