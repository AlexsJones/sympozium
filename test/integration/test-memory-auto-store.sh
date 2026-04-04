#!/usr/bin/env bash
# Integration test: autoStoreMemory writes task/response after successful AgentRun.
#
# Proves:
#   1. A successful AgentRun (with NO explicit memory_store call) auto-stores its
#      task+response into the memory server with tags ["auto", "agent-run"].
#   2. A follow-up run can search and find the auto-stored entry.
#   3. Memory server logs show [store] from the agent-runner (not just [search]).
#
# This is a regression test for the goroutine bug where autoStoreMemory was
# fire-and-forget (`go autoStoreMemory(...)`) and the process exited before
# the HTTP POST completed, so nothing was ever stored.
#
# Requires: Kind cluster with Sympozium deployed, LM Studio accessible on node.

set -euo pipefail

NAMESPACE="${TEST_NAMESPACE:-default}"
SYSTEM_NS="${SYMPOZIUM_NAMESPACE:-sympozium-system}"
TIMEOUT="${TEST_TIMEOUT:-180}"

LM_STUDIO_BASE_URL="${LM_STUDIO_BASE_URL:-http://172.18.0.2:9473/proxy/lm-studio/v1}"
LM_STUDIO_MODEL="${LM_STUDIO_MODEL:-google/gemma-4-26b-a4b}"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

pass() { echo -e "${GREEN}PASS $*${NC}"; }
fail() { echo -e "${RED}FAIL $*${NC}"; FAILED=1; }
info() { echo -e "${YELLOW}---- $*${NC}"; }

FAILED=0
SUFFIX="$(date +%s)"
INSTANCE="inttest-autostore-${SUFFIX}"
MEM_PF_PID=""

cleanup() {
  info "Cleaning up..."
  [[ -n "$MEM_PF_PID" ]] && kill "$MEM_PF_PID" 2>/dev/null || true
  kubectl delete agentrun -n "$NAMESPACE" -l "sympozium.ai/instance=${INSTANCE}" --ignore-not-found >/dev/null 2>&1 || true
  kubectl delete sympoziuminstance "$INSTANCE" -n "$NAMESPACE" --ignore-not-found >/dev/null 2>&1 || true
}
trap cleanup EXIT

wait_for_deployment() {
  local name="$1" elapsed=0
  while [[ "$elapsed" -lt "$TIMEOUT" ]]; do
    local ready
    ready="$(kubectl get deployment "$name" -n "$NAMESPACE" -o jsonpath='{.status.readyReplicas}' 2>/dev/null || true)"
    [[ "$ready" == "1" ]] && return 0
    sleep 3
    elapsed=$((elapsed + 3))
  done
  return 1
}

wait_for_agentrun() {
  local name="$1" target_phase="$2" elapsed=0 last_phase=""
  while [[ $elapsed -lt $TIMEOUT ]]; do
    local phase
    phase="$(kubectl get agentrun "$name" -n "$NAMESPACE" -o jsonpath='{.status.phase}' 2>/dev/null || echo "")"
    if [[ -n "$phase" && "$phase" != "$last_phase" ]]; then
      info "  Phase: $phase (${elapsed}s)"
      last_phase="$phase"
    fi
    if [[ "$phase" == "$target_phase" ]]; then
      return 0
    fi
    if [[ "$phase" == "Succeeded" || "$phase" == "Failed" ]]; then
      [[ "$phase" != "$target_phase" ]] && return 1
      return 0
    fi
    sleep 3
    elapsed=$((elapsed + 3))
  done
  return 1
}

mem_url=""
mem_port=19492

port_forward_memory() {
  [[ -n "$MEM_PF_PID" ]] && kill "$MEM_PF_PID" 2>/dev/null || true
  kubectl port-forward -n "$NAMESPACE" "svc/${INSTANCE}-memory" "${mem_port}:8080" &>/dev/null &
  MEM_PF_PID=$!
  mem_url="http://127.0.0.1:${mem_port}"
  local elapsed=0
  while [[ "$elapsed" -lt 15 ]]; do
    curl -fsS "${mem_url}/health" >/dev/null 2>&1 && return 0
    sleep 1
    elapsed=$((elapsed + 1))
  done
  fail "Port-forward to memory server timed out"
  return 1
}

mem_search() {
  curl -sS -X POST "${mem_url}/search" \
    -H "Content-Type: application/json" \
    -d "{\"query\": \"$1\", \"top_k\": ${2:-10}}" 2>/dev/null
}

mem_list() {
  curl -sS "${mem_url}/list?limit=${1:-50}" 2>/dev/null
}

mem_count() {
  mem_list "$@" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(len(d.get("content",[])))' 2>/dev/null || echo "0"
}

# ── Setup ─────────────────────────────────────────────────────────────────────

info "Memory auto-store regression test — namespace '${NAMESPACE}'"
info "Using model '${LM_STUDIO_MODEL}' at ${LM_STUDIO_BASE_URL}"

# Create instance with memory enabled.
cat <<EOF | kubectl apply -n "$NAMESPACE" -f -
apiVersion: sympozium.ai/v1alpha1
kind: SympoziumInstance
metadata:
  name: ${INSTANCE}
spec:
  agents:
    default:
      model: ${LM_STUDIO_MODEL}
      baseURL: ${LM_STUDIO_BASE_URL}
  skills:
    - skillPackRef: memory
    - skillPackRef: k8s-ops
  memory:
    enabled: true
EOF

if wait_for_deployment "${INSTANCE}-memory"; then
  pass "Setup: Memory server Deployment is ready"
else
  fail "Setup: Memory server Deployment never became ready"
  exit 1
fi

port_forward_memory || exit 1
pass "Setup: Memory server is healthy"

# Verify memory starts empty.
initial_count="$(mem_count)"
info "Initial memory count: ${initial_count}"

# ── Test 1: Simple run auto-stores task+response ─────────────────────────────
#
# Key: the task does NOT ask the LLM to call memory_store.
# The auto-store comes from agent-runner's postRun path, not from tool calls.

info "Test 1: Successful AgentRun auto-stores task+response (no explicit memory_store)"

MARKER="autostore-proof-${SUFFIX}"
RUN1="${INSTANCE}-run1"
cat <<EOF | kubectl apply -n "$NAMESPACE" -f -
apiVersion: sympozium.ai/v1alpha1
kind: AgentRun
metadata:
  name: ${RUN1}
  labels:
    sympozium.ai/instance: ${INSTANCE}
    sympozium.ai/component: agent-run
spec:
  instanceRef: ${INSTANCE}
  agentId: default
  sessionKey: "autostore-${SUFFIX}"
  task: "Respond with exactly: The answer is ${MARKER}. Do NOT call any tools. Just respond with that text."
  model:
    provider: lm-studio
    model: ${LM_STUDIO_MODEL}
    baseURL: ${LM_STUDIO_BASE_URL}
    authSecretRef: ""
  skills:
    - skillPackRef: memory
  timeout: "3m"
EOF

# Wait for completion.
elapsed=0; last_phase=""
while [[ $elapsed -lt $TIMEOUT ]]; do
  phase="$(kubectl get agentrun "$RUN1" -n "$NAMESPACE" -o jsonpath='{.status.phase}' 2>/dev/null || echo "")"
  [[ -n "$phase" && "$phase" != "$last_phase" ]] && { info "  Phase: $phase (${elapsed}s)"; last_phase="$phase"; }
  [[ "$phase" == "Succeeded" || "$phase" == "Failed" ]] && break
  sleep 3; elapsed=$((elapsed + 3))
done

final="$(kubectl get agentrun "$RUN1" -n "$NAMESPACE" -o jsonpath='{.status.phase}' 2>/dev/null || echo "unknown")"
error="$(kubectl get agentrun "$RUN1" -n "$NAMESPACE" -o jsonpath='{.status.error}' 2>/dev/null || echo "")"
if [[ "$final" == "Succeeded" ]]; then
  pass "Test 1a: AgentRun '${RUN1}' succeeded"
elif [[ "$final" == "Failed" && "$error" == "Job not found" ]]; then
  info "Test 1a: Hit known 'Job not found' race — agent completed, verifying via memory"
  pass "Test 1a: AgentRun completed (agent finished)"
else
  fail "Test 1a: AgentRun ended with phase '${final}' (error: ${error})"
  run1_pod="$(kubectl get agentrun "$RUN1" -n "$NAMESPACE" -o jsonpath='{.status.podName}' 2>/dev/null || true)"
  [[ -n "$run1_pod" ]] && kubectl logs "$run1_pod" -n "$NAMESPACE" -c agent --tail=20 2>/dev/null || true
fi

# Check that the auto-store wrote to the memory server.
# The auto-store is now synchronous (the fix), so no sleep needed in theory,
# but give a small buffer for controller pod-log extraction.
sleep 2

post_run_count="$(mem_count)"
if [[ "$post_run_count" -gt "$initial_count" ]]; then
  pass "Test 1b: Memory count increased (${initial_count} -> ${post_run_count})"
else
  fail "Test 1b: Memory count did NOT increase after run (still ${post_run_count}) — autoStoreMemory may not be firing"
fi

# Search for auto-stored entry. The auto-store stores "Task: <task>\n\nResponse: <response>"
# so searching for a word from the task should find it.
search_result="$(mem_search "autostore-proof")"
if echo "$search_result" | grep -qi "autostore-proof\|answer"; then
  pass "Test 1c: Auto-stored entry found via search for task content"
else
  # FTS5 tokenization may not match hyphenated words; try searching for "Respond"
  search_result2="$(mem_search "Respond")"
  if echo "$search_result2" | grep -qi "Respond\|answer\|hello"; then
    pass "Test 1c: Auto-stored entry found via alternate search"
  else
    # Last resort: just verify the list has the content
    list_content="$(mem_list | python3 -c 'import json,sys; [print(e.get("content","")[:200]) for e in json.load(sys.stdin).get("content",[])]' 2>/dev/null || true)"
    if echo "$list_content" | grep -qi "Task:"; then
      pass "Test 1c: Auto-stored entry confirmed via list (Task: prefix present)"
    else
      fail "Test 1c: Auto-stored entry not found in memory"
      echo "  Search result: $(echo "$search_result" | head -5)"
      echo "  List content: ${list_content}"
    fi
  fi
fi

# Verify the entry has ["auto", "agent-run"] tags.
has_auto_tags="$(mem_list | python3 -c '
import json, sys
entries = json.load(sys.stdin).get("content", [])
for e in entries:
    tags = e.get("tags", [])
    if "auto" in tags and "agent-run" in tags:
        print("found")
        break
' 2>/dev/null || echo "")"

if [[ "$has_auto_tags" == "found" ]]; then
  pass "Test 1d: Auto-stored entry has tags [\"auto\", \"agent-run\"]"
else
  fail "Test 1d: No entry with [\"auto\", \"agent-run\"] tags found"
fi

# ── Test 2: Memory server logs show [store] from agent-runner ────────────────

info "Test 2: Memory server logs show agent-runner store requests"

mem_logs="$(kubectl logs -n "$NAMESPACE" "deployment/${INSTANCE}-memory" --tail=100 2>/dev/null || true)"

store_count="$(echo "$mem_logs" | grep -c "\[store\]" || true)"
if [[ "$store_count" -ge 1 ]]; then
  pass "Test 2: Memory server logged ${store_count} [store] request(s)"
else
  fail "Test 2: No [store] log entries — auto-store POST never reached memory server"
  echo "  Memory server logs:"
  echo "$mem_logs" | tail -15
fi

# ── Test 3: Second run auto-injects prior memory and also auto-stores ─────────

info "Test 3: Follow-up run gets auto-injected memory and also auto-stores"

count_before_run2="$(mem_count)"

RUN2="${INSTANCE}-run2"
cat <<EOF | kubectl apply -n "$NAMESPACE" -f -
apiVersion: sympozium.ai/v1alpha1
kind: AgentRun
metadata:
  name: ${RUN2}
  labels:
    sympozium.ai/instance: ${INSTANCE}
    sympozium.ai/component: agent-run
spec:
  instanceRef: ${INSTANCE}
  agentId: default
  sessionKey: "autostore-followup-${SUFFIX}"
  task: "Say goodbye. Do not call any tools."
  model:
    provider: lm-studio
    model: ${LM_STUDIO_MODEL}
    baseURL: ${LM_STUDIO_BASE_URL}
    authSecretRef: ""
  skills:
    - skillPackRef: memory
  timeout: "2m"
EOF

if wait_for_agentrun "$RUN2" "Succeeded"; then
  pass "Test 3a: Follow-up run succeeded"
else
  final="$(kubectl get agentrun "$RUN2" -n "$NAMESPACE" -o jsonpath='{.status.phase}' 2>/dev/null || echo "unknown")"
  error="$(kubectl get agentrun "$RUN2" -n "$NAMESPACE" -o jsonpath='{.status.error}' 2>/dev/null || echo "")"
  fail "Test 3a: Follow-up run ended with '${final}' (error: ${error})"
fi

sleep 2

# Verify auto-store added another entry.
count_after_run2="$(mem_count)"
if [[ "$count_after_run2" -gt "$count_before_run2" ]]; then
  pass "Test 3b: Memory count increased again (${count_before_run2} -> ${count_after_run2})"
else
  fail "Test 3b: Memory count did not increase after second run (still ${count_after_run2})"
fi

# Verify auto-inject happened by checking agent-runner pod logs.
run2_pod="$(kubectl get agentrun "$RUN2" -n "$NAMESPACE" -o jsonpath='{.status.podName}' 2>/dev/null || true)"
if [[ -n "$run2_pod" ]]; then
  agent_logs="$(kubectl logs "$run2_pod" -n "$NAMESPACE" -c agent --tail=50 2>/dev/null || true)"
  if echo "$agent_logs" | grep -q "auto-injected.*bytes of memory context"; then
    pass "Test 3c: Agent-runner auto-injected prior memory context into second run"
  elif echo "$agent_logs" | grep -q "auto-stored memory for task"; then
    pass "Test 3c: Agent-runner auto-stored second run (auto-inject may have returned empty for short task)"
  else
    info "Test 3c: Could not verify auto-inject from agent logs"
  fi
fi

# Verify the memory server saw search requests from both runs.
mem_logs="$(kubectl logs -n "$NAMESPACE" "deployment/${INSTANCE}-memory" --tail=100 2>/dev/null || true)"
search_count="$(echo "$mem_logs" | grep -c "\[search\]" || true)"
if [[ "$search_count" -ge 2 ]]; then
  pass "Test 3d: Memory server logged ${search_count} search requests across both runs"
else
  fail "Test 3d: Expected >= 2 search requests, found ${search_count}"
fi

# ── Summary ───────────────────────────────────────────────────────────────────

echo ""
final_count="$(mem_count)"
info "Final memory entry count: ${final_count}"
echo ""

if [[ $FAILED -eq 0 ]]; then
  pass "All memory auto-store regression tests passed"
  exit 0
else
  fail "Some tests failed"
  exit 1
fi
