package charts

import (
	"os/exec"
	"slices"
	"strings"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/yaml"
)

func TestAPIServerCataloguePermissionIsReadOnly(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm required for chart rendering")
	}
	raw, err := exec.Command("helm", "template", "test", "sympozium", "--show-only", "templates/rbac.yaml").CombinedOutput()
	if err != nil {
		t.Fatalf("render: %v: %s", err, raw)
	}
	found := false
	for _, document := range strings.Split(string(raw), "---") {
		var role rbacv1.ClusterRole
		if yaml.Unmarshal([]byte(document), &role) != nil || role.Kind != "ClusterRole" || !strings.HasSuffix(role.Name, "-apiserver") {
			continue
		}
		for _, rule := range role.Rules {
			if !slices.Contains(rule.Resources, "cellntools") {
				continue
			}
			found = true
			for _, verb := range rule.Verbs {
				if !slices.Contains([]string{"get", "list", "watch"}, verb) {
					t.Fatalf("catalogue write authority: %s", verb)
				}
			}
		}
	}
	if !found {
		t.Fatal("catalogue read permission absent")
	}
}
