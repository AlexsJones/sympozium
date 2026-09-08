package cellnreview

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"time"
)

// ErrExecutionOutcomeUnknown never permits regenerating an ID or failing over.
// Retain the frozen request and reconcile the original router ownership record.
var ErrExecutionOutcomeUnknown = errors.New("execution outcome unknown; retain original request and owner")

// RouterHTTPError retains a status for recovery policy without exposing the
// response body. Even a 404 does not authorize changing request identity.
type RouterHTTPError struct{ StatusCode int }

func (e *RouterHTTPError) Error() string {
	return fmt.Sprintf("router HTTP %d: %v", e.StatusCode, ErrExecutionOutcomeUnknown)
}
func (e *RouterHTTPError) Unwrap() error { return ErrExecutionOutcomeUnknown }

// RouterExecution is transport bookkeeping, not a validated terminal receipt.
// The controller must validate Receipt against the frozen request before using
// terminal output, status or provenance. Raw bytes preserve that full contract.
type RouterExecution struct {
	RequestID string          `json:"requestId"`
	Phase     string          `json:"phase"`
	Reason    string          `json:"reason,omitempty"`
	Output    string          `json:"output,omitempty"`
	Receipt   json.RawMessage `json:"receipt,omitempty"`
}

func executionPathID(id string) bool {
	if len(id) == 0 || len(id) > 512 || id == "." || id == ".." {
		return false
	}
	for _, c := range []byte(id) {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
			return false
		}
	}
	return true
}

// Submit sends the exact previously persisted, independently verified issuance
// bytes once. The router owns deduplication/ambiguity; this client never retries
// or redirects. A prewarm observation is not used as authorization.
func (c *RouterClient) Submit(ctx context.Context, route DispatchRoute, issued json.RawMessage) (*RouterExecution, error) {
	if len(issued) == 0 || len(issued) > 65536 {
		return nil, fmt.Errorf("execution request exceeds bound")
	}
	body := append([]byte(nil), issued...)
	var identity struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(body, &identity) != nil || !executionPathID(identity.ID) {
		return nil, fmt.Errorf("invalid execution identity")
	}
	return c.execution(ctx, route, http.MethodPost, "/v1/executions", identity.ID, body, true)
}

// Lookup uses durable router ownership, not a newly selected backend. A missing
// or unreachable owner never authorizes another submission with a new identity.
func (c *RouterClient) Lookup(ctx context.Context, route DispatchRoute, id string) (*RouterExecution, error) {
	if !executionPathID(id) {
		return nil, fmt.Errorf("invalid execution identity")
	}
	return c.execution(ctx, route, http.MethodGet, "/v1/executions/"+id, id, nil, false)
}

// Cancel asks the recorded owner to cancel; its response is not itself proof
// of guest teardown. Terminal receipt/cleanup validation remains mandatory.
func (c *RouterClient) Cancel(ctx context.Context, route DispatchRoute, id string) (*RouterExecution, error) {
	if !executionPathID(id) {
		return nil, fmt.Errorf("invalid execution identity")
	}
	return c.execution(ctx, route, http.MethodPost, "/v1/executions/"+id+"/cancel", id, nil, false)
}

func (c *RouterClient) execution(ctx context.Context, route DispatchRoute, method, path, id string, body []byte, pin bool) (*RouterExecution, error) {
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	if route.RouterURL != c.origin || !routeOrigin(route.Backend, "http", 1024) {
		return nil, fmt.Errorf("router differs from frozen serving route")
	}
	token, err := issuerToken(c.transport.tokenFile)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, c.origin+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+string(token))
	req.Header.Set("Content-Type", "application/json")
	if pin {
		req.Header.Set("X-Celln-Backend", route.Backend)
	}
	response, err := c.transport.http.Do(req)
	if err != nil {
		return nil, ErrExecutionOutcomeUnknown
	}
	defer response.Body.Close()
	// Even an explicit refusal does not authorize switching identity. Retain
	// HTTP status for diagnostics without exposing arbitrary response bodies.
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusAccepted {
		return nil, &RouterHTTPError{StatusCode: response.StatusCode}
	}
	media, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || response.Header.Get("Content-Encoding") != "" {
		return nil, ErrExecutionOutcomeUnknown
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 262145))
	if err != nil || len(raw) > 262144 {
		return nil, ErrExecutionOutcomeUnknown
	}
	var record RouterExecution
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&record) != nil || d.Decode(new(any)) != io.EOF || record.RequestID != id {
		return nil, ErrExecutionOutcomeUnknown
	}
	switch record.Phase {
	case "Admitting", "Forging", "Resolving", "Running", "Cancelling", "Succeeded", "Failed", "Refused", "Cancelled":
		return &record, nil
	default:
		return nil, ErrExecutionOutcomeUnknown
	}
}
