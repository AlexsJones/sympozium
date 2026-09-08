package cellnreview

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"k8s.io/apimachinery/pkg/types"
)

func TestLoadRunDispatcherConfig(t *testing.T) {
	for _, mode := range []string{"valid", "version", "empty", "duplicate", "sources", "shared-token", "plaintext", "unknown", "trailing"} {
		t.Run(mode, func(t *testing.T) {
			f := provisionFixture(t)
			config := ControllerDispatchConfig{APIVersion: "sympozium.ai/celln-catalogue-controller-v1", Bindings: []ControllerDispatchBinding{{Agent: types.NamespacedName{Namespace: "tenant", Name: "agent"}, Issuer: ControllerEndpoint{URL: "https://issuer.example", TokenFile: "/operator/issuer-token"}, Router: ControllerEndpoint{URL: "https://router.example", TokenFile: "/operator/router-token"}, Backend: "http://host-a:8787", OperatorSource: f.l.Selection.OperatorSource, RuntimeSource: f.l.Selection.RuntimeSource, AgentSource: f.l.Selection.AgentSource, ModelSource: f.l.Source}}}
			switch mode {
			case "version":
				config.APIVersion = "bad"
			case "empty":
				config.Bindings = nil
			case "duplicate":
				config.Bindings = append(config.Bindings, config.Bindings[0])
			case "sources":
				config.Bindings[0].ModelSource = config.Bindings[0].OperatorSource
			case "shared-token":
				config.Bindings[0].Router.TokenFile = config.Bindings[0].Issuer.TokenFile
			case "plaintext":
				config.Bindings[0].Router.URL = "http://router.example"
			}
			raw, err := json.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "unknown" {
				raw = append(raw[:len(raw)-1], []byte(`,"extra":true}`)...)
			}
			if mode == "trailing" {
				raw = append(raw, []byte(`{}`)...)
			}
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			d, close, err := LoadRunDispatcher(path, f.c, f.c)
			if mode == "valid" {
				if err != nil || d == nil || close == nil {
					t.Fatalf("valid config refused: %v", err)
				}
				close()
			} else if err == nil || d != nil {
				if close != nil {
					close()
				}
				t.Fatal("invalid config accepted")
			}
		})
	}
}
