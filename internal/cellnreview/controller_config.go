package cellnreview

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"

	"github.com/sympozium-ai/sympozium/internal/cellnauthority"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ControllerEndpoint struct {
	URL       string `json:"url"`
	TokenFile string `json:"tokenFile"`
	CAFile    string `json:"caFile,omitempty"`
}
type ControllerDispatchBinding struct {
	Agent          types.NamespacedName `json:"agent"`
	Issuer         ControllerEndpoint   `json:"issuer"`
	Router         ControllerEndpoint   `json:"router"`
	Backend        string               `json:"backend"`
	OperatorSource types.NamespacedName `json:"operatorSource"`
	RuntimeSource  types.NamespacedName `json:"runtimeSource"`
	AgentSource    types.NamespacedName `json:"agentSource"`
	ModelSource    types.NamespacedName `json:"modelSource"`
}
type ControllerDispatchConfig struct {
	APIVersion string                      `json:"apiVersion"`
	Bindings   []ControllerDispatchBinding `json:"bindings"`
}

// LoadRunDispatcher reads only operator deployment configuration. It performs
// no network call, provisioning, readiness assertion or Kubernetes mutation.
// Recreate on config/CA changes; token file contents are reread per operation.
func LoadRunDispatcher(path string, writer client.Client, reader client.Reader) (*RunDispatcher, func(), error) {
	if !filepath.IsAbs(path) {
		return nil, nil, fmt.Errorf("absolute catalogue controller config path required")
	}
	raw, err := readLimit(path, 1<<20)
	if err != nil {
		return nil, nil, fmt.Errorf("catalogue controller config unavailable or oversized")
	}
	var config ControllerDispatchConfig
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&config) != nil || d.Decode(new(any)) != io.EOF || config.APIVersion != "sympozium.ai/celln-catalogue-controller-v1" || len(config.Bindings) == 0 || len(config.Bindings) > 1024 {
		return nil, nil, fmt.Errorf("invalid catalogue controller config")
	}
	bindings := make(map[types.NamespacedName]RunDispatchBinding, len(config.Bindings))
	var closers []func()
	closeAll := func() {
		for _, close := range closers {
			close()
		}
	}
	success := false
	defer func() {
		if !success {
			closeAll()
		}
	}()
	for _, b := range config.Bindings {
		if b.Agent.Name == "" || b.Agent.Namespace == "" {
			return nil, nil, fmt.Errorf("namespaced Agent binding required")
		}
		if _, ok := bindings[b.Agent]; ok {
			return nil, nil, fmt.Errorf("duplicate Agent binding")
		}
		seen := map[types.NamespacedName]bool{}
		for _, ref := range []types.NamespacedName{b.OperatorSource, b.RuntimeSource, b.AgentSource, b.ModelSource} {
			if ref.Namespace == "" || ref.Name == "" || seen[ref] {
				return nil, nil, fmt.Errorf("four distinct namespaced authority sources required")
			}
			seen[ref] = true
		}
		issuer, err := NewIssuerClient(IssuerClientOptions{URL: b.Issuer.URL, TokenFile: b.Issuer.TokenFile, CAFile: b.Issuer.CAFile, Route: &DispatchRoute{RouterURL: b.Router.URL, Backend: b.Backend}})
		if err != nil {
			return nil, nil, err
		}
		closers = append(closers, issuer.CloseIdleConnections)
		router, err := NewRouterClient(b.Router.URL, b.Router.TokenFile, b.Router.CAFile)
		if err != nil {
			return nil, nil, err
		}
		closers = append(closers, router.CloseIdleConnections)
		bindings[b.Agent] = RunDispatchBinding{Issuer: issuer, Router: router, Loader: cellnauthority.ModelLoader{Selection: cellnauthority.Loader{Reader: reader, OperatorSource: b.OperatorSource, RuntimeSource: b.RuntimeSource, AgentSource: b.AgentSource}, Source: b.ModelSource}}
	}
	result, err := NewRunDispatcher(writer, reader, bindings)
	if err != nil {
		return nil, nil, err
	}
	success = true
	return result, closeAll, nil
}
