# Isolated Kind pod-to-host proof: network boundary

The controller-Pod/browser-to-host proof now passes in this isolated topology;
it is not a production network recipe. Keep the explicit `kind-celln-deployed` context;
do not use the ambient/default cluster or bind services to the host LAN.

The development cluster is owned by rootless Podman. On 2026-09-08 its private
network namespace had the `kind` gateway `10.89.0.1`, with Kind nodes on the
same private bridge. The ordinary host namespace had no interface at that
address. Inspect the current provider/network before reusing these addresses.

A credential-free TCP diagnostic established these facts:

- A node request to `host.containers.internal` (`169.254.1.2` here) could not
  connect to a server bound only to the ordinary host's `127.0.0.1`.
- A listener bound to `10.89.0.1` under `podman unshare --rootless-netns` received
  the actual node's `/mlp-private-network-probe` request. It deliberately sent no
  HTTP response, so curl reported an empty reply; the listener received the
  request bytes. The diagnostic listener exited afterward.
- This establishes node-to-private-gateway TCP only. It does not prove Pod
  networking, TLS authentication, Kubernetes approval reads or KVM execution in
  that namespace. Those must pass together before deployment is claimed.

The passing controller image is `localhost/sympozium-celln-controller:0cd9668`,
built image ID `3dd10b2d9f304cb461e04ddb8233adbc49d7b560bf944861af25e7b4ae00c253`.
It was built from the committed source archive and loaded into the isolated
cluster with the Podman image-archive workflow in the MLP installation guide.
Loading an image is not a controller rollout or an execution proof.

The private-gateway proof retains verified TLS on the controller's
issuer/router connections. Certificates must name their actual reachable IP or
DNS endpoint and use fresh private keys, not Go's publicly known httptest key.
The integration helper now generates independent, short-lived certificates and
independent issuer/router/backend tokens for each run. Never solve a name or
connectivity error with skip-verification, public fixture execution credentials,
or an unreviewed host-LAN listener.

Moving a host service into the private network namespace also changes its
Kubernetes API route. Its explicit kubeconfig must still verify the API server
certificate and use the intended Kind cluster. Do not assume the outer-host
loopback API URL remains reachable, or modify the user's original kubeconfig.
The standalone issuer must retain only the required approval-read authority;
provider credentials and the admitted store stay out of controller/API Pods.

## Controller watch scope

The controller accepts `--watch-namespace=<proof-namespace>` for a single
namespaced cache scope. Empty/default retains cluster-wide behavior; malformed
names, wildcards and comma-separated lists refuse startup. In scoped mode its
leader-election Lease also uses that namespace. The older `e2edef1` image
predates this flag and cannot be used for the scoped controller proof.

This is **not tenant authorization**: cluster-scoped resources are still
cluster-scoped, and uncached/direct clients can access separately configured
approval sources if RBAC allows it. Pair the flag with a service account whose
write permissions are restricted to the proof namespace and whose approval
reads are restricted to the configured objects. Do not grant cluster-wide
writes just because the informer cache is scoped. Features expecting other
namespaces may need separate readers/configuration; scoped mode is not a claim
that every cross-namespace platform feature has been qualified.

## Running the controller-Pod variant

Keep the original kubeconfig untouched. Create a private copy for the test
network namespace, and change only that copy's endpoint/name verification:

```sh
kube_proof_dir=$(mktemp -d /tmp/celln-private-kube.XXXXXX)
cp "$CELLN_CONTROLLER_KUBECONFIG" "$kube_proof_dir/kubeconfig"
chmod 600 "$kube_proof_dir/kubeconfig"
kubectl --kubeconfig "$kube_proof_dir/kubeconfig" config set-cluster \
  kind-celln-deployed --server=https://10.89.0.2:6443 --tls-server-name=kubernetes
podman unshare --rootless-netns kubectl --kubeconfig "$kube_proof_dir/kubeconfig" \
  --context kind-celln-deployed get nodes
```

These addresses were verified for the recorded cluster, not discovered as a
production default. The original CA/client credentials remain in the copy;
verification is not disabled. Confirm the provider and addresses before use.

Run `test/integration/test-celln-catalogue-harness.sh` under
`podman unshare --rootless-netns env`, with the existing explicit pinned
operator-admission/model/browser inputs and these additional settings:

- `CELLN_CONTROLLER_KUBECONFIG`: the private copy above.
- `CELLN_LIVE_CONTROLLER_IMAGE`: the loaded reviewed controller image.
- `CELLN_LIVE_ISSUER_PROCESS=1` and `CELLN_LIVE_AUTOMATIC_ISSUANCE=1`.

Issuer and router TLS listeners bind only the private gateway. Their certificates
name that IP. The router's backend remains loopback within the same private
network namespace; the controller does not connect to the backend directly.
No provider key, host store, backend token or TLS private key enters the controller
Pod. Its dedicated Secret holds only its config, separate issuer/router tokens
and CA files. The API Pod does not mount that Secret.

The controller gets namespace-only read/watch permissions (including ConfigMaps
needed by registered controllers), AgentRun patch/status/finalizer permission,
and Agent/AgentRuntime status writes. It gets no Secret API reads, approval
writes, Job creation or cluster-wide RBAC. Other registered platform controllers
can log permission refusals; this restricted fixture is not a general-purpose
Sympozium controller installation.

The [passing evidence](../evidence/celln-controller-pod-browser-2026-09-08.json)
includes real browser selection/results, host KVM/model execution, withdrawal,
finalizer completion and zero live cells/Jobs. An earlier run completed the AI
task but failed cleanup by entering Job-sidecar ClusterRole discovery. The fix
skips that path only for recorded Celln-only executions with no pod-plane state;
legacy/mixed Job cleanup stays intact. That failed run required verified manual
test-finalizer release and is not counted as a pass. Subsequent review tightened
the cleanup exemption further: only the immutable Celln-only boundary recorded
on an untouched original-generation run qualifies. A legacy action/request alone
cannot exclude earlier Job-side authority, so legacy runs retain conservative
cluster-RBAC cleanup and need a suitably authorized controller.

Read `test-outcome.json` for the result including registered cleanup;
`summary.json` now describes execution checks only.

Further isolated controller-Pod cases passed:

- [Active cancellation](../evidence/celln-controller-pod-cancellation-2026-09-08.json):
  use `CELLN_LIVE_CANCEL_ACTIVE=1` and omit the browser/API image flags. The
  YAML-created run is deleted only after an actual live cell is observed. Its
  matching cancelled receipt, dissolution, finalizer completion and zero live
  cells/Jobs are required. This is Kubernetes deletion, not browser cancellation.
- [Lost-response recovery](../evidence/celln-controller-pod-recovery-2026-09-08.json):
  add `CELLN_LIVE_LOST_RESPONSE=1` to the browser variant. After the real router
  accepts the submission, the TLS proxy discards its response and blocks
  observation until persisted uncertainty is visible. Recovery retained the
  original request and made exactly one execution POST; the real two-tool model
  result appeared on a fresh browser visit. No controller or host restart was
  injected in this case.
- [Controller-Pod replacement](../evidence/celln-controller-pod-restart-2026-09-08.json):
  also set `CELLN_LIVE_RESTART_CONTROLLER=1`. After persisted uncertainty, delete
  the proof deployment's Pod with a UID precondition and wait for its replacement
  to become ready before restoring observation. A different Pod recovered the
  original request, exactly one execution POST was observed, the real two-tool
  result appeared in the browser, and registered cleanup passed. The issuer,
  router, dispatcher and API stayed alive; this is not host failure qualification.

- [Unissued cancellation](../evidence/celln-controller-pod-unissued-cancellation-2026-09-08.json):
  set `CELLN_LIVE_CANCEL_UNISSUED=1` with the controller-Pod browser variant,
  without other fault flags. Install the matching boundary-aware CRD and image
  first. The fixture omits automatic composition registration, waits for the
  persisted Celln-only boundary and `AwaitingRegisteredComposition`, checks real
  API refusal of boundary removal and backend changes, and deletes the run by
  UID. Zero execution submissions/Jobs and completed finalization are required.
  This is browser creation followed by Kubernetes deletion, not browser cancellation.

- [Browser cancellation](../evidence/celln-browser-cancellation-2026-09-08.json):
  add `CELLN_LIVE_BROWSER_CANCEL=1` to either cancellation mode, retaining the
  deployed API/browser inputs. Both the waiting and actual-live-cell cases
  passed using the existing run-list Delete action. The test observes the real
  DELETE/204 response and row removal, then independently checks finalization;
  active mode also requires the matching cancelled-cell receipt, dissolution
  and zero live cells/Jobs. The UI removes the run rather than retaining a
  cancelled-run detail page; the execution owner retains the receipt separately.

- [Issuer process recovery](../evidence/celln-issuer-restart-2026-09-08.json):
  add `CELLN_LIVE_RESTART_ISSUER=1` to the successful standalone-issuer/browser
  variant. After terminal model execution the actual issuer is killed/reaped and
  restarted over the same state. Its authenticated gate reopens without changing
  profile/journal bytes, and subsequent approval withdrawal and refusal pass.
  This is same-boot recovery after issuance, not the systemd sandbox, in-flight
  issuance interruption or serving-host reboot.

Persistent installation,
host restart, network-policy refusal and general platform/RBAC
qualification remain open. These bounded checks do not complete MLP release
qualification or the broader epic.

## Live service credential separation

Every standalone issuer variant now uses its own `catalogue-issuer` service
account, a namespaced GET-only Role and a ten-minute TokenRequest. The bootstrap
kubeconfig is used only to create the isolated fixture. The issuer kubeconfig
is constructed from the verified API endpoint/CA and the new token, not copied
administrator authentication. It lives in private temporary storage and is
removed with that storage; deleting the fixture namespace removes the account.
The issuer can GET the configured Agent/runtime, namespace runs/tools and four
named approval ConfigMaps. It cannot list approvals, read Secrets, create or
edit approvals, read unbound or foreign-namespace approvals, or list cluster RBAC.
Those eight refusals require actual Kubernetes `Forbidden` responses; mutation
probes additionally use dry-run so an unexpected permission cannot change policy.
Read `issuer-kubernetes-identity.json` alongside `test-outcome.json`. Neither
record contains the Kubernetes token. Successful issuance and withdrawal exercise
the actual shipped issuer with this identity. This short-lived test credential
does not qualify production credential renewal or a systemd installation.

The ordinary success variant now checks the live TLS service endpoints before
controller dispatch. Issuer status/provisioning and router execution endpoints
must reject missing credentials and credentials belonging to the other services
with 401 before request parsing. In particular, the dispatcher/backend token is
not accepted as either ingress credential. Each endpoint must also reject the
other endpoint's CA at TLS verification. No credentials or response bodies are
written to the evidence; the subsequent successful real model journey establishes
that the valid path still works.

Read `service-credential-separation.json` together with the final
`test-outcome.json`. Probes originate on the host inside the private network
namespace. They are not tenant-Pod traffic or ingress/egress NetworkPolicy proof.
They are kept out of cancellation/lost-response modes so those tests retain their
exact execution-POST accounting.

The same ordinary variant sends ten malformed requests with valid issuer/router
credentials: truncated JSON, missing or unsupported issuer version, multiple JSON
values, an unknown tenant policy-root override, absent/non-string/path-traversal
execution identities, and an empty execution body. Each requires its exact HTTP
400 or 413 refusal and unchanged content in the trusted model profile, issuer
journal and router ownership directories. Read `authenticated-request-refusals.json`
with the final outcome; the subsequent real AI journey must still pass. These
checks do not cover valid-shaped foreign grants, oversized/flooded guest output,
tenant networking or exhausted execution budgets.

The lower-cost `TestActualRouterRefusalsThroughTLSProxy` regression needs only an
explicit absolute `CELLN_COMPOSITION_BINARY`. It starts the actual router and a
real TLS reverse proxy, sends 96 rejected requests with 0/2/32768-byte bodies,
and requires exact complete 401 JSON with no retry or backend requests. It does
not access Kubernetes, KVM or a model. This guards the early-close regression
in [Celln #96](https://github.com/sympozium-ai/celln/issues/96); the fixed binary
and separate real-model rerun are recorded in the
[fix evidence](../evidence/celln-router-refusal-fix-2026-09-08.json).
