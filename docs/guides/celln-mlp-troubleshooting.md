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
pending observation. Permission previews remain observations, not grants.

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
