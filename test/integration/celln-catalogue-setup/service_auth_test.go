package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Real host-origin requests to the live issuer/router TLS endpoints. This is
// credential/CA separation evidence, not tenant-Pod NetworkPolicy evidence.
func proveLiveServiceCredentialSeparation(t *testing.T, ctx context.Context, issuer, issuerCA, issuerToken, router, routerCA, routerToken, backendToken, evidence, root, ownership string) {
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

	// Valid ingress authentication must not turn invalid request data into an
	// issuance or durable execution owner. Snapshot actual authority directories;
	// do not infer absence of effects merely from a non-2xx response.
	snapshot := func() map[string][32]byte {
		out := map[string][32]byte{}
		for _, directory := range []string{filepath.Join(root, "trusted-model-profiles"), filepath.Join(root, "sympozium-issuer-journal"), ownership} {
			err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
				if errors.Is(err, os.ErrNotExist) && path == directory {
					return nil
				}
				if err != nil {
					return err
				}
				if entry.IsDir() {
					return nil
				}
				raw, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				out[path] = sha256.Sum256(raw)
				return nil
			})
			must(t, err)
		}
		return out
	}
	before := snapshot()
	var refused []string
	for _, service := range []struct {
		name, endpoint, ca, token, path string
		cases                           []struct {
			name, body string
			code       int
		}
	}{
		{"issuer", issuer, issuerCA, issuerCredential, "/v1/issuances", []struct {
			name, body string
			code       int
		}{
			{"truncated JSON", `{`, 400},
			{"missing version", `{}`, 400},
			{"multiple JSON values", `{} {}`, 400},
			{"tenant policy-root override", `{"apiVersion":"sympozium.ai/celln-issuer-request-v1","policyRoot":"/tenant"}`, 400},
			{"unsupported version", `{"apiVersion":"sympozium.ai/celln-issuer-request-v999"}`, 400},
		}},
		{"router", router, routerCA, routerCredential, "/v1/executions", []struct {
			name, body string
			code       int
		}{
			{"truncated JSON", `{`, 400},
			{"missing identity", `{}`, 400},
			{"path traversal identity", `{"id":"../foreign"}`, 400},
			{"non-string identity", `{"id":42}`, 400},
			{"empty body", ``, 413},
		}},
	} {
		client := clientFor(service.ca)
		for _, test := range service.cases {
			request, err := http.NewRequestWithContext(ctx, http.MethodPost, service.endpoint+service.path, strings.NewReader(test.body))
			must(t, err)
			request.Header.Set("Authorization", "Bearer "+service.token)
			request.Header.Set("Content-Type", "application/json")
			response, err := client.Do(request)
			must(t, err)
			_, err = io.Copy(io.Discard, io.LimitReader(response.Body, 8192))
			response.Body.Close()
			must(t, err)
			if response.StatusCode != test.code {
				t.Fatalf("%s %s: got %d want %d", service.name, test.name, response.StatusCode, test.code)
			}
			if !reflect.DeepEqual(before, snapshot()) {
				t.Fatalf("%s %s changed issuance/owner state", service.name, test.name)
			}
			refused = append(refused, service.name+": "+test.name)
		}
	}
	writeJSON(t, filepath.Join(evidence, "authenticated-request-refusals.json"), map[string]any{"status": "refusal-checks-passed", "cases": refused, "authorityAndOwnerFilesUnchanged": true, "scope": "actual TLS services; malformed requests rejected before issuance/ownership; not guest output flooding, tenant network isolation or a complete adversarial matrix"})
}
