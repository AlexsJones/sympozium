package charts

import (
	"os/exec"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/yaml"
)

func TestControllerCatalogueOptInRendering(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm required for chart rendering")
	}
	for _, mode := range []string{"disabled", "legacy", "catalogue"} {
		t.Run(mode, func(t *testing.T) {
			args := []string{"template", "test", "sympozium", "--show-only", "templates/controller-deployment.yaml"}
			set := func(v string) { args = append(args, "--set", v) }
			if mode != "disabled" {
				for _, v := range []string{"celln.enabled=true", "celln.routerUrl=https://router.example", "celln.tokenSecret=client-token", "celln.capabilityTokenSecret=read-token", "celln.router.clientTokenSecret=client-token", "celln.router.backendTokenSecret=backend-token", "celln.router.capabilityTokenSecret=read-token", "celln.router.ownershipClaim=owners", "celln.router.backends[0]=http://host-a:8787", "celln.router.allowInsecureBackends=true", "celln.router.image.digest=sha256:" + strings.Repeat("a", 64)} {
					set(v)
				}
			}
			if mode == "catalogue" {
				set("celln.catalogueConfigSecret=catalogue-config")
				set("celln.harnessEnabled=true")
			}
			raw, err := exec.Command("helm", args...).CombinedOutput()
			if err != nil {
				t.Fatalf("render failed: %v: %s", err, raw)
			}
			var deployment appsv1.Deployment
			if err := yaml.Unmarshal(raw, &deployment); err != nil {
				t.Fatal(err)
			}
			if len(deployment.Spec.Template.Spec.Containers) != 1 {
				t.Fatal("unexpected controller pod")
			}
			container := deployment.Spec.Template.Spec.Containers[0]
			env := map[string]string{}
			for _, v := range container.Env {
				env[v.Name] = v.Value
			}
			if mode == "catalogue" {
				if env["CELLN_CATALOGUE_CONFIG"] != "/etc/sympozium/celln-catalogue/config.json" || env["CELLN_HARNESS_ENABLED"] != "true" {
					t.Fatal("catalogue opt-in env missing")
				}
				mountOK, secretOK := false, false
				for _, m := range container.VolumeMounts {
					if m.Name == "celln-catalogue-config" {
						mountOK = m.ReadOnly && m.MountPath == "/etc/sympozium/celln-catalogue"
					}
				}
				for _, v := range deployment.Spec.Template.Spec.Volumes {
					if v.Name == "celln-catalogue-config" {
						secretOK = v.Secret != nil && v.Secret.SecretName == "catalogue-config"
					}
				}
				if !mountOK || !secretOK {
					t.Fatal("catalogue configuration is not mounted read-only")
				}
			} else {
				if env["CELLN_CATALOGUE_CONFIG"] != "" {
					t.Fatal("catalogue configuration enabled implicitly")
				}
				if mode == "legacy" && env["CELLN_HARNESS_ENABLED"] != "false" {
					t.Fatal("Harness enabled implicitly")
				}
			}
		})
	}
}
