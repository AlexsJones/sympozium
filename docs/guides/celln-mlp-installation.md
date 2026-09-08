# One-shot Harness-in-Celln: MLP installation checklist

This is the installation entry point for the administrator-assisted MLP. The
native JSON Harness/browser/operator-admission journey has passed with an
authenticated API-server pod in isolated Kind and host-process controller,
issuer and router. **The complete deployment described here still needs
qualification; it is not a GA or arbitrary-Harness compatibility claim.**

The supported scope is a bounded one-shot native JSON Harness with explicitly
lent approved tools and host-mediated DeepSeek access. Persistent conversations,
arbitrary OCI Harnesses, automatic fleet distribution and live fleet revocation
are not part of this first delivery. Never enable insecure transport to make an
incomplete installation appear functional.

## 1. Pin the components and choose the serving host

Use a reviewed Celln build containing PR #95 and a Sympozium build containing
catalogue selection, automatic issuance and permission previews. Build the MLP
integration branch when testing its installation/status improvements. Record
source revisions, image digests, kernel/pilot hashes and the exact host in the
installation record; do not rely on mutable `latest` tags.

The serving host requires Linux/KVM, a compatible pinned kernel/pilot template,
host-owned stores and policy, and local filesystem lock/rename/fsync semantics.
The tested 7.1.13 kernel/pilot combination is evidence, not a generic kernel
support promise. Missing hardware or failed member checks must refuse.

Follow [router deployment](celln-router-deployment.md) for authenticated ingress,
separate discovery/execution/backend credentials, TLS and network restrictions.
Use one explicitly pinned serving host for the first installation. Configure
durable ownership and issuer journal paths; do not point the route at a load
balancer that silently substitutes another host after an ambiguous request.

## 2. Prepare and admit the Harness/tool composition

Follow [runtime profiles](celln-runtime-profile.md), [tool catalogue metadata](celln-tool-catalogue.md),
[tool review](celln-tool-review.md) and [independent tool authority](celln-tool-authority.md).
An administrator reviews BYO tool artifacts and their signed sources; adding a
CellnTool object does not approve executable bytes. Tenant writes must not reach
operator/runtime/Agent grant ConfigMaps or host policies.

Compose the exact ordered runtime/tool sources, then use Celln's
`docs/PINNED_MOTE_PREPARATION.md` workflow on the selected host:
`closure prepare-mote`, optional `closure check-prepared`, and explicit
`closure admit-prepared --approve-mote <reviewed exact hash>`. The admission
command always performs its own hardware check; a supplied report is not a
token. Retain the template, source identities, preparation and admission
evidence. Administrator functional review of the runtime and tool interfaces
remains necessary; sealed member identity alone does not prove useful behavior.

## 3. Configure model authority and catalogue control

Follow [model authority](celln-model-authority.md), [issuer service](celln-issuer-service.md)
and [controller bridge](celln-catalogue-controller-bridge.md). Model credentials
remain in the host credential mapping, not the guest or AgentRun. The issuer
has its own trusted per-Agent approval bindings and durable lifecycle journal.

Register the admitted mote/closure with the exact source sequence using
[registered issuance](celln-registered-issuance.md). Registration is only artifact
lookup: it is not admission, approval or withdrawal. Preserve the original saved
route/request when recovering Prepared/Issued runs.

## 4. Wire the Kubernetes deployment

The relevant Helm values name **existing administrator-owned objects**:

| Value | Consumer and contents |
| --- | --- |
| `celln.catalogueConfigSecret` | Controller: `config.json`, distinct issuer/router credentials and CA files referenced by that config. |
| `celln.permissionPreviewConfigMap` | API server: only `config.json`, containing trusted grant-source locations. No issuer credentials. |
| `celln.capabilityTokenSecret` | API server: read-only discovery token, separate from execution authority. |
| `celln.tokenSecret` | Legacy execution client credential where that path is enabled; do not use it as a discovery token. |
| `celln.harnessEnabled` | Explicit new Harness-submission switch; set true only after host/configuration checks pass. |

Enable `celln.enabled` for controller execution and configure its authenticated
router URL. Keep TLS verification enabled. The catalogue config's issuer/router
URLs and CA files must match the actual deployed endpoints, not fixture loopback
addresses. Neither Helm nor a tenant run generates admission authority.

Create the preview ConfigMap in the API-server namespace with the schema in
[permission preview](celln-permission-preview.md). The chart mounts only
`config.json` read-only. Changing bindings requires restarting the corresponding
API server/controller; grant document changes are read on subsequent checks.
Provide only required source-read RBAC and deny tenant approval writes.

Render and server-dry-run manifests against an explicitly selected test cluster
before deployment. A successful render/dry-run proves neither endpoint access
nor authentication, KVM, capacity, admission or model execution.

## 5. Installation acceptance

Before declaring this installation usable, exercise its real UI and YAML paths:

- Select the native Harness, Celln and two explicitly approved tools; inspect
  effective permissions. An empty list lends no tools.
- Complete a real model task using both tools and verify the result in a fresh
  browser visit, correlated request/audit/receipt identities, and resource cleanup.
- Refuse an unapproved tool, changed publisher/artifact, incompatible template,
  missing authority, wrong credential and forbidden egress without creating a
  replacement execution or leaking model credentials.
- Interrupt connectivity and verify the original run remains recoverable without
  replay or host substitution. Verify its actionable status in the UI.
- Cancel an active run, wait for terminal teardown, then withdraw approval and
  verify new use refuses. Removing mote admission alone does not cancel cells.
- Recheck existing Job/Harness behavior and confirm restart/upgrade instructions
  preserve journals and ownership.

Use [MLP troubleshooting](celln-mlp-troubleshooting.md) for status meanings. Keep
these acceptance results distinct from the existing loopback fixture evidence.
Until they pass on the deployed topology, the MLP installation gate remains open.

### Isolated deployed API/browser proof

The live catalogue integration test has an opt-in deployed API mode. Build the
actual `images/apiserver/Dockerfile` from the tested source revision, use a unique
`localhost/sympozium-celln-api:<revision>` tag, and load that image into the
explicit `celln-deployed` Kind cluster. Set `CELLN_LIVE_APISERVER_IMAGE` to that
tag alongside `CELLN_LIVE_BROWSER_SUBMISSION=1` and the existing explicit live
test inputs. The pod uses `imagePullPolicy: Never`; there is no remote-image or
loopback-server fallback if the selected image cannot start.

Check which container provider owns the cluster before building or loading.
The development `celln-deployed` cluster used for these proofs is rootless
Podman, not Docker. For that cluster, export and load the reviewed image with
an explicit archive (set `CELLN_API_IMAGE` to the reviewed local tag first):

```sh
image_archive_dir=$(mktemp -d /tmp/celln-mlp-image.XXXXXX)
podman save --format docker-archive --output "$image_archive_dir/apiserver.tar" \
  "$CELLN_API_IMAGE"
KIND_EXPERIMENTAL_PROVIDER=podman kind load image-archive \
  "$image_archive_dir/apiserver.tar" --name celln-deployed
```

On this host, Kind's `load docker-image` lookup failed despite the image being
present in Podman; the explicit archive avoids that image-discovery path.
Build with `podman build -f images/apiserver/Dockerfile -t <image> <context>`
using a clean archive of the reviewed Git revision as the context. Do not include
untracked host credentials or live-test stores in the image build context. A
Docker socket permission failure does not diagnose a Podman-backed Kind cluster.

This mode creates a separate API Deployment, ClusterIP Service, service account,
UI-token Secret and preview ConfigMap in the test's private namespace. It never
replaces the installed API server. It forwards only to `127.0.0.1`, verifies
missing/wrong bearer tokens refuse, then uses the real UI to submit and inspect
the run. The ephemeral fixture UI token is public test data, not a production
credential; use this mode only in the isolated test cluster. No model, issuer or
router credential is mounted in this API pod.

The current API cache needs cluster-wide list/watch permission for the selected
catalogue/run resource types, SympoziumConfig pricing enrichment and node/namespace/
Pod discovery (the density poller watches Pods). Test RBAC gives only
those read permissions; AgentRun writes and the three named approval ConfigMap
reads remain scoped to the private namespace. It grants no Secret read or
approval write permission. Cluster-scoped test RBAC is deleted by UID as well as
namespace cleanup. API deployment alone does not qualify the host-process
controller/issuer/router fixture as a fully deployed control plane. Record live
results separately; the portable deployment-boundary test is not live evidence.

The [2026-09-08 deployed API/browser evidence](../evidence/celln-deployed-api-browser-2026-09-08.json)
records the image ID, actual UI selection/permission preview and fresh result
visit, bearer refusal checks, real DeepSeek two-tool execution and cleanup.
It does not qualify a fully deployed controller/issuer/router or production
authentication and tenant isolation beyond the declared test permissions.
