package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Execute fail-closed preflight paths with a recording kubectl stand-in. No
// Kubernetes API is contacted and this is not sandbox lifecycle acceptance.
func TestSandboxScriptRefusesUnsafeTargetsBeforeMutation(t *testing.T) {
	for _, test := range []struct {
		name, context, namespace  string
		missingConfig, expectRead bool
	}{
		{"missing config", "kind-celln-deployed", "inttest-sandbox-proof", true, false},
		{"production context", "kubernetes-admin@kubernetes", "inttest-sandbox-proof", false, false},
		{"shared namespace", "kind-celln-deployed", "default", false, false},
		{"missing CRD", "kind-celln-deployed", "inttest-sandbox-proof", false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			kube, log := filepath.Join(dir, "kubeconfig"), filepath.Join(dir, "calls")
			if err := os.WriteFile(kube, []byte("private test fixture; no credential"), 0600); err != nil {
				t.Fatal(err)
			}
			fake := `#!/bin/sh
printf '%s\n' "$*" >> "$PROBE_LOG"
if [ "$5" = get ] && [ "$6" = namespace ]; then exit 0; fi
exit 1
`
			if err := os.WriteFile(filepath.Join(dir, "kubectl"), []byte(fake), 0700); err != nil {
				t.Fatal(err)
			}
			command := exec.Command("bash", "test-agent-sandbox.sh")
			for _, entry := range os.Environ() {
				if !strings.HasPrefix(entry, "TEST_") && !strings.HasPrefix(entry, "PATH=") && !strings.HasPrefix(entry, "PROBE_LOG=") {
					command.Env = append(command.Env, entry)
				}
			}
			command.Env = append(command.Env, "PATH="+dir+":"+os.Getenv("PATH"), "PROBE_LOG="+log, "TEST_CONTEXT="+test.context, "TEST_NAMESPACE="+test.namespace)
			if !test.missingConfig {
				command.Env = append(command.Env, "TEST_KUBECONFIG="+kube)
			}
			if out, err := command.CombinedOutput(); err == nil {
				t.Fatalf("unsafe/incomplete preflight accepted: %s", out)
			}
			raw, err := os.ReadFile(log)
			if !test.expectRead {
				if !os.IsNotExist(err) {
					t.Fatalf("kubectl reached before target validation: %s (%v)", raw, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			prefix := "--kubeconfig " + kube + " --context " + test.context + " get "
			for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
				if !strings.HasPrefix(line, prefix) {
					t.Fatalf("unscoped or mutating preflight command: %s", line)
				}
			}
		})
	}
}

func TestSandboxScriptCleanupKeepsExactNames(t *testing.T) {
	raw, err := os.ReadFile("test-agent-sandbox.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	for _, forbidden := range []string{"kubectl delete sandbox ", "kubectl delete sandboxclaim ", "delete sympoziuminstance", "kubectl set env", "kubectl rollout restart", `SANDBOX_RUN=""`, "hack/agent-sandbox-crds.yaml"} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("unsafe legacy script operation retained: %s", forbidden)
		}
	}
	if !strings.Contains(script, `kubectl delete agent "${SANDBOX_INSTANCE}"`) || !strings.Contains(script, `return "$failed"`) {
		t.Fatal("exact Agent cleanup and failure propagation required")
	}
}
