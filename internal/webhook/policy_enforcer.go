// Package webhook provides validating and mutating admission webhooks for Sympozium.
// These enforce SympoziumPolicy constraints on AgentRun resources.
package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-logr/logr"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	sympoziumv1alpha1 "github.com/sympozium-ai/sympozium/api/v1alpha1"
)

// PolicyEnforcer is a validating webhook that enforces SympoziumPolicy on AgentRuns.
type PolicyEnforcer struct {
	Client  client.Client
	Log     logr.Logger
	Decoder admission.Decoder
}

// Handle validates AgentRun creation/updates against the bound SympoziumPolicy.
func (pe *PolicyEnforcer) Handle(ctx context.Context, req admission.Request) admission.Response {
	run := &sympoziumv1alpha1.AgentRun{}
	if err := pe.Decoder.Decode(req, run); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	// Skip validation for runs that are being deleted. Otherwise the
	// controller's own finalizer-removal Update gets rejected when the
	// referenced Instance has already been deleted (e.g. PersonaPack
	// disable cascade), leaving the AgentRun stuck in a terminating
	// state forever with no way for kubelet GC to finish.
	if !run.DeletionTimestamp.IsZero() {
		return admission.Allowed("run is being deleted; skipping policy validation")
	}

	// Look up the owning SympoziumInstance
	var instance sympoziumv1alpha1.SympoziumInstance
	if err := pe.Client.Get(ctx, types.NamespacedName{
		Name:      run.Spec.InstanceRef,
		Namespace: run.Namespace,
	}, &instance); err != nil {
		return admission.Errored(http.StatusBadRequest,
			fmt.Errorf("failed to find SympoziumInstance %s: %w", run.Spec.InstanceRef, err))
	}

	// If no policy is bound, allow
	if instance.Spec.PolicyRef == "" {
		return admission.Allowed("no policy bound")
	}

	// Look up the SympoziumPolicy
	var policy sympoziumv1alpha1.SympoziumPolicy
	if err := pe.Client.Get(ctx, types.NamespacedName{
		Name:      instance.Spec.PolicyRef,
		Namespace: run.Namespace,
	}, &policy); err != nil {
		return admission.Errored(http.StatusInternalServerError,
			fmt.Errorf("failed to find SympoziumPolicy %s: %w", instance.Spec.PolicyRef, err))
	}

	// Validate sandbox policy
	if policy.Spec.SandboxPolicy != nil && policy.Spec.SandboxPolicy.Required {
		if run.Spec.Sandbox == nil || !run.Spec.Sandbox.Enabled {
			return admission.Denied("sandbox is required by policy")
		}
	}

	// Validate resource limits
	if err := pe.validateResources(run, &policy); err != nil {
		return admission.Denied(err.Error())
	}

	// Validate sub-agent depth
	if err := pe.validateSubagentDepth(run, &policy); err != nil {
		return admission.Denied(err.Error())
	}

	// Validate tool policy
	if err := pe.validateToolPolicy(run, &policy); err != nil {
		return admission.Denied(err.Error())
	}

	// Validate feature gates
	if err := pe.validateFeatureGates(run, &policy); err != nil {
		return admission.Denied(err.Error())
	}

	// Validate agent-sandbox policy
	if err := pe.validateAgentSandbox(run, &policy); err != nil {
		return admission.Denied(err.Error())
	}

	return admission.Allowed("policy validated")
}

func (pe *PolicyEnforcer) validateResources(run *sympoziumv1alpha1.AgentRun, policy *sympoziumv1alpha1.SympoziumPolicy) error {
	if policy.Spec.SandboxPolicy == nil || run.Spec.Sandbox == nil {
		return nil
	}

	if policy.Spec.SandboxPolicy.MaxCPU != "" {
		maxCPU := resource.MustParse(policy.Spec.SandboxPolicy.MaxCPU)
		_ = maxCPU // Would compare against run's resource requests
	}

	if policy.Spec.SandboxPolicy.MaxMemory != "" {
		maxMem := resource.MustParse(policy.Spec.SandboxPolicy.MaxMemory)
		_ = maxMem
	}

	return nil
}

func (pe *PolicyEnforcer) validateSubagentDepth(run *sympoziumv1alpha1.AgentRun, policy *sympoziumv1alpha1.SympoziumPolicy) error {
	if policy.Spec.SubagentPolicy == nil || run.Spec.Parent == nil {
		return nil
	}

	if policy.Spec.SubagentPolicy.MaxDepth > 0 && run.Spec.Parent.SpawnDepth >= policy.Spec.SubagentPolicy.MaxDepth {
		return fmt.Errorf("sub-agent depth %d exceeds maximum %d",
			run.Spec.Parent.SpawnDepth, policy.Spec.SubagentPolicy.MaxDepth)
	}

	return nil
}

func (pe *PolicyEnforcer) validateToolPolicy(run *sympoziumv1alpha1.AgentRun, policy *sympoziumv1alpha1.SympoziumPolicy) error {
	if run.Spec.ToolPolicy == nil || policy.Spec.ToolGating == nil {
		return nil
	}

	// Check that allowed tools in the run spec don't conflict with policy denied tools
	for _, rule := range policy.Spec.ToolGating.Rules {
		if rule.Action == "deny" {
			for _, allowed := range run.Spec.ToolPolicy.Allow {
				if allowed == rule.Tool {
					return fmt.Errorf("tool %q is denied by policy", rule.Tool)
				}
			}
		}
	}

	return nil
}

func (pe *PolicyEnforcer) validateAgentSandbox(run *sympoziumv1alpha1.AgentRun, policy *sympoziumv1alpha1.SympoziumPolicy) error {
	agentSandboxEnabled := run.Spec.AgentSandbox != nil && run.Spec.AgentSandbox.Enabled
	sidecarSandboxEnabled := run.Spec.Sandbox != nil && run.Spec.Sandbox.Enabled

	// Mutual exclusivity: cannot use both sandbox modes.
	if agentSandboxEnabled && sidecarSandboxEnabled {
		return fmt.Errorf("sandbox.enabled and agentSandbox.enabled are mutually exclusive")
	}

	// Agent Sandbox + server mode not yet supported.
	if agentSandboxEnabled && run.Spec.Mode == "server" {
		return fmt.Errorf("agentSandbox is not supported with mode=server")
	}

	// Policy enforcement: agent-sandbox required.
	if policy.Spec.SandboxPolicy != nil &&
		policy.Spec.SandboxPolicy.AgentSandboxPolicy != nil &&
		policy.Spec.SandboxPolicy.AgentSandboxPolicy.Required {
		if !agentSandboxEnabled {
			return fmt.Errorf("agent-sandbox mode is required by policy")
		}
	}

	// Validate runtime class against allowed list.
	if agentSandboxEnabled &&
		policy.Spec.SandboxPolicy != nil &&
		policy.Spec.SandboxPolicy.AgentSandboxPolicy != nil {
		asp := policy.Spec.SandboxPolicy.AgentSandboxPolicy
		if len(asp.AllowedRuntimeClasses) > 0 && run.Spec.AgentSandbox.RuntimeClass != "" {
			allowed := false
			for _, rc := range asp.AllowedRuntimeClasses {
				if rc == run.Spec.AgentSandbox.RuntimeClass {
					allowed = true
					break
				}
			}
			if !allowed {
				return fmt.Errorf("runtime class %q is not allowed by policy (allowed: %v)",
					run.Spec.AgentSandbox.RuntimeClass, asp.AllowedRuntimeClasses)
			}
		}
	}

	return nil
}

func (pe *PolicyEnforcer) validateFeatureGates(run *sympoziumv1alpha1.AgentRun, policy *sympoziumv1alpha1.SympoziumPolicy) error {
	if policy.Spec.FeatureGates == nil {
		return nil
	}

	// Check sandbox feature gate
	if run.Spec.Sandbox != nil && run.Spec.Sandbox.Enabled {
		if enabled, exists := policy.Spec.FeatureGates["code-execution"]; exists && !enabled {
			return fmt.Errorf("feature gate 'code-execution' is disabled by policy")
		}
	}

	// Check sub-agents feature gate
	if run.Spec.Parent != nil {
		if enabled, exists := policy.Spec.FeatureGates["sub-agents"]; exists && !enabled {
			return fmt.Errorf("feature gate 'sub-agents' is disabled by policy")
		}
	}

	return nil
}

// MutatingPolicyEnforcer is a mutating webhook that injects defaults based on SympoziumPolicy.
type MutatingPolicyEnforcer struct {
	Client  client.Client
	Log     logr.Logger
	Decoder admission.Decoder
}

// Handle mutates AgentRun resources to enforce policy defaults.
func (mpe *MutatingPolicyEnforcer) Handle(ctx context.Context, req admission.Request) admission.Response {
	run := &sympoziumv1alpha1.AgentRun{}
	if err := mpe.Decoder.Decode(req, run); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	// Look up the owning SympoziumInstance
	var instance sympoziumv1alpha1.SympoziumInstance
	if err := mpe.Client.Get(ctx, types.NamespacedName{
		Name:      run.Spec.InstanceRef,
		Namespace: run.Namespace,
	}, &instance); err != nil {
		return admission.Allowed("instance not found, skipping mutation")
	}

	if instance.Spec.PolicyRef == "" {
		return admission.Allowed("no policy")
	}

	var policy sympoziumv1alpha1.SympoziumPolicy
	if err := mpe.Client.Get(ctx, types.NamespacedName{
		Name:      instance.Spec.PolicyRef,
		Namespace: run.Namespace,
	}, &policy); err != nil {
		return admission.Allowed("policy not found, skipping mutation")
	}

	modified := false

	// Inject sandbox defaults
	if policy.Spec.SandboxPolicy != nil && policy.Spec.SandboxPolicy.Required {
		if run.Spec.Sandbox == nil {
			run.Spec.Sandbox = &sympoziumv1alpha1.AgentRunSandboxSpec{
				Enabled: true,
			}
			modified = true
		}
		if policy.Spec.SandboxPolicy.DefaultImage != "" && run.Spec.Sandbox.Image == "" {
			run.Spec.Sandbox.Image = policy.Spec.SandboxPolicy.DefaultImage
			modified = true
		}
	}

	// Inject seccomp profile default from policy
	if policy.Spec.SandboxPolicy != nil && policy.Spec.SandboxPolicy.SeccompProfile != nil {
		if run.Spec.Sandbox == nil {
			run.Spec.Sandbox = &sympoziumv1alpha1.AgentRunSandboxSpec{}
			modified = true
		}
		if run.Spec.Sandbox.SecurityContext == nil {
			run.Spec.Sandbox.SecurityContext = &sympoziumv1alpha1.SandboxSecurityContext{}
			modified = true
		}
		if run.Spec.Sandbox.SecurityContext.SeccompProfile == nil {
			run.Spec.Sandbox.SecurityContext.SeccompProfile = &sympoziumv1alpha1.SeccompProfileSpec{
				Type: policy.Spec.SandboxPolicy.SeccompProfile.Type,
			}
			modified = true
		}
	}

	// Inject tool policy defaults from SympoziumPolicy
	if policy.Spec.ToolGating != nil && run.Spec.ToolPolicy == nil {
		tp := &sympoziumv1alpha1.ToolPolicySpec{}
		for _, rule := range policy.Spec.ToolGating.Rules {
			switch rule.Action {
			case "allow":
				tp.Allow = append(tp.Allow, rule.Tool)
			case "deny":
				tp.Deny = append(tp.Deny, rule.Tool)
			}
		}
		run.Spec.ToolPolicy = tp
		modified = true
	}

	// Inject network isolation labels (used by NetworkPolicy)
	if run.Labels == nil {
		run.Labels = make(map[string]string)
	}
	if _, exists := run.Labels["sympozium.ai/role"]; !exists {
		run.Labels["sympozium.ai/role"] = "agent"
		modified = true
	}
	if run.Spec.Sandbox != nil && run.Spec.Sandbox.Enabled {
		run.Labels["sympozium.ai/sandbox"] = "true"
		modified = true
	}

	// Inject agent-sandbox defaults from policy.
	if policy.Spec.SandboxPolicy != nil && policy.Spec.SandboxPolicy.AgentSandboxPolicy != nil {
		asp := policy.Spec.SandboxPolicy.AgentSandboxPolicy

		// If policy requires agent-sandbox and it's not set, inject it.
		if asp.Required && (run.Spec.AgentSandbox == nil || !run.Spec.AgentSandbox.Enabled) {
			run.Spec.AgentSandbox = &sympoziumv1alpha1.AgentSandboxSpec{
				Enabled: true,
			}
			modified = true
		}

		// Inject default runtime class.
		if run.Spec.AgentSandbox != nil && run.Spec.AgentSandbox.Enabled {
			if asp.DefaultRuntimeClass != "" && run.Spec.AgentSandbox.RuntimeClass == "" {
				run.Spec.AgentSandbox.RuntimeClass = asp.DefaultRuntimeClass
				modified = true
			}
			run.Labels["sympozium.ai/agent-sandbox"] = "true"
			modified = true
		}
	}

	// Disable service account token automount via annotation
	if run.Annotations == nil {
		run.Annotations = make(map[string]string)
	}
	if _, exists := run.Annotations["sympozium.ai/disable-sa-token"]; !exists {
		run.Annotations["sympozium.ai/disable-sa-token"] = "true"
		modified = true
	}

	if !modified {
		return admission.Allowed("no mutations needed")
	}

	// Create the JSON patch
	marshaledRun, err := json.Marshal(run)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledRun)
}

// BuildAgentPodSecurityContext returns a restricted SecurityContext for agent pods.
func BuildAgentPodSecurityContext() *corev1.SecurityContext {
	falseBool := false
	return &corev1.SecurityContext{
		AllowPrivilegeEscalation: &falseBool,
		ReadOnlyRootFilesystem:   &falseBool,
		RunAsNonRoot:             boolPtr(true),
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"},
		},
	}
}

func boolPtr(b bool) *bool {
	return &b
}
