package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Real host-origin requests to the live issuer/router TLS endpoints. This is
// credential/CA separation evidence, not tenant-Pod NetworkPolicy evidence.
func proveLiveServiceCredentialSeparation(t *testing.T, ctx context.Context, issuer, issuerCA, issuerToken, router, routerCA, routerToken, backendToken, evidence string) {
	t.Helper()
	readToken := func(path string) string {
		raw, err := os.ReadFile(path)
		must(t, err)
		return strings.TrimSpace(string(raw))
	}
	issuerCredential, routerCredential, backendCredential := readToken(issuerToken), readToken(routerToken), readToken(backendToken)
	if issuerCredential == routerCredential || issuerCredential == backendCredential || routerCredential == backendCredential {
		t.Fatal("live services share credentials")
	}
	clientFor := func(ca string) *http.Client {
		raw, err := os.ReadFile(ca)
		must(t, err)
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(raw) {
			t.Fatal("invalid proof service CA")
		}
		transport := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots}}
		t.Cleanup(transport.CloseIdleConnections)
		return &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	checks := 0
	for _, service := range []struct {
		name, endpoint, ca string
		paths              []string
		foreign            []string
	}{
		{"issuer", issuer, issuerCA, []string{"/v1/issuer/status", "/v1/issuances"}, []string{routerCredential, backendCredential}},
		{"router", router, routerCA, []string{"/v1/executions"}, []string{issuerCredential, backendCredential}},
	} {
		client := clientFor(service.ca)
		for _, path := range service.paths {
			for _, token := range append([]string{""}, service.foreign...) {
				method := http.MethodPost
				if path == "/v1/issuer/status" {
					method = http.MethodGet
				}
				request, err := http.NewRequestWithContext(ctx, method, service.endpoint+path, strings.NewReader("{}"))
				must(t, err)
				request.Header.Set("Content-Type", "application/json")
				if token != "" {
					request.Header.Set("Authorization", "Bearer "+token)
				}
				response, err := client.Do(request)
				if err != nil {
					t.Fatalf("%s refusal request failed before HTTP authentication", service.name)
				}
				_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 8192))
				response.Body.Close()
				if response.StatusCode != http.StatusUnauthorized {
					t.Fatalf("%s refused with status %d, want 401 before parsing", service.name, response.StatusCode)
				}
				checks++
			}
		}
	}
	// The endpoints use the same IP but independent certificate authorities.
	// Wrong trust must fail in TLS, not merely produce an HTTP error.
	for _, pair := range [][2]string{{issuer, routerCA}, {router, issuerCA}} {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, pair[0]+"/v1/issuer/status", nil)
		must(t, err)
		response, err := clientFor(pair[1]).Do(request)
		if response != nil {
			response.Body.Close()
		}
		var unknown x509.UnknownAuthorityError
		if !errors.As(err, &unknown) {
			t.Fatal("foreign CA did not fail certificate authority validation")
		}
	}
	writeJSON(t, filepath.Join(evidence, "service-credential-separation.json"), map[string]any{"status": "refusal-checks-passed", "scope": "host-origin requests to actual TLS issuer/router before controller dispatch; not tenant-Pod network policy or full adversarial qualification", "unauthorizedRequests": checks, "foreignCARefusals": 2, "missingCredentialsRefused": true, "crossServiceCredentialsRefused": true, "backendCredentialNotIngressAuthority": true})
}
