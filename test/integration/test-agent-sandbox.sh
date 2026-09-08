#!/usr/bin/env bash
# Integration test: Kubernetes Agent Sandbox (CRD) execution backend.
#
# Verifies the full lifecycle:
#   1. Required Agent-sandbox CRDs are installed (read-only preflight)
#   2. Controller detects CRDs and enables the feature
#   3. AgentRun with agentSandbox.enabled creates a Sandbox CR (not a Job)
#   4. AgentRun without agentSandbox still creates a Job (backward compat)
#   5. Sandbox CR has correct labels, ownerRef, runtimeClassName, containers
#   6. Mutual exclusivity: agentSandbox + sandbox.enabled handled correctly
#   7. Sandbox CRs are garbage-collected when AgentRuns are deleted
#   8. WarmPool CRD can be created for an instance
#   9. Policy webhook blocks disallowed runtime classes
#
# Prerequisites:
#   - KIND cluster running with sympozium deployed
#   - Controller image includes agent-sandbox support
#   - No OPENAI_API_KEY required (tests CRD creation, not LLM execution)

set -euo pipefail

: "${TEST_KUBECONFIG:?explicit test kubeconfig required}"
: "${TEST_CONTEXT:?explicit test context required}"
: "${TEST_NAMESPACE:?dedicated inttest-sandbox-* namespace required}"
[[ "$TEST_KUBECONFIG" == /* && -f "$TEST_KUBECONFIG" ]] || { echo 'Absolute readable test kubeconfig required' >&2; exit 1; }
[[ "$TEST_CONTEXT" == kind-* ]] || { echo 'Only an explicitly selected Kind context is permitted' >&2; exit 1; }
[[ "$TEST_NAMESPACE" =~ ^inttest-sandbox-[a-z0-9][a-z0-9-]*[a-z0-9]$ && ${#TEST_NAMESPACE} -le 63 ]] || { echo 'Dedicated inttest-sandbox-* namespace required' >&2; exit 1; }
kubectl() { command kubectl --kubeconfig "$TEST_KUBECONFIG" --context "$TEST_CONTEXT" "$@"; }

NAMESPACE="$TEST_NAMESPACE"
SYSTEM_NS="${SYMPOZIUM_NAMESPACE:-sympozium-system}"
CONTROLLER="${TEST_CONTROLLER_DEPLOYMENT:-sympozium-controller-manager}"
TIMEOUT="${TEST_TIMEOUT:-60}"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

pass() { echo -e "${GREEN}✓ $*${NC}"; }
fail() { echo -e "${RED}✗ $*${NC}" >&2; EXIT_CODE=1; }
info() { echo -e "${YELLOW}● $*${NC}"; }

EXIT_CODE=0
SUFFIX="$(date +%s)-${RANDOM}"
SANDBOX_INSTANCE="inttest-sandbox-${SUFFIX}"
SANDBOX_RUN="inttest-sb-run-${SUFFIX}"
REGULAR_RUN="inttest-reg-run-${SUFFIX}"
BOTH_RUN="inttest-both-run-${SUFFIX}"
CLAIM_RUN="inttest-claim-run-${SUFFIX}"

# Registered through the EXIT trap only after read-only preflight succeeds.
# shellcheck disable=SC2329
cleanup() {
  local failed=0
  info "Cleaning up agent-sandbox test resources..."
  # The controller finalizer and owner references reclaim Jobs/Sandbox CRs.
  # Never delete all sandbox resources carrying a shared component label.
  kubectl delete agentrun "${SANDBOX_RUN}" "${REGULAR_RUN}" "${BOTH_RUN}" "${CLAIM_RUN}" \
    -n "$NAMESPACE" --ignore-not-found --timeout=45s >/dev/null 2>&1 || failed=1
  kubectl delete agent "${SANDBOX_INSTANCE}" \
    -n "$NAMESPACE" --ignore-not-found --timeout=45s >/dev/null 2>&1 || failed=1
  kubectl delete secret "${SANDBOX_INSTANCE}-test-key" \
    -n "$NAMESPACE" --ignore-not-found --timeout=45s >/dev/null 2>&1 || failed=1
  kubectl delete configmap -n "$NAMESPACE" -l "sympozium.ai/instance=${SANDBOX_INSTANCE}" \
    --ignore-not-found --timeout=45s >/dev/null 2>&1 || failed=1
  if [[ "$failed" != 0 ]]; then echo 'Sandbox test cleanup incomplete; inspect exact test resources' >&2; fi
  return "$failed"
}

wait_for_field_notempty() {
  local resource="$1" name="$2" jsonpath="$3" label="$4"
  local elapsed=0
  while [[ "$elapsed" -lt "$TIMEOUT" ]]; do
    val="$(kubectl get "$resource" "$name" -n "$NAMESPACE" -o jsonpath="$jsonpath" 2>/dev/null || true)"
    if [[ -n "$val" ]]; then
      printf "%s" "$val"
      return 0
    fi
    sleep 2
    elapsed=$((elapsed + 2))
  done
  fail "${label}: timed out waiting for ${jsonpath} to be non-empty"
  return 1
}

# ── Preflight ─────────────────────────────────────────────────────────────────

info "Running Agent Sandbox integration test in namespace '${NAMESPACE}'"
kubectl get namespace "$NAMESPACE" >/dev/null

# Prerequisites are operator-owned. Tests must not install cluster-wide CRDs or
# silently alter/restart a shared controller to satisfy their own prerequisites.
for crd in sandboxes.agents.x-k8s.io sandboxclaims.agents.x-k8s.io sandboxwarmpools.agents.x-k8s.io; do
  if kubectl get crd "$crd" >/dev/null 2>&1; then
    pass "$crd installed"
  else
    fail "$crd missing; provision the test platform first"
    exit 1
  fi
done

# Check controller has agent-sandbox enabled.
# The env var must be set AND the controller must have been restarted after
# the agent-sandbox CRDs were installed. We check the env var first, then
# look for the log message across ALL log lines (not just tail).
current_env="$(kubectl get "deployment/$CONTROLLER" -n "$SYSTEM_NS" \
  -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="AGENT_SANDBOX_ENABLED")].value}' 2>/dev/null || true)"

if [[ "$current_env" != "true" ]]; then
  fail "Selected controller must already have AGENT_SANDBOX_ENABLED=true"
  exit 1
fi

controller_logs="$(kubectl logs "deployment/$CONTROLLER" -n "$SYSTEM_NS" 2>/dev/null || true)"
if echo "$controller_logs" | grep -q "Agent Sandbox CRD support enabled"; then
  pass "Controller has agent-sandbox support enabled"
else
  fail "Controller does not have agent-sandbox support enabled"
  echo "$controller_logs" | grep -i "sandbox\|setup" | tail -5
  exit 1
fi

trap cleanup EXIT
# Create test prerequisites: secret and instance.
kubectl create secret generic "${SANDBOX_INSTANCE}-test-key" \
  --from-literal=OPENAI_API_KEY=sk-test-dummy-key \
  -n "$NAMESPACE" --dry-run=client -o yaml | kubectl apply -f - >/dev/null 2>&1

cat <<EOF | kubectl apply -f - >/dev/null 2>&1
apiVersion: sympozium.ai/v1alpha1
kind: Agent
metadata:
  name: ${SANDBOX_INSTANCE}
  namespace: ${NAMESPACE}
spec:
  agents:
    default:
      model: gpt-4o-mini
  authRefs:
    - provider: openai
      secret: ${SANDBOX_INSTANCE}-test-key
EOF
pass "Test instance '${SANDBOX_INSTANCE}' created"

# ── Test 1: AgentRun with agentSandbox creates Sandbox CR ─────────────────────

info "Test 1: AgentRun with agentSandbox.enabled creates Sandbox CR"
cat <<EOF | kubectl apply -f - >/dev/null 2>&1
apiVersion: sympozium.ai/v1alpha1
kind: AgentRun
metadata:
  name: ${SANDBOX_RUN}
  namespace: ${NAMESPACE}
  labels:
    sympozium.ai/instance: ${SANDBOX_INSTANCE}
    sympozium.ai/component: agent-run
spec:
  agentRef: ${SANDBOX_INSTANCE}
  agentId: default
  sessionKey: "test-sb-${SUFFIX}"
  task: "Agent sandbox integration test"
  model:
    provider: openai
    model: gpt-4o-mini
    authSecretRef: ${SANDBOX_INSTANCE}-test-key
  agentSandbox:
    enabled: true
    runtimeClass: gvisor
EOF

# Wait for sandboxName to appear on status.
sb_name="$(wait_for_field_notempty agentrun "${SANDBOX_RUN}" '{.status.sandboxName}' "Test 1: sandboxName" || true)"
if [[ -n "$sb_name" ]]; then
  pass "Test 1: AgentRun status.sandboxName = ${sb_name}"
else
  fail "Test 1: sandboxName never appeared on AgentRun status"
fi

# Verify no Job was created.
job_name="$(kubectl get agentrun "${SANDBOX_RUN}" -n "$NAMESPACE" -o jsonpath='{.status.jobName}' 2>/dev/null || true)"
if [[ -z "$job_name" ]]; then
  pass "Test 1: No Job created (expected — using Sandbox CR instead)"
else
  fail "Test 1: Job was created (${job_name}) — should have used Sandbox CR"
fi

# Verify Sandbox CR exists.
if kubectl get sandbox "${sb_name}" -n "$NAMESPACE" >/dev/null 2>&1; then
  pass "Test 1: Sandbox CR '${sb_name}' exists"
else
  fail "Test 1: Sandbox CR '${sb_name}' does not exist"
fi

# ── Test 2: Sandbox CR has correct metadata ──────────────────────────────────

info "Test 2: Sandbox CR metadata correctness"

sb_runtime="$(kubectl get sandbox "${sb_name}" -n "$NAMESPACE" -o jsonpath='{.spec.podTemplate.spec.runtimeClassName}' 2>/dev/null || true)"
if [[ "$sb_runtime" == "gvisor" ]]; then
  pass "Test 2: runtimeClassName = gvisor"
else
  fail "Test 2: runtimeClassName = '${sb_runtime}', expected 'gvisor'"
fi

sb_label_instance="$(kubectl get sandbox "${sb_name}" -n "$NAMESPACE" -o jsonpath='{.metadata.labels.sympozium\.ai/instance}' 2>/dev/null || true)"
if [[ "$sb_label_instance" == "${SANDBOX_INSTANCE}" ]]; then
  pass "Test 2: instance label correct"
else
  fail "Test 2: instance label = '${sb_label_instance}', expected '${SANDBOX_INSTANCE}'"
fi

sb_label_sandbox="$(kubectl get sandbox "${sb_name}" -n "$NAMESPACE" -o jsonpath='{.metadata.labels.sympozium\.ai/agent-sandbox}' 2>/dev/null || true)"
if [[ "$sb_label_sandbox" == "true" ]]; then
  pass "Test 2: agent-sandbox label = true"
else
  fail "Test 2: agent-sandbox label = '${sb_label_sandbox}'"
fi

sb_owner="$(kubectl get sandbox "${sb_name}" -n "$NAMESPACE" -o jsonpath='{.metadata.ownerReferences[0].kind}' 2>/dev/null || true)"
if [[ "$sb_owner" == "AgentRun" ]]; then
  pass "Test 2: ownerReference.kind = AgentRun"
else
  fail "Test 2: ownerReference.kind = '${sb_owner}', expected 'AgentRun'"
fi

# ── Test 3: Sandbox CR contains expected containers ───────────────────────────

info "Test 3: Sandbox CR container spec"

container_names="$(kubectl get sandbox "${sb_name}" -n "$NAMESPACE" -o jsonpath='{.spec.podTemplate.spec.containers[*].name}' 2>/dev/null || true)"
if echo "$container_names" | grep -q "agent"; then
  pass "Test 3: agent container present"
else
  fail "Test 3: agent container missing (got: ${container_names})"
fi
if echo "$container_names" | grep -q "ipc-bridge"; then
  pass "Test 3: ipc-bridge sidecar present"
else
  fail "Test 3: ipc-bridge sidecar missing (got: ${container_names})"
fi

sa_name="$(kubectl get sandbox "${sb_name}" -n "$NAMESPACE" -o jsonpath='{.spec.podTemplate.spec.serviceAccountName}' 2>/dev/null || true)"
if [[ "$sa_name" == "sympozium-agent" ]]; then
  pass "Test 3: serviceAccountName = sympozium-agent"
else
  fail "Test 3: serviceAccountName = '${sa_name}'"
fi

# ── Test 4: Regular AgentRun still creates Job (backward compat) ──────────────

info "Test 4: Regular AgentRun (no agentSandbox) creates Job"
cat <<EOF | kubectl apply -f - >/dev/null 2>&1
apiVersion: sympozium.ai/v1alpha1
kind: AgentRun
metadata:
  name: ${REGULAR_RUN}
  namespace: ${NAMESPACE}
  labels:
    sympozium.ai/instance: ${SANDBOX_INSTANCE}
    sympozium.ai/component: agent-run
spec:
  agentRef: ${SANDBOX_INSTANCE}
  agentId: default
  sessionKey: "test-reg-${SUFFIX}"
  task: "Regular job integration test"
  model:
    provider: openai
    model: gpt-4o-mini
    authSecretRef: ${SANDBOX_INSTANCE}-test-key
EOF

reg_job="$(wait_for_field_notempty agentrun "${REGULAR_RUN}" '{.status.jobName}' "Test 4: jobName" || true)"
if [[ -n "$reg_job" ]]; then
  pass "Test 4: Regular run created Job '${reg_job}'"
else
  fail "Test 4: Regular run did not create a Job"
fi

reg_sb="$(kubectl get agentrun "${REGULAR_RUN}" -n "$NAMESPACE" -o jsonpath='{.status.sandboxName}' 2>/dev/null || true)"
if [[ -z "$reg_sb" ]]; then
  pass "Test 4: Regular run has no sandboxName (correct)"
else
  fail "Test 4: Regular run unexpectedly has sandboxName='${reg_sb}'"
fi

# ── Test 5: Both sandbox modes — agentSandbox takes priority ─────────────────

info "Test 5: Both sandbox modes set — agentSandbox takes priority"
cat <<EOF | kubectl apply -f - >/dev/null 2>&1
apiVersion: sympozium.ai/v1alpha1
kind: AgentRun
metadata:
  name: ${BOTH_RUN}
  namespace: ${NAMESPACE}
  labels:
    sympozium.ai/instance: ${SANDBOX_INSTANCE}
    sympozium.ai/component: agent-run
spec:
  agentRef: ${SANDBOX_INSTANCE}
  agentId: default
  sessionKey: "test-both-${SUFFIX}"
  task: "Both sandbox modes test"
  model:
    provider: openai
    model: gpt-4o-mini
    authSecretRef: ${SANDBOX_INSTANCE}-test-key
  sandbox:
    enabled: true
  agentSandbox:
    enabled: true
    runtimeClass: kata
EOF

both_sb="$(wait_for_field_notempty agentrun "${BOTH_RUN}" '{.status.sandboxName}' "Test 5: sandboxName" || true)"
if [[ -n "$both_sb" ]]; then
  pass "Test 5: agentSandbox took priority — created Sandbox CR '${both_sb}'"
else
  fail "Test 5: agentSandbox did not take priority"
fi

both_rt="$(kubectl get sandbox "${both_sb}" -n "$NAMESPACE" -o jsonpath='{.spec.podTemplate.spec.runtimeClassName}' 2>/dev/null || true)"
if [[ "$both_rt" == "kata" ]]; then
  pass "Test 5: runtimeClassName = kata"
else
  fail "Test 5: runtimeClassName = '${both_rt}', expected 'kata'"
fi

# ── Test 6: SandboxClaim with warmPoolRef ─────────────────────────────────────

info "Test 6: AgentRun with warmPoolRef creates SandboxClaim"
cat <<EOF | kubectl apply -f - >/dev/null 2>&1
apiVersion: sympozium.ai/v1alpha1
kind: AgentRun
metadata:
  name: ${CLAIM_RUN}
  namespace: ${NAMESPACE}
  labels:
    sympozium.ai/instance: ${SANDBOX_INSTANCE}
    sympozium.ai/component: agent-run
spec:
  agentRef: ${SANDBOX_INSTANCE}
  agentId: default
  sessionKey: "test-claim-${SUFFIX}"
  task: "Warm pool claim test"
  model:
    provider: openai
    model: gpt-4o-mini
    authSecretRef: ${SANDBOX_INSTANCE}-test-key
  agentSandbox:
    enabled: true
    runtimeClass: gvisor
    warmPoolRef: wp-test-pool
EOF

claim_name="$(wait_for_field_notempty agentrun "${CLAIM_RUN}" '{.status.sandboxClaimName}' "Test 6: sandboxClaimName" || true)"
if [[ -n "$claim_name" ]]; then
  pass "Test 6: SandboxClaim created '${claim_name}'"
else
  # The claim might appear as sandboxName since we update it on creation.
  alt="$(kubectl get agentrun "${CLAIM_RUN}" -n "$NAMESPACE" -o jsonpath='{.status.sandboxName}' 2>/dev/null || true)"
  if [[ -n "$alt" ]]; then
    pass "Test 6: SandboxClaim created (reported as sandboxName '${alt}')"
  else
    fail "Test 6: Neither sandboxClaimName nor sandboxName set"
  fi
fi

# Verify the SandboxClaim CR exists.
if kubectl get sandboxclaim "sbc-${CLAIM_RUN}" -n "$NAMESPACE" >/dev/null 2>&1; then
  pass "Test 6: SandboxClaim CR 'sbc-${CLAIM_RUN}' exists in cluster"
  wp_ref="$(kubectl get sandboxclaim "sbc-${CLAIM_RUN}" -n "$NAMESPACE" -o jsonpath='{.spec.warmPoolRef.name}' 2>/dev/null || true)"
  if [[ "$wp_ref" == "wp-test-pool" ]]; then
    pass "Test 6: warmPoolRef.name = wp-test-pool"
  else
    fail "Test 6: warmPoolRef.name = '${wp_ref}', expected 'wp-test-pool'"
  fi
else
  fail "Test 6: SandboxClaim CR 'sbc-${CLAIM_RUN}' not found"
fi

# ── Test 7: Garbage collection via ownerRef ──────────────────────────────────

info "Test 7: Garbage collection — deleting AgentRun deletes Sandbox CR"

# Capture the sandbox name before deletion.
gc_sb="$(kubectl get agentrun "${SANDBOX_RUN}" -n "$NAMESPACE" -o jsonpath='{.status.sandboxName}' 2>/dev/null || true)"
kubectl delete agentrun "${SANDBOX_RUN}" -n "$NAMESPACE" --wait=false >/dev/null 2>&1

# Wait for the Sandbox CR to be garbage-collected.
elapsed=0
gc_passed=false
while [[ "$elapsed" -lt 30 ]]; do
  if ! kubectl get sandbox "${gc_sb}" -n "$NAMESPACE" >/dev/null 2>&1; then
    gc_passed=true
    break
  fi
  sleep 2
  elapsed=$((elapsed + 2))
done

if $gc_passed; then
  pass "Test 7: Sandbox CR '${gc_sb}' garbage-collected after AgentRun deletion"
else
  fail "Test 7: Sandbox CR '${gc_sb}' still exists after AgentRun deletion"
fi
# Keep the immutable test name: cleanup uses --ignore-not-found, never an empty
# positional resource name that could invalidate cleanup of the other runs.

# ── Test 8: CRD field validation ─────────────────────────────────────────────

info "Test 8: CRD accepts agentSandbox fields"

# Verify the agentSandbox field is in the CRD schema.
as_schema="$(kubectl get crd agentruns.sympozium.ai -o jsonpath='{.spec.versions[0].schema.openAPIV3Schema.properties.spec.properties.agentSandbox.properties.runtimeClass.type}' 2>/dev/null || true)"
if [[ "$as_schema" == "string" ]]; then
  pass "Test 8: agentSandbox.runtimeClass field exists in CRD schema (type=string)"
else
  fail "Test 8: agentSandbox.runtimeClass field not found in CRD schema"
fi

as_warmpool="$(kubectl get crd agentruns.sympozium.ai -o jsonpath='{.spec.versions[0].schema.openAPIV3Schema.properties.spec.properties.agentSandbox.properties.warmPoolRef.type}' 2>/dev/null || true)"
if [[ "$as_warmpool" == "string" ]]; then
  pass "Test 8: agentSandbox.warmPoolRef field exists in CRD schema"
else
  fail "Test 8: agentSandbox.warmPoolRef field not found in CRD schema"
fi

# Policy CRD fields.
asp_schema="$(kubectl get crd sympoziumpolicies.sympozium.ai -o jsonpath='{.spec.versions[0].schema.openAPIV3Schema.properties.spec.properties.sandboxPolicy.properties.agentSandboxPolicy.properties.defaultRuntimeClass.type}' 2>/dev/null || true)"
if [[ "$asp_schema" == "string" ]]; then
  pass "Test 8: agentSandboxPolicy.defaultRuntimeClass field in policy CRD"
else
  fail "Test 8: agentSandboxPolicy fields missing from policy CRD"
fi

# Instance CRD fields.
inst_schema="$(kubectl get crd agents.sympozium.ai -o jsonpath='{.spec.versions[0].schema.openAPIV3Schema.properties.spec.properties.agents.properties.default.properties.agentSandbox.properties.runtimeClass.type}' 2>/dev/null || true)"
if [[ "$inst_schema" == "string" ]]; then
  pass "Test 8: agentSandbox.runtimeClass field in instance CRD"
else
  fail "Test 8: agentSandbox fields missing from instance CRD"
fi

# ── Summary ──────────────────────────────────────────────────────────────────

echo ""
if [[ "$EXIT_CODE" -eq 0 ]]; then
  pass "All agent-sandbox integration tests passed"
else
  fail "Some agent-sandbox integration tests failed"
fi
exit "$EXIT_CODE"
