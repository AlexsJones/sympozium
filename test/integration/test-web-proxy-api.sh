#!/usr/bin/env bash
# Web-proxy API integration test: validates the OpenAI-compatible and MCP
# endpoints exposed by the web-proxy sidecar.
#
# Prerequisites:
#   - A running web-proxy Service (server-mode AgentRun with web-endpoint skill)
#   - The API key Secret for the proxy
#
# This test runs SEPARATELY from `make integration-tests`. It exercises the
# web-proxy HTTP API directly via port-forward, not the Sympozium control plane.
#
# Usage:
#   # Auto-detect a web-proxy service and API key in the default namespace:
#   ./test/integration/test-web-proxy-api.sh
#
#   # Explicit service and key:
#   WEB_PROXY_SVC=foo-web-endpoint-server \
#   WEB_PROXY_SECRET=foo-web-proxy-key \
#     ./test/integration/test-web-proxy-api.sh

set -euo pipefail

NAMESPACE="${TEST_NAMESPACE:-default}"
WEB_PROXY_SVC="${WEB_PROXY_SVC:-}"
WEB_PROXY_SECRET="${WEB_PROXY_SECRET:-}"
WEB_PROXY_API_KEY="${WEB_PROXY_API_KEY:-}"
LOCAL_PORT="${WEB_PROXY_PORT:-18080}"
BASE_URL="http://127.0.0.1:${LOCAL_PORT}"

PF_PID=""
failures=0

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

pass() { echo -e "${GREEN}✓ $*${NC}"; }
fail() { echo -e "${RED}✗ $*${NC}"; failures=$((failures + 1)); }
info() { echo -e "${YELLOW}● $*${NC}"; }

cleanup() {
  info "Cleaning up..."
  if [[ -n "${PF_PID}" ]] && kill -0 "${PF_PID}" >/dev/null 2>&1; then
    kill "${PF_PID}" >/dev/null 2>&1 || true
    wait "${PF_PID}" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

require_cmd() { command -v "$1" >/dev/null 2>&1 || { fail "Missing command: $1"; exit 1; }; }

# --- Auto-detect service and secret if not provided ---
auto_detect() {
  if [[ -z "$WEB_PROXY_SVC" ]]; then
    WEB_PROXY_SVC="$(kubectl get svc -n "$NAMESPACE" -l sympozium.ai/component=agent-server \
      -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
    if [[ -z "$WEB_PROXY_SVC" ]]; then
      fail "No web-proxy service found. Set WEB_PROXY_SVC or enable web-endpoint on an instance."
      exit 1
    fi
    info "Auto-detected service: $WEB_PROXY_SVC"
  fi

  if [[ -z "$WEB_PROXY_API_KEY" && -z "$WEB_PROXY_SECRET" ]]; then
    # Derive secret name from service name pattern: <instance>-*-server → <instance>-web-proxy-key
    local instance_name
    instance_name="$(kubectl get svc "$WEB_PROXY_SVC" -n "$NAMESPACE" \
      -o jsonpath='{.metadata.labels.sympozium\.ai/instance}' 2>/dev/null || true)"
    if [[ -n "$instance_name" ]]; then
      WEB_PROXY_SECRET="${instance_name}-web-proxy-key"
    fi
  fi

  if [[ -z "$WEB_PROXY_API_KEY" && -n "$WEB_PROXY_SECRET" ]]; then
    WEB_PROXY_API_KEY="$(kubectl get secret "$WEB_PROXY_SECRET" -n "$NAMESPACE" \
      -o jsonpath='{.data.api-key}' 2>/dev/null | base64 -d 2>/dev/null || true)"
    if [[ -z "$WEB_PROXY_API_KEY" ]]; then
      fail "Could not read API key from secret '$WEB_PROXY_SECRET'"
      exit 1
    fi
    info "Auto-detected API key from secret: $WEB_PROXY_SECRET"
  fi
}

start_port_forward() {
  info "Port-forwarding $WEB_PROXY_SVC to :${LOCAL_PORT}"
  kubectl port-forward -n "$NAMESPACE" "svc/${WEB_PROXY_SVC}" "${LOCAL_PORT}:8080" >/tmp/web-proxy-test-pf.log 2>&1 &
  PF_PID=$!

  local elapsed=0
  while [[ $elapsed -lt 30 ]]; do
    if ! kill -0 "$PF_PID" >/dev/null 2>&1; then
      fail "Port-forward exited early"
      cat /tmp/web-proxy-test-pf.log 2>/dev/null || true
      exit 1
    fi
    if curl -fsS --max-time 2 "${BASE_URL}/healthz" >/dev/null 2>&1; then
      pass "Port-forward ready"
      return 0
    fi
    sleep 1
    elapsed=$((elapsed + 1))
  done
  fail "Timed out waiting for port-forward"
  exit 1
}

# Helper: make a request and capture status code + body
http_request() {
  local method="$1" path="$2" body="${3:-}" auth="${4:-}"
  local url="${BASE_URL}${path}"
  local tmp
  tmp="$(mktemp)"
  local -a opts=(-sS -o "$tmp" -w "%{http_code}" -X "$method" --max-time 10)

  [[ -n "$auth" ]] && opts+=(-H "Authorization: Bearer ${auth}")
  opts+=(-H "Content-Type: application/json")
  [[ -n "$body" ]] && opts+=(--data "$body")

  local code
  code="$(curl "${opts[@]}" "$url")"
  HTTP_CODE="$code"
  HTTP_BODY="$(cat "$tmp")"
  rm -f "$tmp"
}

# =========================================================================
# Tests
# =========================================================================

test_healthz() {
  info "Testing GET /healthz (no auth)"
  http_request GET /healthz "" ""
  if [[ "$HTTP_CODE" == "200" && "$HTTP_BODY" == "ok" ]]; then
    pass "/healthz returns 200 ok"
  else
    fail "/healthz expected 200/ok, got ${HTTP_CODE}/${HTTP_BODY}"
  fi
}

test_auth_reject_no_token() {
  info "Testing auth rejection (no token)"
  http_request GET /v1/models "" ""
  if [[ "$HTTP_CODE" == "401" ]]; then
    pass "Request without token returns 401"
  else
    fail "Expected 401 without token, got $HTTP_CODE"
  fi
}

test_auth_reject_bad_token() {
  info "Testing auth rejection (bad token)"
  http_request GET /v1/models "" "sk-bad-token-12345"
  if [[ "$HTTP_CODE" == "401" ]]; then
    pass "Request with bad token returns 401"
  else
    fail "Expected 401 with bad token, got $HTTP_CODE"
  fi
}

test_list_models() {
  info "Testing GET /v1/models"
  http_request GET /v1/models "" "$WEB_PROXY_API_KEY"
  if [[ "$HTTP_CODE" == "200" ]]; then
    pass "/v1/models returns 200"
  else
    fail "/v1/models expected 200, got $HTTP_CODE"
    echo "$HTTP_BODY"
    return
  fi

  local object
  object="$(echo "$HTTP_BODY" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("object",""))' 2>/dev/null || true)"
  if [[ "$object" == "list" ]]; then
    pass "/v1/models response has object=list"
  else
    fail "/v1/models expected object=list, got '$object'"
  fi

  local data_count
  data_count="$(echo "$HTTP_BODY" | python3 -c 'import json,sys; print(len(json.load(sys.stdin).get("data",[])))' 2>/dev/null || true)"
  if [[ "$data_count" -ge 1 ]]; then
    pass "/v1/models has $data_count model(s)"
  else
    fail "/v1/models expected at least 1 model, got $data_count"
  fi

  local owned_by
  owned_by="$(echo "$HTTP_BODY" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("data",[{}])[0].get("owned_by",""))' 2>/dev/null || true)"
  if [[ "$owned_by" == "sympozium" ]]; then
    pass "/v1/models model owned_by=sympozium"
  else
    fail "/v1/models expected owned_by=sympozium, got '$owned_by'"
  fi
}

test_chat_completions_validation() {
  info "Testing POST /v1/chat/completions input validation"

  # Empty messages array
  http_request POST /v1/chat/completions '{"model":"default","messages":[]}' "$WEB_PROXY_API_KEY"
  if [[ "$HTTP_CODE" == "400" ]]; then
    pass "Empty messages array returns 400"
  else
    fail "Expected 400 for empty messages, got $HTTP_CODE"
  fi

  # No user message
  http_request POST /v1/chat/completions '{"model":"default","messages":[{"role":"system","content":"hello"}]}' "$WEB_PROXY_API_KEY"
  if [[ "$HTTP_CODE" == "400" ]]; then
    pass "No user message returns 400"
  else
    fail "Expected 400 for no user message, got $HTTP_CODE"
  fi

  # Invalid JSON
  http_request POST /v1/chat/completions 'not-json' "$WEB_PROXY_API_KEY"
  if [[ "$HTTP_CODE" == "400" ]]; then
    pass "Invalid JSON returns 400"
  else
    fail "Expected 400 for invalid JSON, got $HTTP_CODE"
  fi
}

test_chat_completions_creates_run() {
  info "Testing POST /v1/chat/completions creates an AgentRun"

  # Count existing web-proxy runs before the request
  local before_count
  before_count="$(kubectl get agentruns -n "$NAMESPACE" -l sympozium.ai/source=web-proxy --no-headers 2>/dev/null | wc -l | tr -d ' ')"

  # Send a valid request — this will block waiting for the agent to complete,
  # so we send it in the background with a short timeout and just verify the
  # AgentRun was created.
  curl -sS --max-time 15 -X POST "${BASE_URL}/v1/chat/completions" \
    -H "Authorization: Bearer ${WEB_PROXY_API_KEY}" \
    -H "Content-Type: application/json" \
    -d '{"model":"default","messages":[{"role":"user","content":"Say hello"}]}' \
    >/dev/null 2>&1 &
  local curl_pid=$!

  # Wait a few seconds for the AgentRun to be created
  sleep 5

  local after_count
  after_count="$(kubectl get agentruns -n "$NAMESPACE" -l sympozium.ai/source=web-proxy --no-headers 2>/dev/null | wc -l | tr -d ' ')"

  if [[ "$after_count" -gt "$before_count" ]]; then
    pass "Chat completions request created a new AgentRun ($before_count → $after_count)"
  else
    fail "No new AgentRun created ($before_count → $after_count)"
  fi

  # Verify the latest run has the correct labels
  local latest_run
  latest_run="$(kubectl get agentruns -n "$NAMESPACE" -l sympozium.ai/source=web-proxy \
    --sort-by=.metadata.creationTimestamp -o jsonpath='{.items[-1].metadata.name}' 2>/dev/null || true)"
  if [[ -n "$latest_run" ]]; then
    local source_label
    source_label="$(kubectl get agentrun "$latest_run" -n "$NAMESPACE" \
      -o jsonpath='{.metadata.labels.sympozium\.ai/source}' 2>/dev/null || true)"
    if [[ "$source_label" == "web-proxy" ]]; then
      pass "AgentRun has source=web-proxy label"
    else
      fail "AgentRun source label mismatch: '$source_label'"
    fi
  fi

  # Kill the background curl (it's likely still waiting for the agent)
  kill "$curl_pid" 2>/dev/null || true
  wait "$curl_pid" 2>/dev/null || true
}

test_mcp_sse_endpoint() {
  info "Testing GET /sse (MCP SSE)"

  # Start SSE connection in background, capture first event
  local tmp_sse
  tmp_sse="$(mktemp)"
  curl -sS --max-time 5 -N "${BASE_URL}/sse" \
    -H "Authorization: Bearer ${WEB_PROXY_API_KEY}" \
    >"$tmp_sse" 2>/dev/null &
  local curl_pid=$!

  sleep 2
  kill "$curl_pid" 2>/dev/null || true
  wait "$curl_pid" 2>/dev/null || true

  local sse_content
  sse_content="$(cat "$tmp_sse")"
  rm -f "$tmp_sse"

  if echo "$sse_content" | grep -q "event: endpoint"; then
    pass "MCP SSE returns endpoint event"
  else
    fail "MCP SSE missing endpoint event"
    echo "$sse_content"
    return
  fi

  if echo "$sse_content" | grep -q "/message?sessionId="; then
    pass "MCP SSE endpoint event contains session ID"
  else
    fail "MCP SSE endpoint event missing sessionId"
  fi
}

test_mcp_message_invalid_session() {
  info "Testing POST /message with invalid session"
  http_request POST "/message?sessionId=invalid-session" \
    '{"jsonrpc":"2.0","id":1,"method":"initialize"}' "$WEB_PROXY_API_KEY"
  if [[ "$HTTP_CODE" == "400" ]]; then
    pass "Invalid MCP session returns 400"
  else
    fail "Expected 400 for invalid MCP session, got $HTTP_CODE"
  fi
}

test_error_response_format() {
  info "Testing error response JSON format"
  http_request GET /v1/models "" ""
  local error_msg
  error_msg="$(echo "$HTTP_BODY" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("error",{}).get("message",""))' 2>/dev/null || true)"
  if [[ -n "$error_msg" ]]; then
    pass "Error response has OpenAI-compatible format (error.message)"
  else
    fail "Error response not in expected format"
    echo "$HTTP_BODY"
  fi
}

# =========================================================================
# Main
# =========================================================================

main() {
  require_cmd kubectl
  require_cmd curl
  require_cmd python3

  info "Running web-proxy API tests in namespace '${NAMESPACE}'"
  auto_detect
  start_port_forward

  test_healthz
  test_auth_reject_no_token
  test_auth_reject_bad_token
  test_error_response_format
  test_list_models
  test_chat_completions_validation
  test_chat_completions_creates_run
  test_mcp_sse_endpoint
  test_mcp_message_invalid_session

  echo ""
  if [[ $failures -gt 0 ]]; then
    fail "$failures check(s) failed"
    exit 1
  fi
  pass "All web-proxy API tests passed"
}

main "$@"
