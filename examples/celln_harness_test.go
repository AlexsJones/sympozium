package examples

import (
	"os"
	"testing"

	api "github.com/sympozium-ai/sympozium/api/v1alpha1"
	"sigs.k8s.io/yaml"
)

func TestBorrowedCellnExampleIsExplicitOneShotIntent(t *testing.T) {
	raw, err := os.ReadFile("celln-harness-one-shot.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var run api.AgentRun
	if err := yaml.UnmarshalStrict(raw, &run); err != nil {
		t.Fatal(err)
	}
	if run.APIVersion != "sympozium.ai/v1alpha1" || run.Kind != "AgentRun" || run.GenerateName == "" || run.Namespace != "" {
		t.Fatal("example must require explicit namespace and create a named run")
	}
	if run.Spec.Backend != "celln" || run.Spec.Celln != nil || !run.Spec.Task.IsString() {
		t.Fatal("example must use catalogue Celln, not forge or OCI task override")
	}
	if run.Spec.AgentID == "" || run.Spec.SessionKey == "" {
		t.Fatal("required agentId and sessionKey must be explicit")
	}
	selection := run.Spec.CellnSelection
	if selection == nil || selection.RuntimeRef != "approved-celln-native" || len(selection.ToolRefs) != 2 {
		t.Fatal("explicit runtime and two borrowed tools required")
	}
	for i, name := range []string{"uppercase-v1", "length-v1"} {
		if selection.ToolRefs[i].Name != name || selection.ToolRefs[i].Revision != "v1" {
			t.Fatal("ordered reviewed revisions changed")
		}
	}
	if run.Spec.Model.Provider != "deepseek" || run.Spec.Model.Model != "deepseek-chat" || run.Spec.Model.AuthSecretRef != "" || run.Spec.Model.BaseURL != "" {
		t.Fatal("explicit host-approved model without Kubernetes credentials required")
	}
}
