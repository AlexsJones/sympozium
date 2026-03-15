package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	content := `
probeInterval: 15s
targets:
  - name: ollama
    port: 11434
    healthPath: /api/tags
    modelsPath: /api/tags
  - name: vllm
    port: 8000
    healthPath: /health
    modelsPath: /v1/models
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(cfgPath)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}

	if cfg.ProbeInterval != "15s" {
		t.Errorf("probeInterval = %q, want 15s", cfg.ProbeInterval)
	}
	if len(cfg.Targets) != 2 {
		t.Fatalf("targets count = %d, want 2", len(cfg.Targets))
	}
	if cfg.Targets[0].Name != "ollama" {
		t.Errorf("targets[0].name = %q, want ollama", cfg.Targets[0].Name)
	}
	if cfg.Targets[0].Port != 11434 {
		t.Errorf("targets[0].port = %d, want 11434", cfg.Targets[0].Port)
	}
	if cfg.Targets[1].Name != "vllm" {
		t.Errorf("targets[1].name = %q, want vllm", cfg.Targets[1].Name)
	}
}

func TestLoadConfig_DefaultInterval(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	content := `targets:
  - name: ollama
    port: 11434
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(cfgPath)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.ProbeInterval != "30s" {
		t.Errorf("probeInterval = %q, want 30s (default)", cfg.ProbeInterval)
	}
}

func TestProbeTarget_OllamaFormat(t *testing.T) {
	// Fake Ollama server returning /api/tags.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"models": []map[string]interface{}{
				{"name": "llama3:latest"},
				{"name": "codellama:7b"},
				{"name": "mistral:latest"},
			},
		})
	}))
	defer srv.Close()

	// Parse the server's port.
	var port int
	for i := len(srv.URL) - 1; i >= 0; i-- {
		if srv.URL[i] == ':' {
			_, err := json.Number(srv.URL[i+1:]).Int64()
			if err == nil {
				p, _ := json.Number(srv.URL[i+1:]).Int64()
				port = int(p)
			}
			break
		}
	}

	client := &http.Client{Timeout: 3e9}
	result := probeTarget(client, ProbeTarget{
		Name:       "ollama",
		Port:       port,
		HealthPath: "/api/tags",
		ModelsPath: "/api/tags",
	})

	if !result.Alive {
		t.Fatal("expected probe to be alive")
	}
	if len(result.Models) != 3 {
		t.Fatalf("models count = %d, want 3", len(result.Models))
	}
	// "llama3:latest" should be stripped to "llama3"
	found := false
	for _, m := range result.Models {
		if m == "llama3" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected model 'llama3' (stripped :latest), got %v", result.Models)
	}
	// "codellama:7b" should keep tag
	found = false
	for _, m := range result.Models {
		if m == "codellama:7b" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected model 'codellama:7b', got %v", result.Models)
	}
}

func TestProbeTarget_Unreachable(t *testing.T) {
	client := &http.Client{Timeout: 1e9}
	result := probeTarget(client, ProbeTarget{
		Name:       "ollama",
		Port:       19999, // unlikely to be listening
		HealthPath: "/api/tags",
	})

	if result.Alive {
		t.Error("expected probe to be not alive for unreachable port")
	}
	if len(result.Models) != 0 {
		t.Errorf("expected 0 models, got %d", len(result.Models))
	}
}

func TestBuildAnnotations_Healthy(t *testing.T) {
	results := []ProbeResult{
		{Name: "ollama", Port: 11434, Alive: true, Models: []string{"llama3", "mistral"}},
		{Name: "vllm", Port: 8000, Alive: false},
	}

	annotations := buildAnnotations(results)

	if annotations[annotationHealthy] != "true" {
		t.Errorf("healthy = %v, want true", annotations[annotationHealthy])
	}
	if annotations[annotationPrefix+"ollama"] != "11434" {
		t.Errorf("ollama port = %v, want 11434", annotations[annotationPrefix+"ollama"])
	}
	if annotations[annotationPrefix+"models-ollama"] != "llama3,mistral" {
		t.Errorf("ollama models = %v, want llama3,mistral", annotations[annotationPrefix+"models-ollama"])
	}
	// vllm should be nil (remove annotation)
	if annotations[annotationPrefix+"vllm"] != nil {
		t.Errorf("vllm port should be nil, got %v", annotations[annotationPrefix+"vllm"])
	}
}

func TestBuildAnnotations_AllDead(t *testing.T) {
	results := []ProbeResult{
		{Name: "ollama", Port: 11434, Alive: false},
	}

	annotations := buildAnnotations(results)

	if annotations[annotationHealthy] != nil {
		t.Errorf("healthy should be nil when nothing is alive, got %v", annotations[annotationHealthy])
	}
}

func TestParseModels_OllamaFormat(t *testing.T) {
	body := `{"models":[{"name":"llama3:latest"},{"name":"codellama:7b"}]}`
	models := parseModels([]byte(body), "ollama")
	if len(models) != 2 {
		t.Fatalf("count = %d, want 2", len(models))
	}
	if models[0] != "llama3" {
		t.Errorf("models[0] = %q, want llama3", models[0])
	}
	if models[1] != "codellama:7b" {
		t.Errorf("models[1] = %q, want codellama:7b", models[1])
	}
}

func TestParseModels_OpenAIFormat(t *testing.T) {
	body := `{"data":[{"id":"model-a"},{"id":"model-b"}]}`
	models := parseModels([]byte(body), "vllm")
	if len(models) != 2 {
		t.Fatalf("count = %d, want 2", len(models))
	}
	if models[0] != "model-a" {
		t.Errorf("models[0] = %q, want model-a", models[0])
	}
}

func TestReverseProxy_RoutesToAliveProvider(t *testing.T) {
	// Start a fake upstream server.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"path": r.URL.Path,
		})
	}))
	defer upstream.Close()

	// Parse upstream port.
	var upstreamPort int
	for i := len(upstream.URL) - 1; i >= 0; i-- {
		if upstream.URL[i] == ':' {
			p, _ := json.Number(upstream.URL[i+1:]).Int64()
			upstreamPort = int(p)
			break
		}
	}

	// Set up registry with the upstream as an alive target.
	registry := newTargetRegistry()
	targets := []ProbeTarget{{Name: "ollama", Host: "127.0.0.1", Port: upstreamPort}}
	results := []ProbeResult{{Name: "ollama", Port: upstreamPort, Alive: true}}
	registry.update(results, targets)

	// Build the mux (same as serveHealthAndProxy but testable).
	mux := http.NewServeMux()
	mux.HandleFunc("/proxy/", buildProxyHandler(registry))

	// Request through the proxy.
	req := httptest.NewRequest(http.MethodGet, "/proxy/ollama/api/tags", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	var body map[string]string
	json.NewDecoder(rec.Body).Decode(&body)
	if body["path"] != "/api/tags" {
		t.Errorf("upstream saw path %q, want /api/tags", body["path"])
	}
}

func TestReverseProxy_RejectsUnknownProvider(t *testing.T) {
	registry := newTargetRegistry()

	mux := http.NewServeMux()
	mux.HandleFunc("/proxy/", buildProxyHandler(registry))

	req := httptest.NewRequest(http.MethodGet, "/proxy/nonexistent/v1/models", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
}

func TestBuildAnnotations_IncludesProxyPort(t *testing.T) {
	results := []ProbeResult{
		{Name: "ollama", Port: 11434, Alive: true, Models: []string{"llama3"}},
	}
	annotations := buildAnnotations(results)

	proxyPort, ok := annotations[annotationPrefix+"proxy-port"]
	if !ok {
		t.Fatal("missing proxy-port annotation")
	}
	if proxyPort != "9473" {
		t.Errorf("proxy-port = %v, want 9473", proxyPort)
	}
}
