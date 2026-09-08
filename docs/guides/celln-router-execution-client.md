# Frozen-route execution transport

`RouterClient.Submit(ctx, route, issuedRequest)` sends the exact previously
verified and durably saved issuance bytes once, with the frozen backend pin.
The client copies its input, limits requests to 64 KiB and never remarshal-normalizes
the execution payload. The caller must complete current approval checks and the
durable dispatch hand-off first; this transport is not an authorization service.

`Lookup(ctx, route, id)` and `Cancel(ctx, route, id)` use the same frozen router
origin but omit the backend header. Polling/cancellation follow the router's
durable owner; they cannot select a replacement host. IDs are bounded ASCII path
components, and cancellation sends no body. `Cancelling` is a nonterminal phase:
the dispatcher still holds the reservation until its worker finishes cleanup.

Every operation uses the separately scoped rotating router token, verified TLS,
no ambient proxy, no redirects, no application retry and a 40-second ceiling.
Responses must be bounded uncompressed JSON, have no unknown wrapper fields or
trailing data, match the original request ID and use a known dispatcher phase.
Unusable HTTP/transport/protocol outcomes return `ErrExecutionOutcomeUnknown`;
HTTP refusal diagnostics expose only the status code, not arbitrary response
bodies. This error never permits regenerating an execution ID, changing request
bytes, rerouting or replaying work outside the router's ownership contract.

`RouterExecution` is bookkeeping, **not terminal receipt validation**. Its raw
receipt remains available for the controller to validate against the frozen
request before accepting output, terminal status, identity or cleanup. A
successful POST or cancellation acknowledgement is not proof of guest teardown.

HTTPS fixture tests cover exact bytes and target credential, owner-based lookup
and cancellation, `Cancelling`, lost responses, redirects, mismatched IDs,
unknown phases/fields, trailing/oversized responses, refusal-body redaction and
invalid path IDs. This transport is not yet called by the AgentRun reconciler;
these tests are not a deployed model-execution or receipt proof.
