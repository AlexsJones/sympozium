package cellnreview

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/zeebo/blake3"
)

// RouterClient uses a separately provisioned router credential and verified TLS.
// Its observations do not confer execution authority or durable readiness.
type RouterClient struct {
	transport *IssuerClient
	origin    string
}

func NewRouterClient(origin, tokenFile, caFile string) (*RouterClient, error) {
	if !routeOrigin(origin, "https", 2048) {
		return nil, fmt.Errorf("explicit HTTPS router origin required")
	}
	c, err := NewIssuerClient(IssuerClientOptions{URL: origin, TokenFile: tokenFile, CAFile: caFile})
	if err != nil {
		return nil, err
	}
	return &RouterClient{transport: c, origin: origin}, nil
}

func (c *RouterClient) CloseIdleConnections() { c.transport.CloseIdleConnections() }

type PrewarmObservation struct {
	APIVersion          string            `json:"apiVersion"`
	Node                string            `json:"node"`
	ProcessEpoch        string            `json:"processEpoch"`
	RequestHash         string            `json:"requestHash"`
	Verification        MemberObservation `json:"verification"`
	WarmState           string            `json:"warmState"`
	Validity            string            `json:"validity"`
	ExecutionAuthorized *bool             `json:"executionAuthorized"`
	Conformance         string            `json:"conformance"`
	ArtifactReadiness   string            `json:"artifactReadiness"`
}

type MemberObservation struct {
	APIVersion        string `json:"apiVersion"`
	Scope             string `json:"scope"`
	Mote              string `json:"mote"`
	Closure           string `json:"closure"`
	Publisher         string `json:"publisher"`
	Toolfs            string `json:"toolfs"`
	Kernel            string `json:"kernel"`
	Initrd            string `json:"initrd"`
	MemberCount       int    `json:"memberCount"`
	RequestHash       string `json:"requestHash"`
	Challenge         string `json:"challenge"`
	MemberIntegrity   string `json:"memberIntegrity"`
	ToolExecution     *bool  `json:"toolExecution"`
	CellDissolved     *bool  `json:"cellDissolved"`
	Conformance       string `json:"conformance"`
	ArtifactReadiness string `json:"artifactReadiness"`
}

// prewarmBody derives a non-executing member-check request from verified issued
// bytes. Caller must first validate those bytes against current frozen approval.
func prewarmBody(issued json.RawMessage) ([]byte, string, string, error) {
	if len(issued) == 0 || len(issued) > 65536 {
		return nil, "", "", fmt.Errorf("issued request exceeds bound")
	}
	var request map[string]json.RawMessage
	if json.Unmarshal(issued, &request) != nil || request == nil {
		return nil, "", "", fmt.Errorf("invalid issued request")
	}
	var identity struct {
		Mote struct {
			Hash string `json:"hash"`
		} `json:"mote"`
		Tools []struct {
			Closure struct {
				Hash string `json:"hash"`
			} `json:"closure"`
		} `json:"tools"`
	}
	if json.Unmarshal(issued, &identity) != nil || len(identity.Tools) != 1 || !artifactHash(identity.Mote.Hash) || !artifactHash(identity.Tools[0].Closure.Hash) {
		return nil, "", "", fmt.Errorf("one exact composed closure and mote required")
	}
	var capabilities map[string]json.RawMessage
	var invocation map[string]json.RawMessage
	if json.Unmarshal(request["capabilities"], &capabilities) != nil || capabilities == nil || json.Unmarshal(request["invocation"], &invocation) != nil || invocation == nil {
		return nil, "", "", fmt.Errorf("bounded capabilities and invocation required")
	}
	request["apiVersion"] = json.RawMessage(`"celln.dev/v1alpha1"`)
	delete(request, "harness")
	delete(request, "forge")
	delete(request, "inputs")
	capabilities["egress"] = json.RawMessage(`[]`)
	capabilities["workspace"] = json.RawMessage(`"none"`)
	invocation["args"] = json.RawMessage(`[]`)
	request["capabilities"], _ = json.Marshal(capabilities)
	request["invocation"], _ = json.Marshal(invocation)
	body, err := json.Marshal(request)
	if err == nil && len(body) > 65536 {
		err = fmt.Errorf("derived prewarm request exceeds 64 KiB")
	}
	return body, identity.Mote.Hash, identity.Tools[0].Closure.Hash, err
}

// Prewarm observes the explicitly pinned serving process, never submits a task.
// Route must come from frozen operator configuration, not tenant data.
func (c *RouterClient) Prewarm(ctx context.Context, route DispatchRoute, issued json.RawMessage) (*PrewarmObservation, error) {
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	if route.RouterURL != c.origin || !routeOrigin(route.Backend, "http", 1024) {
		return nil, fmt.Errorf("router differs from frozen serving route")
	}
	body, mote, closure, err := prewarmBody(issued)
	if err != nil {
		return nil, err
	}
	token, err := issuerToken(c.transport.tokenFile)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.origin+"/v1/artifacts/prewarm", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+string(token))
	req.Header.Set("X-Celln-Backend", route.Backend)
	req.Header.Set("Content-Type", "application/json")
	response, err := c.transport.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("prewarm outcome unavailable; no readiness established")
	}
	defer response.Body.Close()
	media, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if response.StatusCode != http.StatusOK || err != nil || media != "application/json" || response.Header.Get("Content-Encoding") != "" {
		return nil, fmt.Errorf("prewarm refused or incompatible response")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if err != nil || len(raw) > 65536 {
		return nil, fmt.Errorf("prewarm response unavailable or oversized")
	}
	var report PrewarmObservation
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&report) != nil || d.Decode(new(any)) != io.EOF {
		return nil, fmt.Errorf("invalid prewarm response")
	}
	h := blake3.Sum256(body)
	v := report.Verification
	publisher, err := hex.DecodeString(v.Publisher)
	if report.APIVersion != "celln.dev/artifact-prewarm-v1" || report.Node == "" || len(report.Node) > 253 || !artifactHash(report.ProcessEpoch) || report.RequestHash != "blake3:"+hex.EncodeToString(h[:]) || report.WarmState != "present-at-observation" || report.Validity != "observation-only" || report.ExecutionAuthorized == nil || *report.ExecutionAuthorized || report.Conformance != "not_checked" || report.ArtifactReadiness != "not_checked" ||
		v.APIVersion != "celln.dev/sealed-members-verification-v1" || v.Scope != "sealed-member-identities-only" || v.Mote != mote || v.Closure != closure || err != nil || len(publisher) != 32 || v.Publisher != strings.ToLower(v.Publisher) || !artifactHash(v.Toolfs) || !artifactHash(v.Kernel) || !artifactHash(v.Initrd) || !artifactHash(v.RequestHash) || !artifactHash(v.Challenge) || v.MemberCount < 1 || v.MemberIntegrity != "verified-in-sealed-cell" || v.ToolExecution == nil || *v.ToolExecution || v.CellDissolved == nil || !*v.CellDissolved || v.Conformance != "not_checked" || v.ArtifactReadiness != "not_checked" {
		return nil, fmt.Errorf("prewarm observation is unbound or makes incompatible authority claims")
	}
	return &report, nil
}
