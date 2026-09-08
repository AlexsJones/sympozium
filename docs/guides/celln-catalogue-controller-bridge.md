# Catalogue controller bridge — integration in progress

The AgentRun reconciler has an explicitly injected `CatalogueDispatcher` path
for runs with saved `status.cellnIssuance`. Nil configuration refuses rather than
falling through to forge or OCI. This branch runs before mutable Agent/OCI task
normalization so previously issued work can still be observed without those
prerequisites. It does not yet create initial catalogue issuance or expose new
selection fields. Controller executable/Helm registration remains outstanding.

`cellnreview.RunDispatcher` consumes independently verified, committed issuance.
Its bounded per-Agent bindings contain an issuer, router and trusted model/grant
loader using an uncached reader. A frozen route is mandatory; the issuer and
router use separate configured credential paths. Operators must also provision
distinct scoped credential contents. The route names are not physical-host
attestation. Existing work resolves its binding from frozen history, not a later
mutable Agent reference.

For a pending run with a dispatch journal, recovery first looks up the original
execution. A returned record can be processed even when approval has since been
withdrawn. Only a 404 allows considering the identical request for submission;
other uncertain outcomes retain the journal and refuse to choose a new identity
or host. Router/dispatcher journals remain the replay boundary; losing their
storage is not a supported reset/retry operation.

Before submitting, the service requires a controller admission callback, checks
current policy/gates/token budget and the experimental Harness flag, revalidates
frozen catalogue/model approval, persists exact dispatch bytes, prewarms the
pinned serving process, then repeats admission and approval checks before one
submission. A prewarm observation is never authority. Poll and cancel do not
need fresh approval or the feature flag to remain enabled.

The controller refreshes status through its uncached reader and refuses to
overwrite a concurrently terminal/deleting/recreated run. It retains the issued
ID and uses the existing strict terminal receipt validation for results and
cleanup. `Cancelling` retains cleanup obligations. No Job or OCI task is created
on this branch.

Current tests separately cover the real TLS/KVM prewarm transport, HTTPS
submission/refusal behavior, service-level lookup/cancel after approval deletion,
and controller lifecycle using an injected service fixture. Those are not yet a
single deployed controller-to-model journey. Remaining work includes positive
service submission/404 tests, operator configuration/registration, initial
selection-to-issuance orchestration and deployed regression/real-model proof.
