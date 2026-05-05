#!/usr/bin/env bash
# Preflight check for `make ux-tests*`:
# ensure LM Studio is running, has a model loaded, and can produce completions.
# Without this, LLM-dependent Cypress tests silently wait 3-6 minutes per spec
# before timing out.

set -euo pipefail

LM_STUDIO_URL="${LM_STUDIO_URL:-http://localhost:1234}"
TEST_MODEL="${1:-qwen/qwen3.5-9b}"

# 1. Check LM Studio is reachable.
models_status=$(curl -s -o /dev/null -w "%{http_code}" --max-time 5 \
  "${LM_STUDIO_URL}/v1/models" 2>/dev/null || true)
models_status="${models_status:-000}"

if [ "$models_status" = "000" ]; then
  cat >&2 <<EOF

ERROR: LM Studio is not running.

  Probed: GET ${LM_STUDIO_URL}/v1/models
  Got:    no response (connection refused)

Fix:
  1. Open LM Studio
  2. Load a model (e.g. ${TEST_MODEL})
  3. Re-run tests

EOF
  exit 1
fi

if [ "$models_status" != "200" ]; then
  cat >&2 <<EOF

ERROR: LM Studio returned HTTP ${models_status}.

  Probed: GET ${LM_STUDIO_URL}/v1/models

Fix: Ensure LM Studio is running and healthy on ${LM_STUDIO_URL}.

EOF
  exit 1
fi

# 2. Check at least one model is loaded.
model_count=$(curl -s "${LM_STUDIO_URL}/v1/models" 2>/dev/null \
  | python3 -c "import sys,json; print(len(json.load(sys.stdin).get('data',[])))" 2>/dev/null || echo "0")

if [ "$model_count" = "0" ]; then
  cat >&2 <<EOF

ERROR: LM Studio is running but no models are loaded.

  Probed: GET ${LM_STUDIO_URL}/v1/models
  Models: 0

Fix:
  1. Open LM Studio
  2. Load a model (e.g. ${TEST_MODEL})
  3. Re-run tests

EOF
  exit 1
fi

# 3. Verify the model can produce a completion.
completion_status=$(curl -s -o /dev/null -w "%{http_code}" --max-time 30 \
  -X POST "${LM_STUDIO_URL}/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -d "{\"model\":\"${TEST_MODEL}\",\"messages\":[{\"role\":\"user\",\"content\":\"Reply with OK\"}],\"max_tokens\":3}" \
  2>/dev/null || true)
completion_status="${completion_status:-000}"

if [ "$completion_status" != "200" ]; then
  cat >&2 <<EOF

ERROR: LM Studio model "${TEST_MODEL}" failed to produce a completion.

  Probed: POST ${LM_STUDIO_URL}/v1/chat/completions
  Got:    HTTP ${completion_status}

Fix:
  1. Ensure "${TEST_MODEL}" is loaded in LM Studio
  2. Or override: make ux-tests CYPRESS_TEST_MODEL=<your-model-id>

EOF
  exit 1
fi

echo "  ✓ LM Studio ready (${model_count} model(s) loaded, ${TEST_MODEL} responsive)"
