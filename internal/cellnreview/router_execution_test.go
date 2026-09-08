package cellnreview

import (
	"context"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestRouterExecutionPreservesBytesAndOwnershipAndRefusesAmbiguity(t *testing.T) {
	for _, mode := range []string{"submit", "lookup", "cancel", "wrong-id", "unknown-phase", "extra", "trailing", "oversized", "redirect", "lost", "refused"} {
		t.Run(mode, func(t *testing.T) {
			body := `{"id":"catalogue-123", "preserve":"exact wire bytes"}`
			var calls atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Header.Get("Authorization") != "Bearer public-execution-router-token" {
					t.Error("wrong router credential")
				}
				got, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				switch mode {
				case "lookup":
					if r.Method != "GET" || r.URL.Path != "/v1/executions/catalogue-123" || len(got) != 0 || r.Header.Get("X-Celln-Backend") != "" {
						t.Error("lookup attempted to reselect owner")
					}
				case "cancel":
					if r.Method != "POST" || r.URL.Path != "/v1/executions/catalogue-123/cancel" || len(got) != 0 || r.Header.Get("X-Celln-Backend") != "" {
						t.Error("cancel attempted to reselect owner")
					}
				default:
					if r.Method != "POST" || r.URL.Path != "/v1/executions" || string(got) != body || r.Header.Get("X-Celln-Backend") != "http://host-a:8787" {
						t.Error("submission changed bytes or route")
					}
				}
				w.Header().Set("Content-Type", "application/json")
				response := `{"requestId":"catalogue-123","phase":"Running"}`
				switch mode {
				case "cancel":
					response = `{"requestId":"catalogue-123","phase":"Cancelling"}`
				case "wrong-id":
					response = `{"requestId":"other","phase":"Running"}`
				case "unknown-phase":
					response = `{"requestId":"catalogue-123","phase":"Ready"}`
				case "extra":
					response = `{"requestId":"catalogue-123","phase":"Running","extra":true}`
				case "trailing":
					response += `{}`
				case "oversized":
					response = strings.Repeat(" ", 262145)
				case "redirect":
					w.Header().Set("Location", "/redirected")
					w.WriteHeader(http.StatusTemporaryRedirect)
					return
				case "refused":
					w.WriteHeader(http.StatusConflict)
					_, _ = io.WriteString(w, "private diagnostic must not surface")
					return
				case "lost":
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					_ = conn.Close()
					return
				}
				w.WriteHeader(http.StatusAccepted)
				_, _ = io.WriteString(w, response)
			}))
			defer server.Close()
			dir := t.TempDir()
			ca, token := filepath.Join(dir, "ca.pem"), filepath.Join(dir, "token")
			if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(token, []byte("public-execution-router-token"), 0600); err != nil {
				t.Fatal(err)
			}
			client, err := NewRouterClient(server.URL, token, ca)
			if err != nil {
				t.Fatal(err)
			}
			defer client.CloseIdleConnections()
			route := DispatchRoute{RouterURL: server.URL, Backend: "http://host-a:8787"}
			var record *RouterExecution
			switch mode {
			case "lookup":
				record, err = client.Lookup(context.Background(), route, "catalogue-123")
			case "cancel":
				record, err = client.Cancel(context.Background(), route, "catalogue-123")
			default:
				record, err = client.Submit(context.Background(), route, []byte(body))
			}
			if mode == "submit" || mode == "lookup" || mode == "cancel" {
				if err != nil || record == nil {
					t.Fatalf("valid response refused: %v", err)
				}
				if mode == "cancel" && record.Phase != "Cancelling" {
					t.Fatal("cancel response implied teardown")
				}
			} else if !errors.Is(err, ErrExecutionOutcomeUnknown) || record != nil {
				t.Fatalf("ambiguous outcome accepted: %v", err)
			}
			if err != nil && strings.Contains(err.Error(), "private diagnostic") {
				t.Fatal("response body leaked")
			}
			if mode == "refused" {
				var refused *RouterHTTPError
				if !errors.As(err, &refused) || refused.StatusCode != http.StatusConflict {
					t.Fatal("HTTP refusal lost its structured status")
				}
			}
			if calls.Load() != 1 {
				t.Fatalf("unexpected replay/redirect: %d", calls.Load())
			}
			for _, id := range []string{"", ".", "..", "x/cancel", "x?query", "x\r\nheader", strings.Repeat("a", 513)} {
				if _, err := client.Lookup(context.Background(), route, id); err == nil {
					t.Fatal("invalid path identity accepted")
				}
				if _, err := client.Cancel(context.Background(), route, id); err == nil {
					t.Fatal("invalid cancel identity accepted")
				}
			}
			if calls.Load() != 1 {
				t.Fatal("invalid identity reached router")
			}
		})
	}
}
