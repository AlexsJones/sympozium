package apiserver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	sympoziumv1alpha1 "github.com/sympozium-ai/sympozium/api/v1alpha1"
)

func TestCreateRunWithRuntimeRefCarriesHarnessPrompt(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := sympoziumv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	agent := &sympoziumv1alpha1.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: "test-agent", Namespace: "default"},
		Spec: sympoziumv1alpha1.AgentSpec{Agents: sympoziumv1alpha1.AgentsSpec{
			Default: sympoziumv1alpha1.AgentConfig{Model: "local", BaseURL: "http://llm.local/v1"},
		}},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(agent).Build()
	srv := NewServer(cl, nil, nil, logr.Discard())
	body := bytes.NewBufferString(`{"agentRef":"test-agent","task":"prove harness prompt","runtimeRef":"reference-v1"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs", body)
	resp := httptest.NewRecorder()

	srv.Handler(nil).ServeHTTP(resp, req)
	if resp.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", resp.Code, http.StatusCreated, resp.Body.String())
	}
	var created sympoziumv1alpha1.AgentRun
	if err := json.Unmarshal(resp.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Spec.Task.Mode != "harness" {
		t.Fatalf("task.mode = %q, want harness", created.Spec.Task.Mode)
	}
	if got := created.Spec.Task.Parameters["runtime"]; got != "reference-v1" {
		t.Errorf("task.parameters.runtime = %q", got)
	}
	if got := created.Spec.Task.Parameters["prompt"]; got != "prove harness prompt" {
		t.Errorf("task.parameters.prompt = %q", got)
	}
}

func TestCreateCatalogueRunPreservesIntentAndUsesHostModelAuthority(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := sympoziumv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	agent := &sympoziumv1alpha1.Agent{ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "default"}, Spec: sympoziumv1alpha1.AgentSpec{RuntimeRef: "default-runtime", AuthRefs: []sympoziumv1alpha1.SecretRef{}}}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(agent).Build()
	srv := NewServer(cl, nil, nil, logr.Discard())
	body := `{"agentRef":"agent","task":"use tools","backend":"celln","provider":"deepseek","model":"deepseek-chat","cellnSelection":{"runtimeRef":"override-runtime","toolRefs":[{"name":"uppercase","revision":"v1"},{"name":"length","revision":"v1"}]}}`
	response := httptest.NewRecorder()
	srv.Handler(nil).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/runs", bytes.NewBufferString(body)))
	if response.Code != http.StatusCreated {
		t.Fatalf("catalogue create: %d %s", response.Code, response.Body.String())
	}
	if got := response.Result().Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("committed response content type = %q, want application/json", got)
	}
	var run sympoziumv1alpha1.AgentRun
	if err := json.Unmarshal(response.Body.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	if !run.Spec.Task.IsString() || run.Spec.Celln != nil || run.Spec.CellnSelection == nil || run.Spec.CellnSelection.RuntimeRef != "override-runtime" || len(run.Spec.CellnSelection.ToolRefs) != 2 || run.Spec.CellnSelection.ToolRefs[0].Name != "uppercase" || run.Spec.CellnSelection.ToolRefs[1].Name != "length" {
		t.Fatal("catalogue intent changed or converted to OCI task")
	}
	if run.Spec.Model.Provider != "deepseek" || run.Spec.Model.Model != "deepseek-chat" || run.Spec.Model.AuthSecretRef != "" || run.Spec.Model.BaseURL != "" {
		t.Fatal("model authority inherited instead of explicit host selection")
	}
}

func TestCreateCatalogueRunRefusesAmbiguousOrImplicitModel(t *testing.T) {
	for _, body := range []string{
		`{"agentRef":"agent","task":"task","backend":"celln","provider":"deepseek","model":"deepseek-chat","celln":{},"cellnSelection":{"toolRefs":[]}}`,
		`{"agentRef":"agent","task":"task","backend":"job","provider":"deepseek","model":"deepseek-chat","cellnSelection":{"toolRefs":[]}}`,
		`{"agentRef":"agent","task":"task","backend":"celln","model":"deepseek-chat","cellnSelection":{"toolRefs":[]}}`,
		`{"agentRef":"agent","task":"task","backend":"celln","provider":"deepseek","cellnSelection":{"toolRefs":[]}}`,
		`{"agentRef":"agent","task":"task","backend":"celln","provider":"deepseek","model":"deepseek-chat","runtimeRef":"ambiguous","cellnSelection":{"toolRefs":[]}}`,
		`{"agentRef":"agent","task":"task","backend":"celln","provider":"deepseek","model":"deepseek-chat","cellnSelection":{}}`,
	} {
		srv := NewServer(nil, nil, nil, logr.Discard())
		response := httptest.NewRecorder()
		srv.Handler(nil).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/runs", bytes.NewBufferString(body)))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("ambiguous catalogue accepted: %d", response.Code)
		}
	}
}
