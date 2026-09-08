# Catalogue controller bridge — integration in progress

The AgentRun reconciler has an explicitly injected `CatalogueDispatcher` path
for runs with saved `status.cellnIssuance`. Nil configuration refuses rather than
falling through to forge or OCI. This branch runs before mutable Agent/OCI task
normalization so previously issued work can still be observed without those
prerequisites. It does not yet create initial catalogue issuance or expose new
selection fields. Controller executable/Helm registration is explicit and disabled
by default; see the operator configuration below.

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

Current tests separately cover real TLS/KVM prewarm, HTTPS submission/refusal,
first submission and 404 recovery, unavailable owners, both admission gates,
approval withdrawal during prewarm, lost submission acknowledgement without
replay, service lookup/cancel after approval deletion, and controller lifecycle
through the full Reconcile entry point using an injected service fixture. Recovery
adds the cleanup finalizer without creating a Job, and deletion retains it until
terminal cancellation. Config refusal and default/legacy/catalogue
Helm renders are also tested. Those are not yet a single deployed
controller-to-model journey. Initial selection-to-issuance orchestration and
deployed regression/real-model proof remain outstanding.

## Operator configuration

Set `CELLN_CATALOGUE_CONFIG` to an absolute JSON file path in the controller.
Invalid/oversized/unknown-field configuration fails startup. The file is read
once; restart the controller to reload bindings or CA bundles. Token file
contents are read per operation. Configuration loading makes no network calls
and does not provision a run or assert readiness.

Example `config.json` (replace all example names with approved deployment values):

```json
{
  "apiVersion": "sympozium.ai/celln-catalogue-controller-v1",
  "bindings": [{
    "agent": {"namespace": "tenant", "name": "my-agent"},
    "issuer": {
      "url": "https://issuer-host-a.example.internal",
      "tokenFile": "/etc/sympozium/celln-catalogue/issuer-token",
      "caFile": "/etc/sympozium/celln-catalogue/issuer-ca.pem"
    },
    "router": {
      "url": "https://router.example.internal",
      "tokenFile": "/etc/sympozium/celln-catalogue/router-token",
      "caFile": "/etc/sympozium/celln-catalogue/router-ca.pem"
    },
    "backend": "http://host-a:8787",
    "operatorSource": {"namespace": "operators", "name": "operator-grants"},
    "runtimeSource": {"namespace": "operators", "name": "runtime-grants"},
    "agentSource": {"namespace": "operators", "name": "agent-grants"},
    "modelSource": {"namespace": "operators", "name": "model-policy"}
  }]
}
```

Bindings must be unique per namespaced Agent and specify four distinct authority
sources. Protect these sources and the configuration Secret from tenant writes;
configuration names alone do not prove ownership. The controller needs read
access to the sources and status-write access to the run, not provider Secrets.
The issuer retains host provider credential mapping and grant issuance authority.

With the existing complete Celln router deployment values configured, Helm adds:

```yaml
celln:
  enabled: true
  catalogueConfigSecret: celln-catalogue-controller
  harnessEnabled: true
```

Create that Secret in the controller namespace with `config.json` and the token/CA
files referenced above. It is mounted read-only at
`/etc/sympozium/celln-catalogue`; the chart generates no credentials. The router
token must match the configured router's execution credential, and issuer token
must match its host service credential. The existing legacy/discovery Secret and
router ownership/transport requirements still apply; these three values alone
are not a complete Celln installation. Disabling `harnessEnabled` refuses new
submission while leaving configured recovery/cancellation available. Removing
the configuration or retargeting endpoints while runs are active prevents their
recovery and is not a safe drain operation.
