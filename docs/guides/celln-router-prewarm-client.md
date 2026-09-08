# Verified router prewarm client

`cellnreview.NewRouterClient(origin, tokenFile, caFile)` uses verified TLS 1.3,
a rotating mounted router credential and optional dedicated CA trust. It shares
the issuer client's bounded transport implementation, but not its credential.
Ambient proxies and redirects are disabled. The origin must exactly match the
frozen operator route; the backend pin must be an HTTP origin matching Celln's
configured-backend contract. This is a library integration step; the AgentRun
reconciler does not call it yet.

`Prewarm(ctx, frozenRoute, verifiedIssuedRequest)` derives a member-check request
from the previously validated issuance, preserving exact mote/closure and resource
values while removing Harness, forge, inputs, egress, workspace and invocation
arguments. It posts at most 64 KiB to `/v1/artifacts/prewarm` with the frozen
`X-Celln-Backend`. It has a 40-second operation ceiling and no automatic retry.
The caller must validate current frozen approval before calling; this client
does not infer approval from arbitrary JSON.

A response must be bounded, uncompressed JSON with no unknown fields or trailing
data. The client checks the BLAKE3 hash of the actual request bytes, exact mote and
closure, versioned sealed-member report, process epoch, nonempty node identity,
sealed verification and explicit no-tool-execution/cell-dissolved assertions.
Responses claiming execution authority, functional conformance or artifact
readiness refuse. Hashing uses pinned `github.com/zeebo/blake3 v0.2.4` rather than
substituting SHA-256 for Celln's wire hash.

The resulting `PrewarmObservation` is **only a serving-process observation**.
It is not a lease or a guarantee that the process/host remains warm or eligible.
The node/epoch are reported over authenticated transport, not independently
attested physical-host identity. Member verification is not functional Harness
conformance. Do not set catalogue `CellnReady` from this result alone. Actual
execution still needs live host admission, unexpired grants and its own durable
submission/ownership handling.

Current HTTPS fixture tests cover exact target/credential/body handling, request
hash and closure substitution, missing/changed execution flags, teardown,
readiness claims, invalid epoch/node, oversized/trailing responses and redirects.
The opt-in real catalogue test additionally exercises this client through a TLS
terminator, the actual Celln router and a separate actual KVM dispatcher process.
Two prewarms bind the same request/process and fresh sealed-verification
challenges, with zero router ownership or dispatcher execution records. These
test-owned processes bind loopback and are stopped and joined on cleanup.
The fixture uses fake Kubernetes metadata; there is no task submission, model
call, deployed Kubernetes controller or final user-journey acceptance in this
proof. See `docs/evidence/celln-router-prewarm-client-2026-09-08.json`.
