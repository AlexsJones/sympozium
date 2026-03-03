#!/bin/bash
# llmfit-probe-json.sh
# Runs on a single node and emits JSON with local llmfit recommendations.

set -euo pipefail

MODEL_QUERY="${MODEL_QUERY:-}"
LLMFIT_LIMIT="${LLMFIT_LIMIT:-5}"
LLMFIT_MIN_FIT="${LLMFIT_MIN_FIT:-good}"
LLMFIT_USE_CASE="${LLMFIT_USE_CASE:-}"
NODE_NAME="${NODE_NAME:-unknown}"

case "${LLMFIT_MIN_FIT}" in
  perfect|good|marginal|too_tight) ;;
  *) LLMFIT_MIN_FIT="good" ;;
esac

if [[ -z "${MODEL_QUERY}" ]]; then
  echo '{"error":"MODEL_QUERY env var is required"}'
  exit 2
fi

MODEL_QUERY_NORMALIZED="$(printf '%s' "${MODEL_QUERY}" | tr '[:upper:]' '[:lower:]')"
if [[ "${MODEL_QUERY_NORMALIZED}" == "*" || "${MODEL_QUERY_NORMALIZED}" == "any" || "${MODEL_QUERY_NORMALIZED}" == "all" ]]; then
  MODEL_QUERY="*"
fi

tmp_system=$(mktemp)
tmp_models=$(mktemp)
trap 'rm -f "$tmp_system" "$tmp_models"' EXIT

llmfit --json system >"$tmp_system"

if [[ -n "${LLMFIT_USE_CASE}" ]]; then
  llmfit recommend --json --limit "${LLMFIT_LIMIT}" --use-case "${LLMFIT_USE_CASE}" >"$tmp_models"
else
  llmfit recommend --json --limit "${LLMFIT_LIMIT}" >"$tmp_models"
fi

jq -n \
  --arg node "$NODE_NAME" \
  --arg modelQuery "$MODEL_QUERY" \
  --arg minFit "$LLMFIT_MIN_FIT" \
  --arg useCase "$LLMFIT_USE_CASE" \
  --slurpfile system "$tmp_system" \
  --slurpfile rec "$tmp_models" '
  def fitrank($f):
    if $f == "perfect" then 4
    elif $f == "good" then 3
    elif $f == "marginal" then 2
    elif $f == "too_tight" then 1
    else 0
    end;

  def rows:
    if ($rec[0] | type) == "array" then $rec[0]
    elif ($rec[0].models? != null) then $rec[0].models
    else []
    end;

  def normalized:
    rows
    | map(
        . + {
          fit_level: ((.fit_level // .fit // "") | tostring | ascii_downcase),
          score: (.score // 0)
        }
      );

  def filtered:
    normalized
    | map(select(fitrank(.fit_level) >= fitrank($minFit | ascii_downcase)));

  def matches:
    if ($modelQuery == "*") then
      filtered
    else
      filtered
      | map(select((.name // "" | tostring | ascii_downcase) | contains($modelQuery | ascii_downcase)))
    end;

  def picks:
    if (matches | length) > 0 then matches else filtered end;

  {
    node: $node,
    model_query: $modelQuery,
    use_case: (if $useCase == "" then null else $useCase end),
    min_fit: $minFit,
    system: ($system[0] // {}),
    matched_count: (matches | length),
    candidate_count: (filtered | length),
    top: (picks | sort_by(-(.score // 0)) | .[:3])
  }
'
