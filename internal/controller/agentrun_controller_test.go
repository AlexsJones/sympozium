package controller

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	sympoziumv1alpha1 "github.com/alexsjones/sympozium/api/v1alpha1"
)

// helper builds a minimal AgentRun for testing.
func newTestRun() *sympoziumv1alpha1.AgentRun {
	return &sympoziumv1alpha1.AgentRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-run",
			Namespace: "default",
		},
		Spec: sympoziumv1alpha1.AgentRunSpec{
			InstanceRef: "my-instance",
			AgentID:     "default",
			SessionKey:  "sess-1",
			Task:        "do stuff",
			Model: sympoziumv1alpha1.ModelSpec{
				Provider:      "openai",
				Model:         "gpt-4o",
				AuthSecretRef: "my-secret",
			},
		},
	}
}

// ── buildJob tests ───────────────────────────────────────────────────────────

func TestBuildJob_BasicMetadata(t *testing.T) {
	r := &AgentRunReconciler{}
	run := newTestRun()
	job := r.buildJob(run, false, nil, nil, nil)

	if job.Name != "test-run" {
		t.Errorf("name = %q, want test-run", job.Name)
	}
	if job.Namespace != "default" {
		t.Errorf("namespace = %q, want default", job.Namespace)
	}
}

func TestBuildJob_Labels(t *testing.T) {
	r := &AgentRunReconciler{}
	run := newTestRun()
	job := r.buildJob(run, false, nil, nil, nil)

	labels := job.Spec.Template.Labels
	if labels["sympozium.ai/instance"] != "my-instance" {
		t.Errorf("instance label = %q", labels["sympozium.ai/instance"])
	}
	if labels["sympozium.ai/agent-run"] != "test-run" {
		t.Errorf("agent-run label = %q", labels["sympozium.ai/agent-run"])
	}
	if labels["sympozium.ai/component"] != "agent-run" {
		t.Errorf("component label = %q", labels["sympozium.ai/component"])
	}
}

func TestBuildJob_TTLAndBackoff(t *testing.T) {
	r := &AgentRunReconciler{}
	job := r.buildJob(newTestRun(), false, nil, nil, nil)

	if job.Spec.TTLSecondsAfterFinished == nil || *job.Spec.TTLSecondsAfterFinished != 300 {
		t.Error("TTL should be 300")
	}
	if job.Spec.BackoffLimit == nil || *job.Spec.BackoffLimit != 0 {
		t.Error("BackoffLimit should be 0")
	}
}

func TestBuildJob_DeadlineDefault(t *testing.T) {
	r := &AgentRunReconciler{}
	job := r.buildJob(newTestRun(), false, nil, nil, nil)

	if job.Spec.ActiveDeadlineSeconds == nil || *job.Spec.ActiveDeadlineSeconds != 600 {
		t.Errorf("deadline = %v, want 600", job.Spec.ActiveDeadlineSeconds)
	}
}

func TestBuildJob_DeadlineWithTimeout(t *testing.T) {
	r := &AgentRunReconciler{}
	run := newTestRun()
	run.Spec.Timeout = &metav1.Duration{Duration: 5 * time.Minute}
	job := r.buildJob(run, false, nil, nil, nil)

	// 5min = 300s + 60 = 360
	if job.Spec.ActiveDeadlineSeconds == nil || *job.Spec.ActiveDeadlineSeconds != 360 {
		t.Errorf("deadline = %v, want 360", job.Spec.ActiveDeadlineSeconds)
	}
}

func TestBuildJob_ServiceAccount(t *testing.T) {
	r := &AgentRunReconciler{}
	job := r.buildJob(newTestRun(), false, nil, nil, nil)

	if job.Spec.Template.Spec.ServiceAccountName != "sympozium-agent" {
		t.Errorf("SA = %q, want sympozium-agent", job.Spec.Template.Spec.ServiceAccountName)
	}
}

func TestBuildJob_PodSecurityContext(t *testing.T) {
	r := &AgentRunReconciler{}
	job := r.buildJob(newTestRun(), false, nil, nil, nil)

	psc := job.Spec.Template.Spec.SecurityContext
	if psc == nil {
		t.Fatal("pod security context is nil")
	}
	if psc.RunAsNonRoot == nil || !(*psc.RunAsNonRoot) {
		t.Error("RunAsNonRoot should be true")
	}
	if psc.RunAsUser == nil || *psc.RunAsUser != 1000 {
		t.Errorf("RunAsUser = %v, want 1000", psc.RunAsUser)
	}
}

func TestBuildJob_RestartPolicy(t *testing.T) {
	r := &AgentRunReconciler{}
	job := r.buildJob(newTestRun(), false, nil, nil, nil)

	if job.Spec.Template.Spec.RestartPolicy != corev1.RestartPolicyNever {
		t.Errorf("restart = %q, want Never", job.Spec.Template.Spec.RestartPolicy)
	}
}

func TestBuildJob_DefaultSeccompProfile(t *testing.T) {
	r := &AgentRunReconciler{}
	job := r.buildJob(newTestRun(), false, nil, nil, nil)

	psc := job.Spec.Template.Spec.SecurityContext
	if psc == nil {
		t.Fatal("pod security context is nil")
	}
	if psc.SeccompProfile == nil {
		t.Fatal("seccomp profile is nil, want RuntimeDefault")
	}
	if psc.SeccompProfile.Type != corev1.SeccompProfileTypeRuntimeDefault {
		t.Errorf("seccomp type = %q, want RuntimeDefault", psc.SeccompProfile.Type)
	}
}

func TestBuildJob_CustomSeccompProfile(t *testing.T) {
	r := &AgentRunReconciler{}
	run := newTestRun()
	run.Spec.Sandbox = &sympoziumv1alpha1.AgentRunSandboxSpec{
		Enabled: true,
		SecurityContext: &sympoziumv1alpha1.SandboxSecurityContext{
			SeccompProfile: &sympoziumv1alpha1.SeccompProfileSpec{
				Type: "Unconfined",
			},
		},
	}
	job := r.buildJob(run, false, nil, nil, nil)

	psc := job.Spec.Template.Spec.SecurityContext
	if psc.SeccompProfile == nil {
		t.Fatal("seccomp profile is nil")
	}
	if psc.SeccompProfile.Type != corev1.SeccompProfileTypeUnconfined {
		t.Errorf("seccomp type = %q, want Unconfined", psc.SeccompProfile.Type)
	}
}

// ── buildContainers tests ────────────────────────────────────────────────────

func TestBuildContainers_BasicCount(t *testing.T) {
	r := &AgentRunReconciler{}
	cs, _ := r.buildContainers(newTestRun(), false, nil, nil, nil)
	// agent + ipc-bridge = 2
	if len(cs) != 2 {
		t.Fatalf("container count = %d, want 2", len(cs))
	}
}

func TestBuildContainers_AgentImage(t *testing.T) {
	r := &AgentRunReconciler{}
	cs, _ := r.buildContainers(newTestRun(), false, nil, nil, nil)
	// agent container should reference agent-runner image
	if cs[0].Name != "agent" {
		t.Fatalf("first container name = %q, want agent", cs[0].Name)
	}
	if cs[0].Image == "" {
		t.Error("agent image is empty")
	}
}

func TestBuildContainers_IPCBridgeImage(t *testing.T) {
	r := &AgentRunReconciler{}
	cs, _ := r.buildContainers(newTestRun(), false, nil, nil, nil)
	if cs[1].Name != "ipc-bridge" {
		t.Fatalf("second container name = %q, want ipc-bridge", cs[1].Name)
	}
	if cs[1].Image == "" {
		t.Error("ipc-bridge image is empty")
	}
}

func TestBuildContainers_AgentEnvVars(t *testing.T) {
	r := &AgentRunReconciler{}
	run := newTestRun()
	cs, _ := r.buildContainers(run, false, nil, nil, nil)

	envMap := map[string]string{}
	for _, e := range cs[0].Env {
		envMap[e.Name] = e.Value
	}
	if envMap["TASK"] != "do stuff" {
		t.Errorf("TASK = %q", envMap["TASK"])
	}
	if envMap["MODEL_PROVIDER"] != "openai" {
		t.Errorf("MODEL_PROVIDER = %q", envMap["MODEL_PROVIDER"])
	}
	if envMap["MODEL_NAME"] != "gpt-4o" {
		t.Errorf("MODEL_NAME = %q", envMap["MODEL_NAME"])
	}
}

func TestBuildContainers_AuthSecretRef(t *testing.T) {
	r := &AgentRunReconciler{}
	run := newTestRun()
	cs, _ := r.buildContainers(run, false, nil, nil, nil)

	if len(cs[0].EnvFrom) == 0 {
		t.Fatal("expected envFrom for auth secret")
	}
	if cs[0].EnvFrom[0].SecretRef.Name != "my-secret" {
		t.Errorf("secret = %q, want my-secret", cs[0].EnvFrom[0].SecretRef.Name)
	}
}

func TestBuildContainers_NoAuthSecretRef(t *testing.T) {
	r := &AgentRunReconciler{}
	run := newTestRun()
	run.Spec.Model.AuthSecretRef = ""
	cs, _ := r.buildContainers(run, false, nil, nil, nil)

	if len(cs[0].EnvFrom) != 0 {
		t.Errorf("envFrom should be empty for no-auth providers, got %d", len(cs[0].EnvFrom))
	}
}

func TestBuildContainers_AgentSecurityContext(t *testing.T) {
	r := &AgentRunReconciler{}
	cs, _ := r.buildContainers(newTestRun(), false, nil, nil, nil)

	sc := cs[0].SecurityContext
	if sc == nil {
		t.Fatal("agent security context is nil")
	}
	if sc.ReadOnlyRootFilesystem == nil || !(*sc.ReadOnlyRootFilesystem) {
		t.Error("ReadOnlyRootFilesystem should be true")
	}
}

func TestBuildContainers_AgentVolumeMounts(t *testing.T) {
	r := &AgentRunReconciler{}
	cs, _ := r.buildContainers(newTestRun(), false, nil, nil, nil)

	mounts := map[string]bool{}
	for _, m := range cs[0].VolumeMounts {
		mounts[m.Name] = true
	}
	for _, want := range []string{"workspace", "ipc", "tmp", "skills"} {
		if !mounts[want] {
			t.Errorf("missing volume mount %q", want)
		}
	}
}

func TestBuildContainers_AgentResources(t *testing.T) {
	r := &AgentRunReconciler{}
	cs, _ := r.buildContainers(newTestRun(), false, nil, nil, nil)

	req := cs[0].Resources.Requests
	if req.Cpu().Cmp(resource.MustParse("250m")) != 0 {
		t.Errorf("cpu request = %v", req.Cpu())
	}
	if req.Memory().Cmp(resource.MustParse("512Mi")) != 0 {
		t.Errorf("memory request = %v", req.Memory())
	}
}

func TestBuildContainers_IPCBridgeEnvVars(t *testing.T) {
	r := &AgentRunReconciler{}
	run := newTestRun()
	cs, _ := r.buildContainers(run, false, nil, nil, nil)

	envMap := map[string]string{}
	for _, e := range cs[1].Env {
		envMap[e.Name] = e.Value
	}
	if envMap["AGENT_RUN_ID"] != "test-run" {
		t.Errorf("AGENT_RUN_ID = %q", envMap["AGENT_RUN_ID"])
	}
	if envMap["INSTANCE_NAME"] != "my-instance" {
		t.Errorf("INSTANCE_NAME = %q", envMap["INSTANCE_NAME"])
	}
}

func TestBuildContainers_WithSandbox(t *testing.T) {
	r := &AgentRunReconciler{}
	run := newTestRun()
	run.Spec.Sandbox = &sympoziumv1alpha1.AgentRunSandboxSpec{Enabled: true}
	cs, _ := r.buildContainers(run, false, nil, nil, nil)
	// agent + ipc-bridge + sandbox = 3
	if len(cs) != 3 {
		t.Fatalf("container count = %d, want 3", len(cs))
	}
	if cs[2].Name != "sandbox" {
		t.Errorf("third container name = %q, want sandbox", cs[2].Name)
	}
}

func TestBuildContainers_SandboxCustomImage(t *testing.T) {
	r := &AgentRunReconciler{}
	run := newTestRun()
	run.Spec.Sandbox = &sympoziumv1alpha1.AgentRunSandboxSpec{
		Enabled: true,
		Image:   "my-sandbox:v1",
	}
	cs, _ := r.buildContainers(run, false, nil, nil, nil)
	if cs[2].Image != "my-sandbox:v1" {
		t.Errorf("sandbox image = %q, want my-sandbox:v1", cs[2].Image)
	}
}

func TestBuildContainers_SandboxDisabled(t *testing.T) {
	r := &AgentRunReconciler{}
	run := newTestRun()
	run.Spec.Sandbox = &sympoziumv1alpha1.AgentRunSandboxSpec{Enabled: false}
	cs, _ := r.buildContainers(run, false, nil, nil, nil)
	if len(cs) != 2 {
		t.Errorf("container count = %d, want 2 (sandbox disabled)", len(cs))
	}
}

// ── buildVolumes tests ───────────────────────────────────────────────────────

func TestBuildVolumes_DefaultVolumes(t *testing.T) {
	r := &AgentRunReconciler{}
	vols := r.buildVolumes(newTestRun(), false, nil, nil)

	names := map[string]bool{}
	for _, v := range vols {
		names[v.Name] = true
	}
	for _, want := range []string{"workspace", "ipc", "tmp", "skills"} {
		if !names[want] {
			t.Errorf("missing volume %q", want)
		}
	}
}

func TestBuildVolumes_IPCUsesMemory(t *testing.T) {
	r := &AgentRunReconciler{}
	vols := r.buildVolumes(newTestRun(), false, nil, nil)

	for _, v := range vols {
		if v.Name == "ipc" {
			if v.EmptyDir == nil {
				t.Fatal("ipc volume should be emptyDir")
			}
			if v.EmptyDir.Medium != corev1.StorageMediumMemory {
				t.Errorf("ipc medium = %q, want Memory", v.EmptyDir.Medium)
			}
			return
		}
	}
	t.Error("ipc volume not found")
}

// ── result parsing tests ─────────────────────────────────────────────────────

func TestParseAgentResultFromLogs_Success(t *testing.T) {
	logs := "noise\n" +
		"__SYMPOZIUM_RESULT__" +
		`{"status":"success","response":"all good","metrics":{"durationMs":1200,"inputTokens":10,"outputTokens":20,"toolCalls":1}}` +
		"__SYMPOZIUM_END__\n"

	result, errMsg, usage := parseAgentResultFromLogs(logs, logr.Discard())
	if errMsg != "" {
		t.Fatalf("unexpected error message: %q", errMsg)
	}
	if result != "all good" {
		t.Fatalf("result = %q, want %q", result, "all good")
	}
	if usage == nil {
		t.Fatal("expected token usage, got nil")
	}
	if usage.TotalTokens != 30 {
		t.Fatalf("total tokens = %d, want 30", usage.TotalTokens)
	}
}

func TestParseAgentResultFromLogs_Error(t *testing.T) {
	want := "OpenAI API error (HTTP 429): insufficient_quota"
	logs := "__SYMPOZIUM_RESULT__" +
		fmt.Sprintf(`{"status":"error","error":%q,"metrics":{"durationMs":123}}`, want) +
		"__SYMPOZIUM_END__\n"

	result, errMsg, usage := parseAgentResultFromLogs(logs, logr.Discard())
	if result != "" {
		t.Fatalf("expected empty result, got %q", result)
	}
	if errMsg != want {
		t.Fatalf("error = %q, want %q", errMsg, want)
	}
	if usage != nil {
		t.Fatalf("expected nil usage on error, got %+v", usage)
	}
}

func TestExtractLikelyProviderErrorFromLogs_Quota(t *testing.T) {
	logs := `
2026/03/01 12:00:00 agent-runner starting
2026/03/01 12:00:01 LLM call failed: Anthropic API error (HTTP 429): {"type":"error","error":{"type":"rate_limit_error","message":"You have run out of credits"}}
2026/03/01 12:00:01 agent-runner finished with error
`
	got := extractLikelyProviderErrorFromLogs(logs)
	if got == "" {
		t.Fatal("expected quota/rate-limit message, got empty")
	}
	if want := "HTTP 429"; !containsIgnoreCase(got, want) {
		t.Fatalf("message %q does not contain %q", got, want)
	}
}

func containsIgnoreCase(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}

func TestBuildVolumes_SkillsWithRefs(t *testing.T) {
	r := &AgentRunReconciler{}
	run := newTestRun()
	run.Spec.Skills = []sympoziumv1alpha1.SkillRef{
		{ConfigMapRef: "my-skills"},
	}
	vols := r.buildVolumes(run, false, nil, nil)

	for _, v := range vols {
		if v.Name == "skills" {
			if v.Projected == nil {
				t.Fatal("skills volume should be projected when refs exist")
			}
			return
		}
	}
	t.Error("skills volume not found")
}

func TestBuildVolumes_SkillsEmptyWhenNoRefs(t *testing.T) {
	r := &AgentRunReconciler{}
	run := newTestRun()
	run.Spec.Skills = nil
	vols := r.buildVolumes(run, false, nil, nil)

	for _, v := range vols {
		if v.Name == "skills" {
			if v.EmptyDir == nil {
				t.Fatal("skills volume should be emptyDir when no refs")
			}
			return
		}
	}
	t.Error("skills volume not found")
}

func TestBuildVolumes_MemoryEnabled(t *testing.T) {
	r := &AgentRunReconciler{}
	run := newTestRun()
	vols := r.buildVolumes(run, true, nil, nil)

	for _, v := range vols {
		if v.Name == "memory" {
			if v.ConfigMap == nil {
				t.Fatal("memory volume should be a ConfigMap volume")
			}
			expected := run.Spec.InstanceRef + "-memory"
			if v.ConfigMap.Name != expected {
				t.Errorf("memory ConfigMap name = %q, want %q", v.ConfigMap.Name, expected)
			}
			return
		}
	}
	t.Error("memory volume not found when memoryEnabled=true")
}

func TestBuildVolumes_MemoryDisabled(t *testing.T) {
	r := &AgentRunReconciler{}
	run := newTestRun()
	vols := r.buildVolumes(run, false, nil, nil)

	for _, v := range vols {
		if v.Name == "memory" {
			t.Error("memory volume should not exist when memoryEnabled=false")
			return
		}
	}
}

func TestBuildContainers_MemoryMount(t *testing.T) {
	r := &AgentRunReconciler{}
	run := newTestRun()
	cs, _ := r.buildContainers(run, true, nil, nil, nil)

	agent := cs[0]
	var hasMount bool
	for _, vm := range agent.VolumeMounts {
		if vm.Name == "memory" && vm.MountPath == "/memory" {
			hasMount = true
			break
		}
	}
	if !hasMount {
		t.Error("agent container should have /memory volume mount when memoryEnabled=true")
	}

	var hasEnv bool
	for _, e := range agent.Env {
		if e.Name == "MEMORY_ENABLED" && e.Value == "true" {
			hasEnv = true
			break
		}
	}
	if !hasEnv {
		t.Error("agent container should have MEMORY_ENABLED=true env when memoryEnabled=true")
	}
}

// ── Skill sidecar injection tests ────────────────────────────────────────────

func TestBuildContainers_SkillSidecarInjected(t *testing.T) {
	r := &AgentRunReconciler{}
	sidecars := []resolvedSidecar{
		{
			skillPackName: "k8s-ops",
			sidecar: sympoziumv1alpha1.SkillSidecar{
				Image:          "ghcr.io/alexsjones/sympozium/skill-k8s-ops:latest",
				MountWorkspace: true,
				Resources: &sympoziumv1alpha1.SidecarResources{
					CPU:    "100m",
					Memory: "128Mi",
				},
			},
		},
	}
	cs, _ := r.buildContainers(newTestRun(), false, nil, sidecars, nil)
	// agent + ipc-bridge + skill sidecar = 3
	if len(cs) != 3 {
		t.Fatalf("container count = %d, want 3", len(cs))
	}
	sc := cs[2]
	if sc.Name != "skill-k8s-ops" {
		t.Errorf("sidecar name = %q, want skill-k8s-ops", sc.Name)
	}
	if sc.Image != "ghcr.io/alexsjones/sympozium/skill-k8s-ops:latest" {
		t.Errorf("sidecar image = %q", sc.Image)
	}
	// Should have workspace mount
	var hasWorkspace bool
	for _, m := range sc.VolumeMounts {
		if m.MountPath == "/workspace" {
			hasWorkspace = true
			break
		}
	}
	if !hasWorkspace {
		t.Error("sidecar should mount /workspace when MountWorkspace=true")
	}
}

func TestBuildContainers_SkillSidecarDefaultCommand(t *testing.T) {
	r := &AgentRunReconciler{}
	sidecars := []resolvedSidecar{
		{
			skillPackName: "test-skill",
			sidecar: sympoziumv1alpha1.SkillSidecar{
				Image:          "test:latest",
				MountWorkspace: false,
			},
		},
	}
	cs, _ := r.buildContainers(newTestRun(), false, nil, sidecars, nil)
	sc := cs[2]
	// When no command is specified in the SkillPack, the container should
	// have no Command override so the image's default CMD runs.
	if len(sc.Command) != 0 {
		t.Errorf("sidecar command = %v, want empty (use image CMD)", sc.Command)
	}
	// Agent container should always have TOOLS_ENABLED.
	var toolsEnabled bool
	for _, env := range cs[0].Env {
		if env.Name == "TOOLS_ENABLED" && env.Value == "true" {
			toolsEnabled = true
		}
	}
	if !toolsEnabled {
		t.Error("agent container should have TOOLS_ENABLED=true")
	}
	// Should NOT have workspace mount
	for _, m := range sc.VolumeMounts {
		if m.MountPath == "/workspace" {
			t.Error("sidecar should NOT mount /workspace when MountWorkspace=false")
		}
	}
}

func TestBuildContainers_MultipleSkillSidecars(t *testing.T) {
	r := &AgentRunReconciler{}
	sidecars := []resolvedSidecar{
		{skillPackName: "skill-a", sidecar: sympoziumv1alpha1.SkillSidecar{Image: "a:latest", MountWorkspace: true}},
		{skillPackName: "skill-b", sidecar: sympoziumv1alpha1.SkillSidecar{Image: "b:latest", MountWorkspace: true}},
	}
	cs, _ := r.buildContainers(newTestRun(), false, nil, sidecars, nil)
	// agent + ipc-bridge + 2 sidecars = 4
	if len(cs) != 4 {
		t.Fatalf("container count = %d, want 4", len(cs))
	}
	if cs[2].Name != "skill-skill-a" {
		t.Errorf("first sidecar name = %q", cs[2].Name)
	}
	if cs[3].Name != "skill-skill-b" {
		t.Errorf("second sidecar name = %q", cs[3].Name)
	}
}

func TestBuildJob_WithSkillSidecars(t *testing.T) {
	r := &AgentRunReconciler{}
	sidecars := []resolvedSidecar{
		{skillPackName: "k8s-ops", sidecar: sympoziumv1alpha1.SkillSidecar{Image: "k8s:latest", MountWorkspace: true}},
	}
	job := r.buildJob(newTestRun(), false, nil, sidecars, nil)
	containers := job.Spec.Template.Spec.Containers
	if len(containers) != 3 {
		t.Fatalf("job container count = %d, want 3", len(containers))
	}
}

func TestBuildContainers_ObservabilityEnv(t *testing.T) {
	r := &AgentRunReconciler{}
	run := newTestRun()
	obs := &sympoziumv1alpha1.ObservabilitySpec{
		Enabled:      true,
		OTLPEndpoint: "otel-collector.observability.svc:4317",
		OTLPProtocol: "grpc",
		ServiceName:  "sympozium",
		ResourceAttributes: map[string]string{
			"deployment.environment": "production",
		},
	}

	cs, _ := r.buildContainers(run, false, obs, nil, nil)

	agentEnv := map[string]string{}
	for _, e := range cs[0].Env {
		agentEnv[e.Name] = e.Value
	}
	if agentEnv["SYMPOZIUM_OTEL_ENABLED"] != "true" {
		t.Fatalf("SYMPOZIUM_OTEL_ENABLED not injected")
	}
	if agentEnv["SYMPOZIUM_OTEL_OTLP_ENDPOINT"] != obs.OTLPEndpoint {
		t.Fatalf("SYMPOZIUM_OTEL_OTLP_ENDPOINT = %q", agentEnv["SYMPOZIUM_OTEL_OTLP_ENDPOINT"])
	}
	if !strings.Contains(agentEnv["SYMPOZIUM_OTEL_RESOURCE_ATTRIBUTES"], "sympozium.agent_run.id=test-run") {
		t.Fatalf("missing run id in resource attributes: %q", agentEnv["SYMPOZIUM_OTEL_RESOURCE_ATTRIBUTES"])
	}
}

// ── Seccomp profile tests ────────────────────────────────────────────────────

func TestBuildContainers_PrivilegedSidecarUnconfinedSeccomp(t *testing.T) {
	r := &AgentRunReconciler{}
	sidecars := []resolvedSidecar{
		{
			skillPackName: "llmfit",
			sidecar: sympoziumv1alpha1.SkillSidecar{
				Image:          "llmfit:latest",
				MountWorkspace: true,
				HostAccess: &sympoziumv1alpha1.HostAccessSpec{
					Enabled:    true,
					Privileged: true,
				},
			},
		},
	}
	cs, _ := r.buildContainers(newTestRun(), false, nil, sidecars, nil)

	sidecar := cs[2] // agent, ipc-bridge, then skill sidecar
	if sidecar.SecurityContext == nil {
		t.Fatal("privileged sidecar security context is nil")
	}
	if sidecar.SecurityContext.SeccompProfile == nil {
		t.Fatal("privileged sidecar seccomp profile is nil")
	}
	if sidecar.SecurityContext.SeccompProfile.Type != corev1.SeccompProfileTypeUnconfined {
		t.Errorf("sidecar seccomp = %q, want Unconfined", sidecar.SecurityContext.SeccompProfile.Type)
	}
}

func TestBuildContainers_NonPrivilegedSidecarNoSeccompOverride(t *testing.T) {
	r := &AgentRunReconciler{}
	sidecars := []resolvedSidecar{
		{
			skillPackName: "basic-skill",
			sidecar: sympoziumv1alpha1.SkillSidecar{
				Image:          "basic:latest",
				MountWorkspace: true,
			},
		},
	}
	cs, _ := r.buildContainers(newTestRun(), false, nil, sidecars, nil)

	sidecar := cs[2]
	if sidecar.SecurityContext != nil {
		t.Errorf("non-privileged sidecar should have nil SecurityContext, got %+v", sidecar.SecurityContext)
	}
}

// ── Server-mode detection tests ───────────────────────────────────────────────

func TestServerModeDetection_RequiresServerSidecar(t *testing.T) {
	sidecars := []resolvedSidecar{
		{
			skillPackName: "web-endpoint",
			sidecar: sympoziumv1alpha1.SkillSidecar{
				Image:          "ghcr.io/alexsjones/sympozium/web-proxy:latest",
				RequiresServer: true,
				MountWorkspace: false,
				Ports: []sympoziumv1alpha1.SidecarPort{
					{Name: "http", ContainerPort: 8080},
				},
			},
		},
	}

	effectiveMode := "task"
	for _, sc := range sidecars {
		if sc.sidecar.RequiresServer {
			effectiveMode = "server"
			break
		}
	}

	if effectiveMode != "server" {
		t.Errorf("effective mode = %q, want server", effectiveMode)
	}
}

func TestServerModeDetection_NoRequiresServer(t *testing.T) {
	sidecars := []resolvedSidecar{
		{
			skillPackName: "k8s-ops",
			sidecar: sympoziumv1alpha1.SkillSidecar{
				Image:          "ghcr.io/alexsjones/sympozium/skill-k8s-ops:latest",
				MountWorkspace: true,
			},
		},
	}

	effectiveMode := "task"
	for _, sc := range sidecars {
		if sc.sidecar.RequiresServer {
			effectiveMode = "server"
			break
		}
	}

	if effectiveMode != "task" {
		t.Errorf("effective mode = %q, want task", effectiveMode)
	}
}

func TestServerModeDetection_MixedSidecars(t *testing.T) {
	sidecars := []resolvedSidecar{
		{
			skillPackName: "k8s-ops",
			sidecar: sympoziumv1alpha1.SkillSidecar{
				Image:          "k8s:latest",
				MountWorkspace: true,
			},
		},
		{
			skillPackName: "web-endpoint",
			sidecar: sympoziumv1alpha1.SkillSidecar{
				Image:          "web:latest",
				RequiresServer: true,
				Ports: []sympoziumv1alpha1.SidecarPort{
					{Name: "http", ContainerPort: 8080},
				},
			},
		},
	}

	effectiveMode := "task"
	for _, sc := range sidecars {
		if sc.sidecar.RequiresServer {
			effectiveMode = "server"
			break
		}
	}

	if effectiveMode != "server" {
		t.Errorf("effective mode = %q, want server (web-endpoint has RequiresServer)", effectiveMode)
	}
}

func TestServerMode_ExplicitModeField(t *testing.T) {
	run := newTestRun()
	run.Spec.Mode = "server"
	if run.Spec.Mode != "server" {
		t.Errorf("mode = %q, want server", run.Spec.Mode)
	}
}

func TestServerMode_DefaultModeIsTask(t *testing.T) {
	run := newTestRun()
	effectiveMode := run.Spec.Mode
	if effectiveMode == "" {
		effectiveMode = "task"
	}
	if effectiveMode != "task" {
		t.Errorf("default mode = %q, want task", effectiveMode)
	}
}

func TestServerMode_PhaseServing(t *testing.T) {
	if sympoziumv1alpha1.AgentRunPhaseServing != "Serving" {
		t.Errorf("AgentRunPhaseServing = %q, want Serving", sympoziumv1alpha1.AgentRunPhaseServing)
	}
}

func TestServerMode_StatusFields(t *testing.T) {
	run := newTestRun()
	run.Status.DeploymentName = "test-run-server"
	run.Status.ServiceName = "test-run-server"

	if run.Status.DeploymentName != "test-run-server" {
		t.Errorf("deploymentName = %q", run.Status.DeploymentName)
	}
	if run.Status.ServiceName != "test-run-server" {
		t.Errorf("serviceName = %q", run.Status.ServiceName)
	}
}

func TestBuildContainers_IPCBridgeSecurityContext(t *testing.T) {
	r := &AgentRunReconciler{}
	cs, _ := r.buildContainers(newTestRun(), false, nil, nil, nil)

	ipc := cs[1]
	if ipc.SecurityContext == nil {
		t.Fatal("ipc-bridge security context is nil")
	}
	if ipc.SecurityContext.ReadOnlyRootFilesystem == nil || !*ipc.SecurityContext.ReadOnlyRootFilesystem {
		t.Error("ipc-bridge ReadOnlyRootFilesystem should be true")
	}
	if ipc.SecurityContext.AllowPrivilegeEscalation == nil || *ipc.SecurityContext.AllowPrivilegeEscalation {
		t.Error("ipc-bridge AllowPrivilegeEscalation should be false")
	}
	if ipc.SecurityContext.Capabilities == nil || len(ipc.SecurityContext.Capabilities.Drop) == 0 {
		t.Error("ipc-bridge should drop ALL capabilities")
	}
}

// ── MCP bridge sidecar tests ─────────────────────────────────────────────────

func testMCPServers() []sympoziumv1alpha1.MCPServerRef {
	return []sympoziumv1alpha1.MCPServerRef{
		{
			Name:        "k8s-networking",
			URL:         "http://mcp-k8s-networking:8080",
			ToolsPrefix: "k8s_net",
			Timeout:     30,
		},
		{
			Name:        "otel-collector",
			URL:         "http://otel-collector-mcp:8080",
			ToolsPrefix: "otel",
			Timeout:     60,
			AuthSecret:  "otel-auth",
			AuthKey:     "api-key",
		},
	}
}

func TestBuildContainers_WithMCPBridge(t *testing.T) {
	r := &AgentRunReconciler{}
	mcpServers := testMCPServers()
	cs, initCs := r.buildContainers(newTestRun(), false, nil, nil, mcpServers)
	// agent + ipc-bridge + mcp-bridge = 3
	if len(cs) != 3 {
		t.Fatalf("container count = %d, want 3", len(cs))
	}
	mcpBridge := cs[2]
	if mcpBridge.Name != "mcp-bridge" {
		t.Errorf("third container name = %q, want mcp-bridge", mcpBridge.Name)
	}
	// init container: mcp-discover
	if len(initCs) != 1 {
		t.Fatalf("init container count = %d, want 1", len(initCs))
	}
	if initCs[0].Name != "mcp-discover" {
		t.Errorf("init container name = %q, want mcp-discover", initCs[0].Name)
	}
	// Verify MCP_DISCOVER_ONLY env var
	var hasDiscoverOnly bool
	for _, e := range initCs[0].Env {
		if e.Name == "MCP_DISCOVER_ONLY" && e.Value == "true" {
			hasDiscoverOnly = true
		}
	}
	if !hasDiscoverOnly {
		t.Error("init container missing MCP_DISCOVER_ONLY=true env var")
	}
}

func TestBuildContainers_MCPBridgeImage(t *testing.T) {
	r := &AgentRunReconciler{}
	cs, _ := r.buildContainers(newTestRun(), false, nil, nil, testMCPServers())
	mcpBridge := cs[2]
	if mcpBridge.Image == "" {
		t.Error("mcp-bridge image is empty")
	}
}

func TestBuildContainers_MCPBridgeEnvVars(t *testing.T) {
	r := &AgentRunReconciler{}
	cs, _ := r.buildContainers(newTestRun(), false, nil, nil, testMCPServers())
	mcpBridge := cs[2]

	envMap := map[string]string{}
	for _, e := range mcpBridge.Env {
		envMap[e.Name] = e.Value
	}
	if envMap["AGENT_RUN_ID"] != "test-run" {
		t.Errorf("AGENT_RUN_ID = %q", envMap["AGENT_RUN_ID"])
	}
	if envMap["MCP_CONFIG_PATH"] != "/config/mcp-servers.yaml" {
		t.Errorf("MCP_CONFIG_PATH = %q", envMap["MCP_CONFIG_PATH"])
	}
	if envMap["MCP_IPC_PATH"] != "/ipc/tools" {
		t.Errorf("MCP_IPC_PATH = %q", envMap["MCP_IPC_PATH"])
	}
}

func TestBuildContainers_MCPBridgeAuthSecretEnv(t *testing.T) {
	r := &AgentRunReconciler{}
	cs, _ := r.buildContainers(newTestRun(), false, nil, nil, testMCPServers())
	mcpBridge := cs[2]

	// Should have auth env var for otel-collector (has AuthSecret)
	var found bool
	for _, e := range mcpBridge.Env {
		if e.Name == "MCP_AUTH_OTEL_COLLECTOR" {
			found = true
			if e.ValueFrom == nil || e.ValueFrom.SecretKeyRef == nil {
				t.Fatal("expected SecretKeyRef for auth env")
			}
			if e.ValueFrom.SecretKeyRef.Name != "otel-auth" {
				t.Errorf("secret name = %q, want otel-auth", e.ValueFrom.SecretKeyRef.Name)
			}
			if e.ValueFrom.SecretKeyRef.Key != "api-key" {
				t.Errorf("secret key = %q, want api-key", e.ValueFrom.SecretKeyRef.Key)
			}
			break
		}
	}
	if !found {
		t.Error("missing MCP_AUTH_OTEL_COLLECTOR env var")
	}
}

func TestBuildContainers_MCPBridgeSecurityContext(t *testing.T) {
	r := &AgentRunReconciler{}
	cs, _ := r.buildContainers(newTestRun(), false, nil, nil, testMCPServers())
	mcpBridge := cs[2]

	sc := mcpBridge.SecurityContext
	if sc == nil {
		t.Fatal("mcp-bridge security context is nil")
	}
	if sc.ReadOnlyRootFilesystem == nil || !*sc.ReadOnlyRootFilesystem {
		t.Error("ReadOnlyRootFilesystem should be true")
	}
	if sc.AllowPrivilegeEscalation == nil || *sc.AllowPrivilegeEscalation {
		t.Error("AllowPrivilegeEscalation should be false")
	}
	if sc.Capabilities == nil || len(sc.Capabilities.Drop) == 0 {
		t.Error("should drop ALL capabilities")
	}
}

func TestBuildContainers_MCPBridgeVolumeMounts(t *testing.T) {
	r := &AgentRunReconciler{}
	cs, _ := r.buildContainers(newTestRun(), false, nil, nil, testMCPServers())
	mcpBridge := cs[2]

	mounts := map[string]string{}
	for _, m := range mcpBridge.VolumeMounts {
		mounts[m.Name] = m.MountPath
	}
	if mounts["ipc"] != "/ipc" {
		t.Error("missing /ipc volume mount")
	}
	if mounts["mcp-config"] != "/config" {
		t.Error("missing /config volume mount")
	}
}

func TestBuildContainers_MCPBridgeResources(t *testing.T) {
	r := &AgentRunReconciler{}
	cs, _ := r.buildContainers(newTestRun(), false, nil, nil, testMCPServers())
	mcpBridge := cs[2]

	req := mcpBridge.Resources.Requests
	if req.Cpu().Cmp(resource.MustParse("100m")) != 0 {
		t.Errorf("cpu request = %v", req.Cpu())
	}
	if req.Memory().Cmp(resource.MustParse("128Mi")) != 0 {
		t.Errorf("memory request = %v", req.Memory())
	}
}

func TestBuildContainers_NoMCPBridgeWhenNoServers(t *testing.T) {
	r := &AgentRunReconciler{}
	cs, _ := r.buildContainers(newTestRun(), false, nil, nil, nil)
	// agent + ipc-bridge = 2
	if len(cs) != 2 {
		t.Fatalf("container count = %d, want 2 (no MCP servers)", len(cs))
	}
}

func TestBuildContainers_MCPBridgeWithOTel(t *testing.T) {
	r := &AgentRunReconciler{}
	run := newTestRun()
	run.Annotations = map[string]string{"otel.dev/traceparent": "00-abc-def-01"}
	obs := &sympoziumv1alpha1.ObservabilitySpec{
		Enabled:      true,
		OTLPEndpoint: "otel-collector:4317",
	}
	cs, _ := r.buildContainers(run, false, obs, nil, testMCPServers())
	mcpBridge := cs[2]

	envMap := map[string]string{}
	for _, e := range mcpBridge.Env {
		envMap[e.Name] = e.Value
	}
	if envMap["TRACEPARENT"] != "00-abc-def-01" {
		t.Errorf("TRACEPARENT = %q", envMap["TRACEPARENT"])
	}
	if envMap["SYMPOZIUM_OTEL_ENABLED"] != "true" {
		t.Error("missing SYMPOZIUM_OTEL_ENABLED")
	}
}

func TestBuildVolumes_MCPConfig(t *testing.T) {
	r := &AgentRunReconciler{}
	run := newTestRun()
	vols := r.buildVolumes(run, false, nil, testMCPServers())

	for _, v := range vols {
		if v.Name == "mcp-config" {
			if v.ConfigMap == nil {
				t.Fatal("mcp-config volume should be a ConfigMap volume")
			}
			expected := run.Name + "-mcp-servers"
			if v.ConfigMap.Name != expected {
				t.Errorf("mcp-config ConfigMap name = %q, want %q", v.ConfigMap.Name, expected)
			}
			return
		}
	}
	t.Error("mcp-config volume not found")
}

func TestBuildVolumes_NoMCPConfigWhenNoServers(t *testing.T) {
	r := &AgentRunReconciler{}
	vols := r.buildVolumes(newTestRun(), false, nil, nil)

	for _, v := range vols {
		if v.Name == "mcp-config" {
			t.Error("mcp-config volume should not exist when no MCP servers")
			return
		}
	}
}

func TestBuildMCPServersYAML(t *testing.T) {
	servers := testMCPServers()
	y, err := buildMCPServersYAML(servers)
	if err != nil {
		t.Fatalf("buildMCPServersYAML failed: %v", err)
	}

	if !strings.Contains(y, "name: k8s-networking") {
		t.Error("missing k8s-networking server name")
	}
	if !strings.Contains(y, "url: http://mcp-k8s-networking:8080") {
		t.Error("missing k8s-networking URL")
	}
	if !strings.Contains(y, "toolsPrefix: k8s_net") {
		t.Error("missing k8s_net prefix")
	}
	if !strings.Contains(y, "tokenEnv: MCP_AUTH_OTEL_COLLECTOR") {
		t.Error("missing auth env var reference for otel-collector")
	}
}

func TestBuildMCPServersYAML_DefaultTimeout(t *testing.T) {
	servers := []sympoziumv1alpha1.MCPServerRef{
		{Name: "test", URL: "http://test:8080", ToolsPrefix: "t"},
	}
	y, err := buildMCPServersYAML(servers)
	if err != nil {
		t.Fatalf("buildMCPServersYAML failed: %v", err)
	}
	if !strings.Contains(y, "timeout: 30") {
		t.Error("expected default timeout of 30")
	}
}

func TestBuildMCPServersYAML_WithHeaders(t *testing.T) {
	servers := []sympoziumv1alpha1.MCPServerRef{
		{
			Name:        "test",
			URL:         "http://test:8080",
			ToolsPrefix: "t",
			Headers:     map[string]string{"X-Custom": "value"},
		},
	}
	y, err := buildMCPServersYAML(servers)
	if err != nil {
		t.Fatalf("buildMCPServersYAML failed: %v", err)
	}
	if !strings.Contains(y, "X-Custom: value") {
		t.Error("missing custom header in YAML")
	}
}

func TestBuildMCPServersYAML_InjectionSafe(t *testing.T) {
	servers := []sympoziumv1alpha1.MCPServerRef{
		{
			Name:        "evil\n  injected: true",
			URL:         "http://test:8080",
			ToolsPrefix: "t",
		},
	}
	y, err := buildMCPServersYAML(servers)
	if err != nil {
		t.Fatalf("buildMCPServersYAML failed: %v", err)
	}
	// Proper YAML serializer uses block scalar (|-) to safely encode
	// the newline inside the name value, so "injected: true" stays
	// as part of the name string, not a separate YAML key.
	if !strings.Contains(y, "|-") {
		t.Errorf("YAML serializer should use block scalar for multi-line value: %s", y)
	}
}

func TestBuildJob_WithMCPServers(t *testing.T) {
	r := &AgentRunReconciler{}
	job := r.buildJob(newTestRun(), false, nil, nil, testMCPServers())
	containers := job.Spec.Template.Spec.Containers
	// agent + ipc-bridge + mcp-bridge = 3
	if len(containers) != 3 {
		t.Fatalf("job container count = %d, want 3", len(containers))
	}
	if containers[2].Name != "mcp-bridge" {
		t.Errorf("third container = %q, want mcp-bridge", containers[2].Name)
	}

	// Should have mcp-config volume
	var hasMCPVol bool
	for _, v := range job.Spec.Template.Spec.Volumes {
		if v.Name == "mcp-config" {
			hasMCPVol = true
			break
		}
	}
	if !hasMCPVol {
		t.Error("job should have mcp-config volume")
	}
}
