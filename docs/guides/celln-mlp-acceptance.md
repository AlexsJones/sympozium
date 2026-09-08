# One-shot Harness-in-Celln MLP acceptance index

This index separates proven integration slices from remaining release gates for
[epic #426](https://github.com/sympozium-ai/sympozium/issues/426). The consolidated
implementation is [draft PR #463](https://github.com/sympozium-ai/sympozium/pull/463),
not a merged or release-qualified product. Historical evidence records retain
their original image revisions and limitations.

## Delivery scope

A user selects the supported native JSON Harness, Celln execution, and an explicit
ordered set of approved borrowed tools. BYO artifacts require administrator review,
signed composition and host admission. Execution is a bounded one-shot task;
credentials remain host-side. Conversations, arbitrary OCI/Pi/Hermes compatibility,
automated fleet distribution and live fleet revocation remain later iterations
of the full epic, not completed requirements or silent fallbacks.

## Evidence available

| Requirement | Current evidence and limit |
| --- | --- |
| UI selection, effective permission preview, actual two-tool model result | [API/controller Pod browser proof](../evidence/celln-controller-pod-browser-2026-09-08.json): real KVM/DeepSeek, fresh browser result. Temporary isolated host topology. |
| Administrator admission, exact source composition and local withdrawal | [Operator-admitted browser proof](../evidence/celln-operator-admitted-browser-2026-09-08.json), with Celln PR #95. Not unattended BYO admission or fleet distribution. |
| Cancellation before issuance | [Boundary-aware early cancellation](../evidence/celln-controller-pod-unissued-cancellation-2026-09-08.json): persisted boundary, real API refusals, no execution submission, completed finalization. |
| Browser cancellation of waiting and live runs | [Browser cancellation](../evidence/celln-browser-cancellation-2026-09-08.json): real Delete action, matched cancelled-cell receipt, dissolution, no live cells/Jobs. Delete removes the run record; receipt retention is host-side. |
| Ambiguous accepted-response recovery | [Deployed recovery](../evidence/celln-controller-pod-recovery-2026-09-08.json): unchanged saved identity, exactly one submission, real model result. Surviving execution owner. |
| Controller replacement | [Controller-Pod replacement](../evidence/celln-controller-pod-restart-2026-09-08.json): different ready Pod recovers original request, no replay. Host services survive. |
| Issuer process recovery | [Issuer restart](../evidence/celln-issuer-restart-2026-09-08.json): unchanged profile/journal bytes and subsequent withdrawal/refusal. Same boot, after issuance, not systemd qualification. |
| Dedicated read-only issuer Kubernetes identity | [Restricted-identity journey and restart](../evidence/celln-issuer-kubernetes-identity-2026-09-08.json): four named approval reads, eight actual Forbidden responses, real browser/KVM/DeepSeek result and withdrawal after issuer restart. Short-lived test identity, not production renewal or installed sandbox. |
| Service credential and CA separation | [Live negative checks](../evidence/celln-service-credential-separation-2026-09-08.json): nine HTTP credential refusals and two CA refusals, then valid AI journey. Host-origin probes, not tenant NetworkPolicy. |
| Authenticated malformed-request refusal | [Ten actual service refusals](../evidence/celln-authenticated-request-refusals-2026-09-08.json): exact 400/413 responses with unchanged issuer/profile/owner files after each request, followed by valid real AI and cleanup. Not guest flooding, tenant networking or the complete negative matrix. |
| Valid-shaped authority substitution | [Ten issuer authority refusals](../evidence/celln-authority-substitution-refusals-2026-09-08.json): single-field changes to real approved tenant identities, approval sources, credential profile, model URL and budget all return 409 without authority/owner file changes; normal real AI and cleanup still pass. Not exhaustive coordinated forgery or tenant network isolation. |
| Tenant-Pod egress to host services | [Calico before/deny/after proof](../evidence/celln-tenant-host-network-2026-09-08.json): credential-free non-root Pods copying controller labels reach both live host endpoints before policy, time out under namespace egress restriction, and reach them after removal; subsequent AI/cleanup pass. Not a production firewall or additive-policy bypass guarantee. |
| Portable regressions | Full local Go race/build/vet passes recorded in PR history; CI verifies formatting, vet/build, short race tests, generation and Helm CRD synchronization. Hardware-skipped CI is not hardware proof. |

The latest [portable regression record](../evidence/celln-mlp-portable-regression-2026-09-08.json)
covers application source `ad2255c`: full Go race/build/vet, web build and five
browser selection contract checks. The initial browser assertion failed because
the incompatible-Harness alert was below the scrollable dialog fold; the corrected
test scrolls to the alert while preserving visibility/disabled-submit assertions.
Deployed Job/sandbox/Harness regressions are still open. The sandbox script now
requires explicit test targeting, checks controller/CRD prerequisites read-only,
and removes broad sandbox cleanup. Its command-stand-in safety tests pass; this
does not establish deployed lifecycle behavior. The checked isolated cluster lacks
sandbox CRDs and its existing proof controller has sandbox support disabled.
See [sandbox test setup](writing-integration-tests.md#sandbox-regression-targeting).

The cleanup exemption was tightened during review in `35b45cd`: only an immutable
Celln-only boundary recorded on an untouched original-generation run qualifies.
Legacy action/request identity alone is insufficient. Legacy/mixed-state runs
require conservative cleanup and appropriately authorized controllers. See
[troubleshooting](celln-mlp-troubleshooting.md) before upgrading or migrating runs.

## Remaining release gates

Latest review revalidation on controller `35b45cd`: CI passed and deployed
browser early-cancellation passed. The subsequent full success journey failed
before dispatch because an early router auth refusal surfaced as proxy HTTP 502
with unexpected EOF. See [Celln #96](https://github.com/sympozium-ai/celln/issues/96).
That failed run remains failed. The bounded transport fix in
[Celln PR #97](https://github.com/sympozium-ai/celln/pull/97) subsequently passed
the reproducing raw TCP test, 96 complete refusals through a real TLS proxy and
the full browser/KVM/DeepSeek journey with cleanup. See the
[fix acceptance record](../evidence/celln-router-refusal-fix-2026-09-08.json).
Celln PR #97 is merged as `c83be719872c97983c1cba307246503425a5a193`;
its CI check passed. The CI hardware job explicitly skipped for missing KVM;
the actual hardware/model evidence is the separate local acceptance run.

- [x] Review and merge Celln PR #97 after local reproduction, corrected TLS
  refusal tests, real AI rerun and green CI. Issue #96 is closed.
- [ ] Qualify the persistent installed issuer/router/dispatcher layout, actual
  systemd sandbox, fixed service identity, certificates, least-privilege issuer
  kubeconfig and durable storage. The checked-in unit has syntax checks only.
- [ ] Qualify serving-host restart/upgrade/recovery and consistent journal/store
  retention. Same-boot issuer or controller replacement is not host reboot.
- [ ] Finish negative tenant/approval/network/guest checks against the final
  installed journey, including cross-namespace authority, authenticated malformed
  requests, forbidden egress and exhausted budgets. Do not substitute host-side
  assertions for guest restriction attempts.
- [ ] Run the required deployed Job/sandbox/Harness regressions on the final
  installation; retain exact supported/unsupported cases and skipped hardware.
- [ ] Finalize pinned install/BYO examples, operator handoff and acceptance
  evidence, then complete review/merge of the consolidated PR.

Host qualification currently needs administrator setup: the checked workstation
has no dedicated `celln` service account, and non-interactive sudo requires a
password. No root-owned service was installed or enabled, and no weaker user
service was treated as equivalent evidence. The requested host/setup choice is
pending; independent review and security work can continue.

Use [installation](celln-mlp-installation.md), [systemd procedure](celln-issuer-systemd.md)
and [reproducible isolated modes](celln-mlp-kind-network.md) for the concrete
steps and limits. Keep the epic open until its full remaining requirements—not
just this first one-shot delivery—are implemented and proven.
