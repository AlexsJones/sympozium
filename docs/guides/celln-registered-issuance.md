# Automatic issuance for registered catalogue compositions

The catalogue controller can issue a named `cellnSelection` without an operator
invoking `issue-run` for every AgentRun. This works only for exact compositions
already materialized and admitted on the configured host. It is not an automatic
compositor, admission service or distribution system.

Add `compositions` to the relevant Agent binding in `CELLN_CATALOGUE_CONFIG`:

```json
{
  "compositions": [{
    "sources": ["blake3:RUNTIME_SOURCE", "blake3:FIRST_TOOL_SOURCE", "blake3:SECOND_TOOL_SOURCE"],
    "imageBytes": 33554432,
    "artifacts": {
      "mote": {"hash": "blake3:ADMITTED_MOTE"},
      "closure": {"hash": "blake3:COMPOSED_CLOSURE"}
    }
  }]
}
```

This is a binding fragment, not a complete config. Replace every placeholder
with a full BLAKE3 identity. Source order must exactly match the composition
plan, including the runtime source. The constructor rejects ambiguous duplicate
sequences, malformed hashes, more than 128 entries per Agent, and image sizes
outside 32–512 MiB or not 2 MiB aligned. Configuration is copied at startup;
restart the controller after changes.

## Authority and recovery

The controller resolves the run's ordered same-namespace intent, checks current
independent approvals and admission gates, and matches the registered source
sequence. Registration supplies artifact locations, not authority: the issuer
still verifies the signed composition, admitted mote, model policy and host
credential mapping. No signing keys, host paths, grant-source configuration or
router credentials come from the user's run.

`Prepared` is saved before contacting the issuer. The verified result becomes
`Issued`, then the existing pinned prewarm/submission lifecycle continues.
Interrupted preparation resumes the saved request and route, without choosing
another registration or regenerating identity. Current approval/admission
checks still apply. Removing a registration prevents new preparation through
that entry; it does not revoke a saved request or grant. Use independent policy
and host withdrawal mechanisms for revocation, not registry edits. Saved
Prepared requests remain recoverable after registration removal.

An unmatched run remains unsubmitted with `CellnIssuanceCommitted=False`, reason
`AwaitingRegisteredComposition`. Omitting registrations disables automatic
initial issuance; the operator CLI remains available. Harness enablement and
controller admission gates are required before issuance and submission.

## Proof and limits

Set `CELLN_LIVE_HTTP_SUBMISSION=1` as well to create the execution run through
the actual HTTP API handler backed by the isolated Kubernetes API. This variant
replaces the unissued setup run before starting the controller and freezes the
new persisted identity/spec without patching the API output. It records
`http-created-run.json`. The loopback test server deliberately has no API auth;
this proves HTTP request translation and automatic execution, not browser,
deployed authentication, or production topology acceptance.

Set `CELLN_LIVE_AUTOMATIC_ISSUANCE=1` when running the opt-in
`test/integration/test-celln-catalogue-harness.sh`. The test creates a named
selection and registers public fixture artifacts, skips the issuance CLI, and
requires the actual controller to issue and execute through TLS issuer/router
and KVM. It checks the real DeepSeek two-tool answer, audit/receipt correlation,
cleanup and withdrawal refusal.

This proves registered automatic issuance/execution. The fixture materializer
is not production admission. On-demand packaging/distribution, selection-specific
readiness, live browser-to-model, conversations and fleet qualification remain
separate release gates.
