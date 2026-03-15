// Package main is the entry point for the Sympozium node-probe DaemonSet.
// It probes localhost ports for local inference providers (Ollama, vLLM, llama-cpp)
// and annotates the Kubernetes node with discovered providers and models.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

const (
	annotationPrefix  = "sympozium.ai/inference-"
	annotationHealthy = "sympozium.ai/inference-healthy"
	annotationLastPr  = "sympozium.ai/inference-last-probe"
	defaultConfigPath = "/etc/node-probe/config.yaml"
	defaultHealthPort = 9473
)

// ProbeConfig is the top-level config loaded from the ConfigMap.
type ProbeConfig struct {
	ProbeInterval string        `yaml:"probeInterval"`
	Targets       []ProbeTarget `yaml:"targets"`
}

// ProbeTarget describes a single inference provider to probe.
type ProbeTarget struct {
	Name       string `yaml:"name"`
	Port       int    `yaml:"port"`
	HealthPath string `yaml:"healthPath"`
	ModelsPath string `yaml:"modelsPath"`
}

// ProbeResult holds the outcome of probing a single target.
type ProbeResult struct {
	Name   string
	Port   int
	Alive  bool
	Models []string
}

var log = ctrl.Log.WithName("node-probe")

func main() {
	ctrl.SetLogger(zap.New())

	nodeName := os.Getenv("NODE_NAME")
	if nodeName == "" {
		fmt.Fprintln(os.Stderr, "NODE_NAME environment variable is required")
		os.Exit(1)
	}

	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = defaultConfigPath
	}

	cfg, err := loadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	interval, err := time.ParseDuration(cfg.ProbeInterval)
	if err != nil {
		interval = 30 * time.Second
	}

	restCfg, err := rest.InClusterConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to get in-cluster config: %v\n", err)
		os.Exit(1)
	}

	clientset, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create kubernetes client: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sigCh
		log.Info("received shutdown signal, cleaning up annotations")
		cleanupAnnotations(context.Background(), clientset, nodeName, cfg.Targets)
		cancel()
	}()

	// Start health endpoint.
	go serveHealth()

	log.Info("starting node-probe", "node", nodeName, "interval", interval, "targets", len(cfg.Targets))

	// Run the first probe immediately.
	runProbeLoop(ctx, clientset, nodeName, cfg.Targets, interval)
}

func loadConfig(path string) (*ProbeConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}
	var cfg ProbeConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config YAML: %w", err)
	}
	if cfg.ProbeInterval == "" {
		cfg.ProbeInterval = "30s"
	}
	return &cfg, nil
}

func runProbeLoop(ctx context.Context, clientset kubernetes.Interface, nodeName string, targets []ProbeTarget, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run immediately on start.
	results := probeAll(targets)
	if err := patchNodeAnnotations(ctx, clientset, nodeName, results); err != nil {
		log.Error(err, "failed to patch node annotations")
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			results := probeAll(targets)
			if err := patchNodeAnnotations(ctx, clientset, nodeName, results); err != nil {
				log.Error(err, "failed to patch node annotations")
			}
		}
	}
}

func probeAll(targets []ProbeTarget) []ProbeResult {
	results := make([]ProbeResult, 0, len(targets))
	client := &http.Client{
		Timeout: 3 * time.Second,
		Transport: &http.Transport{
			DialContext: (&net.Dialer{Timeout: 2 * time.Second}).DialContext,
		},
	}

	for _, t := range targets {
		result := probeTarget(client, t)
		results = append(results, result)
		if result.Alive {
			log.V(1).Info("probe succeeded", "target", t.Name, "port", t.Port, "models", len(result.Models))
		} else {
			log.V(1).Info("probe failed", "target", t.Name, "port", t.Port)
		}
	}
	return results
}

func probeTarget(client *http.Client, target ProbeTarget) ProbeResult {
	result := ProbeResult{
		Name: target.Name,
		Port: target.Port,
	}

	// Use modelsPath if available (it also serves as health check), otherwise healthPath.
	probePath := target.ModelsPath
	if probePath == "" {
		probePath = target.HealthPath
	}
	if probePath == "" {
		probePath = "/"
	}

	url := fmt.Sprintf("http://localhost:%d%s", target.Port, probePath)
	resp, err := client.Get(url)
	if err != nil {
		return result
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return result
	}

	result.Alive = true

	// Try to parse models from response.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB limit
	if err != nil {
		return result
	}

	result.Models = parseModels(body, target.Name)
	return result
}

// parseModels extracts model names from a JSON response.
// Supports Ollama format ({"models":[{"name":"..."}]}) and
// OpenAI-compatible format ({"data":[{"id":"..."}]}).
func parseModels(body []byte, providerName string) []string {
	// Try Ollama format first.
	var ollamaResp struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &ollamaResp); err == nil && len(ollamaResp.Models) > 0 {
		names := make([]string, 0, len(ollamaResp.Models))
		for _, m := range ollamaResp.Models {
			// Strip tag suffix for cleaner display (e.g., "llama3:latest" → "llama3").
			name := m.Name
			if idx := strings.Index(name, ":"); idx > 0 {
				base := name[:idx]
				tag := name[idx+1:]
				if tag == "latest" {
					name = base
				}
			}
			names = append(names, name)
		}
		return names
	}

	// Try OpenAI-compatible format.
	var openaiResp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &openaiResp); err == nil && len(openaiResp.Data) > 0 {
		names := make([]string, 0, len(openaiResp.Data))
		for _, m := range openaiResp.Data {
			names = append(names, m.ID)
		}
		return names
	}

	return nil
}

// buildAnnotations converts probe results into the annotation map to set on the node.
func buildAnnotations(results []ProbeResult) map[string]interface{} {
	annotations := make(map[string]interface{})
	anyHealthy := false

	for _, r := range results {
		portKey := annotationPrefix + r.Name
		modelsKey := annotationPrefix + "models-" + r.Name
		if r.Alive {
			anyHealthy = true
			annotations[portKey] = fmt.Sprintf("%d", r.Port)
			if len(r.Models) > 0 {
				annotations[modelsKey] = strings.Join(r.Models, ",")
			} else {
				annotations[modelsKey] = nil // remove if no models
			}
		} else {
			annotations[portKey] = nil  // remove annotation
			annotations[modelsKey] = nil // remove annotation
		}
	}

	if anyHealthy {
		annotations[annotationHealthy] = "true"
		annotations[annotationLastPr] = time.Now().UTC().Format(time.RFC3339)
	} else {
		annotations[annotationHealthy] = nil
		annotations[annotationLastPr] = nil
	}

	return annotations
}

func patchNodeAnnotations(ctx context.Context, clientset kubernetes.Interface, nodeName string, results []ProbeResult) error {
	annotations := buildAnnotations(results)

	patch := map[string]interface{}{
		"metadata": map[string]interface{}{
			"annotations": annotations,
		},
	}

	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("marshalling patch: %w", err)
	}

	_, err = clientset.CoreV1().Nodes().Patch(
		ctx,
		nodeName,
		k8stypes.MergePatchType,
		patchBytes,
		metav1.PatchOptions{},
	)
	if err != nil {
		return fmt.Errorf("patching node %s: %w", nodeName, err)
	}

	log.V(1).Info("patched node annotations", "node", nodeName)
	return nil
}

func cleanupAnnotations(ctx context.Context, clientset kubernetes.Interface, nodeName string, targets []ProbeTarget) {
	annotations := make(map[string]interface{})
	for _, t := range targets {
		annotations[annotationPrefix+t.Name] = nil
		annotations[annotationPrefix+"models-"+t.Name] = nil
	}
	annotations[annotationHealthy] = nil
	annotations[annotationLastPr] = nil

	patch := map[string]interface{}{
		"metadata": map[string]interface{}{
			"annotations": annotations,
		},
	}

	patchBytes, err := json.Marshal(patch)
	if err != nil {
		log.Error(err, "failed to marshal cleanup patch")
		return
	}

	_, err = clientset.CoreV1().Nodes().Patch(
		ctx,
		nodeName,
		k8stypes.MergePatchType,
		patchBytes,
		metav1.PatchOptions{},
	)
	if err != nil {
		log.Error(err, "failed to clean up node annotations", "node", nodeName)
	} else {
		log.Info("cleaned up node annotations", "node", nodeName)
	}
}

func serveHealth() {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	addr := fmt.Sprintf(":%d", defaultHealthPort)
	log.Info("starting health server", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Error(err, "health server failed")
	}
}
