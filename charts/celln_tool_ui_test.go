package charts

import (
	"os/exec"
	"slices"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
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

func TestPreviewMountIsExplicitReadOnlyAndDoesNotEnableExecution(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm required")
	}
	for _, enabled := range []bool{false, true} {
		args := []string{"template", "test", "sympozium", "--show-only", "templates/apiserver-deployment.yaml", "--set", "celln.enabled=false", "--set", "apiserver.webUI.token=public-test-token"}
		if enabled {
			args = append(args, "--set", "celln.permissionPreviewConfigMap=operator-preview")
		}
		raw, err := exec.Command("helm", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("render: %v %s", err, raw)
		}
		var deployment appsv1.Deployment
		for _, doc := range strings.Split(string(raw), "---") {
			var d appsv1.Deployment
			if yaml.Unmarshal([]byte(doc), &d) == nil && d.Kind == "Deployment" {
				deployment = d
			}
		}
		if len(deployment.Spec.Template.Spec.Containers) != 1 {
			t.Fatal("missing deployment")
		}
		container := deployment.Spec.Template.Spec.Containers[0]
		foundEnv, foundMount, foundVolume := false, false, false
		for _, env := range container.Env {
			if env.Name == "CELLN_ENABLED" || env.Name == "CELLN_CATALOGUE_CONFIG" {
				t.Fatal("preview enabled execution or issuer configuration")
			}
			if env.Name == "CELLN_PERMISSION_PREVIEW_CONFIG" {
				foundEnv = env.Value == "/etc/sympozium/celln-preview/config.json"
			}
		}
		for _, mount := range container.VolumeMounts {
			if mount.Name == "celln-permission-preview" {
				foundMount = mount.ReadOnly && mount.MountPath == "/etc/sympozium/celln-preview"
			}
		}
		for _, volume := range deployment.Spec.Template.Spec.Volumes {
			if volume.Name == "celln-permission-preview" {
				foundVolume = volume.Secret == nil && volume.ConfigMap != nil && volume.ConfigMap.Name == "operator-preview" && len(volume.ConfigMap.Items) == 1 && volume.ConfigMap.Items[0].Key == "config.json"
			}
		}
		if foundEnv != enabled || foundMount != enabled || foundVolume != enabled {
			t.Fatalf("preview wiring enabled=%t env=%t mount=%t volume=%t", enabled, foundEnv, foundMount, foundVolume)
		}
	}
}
