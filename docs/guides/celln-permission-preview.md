# Celln tool permission preview

The run dialog displays the current intersection of the tool declaration and
the operator, runtime and Agent grant layers. It preserves ordered explicit
lending, supports the one-run runtime override, and shows the shared cell
memory ceiling after tool limits are applied. No selected tools means none lent.

This is **not readiness or execution authorization**. It does not resolve model
authority, validate host artifacts, prewarm, reserve capacity, issue a grant or
submit a run. All execution-time checks still run. The observation refreshes
every ten seconds while the panel is mounted; errors hide prior permissions.
The preview is not a Kubernetes transaction or a lease.

## Administrator configuration

Set `CELLN_PERMISSION_PREVIEW_CONFIG` on the API server to an absolute path to
an operator-owned JSON file with this structure (example names only):

```json
{
  "apiVersion": "sympozium.ai/celln-permission-preview-v1",
  "bindings": [{
    "agent": {"namespace": "tenant", "name": "agent"},
    "operatorSource": {"namespace": "authority", "name": "operator-grants"},
    "runtimeSource": {"namespace": "authority", "name": "runtime-grants"},
    "agentSource": {"namespace": "authority", "name": "agent-grants"}
  }]
}
```

Use the same trusted grant locations as the catalogue controller. The preview
configuration intentionally contains no issuer/router credential or host path.
The API server uses its uncached Kubernetes reader. Grant ConfigMaps must deny
tenant writes; naming or labelling a ConfigMap cannot establish ownership.
Provide narrowly scoped read access to these sources using deployment RBAC.
Do not grant approval writes to enable preview. With Helm, set
`celln.permissionPreviewConfigMap` to an existing operator-owned ConfigMap in
the API-server namespace containing the `config.json` key. The chart mounts
only that key read-only and sets the environment variable. It neither creates
the ConfigMap nor shares the controller's issuer/router credential Secret.
The default is disabled; configuring preview does not enable Celln execution.
For non-Helm deployments, mount the file and set the variable explicitly.
Restart the API server to change its source bindings. Grant document changes
are observed on subsequent requests without restart.

Malformed startup configuration refuses startup. An absent Agent binding gives
503; unresolved, withdrawn or stale permissions give 422, without exposing
privileged source names or document contents. The UI still allows requesting a
run when preview is unavailable: the independent controller/issuer must approve
it, and preview availability must never become an alternate admission bypass.

## HTTP and YAML

`POST /api/v1/celln-selection/preview?namespace=tenant` accepts only:

```json
{"agentRef":"agent","cellnSelection":{"toolRefs":[{"name":"uppercase","revision":"v1"}]}}
```

Optional `cellnSelection.runtimeRef` is a same-namespace override. An explicit
empty `toolRefs: []` lends nothing. Omitted lists, duplicate names, oversized
requests and caller-supplied authority locations refuse. Successful responses
have `Cache-Control: no-store`, effective limits and frozen subject identities,
`executionAuthorized: false`, and `readiness: "not-established"`.

YAML AgentRuns retain the same `spec.cellnSelection` fields. A YAML author can
preview those fields with this read-only HTTP operation, but cannot attach a
preview result as an authority token. The operation creates no Kubernetes object.
