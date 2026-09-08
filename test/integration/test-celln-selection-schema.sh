#!/usr/bin/env bash
# Real schema validation only: all AgentRuns are server-side dry runs.
set -euo pipefail
cd "$(dirname "$0")/../.."
: "${CELLN_CATALOGUE_KUBECONFIG:?explicit isolated kubeconfig required}"
k() { kubectl --kubeconfig "$CELLN_CATALOGUE_KUBECONFIG" --context kind-celln-deployed "$@"; }
[[ $(kubectl --kubeconfig "$CELLN_CATALOGUE_KUBECONFIG" config current-context) == kind-celln-deployed ]]
k apply -f config/crd/bases/sympozium.ai_agentruns.yaml >/dev/null
k wait --for=condition=Established crd/agentruns.sympozium.ai --timeout=30s >/dev/null
namespace="celln-selection-schema-$$"
k create namespace "$namespace" >/dev/null
trap 'k delete namespace "$namespace" --wait=true --timeout=45s >/dev/null' EXIT
base='{"apiVersion":"sympozium.ai/v1alpha1","kind":"AgentRun","metadata":{"name":"selection-only"},"spec":{"agentRef":"not-created","agentId":"default","sessionKey":"schema","backend":"celln","task":"schema only","model":{"provider":"deepseek","model":"deepseek-chat","authSecretRef":""},"cellnSelection":{"toolRefs":[]}}}'
accept() { k create -n "$namespace" --dry-run=server -f - -o json <<<"$1"; }
refuse() {
  local output
  if output=$(accept "$1" 2>&1); then echo 'Unexpected selection acceptance' >&2; exit 1; fi
  [[ "$output" == *'is invalid:'* || "$output" == *'(Invalid)'* ]] || { echo "Wrong refusal: $output" >&2; exit 1; }
}
accept "$base" | jq -e '.spec.cellnSelection.toolRefs==[]' >/dev/null
selected=$(jq '.spec.cellnSelection={runtimeRef:"approved-runtime",toolRefs:[{name:"uppercase",revision:"v1"},{name:"length",revision:"v1"}]}' <<<"$base")
accept "$selected" | jq -e '.spec.cellnSelection.runtimeRef=="approved-runtime" and [.spec.cellnSelection.toolRefs[].name]==["uppercase","length"]' >/dev/null
refuse "$(jq '.spec.backend="job"' <<<"$base")"
refuse "$(jq 'del(.spec.backend)' <<<"$base")"
refuse "$(jq '.spec.cellnSelection.toolRefs=[{name:"same",revision:"v1"},{name:"same",revision:"v2"}]' <<<"$base")"
refuse "$(jq '.spec.cellnSelection.toolRefs=null' <<<"$base")"
refuse "$(jq 'del(.spec.cellnSelection.toolRefs)' <<<"$base")"
refuse "$(jq '.spec.cellnSelection.toolRefs=[{name:"other/namespace",revision:"v1"}]' <<<"$base")"
refuse "$(jq '.spec.cellnSelection.toolRefs=[{name:"tool",revision:""}]' <<<"$base")"
refuse "$(jq '.spec.cellnSelection.runtimeRef="other/runtime"' <<<"$base")"
refuse "$(jq '.spec.cellnSelection.toolRefs=[range(17)|{name:("tool-"+tostring),revision:"v1"}]' <<<"$base")"
# Supply a valid explicit artifact block so refusal proves mutual exclusion.
hash="blake3:$(printf 'a%.0s' {1..64})"
mixed=$(jq --arg hash "$hash" '.spec.celln={mote:{hash:$hash},tools:[{alias:"/tool",hash:$hash}],invocation:{alias:"/tool"},lane:"tool",capabilities:{workspace:"none",memoryBytes:1024,outputBytes:1024}}' <<<"$base")
accept "$(jq 'del(.spec.cellnSelection)' <<<"$mixed")" >/dev/null
refuse "$mixed"
[[ $(k get agentruns -n "$namespace" -o json | jq '.items|length') == 0 ]]
[[ $(k get jobs -n "$namespace" -o json | jq '.items|length') == 0 ]]
echo 'PASS actual API: empty and ordered catalogue selections, legacy explicit binding, 10 refusals; no AgentRuns, Jobs or model calls'
