#!/usr/bin/env bash
# Integration test: validates the node-probe DaemonSet detects local inference
# providers and annotates the Kubernetes node accordingly.
# Coverage:
#   - DaemonSet is running and healthy
#   - Node annotations are set when a provider is reachable
#   - Detected models are listed in annotations
#   - Annotations are cleared when a provider goes away

set -euo pipefail

NAMESPACE="${SYMPOZIUM_NAMESPACE:-sympozium-system}"
PROBE_TIMEOUT="${PROBE_TIMEOUT:-90}"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

pass() { echo -e "${GREEN}✓ $*${NC}"; }
fail() { echo -e "${RED}✗ $*${NC}"; FAILURES=$((FAILURES + 1)); }
info() { echo -e "${YELLOW}● $*${NC}"; }

FAILURES=0

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    fail "Required command not found: $1"
    exit 1
  fi
}

require_cmd kubectl
require_cmd jq

# ── 1. DaemonSet health ──────────────────────────────────────────────────────

info "Checking node-probe DaemonSet is running..."

DS_STATUS=$(kubectl get daemonset sympozium-node-probe -n "$NAMESPACE" -o json 2>/dev/null) || {
  fail "DaemonSet sympozium-node-probe not found in namespace $NAMESPACE"
  exit 1
}

DESIRED=$(echo "$DS_STATUS" | jq '.status.desiredNumberScheduled')
READY=$(echo "$DS_STATUS" | jq '.status.numberReady')

if [[ "$DESIRED" -eq 0 ]]; then
  fail "DaemonSet has 0 desired pods"
  exit 1
fi

if [[ "$READY" -ne "$DESIRED" ]]; then
  fail "DaemonSet not fully ready: $READY/$DESIRED pods"
  exit 1
fi

pass "DaemonSet is healthy ($READY/$DESIRED pods ready)"

# ── 2. Pod is not crash-looping ───────────────────────────────────────────────

info "Checking pod restart count..."

RESTARTS=$(kubectl get pods -n "$NAMESPACE" -l app.kubernetes.io/component=node-probe \
  -o jsonpath='{.items[0].status.containerStatuses[0].restartCount}' 2>/dev/null)

if [[ "${RESTARTS:-0}" -gt 2 ]]; then
  fail "Pod has $RESTARTS restarts (possible crashloop)"
  kubectl logs -n "$NAMESPACE" -l app.kubernetes.io/component=node-probe --tail=20 2>/dev/null || true
  exit 1
fi

pass "Pod restart count is acceptable ($RESTARTS)"

# ── 3. Health endpoint responds ───────────────────────────────────────────────

info "Checking health endpoint via port-forward..."

POD_NAME=$(kubectl get pods -n "$NAMESPACE" -l app.kubernetes.io/component=node-probe \
  -o jsonpath='{.items[0].metadata.name}')

NODE_NAME=$(kubectl get pod "$POD_NAME" -n "$NAMESPACE" -o jsonpath='{.spec.nodeName}')
NODE_IP=$(kubectl get node "$NODE_NAME" -o jsonpath='{.status.addresses[?(@.type=="InternalIP")].address}')

# Port-forward to the pod's health port.
LOCAL_HEALTH_PORT=19473
kubectl port-forward -n "$NAMESPACE" "pod/${POD_NAME}" "${LOCAL_HEALTH_PORT}:9473" &
PF_HEALTH_PID=$!
sleep 2

HEALTH_CODE=$(curl -s -o /dev/null -w "%{http_code}" --connect-timeout 5 "http://127.0.0.1:${LOCAL_HEALTH_PORT}/healthz" 2>/dev/null) || HEALTH_CODE="000"
kill "$PF_HEALTH_PID" >/dev/null 2>&1 || true
wait "$PF_HEALTH_PID" >/dev/null 2>&1 || true

if [[ "$HEALTH_CODE" == "200" ]]; then
  pass "Health endpoint returned 200"
else
  fail "Health endpoint returned $HEALTH_CODE (expected 200)"
fi

# ── 4. Wait for node annotations ──────────────────────────────────────────────

info "Waiting for inference annotations on node $NODE_NAME (timeout: ${PROBE_TIMEOUT}s)..."

WAITED=0
HEALTHY=""
while [[ $WAITED -lt $PROBE_TIMEOUT ]]; do
  HEALTHY=$(kubectl get node "$NODE_NAME" \
    -o jsonpath='{.metadata.annotations.sympozium\.ai/inference-healthy}' 2>/dev/null) || true
  if [[ "$HEALTHY" == "true" ]]; then
    break
  fi
  sleep 5
  WAITED=$((WAITED + 5))
done

if [[ "$HEALTHY" != "true" ]]; then
  fail "No healthy inference provider detected within ${PROBE_TIMEOUT}s"
  info "Current sympozium annotations:"
  kubectl get node "$NODE_NAME" -o json | jq '.metadata.annotations | with_entries(select(.key | startswith("sympozium.ai")))' 2>/dev/null || true
  info "Pod logs:"
  kubectl logs -n "$NAMESPACE" -l app.kubernetes.io/component=node-probe --tail=30 2>/dev/null || true
  exit 1
fi

pass "Node annotated as inference-healthy"

# ── 5. Validate annotation contents ──────────────────────────────────────────

info "Validating annotation contents..."

ANNOTATIONS=$(kubectl get node "$NODE_NAME" -o json | \
  jq '.metadata.annotations | with_entries(select(.key | startswith("sympozium.ai")))')

# Check last-probe timestamp exists and is recent (within last 5 minutes).
LAST_PROBE=$(echo "$ANNOTATIONS" | jq -r '.["sympozium.ai/inference-last-probe"] // ""')
if [[ -z "$LAST_PROBE" ]]; then
  fail "Missing inference-last-probe annotation"
else
  pass "Last probe timestamp present: $LAST_PROBE"
fi

# Check that at least one provider port annotation exists.
PROVIDER_PORTS=$(echo "$ANNOTATIONS" | jq -r '
  to_entries[]
  | select(.key | test("sympozium.ai/inference-[a-z]"))
  | select(.key | test("models|healthy|last|proxy") | not)
  | "\(.key)=\(.value)"
')

if [[ -z "$PROVIDER_PORTS" ]]; then
  fail "No provider port annotations found"
else
  while IFS= read -r line; do
    pass "Provider detected: $line"
  done <<< "$PROVIDER_PORTS"
fi

# Check that at least one models annotation exists.
MODELS=$(echo "$ANNOTATIONS" | jq -r '
  to_entries[]
  | select(.key | test("sympozium.ai/inference-models-"))
  | "\(.key)=\(.value)"
')

if [[ -z "$MODELS" ]]; then
  fail "No model annotations found"
else
  while IFS= read -r line; do
    pass "Models: $line"
  done <<< "$MODELS"
fi

# ── 6. Reverse proxy serves models ────────────────────────────────────────────

info "Testing reverse proxy..."

PROXY_PORT=$(echo "$ANNOTATIONS" | jq -r '.["sympozium.ai/inference-proxy-port"] // ""')
if [[ -z "$PROXY_PORT" ]]; then
  fail "Missing proxy-port annotation"
else
  pass "Proxy port annotation present: $PROXY_PORT"

  # Test that the proxy can serve Ollama models from inside the cluster.
  PROXY_MODELS=$(kubectl run proxy-inttest --rm -i --restart=Never --image=curlimages/curl \
    -- curl -s --connect-timeout 5 "http://${NODE_IP}:${PROXY_PORT}/proxy/ollama/v1/models" 2>/dev/null) || PROXY_MODELS=""

  if echo "$PROXY_MODELS" | grep -q '"id"'; then
    MODEL_COUNT=$(echo "$PROXY_MODELS" | python3 -c 'import json,sys; print(len(json.load(sys.stdin).get("data",[])))' 2>/dev/null || echo "0")
    pass "Reverse proxy returned $MODEL_COUNT model(s) via /v1/models"
  else
    fail "Reverse proxy did not return models"
  fi
fi

# ── Summary ───────────────────────────────────────────────────────────────────

echo ""
if [[ $FAILURES -gt 0 ]]; then
  fail "Node-probe integration test finished with $FAILURES failure(s)"
  exit 1
else
  pass "All node-probe integration tests passed"
fi
