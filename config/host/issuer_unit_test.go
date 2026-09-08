package host_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// These are packaging checks, not service startup, sandbox or KVM evidence.
func TestIssuerUnitBoundaries(t *testing.T) {
	raw, err := os.ReadFile("sympozium-celln-issuer.service")
	if err != nil {
		t.Fatal(err)
	}
	unit := string(raw)
	for _, line := range []string{
		"Type=exec", "User=celln", "Group=celln", "SupplementaryGroups=kvm",
		"ExecStart=/opt/sympozium-celln/bin/sympozium --kubeconfig /etc/sympozium-issuer/kubeconfig celln-tool serve-issuer --config /etc/sympozium-issuer/issuer.json",
		"KillMode=control-group", "UMask=0077", "NoNewPrivileges=yes",
		"ProtectSystem=strict", "ProtectHome=yes", "PrivateDevices=no",
		"DevicePolicy=closed", "DeviceAllow=/dev/kvm rw",
		"ReadWritePaths=/var/lib/celln", "ReadOnlyPaths=/etc/sympozium-issuer /opt/sympozium-celln",
	} {
		if !strings.Contains("\n"+unit, "\n"+line+"\n") {
			t.Fatalf("missing boundary: %s", line)
		}
	}
	for _, line := range strings.Split(unit, "\n") {
		if strings.HasPrefix(line, "Environment=") || strings.HasPrefix(line, "EnvironmentFile=") || strings.HasPrefix(line, "ExecStopPost=") || strings.HasPrefix(line, "StateDirectory=") {
			t.Fatalf("credentials or destructive/implicit state management in unit: %s", line)
		}
	}
}

func TestIssuerUnitSystemdSyntax(t *testing.T) {
	verify, err := exec.LookPath("systemd-analyze")
	if err != nil {
		t.Skip("systemd-analyze unavailable; syntax verification not performed")
	}
	raw, err := os.ReadFile("sympozium-celln-issuer.service")
	if err != nil {
		t.Fatal(err)
	}
	// The packaged binary is not installed on developer/CI hosts. Substitute
	// this existing test executable for verify's path check only. Never start it.
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	unit := strings.Replace(string(raw), "ExecStart=/opt/sympozium-celln/bin/sympozium ", "ExecStart="+executable+" ", 1)
	path := filepath.Join(t.TempDir(), "sympozium-celln-issuer.service")
	if err := os.WriteFile(path, []byte(unit), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(verify, "verify", "--man=no", path).CombinedOutput(); err != nil {
		t.Fatalf("systemd verify: %v\n%s", err, out)
	}
}

func TestIssuerHostPreflightIsReadOnly(t *testing.T) {
	raw, err := os.ReadFile("check-issuer-host.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	for _, forbidden := range []string{"sudo ", "systemctl ", "useradd ", "groupadd ", "chmod ", "chown ", "install ", "cat /etc/", "source ", "rm "} {
		for _, line := range strings.Split(script, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue
			}
			if strings.Contains(line, forbidden) {
				t.Fatalf("unexpected mutation/credential access: %s", line)
			}
		}
	}
	if !strings.Contains(script, "NOT TESTED:") || !strings.Contains(script, "not installation acceptance") {
		t.Fatal("preflight must disclose its limits")
	}
	if output, err := exec.Command("bash", "-n", "check-issuer-host.sh").CombinedOutput(); err != nil {
		t.Fatalf("preflight syntax: %v %s", err, output)
	}
}
