# Security

Sympozium enforces defence-in-depth at every layer — from network isolation to per-run RBAC.

## Security Layers

| Layer | Mechanism | Scope |
|-------|-----------|-------|
| **Network** | `NetworkPolicy` deny-all egress on agent pods | Only the IPC bridge can reach NATS; agents cannot reach the internet or other pods |
| **Pod sandbox** | `SecurityContext` — `runAsNonRoot`, UID 1000, read-only root filesystem | Every agent and sidecar container runs with least privilege |
| **Admission control** | `SympoziumPolicy` admission webhook | Feature and tool gates enforced before the pod is created |
| **Skill RBAC** | Ephemeral `Role`/`ClusterRole` per AgentRun | Each skill declares exactly the API permissions it needs — the controller auto-provisions them at run start and revokes them on completion |
| **RBAC lifecycle** | `ownerReference` (namespace) + label-based cleanup (cluster) | Namespace RBAC is garbage-collected by Kubernetes. Cluster RBAC is cleaned up by the controller on AgentRun completion and deletion |
| **Controller privilege** | `cluster-admin` binding | The controller needs `cluster-admin` to create arbitrary RBAC rules declared by SkillPacks (Kubernetes prevents RBAC escalation otherwise) |
| **Multi-tenancy** | Namespaced CRDs + Kubernetes RBAC | Instances, runs, and policies are namespace-scoped; standard K8s RBAC controls who can create them |

## Ephemeral Skill RBAC

The skill sidecar RBAC model deserves special attention: permissions are **created on-demand** when an AgentRun starts, scoped to exactly the APIs the skill needs, and **deleted when the run finishes**. There is no standing god-role — each run gets its own short-lived credentials.

This is the Kubernetes-native equivalent of temporary IAM session credentials.

```
AgentRun starts
  → Controller reads SkillPack RBAC declarations
  → Creates Role + RoleBinding (namespace-scoped, ownerRef → AgentRun)
  → Creates ClusterRole + ClusterRoleBinding (label-based)
  → Agent pod uses these credentials during execution

AgentRun completes/deleted
  → Namespace RBAC: garbage-collected via ownerReference
  → Cluster RBAC: cleaned up by controller via label selector
```

## Policies

`SympoziumPolicy` resources gate what tools and features an agent can use. They are enforced by an admission webhook **before the pod is created** — not at runtime.

| Policy | Who it is for | Key rules |
|--------|---------------|-----------|
| **Permissive** | Dev clusters, demos | All tools allowed, no approval needed, generous resource limits |
| **Default** | General use | `execute_command` requires approval, everything else allowed |
| **Restrictive** | Production, security | All tools denied by default, must be explicitly allowed, sandbox required |
