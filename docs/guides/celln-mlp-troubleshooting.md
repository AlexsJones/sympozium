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
admission inputs. By default it exercises the Kubernetes/YAML deletion path
with the real controller, issuer, router and KVM. For the deployed API/browser
variant, also set `CELLN_LIVE_BROWSER_CANCEL=1` and the existing deployed API,
controller image and browser submission flags. The authorized model
credential stays in the host mapping, as in the success proof.

The test requires an issued AgentRun, its correlated nonterminal owner, a live
cell registry entry and a node reservation before issuing UID-bound Kubernetes
deletion or invoking the browser's run-list Delete action. Browser mode observes
the actual DELETE response (204), then independently verifies finalization and
the correlated cancelled receipt; it does not stub responses or count UI
disappearance alone as teardown.
The reservation count alone is insufficient: it includes admission/preparation.
The current owner phase can remain `Resolving` throughout synchronous Harness
execution; the live registry and matching terminal receipt establish which
cell was actually cancelled. Success requires that exact cell's `Cancelled`
receipt, a dissolution audit, completed Kubernetes finalizer and zero remaining
live cells or workload Jobs. A run finishing before cancellation fails this test
rather than being counted as a cancellation pass.

This is separate evidence from model-task success. Neither this mode nor local mote withdrawal proves
fleet-wide revocation or interruption recovery.

## Lost dispatch response integration mode

Set `CELLN_LIVE_LOST_RESPONSE=1` with automatic issuance and the existing explicit
live test inputs. This mode is separate from active cancellation. The real router
must accept the execution POST before the test TLS proxy discards its successful
response and returns an injected 503. The proxy then temporarily refuses owner
lookups until the Kubernetes run reports `ExecutionOutcomeUnconfirmed` with its
saved request identity. It never synthesizes a successful execution response.

After restoring connectivity, the test requires the original UID, request ID and
request bytes, exactly one execution POST through the proxy, and the ordinary
real-model two-tool result, correlated audit/receipt, cleanup and approval
withdrawal checks. A passing portable proxy test only validates fault injection;
it is not evidence for the controller/KVM journey. This scenario is response
loss with a surviving owner, not host/process loss or fleet failover. The
[isolated deployed browser variant](celln-mlp-kind-network.md) exercises this
same fault through API and controller Pods.

Add `CELLN_LIVE_RESTART_CONTROLLER=1` to the lost-response mode to kill and reap
the test-owned controller after uncertainty is persisted, then start a fresh
controller with the same explicit configuration before restoring observation.
With `CELLN_LIVE_CONTROLLER_IMAGE` set, the test instead deletes the owned
controller Pod using a UID precondition, after verifying its ReplicaSet belongs
to the proof deployment. It waits for the old Pod to disappear and a different
Pod from that ReplicaSet to become ready. `controller-pod-restart.json` records
both identities. Without that image flag, only the owned host process is
restarted. The issuer, router, dispatcher and Kind API stay alive in both modes.
The normal single-POST, unchanged-request, real-model and
cleanup assertions still apply. This tests recovery from durable Kubernetes
issuance/request state rather than the original controller's memory; it does
not prove dispatcher or serving-host restart recovery.

## Cancellation before issuance

New untouched Celln runs record `status.cellnOnly: true` before their finalizer
or any workload effects. The controller must read that record back before it
continues. This permits deletion while waiting for issuance without discovering
Job-sidecar cluster RBAC. The CRD rejects a subsequent change to another backend;
the controller also refuses such a change if it encounters inconsistent stored
state. Create a new run to change execution planes. The CRD prevents removal of
a true record, including removal of the entire status object. Only trusted
controller identities should be able to write run status.

Install the matching CRD before the controller. An old schema that prunes this
field causes the new controller to keep trying to record it without dispatching.
Do not downgrade to a controller that ignores this boundary while such runs
exist. Older runs are not retroactively marked: their mutable backend alone
cannot establish that they never created Job-side authority. Any recorded
pod-plane resources still require the existing cleanup path.

Unit tests cover record-before-finalizer ordering, backend-change refusal,
unissued finalizer completion without cluster-RBAC reads, and conservative
legacy/mixed-state cleanup. The [deployed early-cancellation proof](../evidence/celln-controller-pod-unissued-cancellation-2026-09-08.json)
also passed on the isolated Kind API with image/schema `f3e1f64`: browser-created
run, recorded boundary, waiting before issuance, real API refusal of boundary
removal/status removal/backend change, zero execution submissions or Jobs, and
automatic finalizer completion. Cancellation used Kubernetes deletion, not a
browser cancel button. Add `CELLN_LIVE_BROWSER_CANCEL=1` to the unissued browser
variant to exercise the real run-list Delete action instead of direct Kubernetes
deletion, retaining all boundary and finalization checks.
