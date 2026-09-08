# One-shot Harness-in-Celln troubleshooting

These are current MLP behaviors, not a claim that deployment qualification is
complete. A registered native JSON Harness and explicit approved borrowed tools
are required. OCI-only Harnesses and persistent Celln sessions remain unsupported.

The run detail page displays catalogue issuance and execution observations:

| Observation | Action |
| --- | --- |
| DispatcherNotConfigured | Ask the administrator to configure the catalogue controller binding for this Agent. |
| AwaitingRegisteredComposition | The exact ordered runtime/tool source combination needs preparation, host admission and registration. Catalogue metadata alone cannot run. |
| IssuanceNeedsAttention | Check current Agent/runtime/tool/model approvals and issuer configuration. The same run is retried; do not create a duplicate to bypass the failure. |
| ExecutionOutcomeUnconfirmed | Check the pinned router/host and its original execution owner. A lost response does not mean the task never ran. Do not resubmit or switch hosts to work around it. |
| OwnerRecordObserved | The configured owner returned a record for the exact request. Use the run phase/result to determine completion; this is not a fleet readiness signal. |

Controller logs retain detailed errors; user-visible observations intentionally
exclude privileged issuer details, credentials and policy document contents.
Conditions do not weaken admission, change durable request bytes, create a new
execution ID or authorize rerouting. A terminal run is not changed by a stale
pending or running observation. A running owner's lookup failure changes the
execution observation to Unknown without changing the run phase or saved request.
A subsequent correlated owner response clears that warning; repeated identical
failures do not continually rewrite status. Permission previews remain
observations, not grants.

For an operator-admission failure, keep the failed candidate and inspect its
template/kernel/pilot compatibility. A sealed-member refusal must not be bypassed
by editing the allowlist. The live MLP proof passed with its explicitly pinned
7.1.13 kernel; a 6.19.10 attempt refused member verification before model use.
That is evidence for the tested combination, not broad kernel compatibility.

Removing host mote admission prevents new use but does not stop an active cell.
Withdraw relevant model/tool approval, cancel the AgentRun through its existing
owner, and wait for terminal teardown. Unknown owner outcomes require recovery,
not a replacement execution. Full deployed cancellation/loss qualification is
still an MLP acceptance item.

## Active-cancellation integration mode

Run the existing isolated live catalogue test with `CELLN_LIVE_CANCEL_ACTIVE=1`
and `CELLN_LIVE_AUTOMATIC_ISSUANCE=1`, using the same explicit pinned operator
admission inputs. Do not combine this mode with HTTP/browser submission: it
exercises the Kubernetes/YAML deletion path with the real host controller,
issuer, router and KVM, not browser result rendering. The authorized model
credential stays in the host mapping, as in the success proof.

The test requires an issued AgentRun, its correlated nonterminal owner, a live
cell registry entry and a node reservation before issuing UID-bound deletion.
The reservation count alone is insufficient: it includes admission/preparation.
The current owner phase can remain `Resolving` throughout synchronous Harness
execution; the live registry and matching terminal receipt establish which
cell was actually cancelled. Success requires that exact cell's `Cancelled`
receipt, a dissolution audit, completed Kubernetes finalizer and zero remaining
live cells or workload Jobs. A run finishing before cancellation fails this test
rather than being counted as a cancellation pass.

This is separate evidence from model-task success and from deployed
API/browser cancellation. Neither this mode nor local mote withdrawal proves
fleet-wide revocation or interruption recovery.
