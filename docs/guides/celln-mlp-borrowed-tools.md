# One-shot borrowed-tool handoff

This is the administrator-assisted MLP workflow for [epic #426](https://github.com/sympozium-ai/sympozium/issues/426).
It is not unattended upload-to-execution or arbitrary OCI Harness support.
The [acceptance index](celln-mlp-acceptance.md) records tested revisions and open
installation gates; [installation](celln-mlp-installation.md) is a prerequisite.

## What the administrator supplies

Before inviting users to run the example, hand over the tenant namespace, Agent
name, supported native Harness name, exact reviewed tool names/revisions, allowed
model, effective permission limits and support contact. Keep a separate immutable
record of source revisions, publisher keys, closure/mote/template hashes, binary
and image digests, and the reviewed installation evidence. Catalogue names alone
do not pin executable bytes; the controller resolves and freezes the current full
identities, and issuance checks them against independent approvals.

For each BYO tool:

1. Review behavior, schemas, declared effects/limits and signed provenance. Stage
   the bundle under operator control; never execute a submitted tool on the host
   merely to inspect it. Use [inspect and approve](celln-tool-review.md) with the
   exact submission UID and full-spec digest. This publishes metadata, not grants.
2. Configure independent operator/runtime/Agent grants and separate model policy
   for the exact subjects. Submitters must not write these sources. Model keys
   remain in the host credential mapping, not the AgentRun or browser.
3. Compose the exact ordered runtime/tool sources, prepare the pinned mote, and
   explicitly admit its exact hash using the actual host member check. Follow
   [installation steps 2–3](celln-mlp-installation.md#2-prepare-and-admit-the-harnesstool-composition)
   and the Celln pinned-mote procedure. A signature/member check is not behavioral
   conformance; the administrator must verify the supported tool ABI too.
4. Register the resulting source sequence and artifacts in the controller binding
   as described in [registered issuance](celln-registered-issuance.md), then reload
   the controller configuration. Registration does not replace independent grants
   or host admission. Do not alter a saved run identity when changing registration.

## User YAML and UI

Copy [the one-shot example](../../examples/celln-harness-one-shot.yaml) and replace
the Agent/runtime/tool references with the approved names above. The example's
`uppercase-v1` and `length-v1` are illustrative catalogue names, not automatically
installed or trusted artifacts. Adjust the task if your tools use different names.
Leave `cellnSelection.toolRefs` explicit and ordered; `[]` lends no tools.
Use the configured `agentId` for that Agent and an appropriate `sessionKey` for
correlation. A session key does not enable persistent Harness-in-Celln chat.

First validate the edited file using the intended API and namespace:

```sh
kubectl --kubeconfig /absolute/tenant/kubeconfig --context YOUR_CONTEXT \
  --namespace YOUR_TENANT create --dry-run=server \
  -f examples/celln-harness-one-shot.yaml
```

Dry-run validates the admitted object shape, not executable readiness. Removing
`--dry-run=server` creates a real task, potentially invokes the model/tools and
incurs cost. Save the returned run name and UID. Do not repeatedly create new runs
to recover an uncertain response: reconcile the original run first.

In Runs → New Run, choose the Agent, inherit or override its Harness, select Celln,
then select the same ordered tool revisions and explicit model. Read the effective
permission preview and any unavailable/blocking state before requesting the run.
An incompatible OCI-only Harness must remain blocked, without backend fallback.

## Observe, cancel and withdraw

Inspect the run's issuance condition, phase and result. An unregistered selection
waits for operator preparation; it must not fall back to forge or a Kubernetes Job.
For the example, verify the final answer and correlated host audit/receipt for the
two borrowed calls. Tool transcript entries are not individually hardware-signed
receipts. Use [troubleshooting](celln-mlp-troubleshooting.md) for uncertain outcomes.

The UI's Delete action requests cancellation/finalization and eventually removes
the run record; host receipt retention is separate. Verify terminal teardown rather
than assuming a successful DELETE means the cell already stopped. An administrator
withdraws approval/admission using the documented protocols and verifies further
issuance is refused. Removing a registration alone is not revocation, and removing
mote admission alone does not cancel an active cell.

This workflow remains bounded and one-shot. Conversational checkpoints, arbitrary
Harness compatibility, automatic fleet distribution and fleet-wide live withdrawal
remain separate parts of the full epic.
