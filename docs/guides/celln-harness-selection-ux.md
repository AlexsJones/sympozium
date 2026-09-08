# Harness + Celln selection UX: API foundation, UI still pending

Recorded for [epic #426](https://github.com/sympozium-ai/sympozium/issues/426)
after the user's UI/YAML question. The approved experience has three distinct
choices: Harness identity, execution placement, and explicitly borrowed tools.
The [advanced JSON API](celln-json-harness.md) and the high-level AgentRun
`cellnSelection` schema are implemented. The UI selector and automatic
provisioning remain pending: a catalogue request waits for trusted issuance.

## UI requirements

- Agent → Harness selects the approved default runtime. New Run inherits it
  and offers a one-run override, preserving the existing runtimeRef workflow.
- Execution placement selects Job or Celln. Only proven compatible combinations
  can run. Explain unsupported profiles, rather than silently switching backend
  or pretending an arbitrary OCI/Pi/Hermes runtime is Celln-compatible.
- Borrowed tools is an explicit multi-select of approved catalogue revisions,
  displaying description, publisher/owner, effective permissions and limits,
  and selection-specific readiness. Empty selection means no borrowed tools.
- Before submission, show the resolved runtime, model/provider, ordered tools,
  permission intersection and any review/distribution/prewarm blockers. A node
  preflight success must not make an unverified selection runnable.
- BYO tools enter a submission/review workflow; users cannot self-approve or
  turn an uploaded artifact into authority. Pending/refused status is visible.
- Results show the answer, tool-call timeline, pinned identities, receipt and
  correlated audit, with cancellation and cleanup state. Runtime transcript
  events must not be labelled individually hardware-attested tool receipts.

“Harness in Celln” means the model loop executes in the cell. “Celln tool
execution” means an outside Harness invokes Celln-backed tools. Keep these
labels distinct. Model selection must be explicit and checked against the
host grant; the existing forge-path warning about ambient host model selection
must not be reused for this Harness path.

## YAML and API parity

Keep `Agent.spec.runtimeRef` as the default Harness reference and
`AgentRun.spec.backend` as placement. `cellnSelection.toolRefs` names catalogue
revisions. It is deliberately separate from the existing required explicit
artifact fields in `celln`, preserving that API and rejecting ambiguous mixtures:

```yaml
# Requires the current catalogue-selection CRD and controller.
spec:
  agentRef: my-agent # its runtimeRef selects an approved Celln-capable Harness
  backend: celln
  task: Uppercase "celln", then measure its length.
  model:
    provider: deepseek
    model: deepseek-chat
    authSecretRef: "" # host-issued model authority, not a Kubernetes key
  cellnSelection:
    # Optional one-run override; otherwise inherit Agent.spec.runtimeRef.
    # runtimeRef: approved-celln-runtime
    toolRefs:
      - name: uppercase-v1
        revision: v1
      - name: length-v1
        revision: v1
```

Names/revisions are user intent, not the final authority. The controller must
resolve live same-namespace UIDs/generations/full-spec identities, intersect
independently trusted operator/runtime/Agent/run grants, and freeze the exact
signed composition, model grant, policy and execution identity. Users should
not assemble mote hashes, closure signatures or host credential/grant files.

UI and YAML use the same admission and resolution path; YAML is not an escape
hatch around the UI. The API must distinguish high-level selection from the
existing advanced explicit-artifact binding and reject ambiguous mixtures.
Omitted grants must not mean “all installed tools”. Retrying a denied or
ambiguous attempt must not invent a new execution ID or duplicate side effects.

The same-namespace runtime override and ordered tool list participate in the
frozen full-run identity. Operator CLI selection must exactly match the run's
names/revisions/order; every grant layer must approve the overridden runtime.
Empty `toolRefs: []` lends nothing. Missing/null lists and duplicate names refuse.

`POST /api/v1/runs` accepts the same `cellnSelection` object, `backend: celln`,
and explicit `provider: deepseek` plus `model: deepseek-chat`. It does not convert
the request into an OCI harness task or inherit Kubernetes model credentials.
Do not also supply the endpoint's legacy top-level `runtimeRef`; put a one-run
override inside `cellnSelection`. Explicit artifact blocks use the Kubernetes
AgentRun API, not this endpoint.

Without committed issuance the current controller reports
`CellnIssuanceCommitted=False` with `AwaitingIssuance` (or
`DispatcherNotConfigured`) and does not enter legacy execution. Operators can
use the [durable issuance CLI](celln-durable-issuance-cli.md) with matching
selection flags. This waiting condition is not selection readiness or an
automatic packaging/distribution service. Deploy the new controller before
creating these requests; older controllers cannot enforce the new waiting path.

Agent-level placement/tool defaults and conversational selection still need
explicit immutable/frozen identity semantics before exposure.
Celln conversations remain unsupported until the ADR's external checkpoint and
disposable-turn lifecycle is implemented and proven. Do not expose a chat
button that accidentally starts an OCI HarnessSession for a Celln selection.
