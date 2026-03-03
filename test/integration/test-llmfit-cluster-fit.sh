#!/usr/bin/env bash
# Integration test: verify the llmfit skill works end-to-end.
#
# What it does:
#   1. Creates a test SympoziumInstance + AgentRun with the llmfit skill
#   2. The AgentRun task tells the LLM to run llmfit-cluster-fit.sh for a model
#   3. Waits for AgentRun completion
#   4. Validates the result includes ranked_nodes JSON evidence
#   5. Cleans up test resources
#
# Prerequisites:
#   - Kind cluster running with Sympozium installed
#   - llmfit SkillPack applied (kubectl get skillpack llmfit -n sympozium-system)
#   - llmfit sidecar image available in cluster registry/runtime
#   - OPENAI_API_KEY env var set, or secret inttest-openai-key in namespace
#
# Usage:
#   ./test/integration/test-llmfit-cluster-fit.sh
#   TEST_MODEL=gpt-5.2 ./test/integration/test-llmfit-cluster-fit.sh

set -euo pipefail

# --- Configuration ---
NAMESPACE="${TEST_NAMESPACE:-default}"
INSTANCE_NAME="inttest-llmfit"
RUN_NAME="inttest-llmfit-fit"
SECRET_NAME="inttest-openai-key"
MODEL="${TEST_MODEL:-gpt-4o-mini}"
TIMEOUT="${TEST_TIMEOUT:-240}"
TARGET_MODEL_QUERY="${TEST_LLMFIT_MODEL_QUERY:-Qwen2.5}"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

pass() { echo -e "${GREEN}✓ $*${NC}"; }
fail() { echo -e "${RED}✗ $*${NC}"; }
info() { echo -e "${YELLOW}● $*${NC}"; }
failures=0

cleanup() {
    info "Cleaning up test resources..."
    kubectl delete agentrun "$RUN_NAME" -n "$NAMESPACE" --ignore-not-found >/dev/null 2>&1 || true
    kubectl delete sympoziuminstance "$INSTANCE_NAME" -n "$NAMESPACE" --ignore-not-found >/dev/null 2>&1 || true
    kubectl delete jobs -n "$NAMESPACE" -l "sympozium.ai/agentrun=$RUN_NAME" --ignore-not-found >/dev/null 2>&1 || true
    kubectl delete pods -n "$NAMESPACE" -l "sympozium.ai/agentrun=$RUN_NAME" --ignore-not-found >/dev/null 2>&1 || true
}

info "Running integration test: llmfit skill (cluster placement)"

if ! kubectl get crd agentruns.sympozium.ai >/dev/null 2>&1; then
    fail "Sympozium CRDs not installed."
    exit 1
fi

if ! kubectl get deployment sympozium-controller-manager -n sympozium-system >/dev/null 2>&1; then
    fail "Sympozium controller not running."
    exit 1
fi

if ! kubectl get skillpack llmfit -n sympozium-system >/dev/null 2>&1; then
    fail "llmfit SkillPack not found in sympozium-system."
    echo "  Apply it: kubectl apply -f config/skills/llmfit.yaml"
    exit 1
fi

if ! kubectl get secret "$SECRET_NAME" -n "$NAMESPACE" >/dev/null 2>&1; then
    if [[ -z "${OPENAI_API_KEY:-}" ]]; then
        fail "No OPENAI_API_KEY set and secret '$SECRET_NAME' not found."
        echo "  Either: export OPENAI_API_KEY=sk-..."
        echo "  Or:     kubectl create secret generic $SECRET_NAME --from-literal=OPENAI_API_KEY=sk-..."
        exit 1
    fi
    info "Creating secret $SECRET_NAME from OPENAI_API_KEY env var"
    kubectl create secret generic "$SECRET_NAME" \
        --from-literal=OPENAI_API_KEY="$OPENAI_API_KEY" \
        -n "$NAMESPACE"
fi

NODE_COUNT=$(kubectl get nodes --no-headers 2>/dev/null | wc -l | tr -d ' ')
if [[ -z "$NODE_COUNT" || "$NODE_COUNT" -lt 1 ]]; then
    fail "No nodes found in cluster"
    exit 1
fi
info "Cluster nodes detected: $NODE_COUNT"

cleanup 2>/dev/null || true
sleep 2

info "Creating SympoziumInstance: $INSTANCE_NAME (with llmfit skill)"
cat <<EOF | kubectl apply -f -
apiVersion: sympozium.ai/v1alpha1
kind: SympoziumInstance
metadata:
  name: ${INSTANCE_NAME}
  namespace: ${NAMESPACE}
spec:
  agents:
    default:
      model: ${MODEL}
  authRefs:
    - secret: ${SECRET_NAME}
  skills:
    - skillPackRef: llmfit
EOF

info "Creating AgentRun: $RUN_NAME"
cat <<EOF | kubectl apply -f -
apiVersion: sympozium.ai/v1alpha1
kind: AgentRun
metadata:
  name: ${RUN_NAME}
  namespace: ${NAMESPACE}
  labels:
    sympozium.ai/instance: ${INSTANCE_NAME}
spec:
  instanceRef: ${INSTANCE_NAME}
  agentId: default
  sessionKey: "inttest-llmfit-$(date +%s)"
  task: |
    Use execute_command to run this exact command once:
    llmfit-cluster-fit.sh --model "${TARGET_MODEL_QUERY}" --min-fit marginal --limit 5

    Then return a short summary and include the full raw JSON output in your response.
  model:
    provider: openai
    model: ${MODEL}
    authSecretRef: ${SECRET_NAME}
  skills:
    - skillPackRef: llmfit
  timeout: "4m"
EOF

info "Waiting up to ${TIMEOUT}s for AgentRun to complete..."
elapsed=0
phase=""
pod=""
while [[ $elapsed -lt $TIMEOUT ]]; do
    phase=$(kubectl get agentrun "$RUN_NAME" -n "$NAMESPACE" -o jsonpath='{.status.phase}' 2>/dev/null || echo "")
    if [[ -z "$pod" ]]; then
        pod=$(kubectl get pods -n "$NAMESPACE" -l "sympozium.ai/agentrun=$RUN_NAME" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")
        if [[ -n "$pod" ]]; then
            info "Pod found: $pod"
        fi
    fi
    if [[ "$phase" == "Succeeded" || "$phase" == "Failed" ]]; then
        break
    fi
    sleep 5
    elapsed=$((elapsed + 5))
    if (( elapsed % 20 == 0 )); then
        info "  ...${elapsed}s elapsed (phase: ${phase:-Pending})"
    fi
done

if [[ "$phase" != "Succeeded" && "$phase" != "Failed" ]]; then
    fail "AgentRun did not complete within ${TIMEOUT}s (last phase: ${phase:-unknown})"
    if [[ -n "$pod" ]]; then
        info "Pod events:"
        kubectl describe pod "$pod" -n "$NAMESPACE" 2>/dev/null | tail -30 || true
    fi
    cleanup
    exit 1
fi

echo ""
if [[ "$phase" == "Failed" ]]; then
    fail "AgentRun phase: Failed"
    result=$(kubectl get agentrun "$RUN_NAME" -n "$NAMESPACE" -o jsonpath='{.status.result}' 2>/dev/null || echo "")
    info "Result: $result"
    if [[ -n "$pod" ]]; then
        info "Agent logs:"
        kubectl logs "$pod" -c agent -n "$NAMESPACE" 2>/dev/null | tail -30 || true
    fi
    cleanup
    exit 1
fi

pass "AgentRun phase: Succeeded"

result=$(kubectl get agentrun "$RUN_NAME" -n "$NAMESPACE" -o jsonpath='{.status.result}' 2>/dev/null || echo "")
logs=""
if [[ -n "$pod" ]]; then
    logs=$(kubectl logs "$pod" -c agent -n "$NAMESPACE" 2>/dev/null || echo "")
fi

# Validation 1: result should include ranked_nodes JSON key
if echo "$result" | grep -q 'ranked_nodes'; then
    pass "Result includes ranked_nodes"
else
    fail "Result does not include ranked_nodes"
    failures=$((failures + 1))
fi

# Validation 2: result should include node_results JSON key
if echo "$result" | grep -q 'node_results'; then
    pass "Result includes node_results"
else
    fail "Result does not include node_results"
    failures=$((failures + 1))
fi

# Validation 3: evidence command was run
if echo "$result" | grep -qi 'llmfit-cluster-fit.sh\|requested_model\|best_model'; then
    pass "Result contains llmfit placement evidence"
elif [[ -n "$logs" ]] && echo "$logs" | grep -qi 'llmfit-cluster-fit.sh\|execute_command'; then
    pass "Logs contain llmfit command evidence"
else
    fail "No llmfit command evidence found in result/logs"
    failures=$((failures + 1))
fi

# Validation 4: result should mention the model query string somewhere
if echo "$result" | grep -qi "$TARGET_MODEL_QUERY"; then
    pass "Result mentions model query: $TARGET_MODEL_QUERY"
else
    fail "Result does not mention model query '$TARGET_MODEL_QUERY'"
    failures=$((failures + 1))
fi

echo ""
echo "=============================="
echo " LLMFit Integration Summary"
echo "=============================="
echo " AgentRun:    $RUN_NAME"
echo " Phase:       $phase"
echo " Model:       $MODEL"
echo " Skill:       llmfit"
echo " Query:       $TARGET_MODEL_QUERY"
echo " Nodes:       $NODE_COUNT"
if [[ -n "$pod" ]]; then
    echo " Pod:         $pod"
fi
echo " Failures:    $failures"
echo "=============================="
echo ""

cleanup

if [[ $failures -gt 0 ]]; then
    fail "Integration test finished with $failures failure(s)"
    exit 1
fi

pass "Integration test complete"
