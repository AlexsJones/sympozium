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
| Service credential and CA separation | [Live negative checks](../evidence/celln-service-credential-separation-2026-09-08.json): nine HTTP credential refusals and two CA refusals, then valid AI journey. Host-origin probes, not tenant NetworkPolicy. |
| Portable regressions | Full local Go race/build/vet passes recorded in PR history; CI verifies formatting, vet/build, short race tests, generation and Helm CRD synchronization. Hardware-skipped CI is not hardware proof. |

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
Earlier passing credential probes remain historical evidence, not proof this
transport path is reliable. Do not count the failed rerun as AI acceptance.

- [ ] Fix and reproduce Celln #96, then repeat live TLS refusal and AI checks.
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
