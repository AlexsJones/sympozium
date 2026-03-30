# Lifecycle Hooks

Run containers before and after your agent — fetch context from external systems, upload artifacts, notify Slack, clean up resources. Lifecycle hooks let you wire arbitrary setup and teardown logic into the agent execution flow without modifying the agent itself.

## How It Works

```
AgentRun created
  → Pending phase
    → Controller creates workspace PVC (if postRun defined)
    → Controller creates lifecycle RBAC (if rbac defined)
    → PreRun init containers execute sequentially
  → Running phase
    → Agent container runs
  → PostRunning phase (if postRun defined)
    → Controller creates follow-up Job
    → PostRun init containers execute sequentially
  → Succeeded / Failed
    → Workspace PVC cleaned up
    → Lifecycle RBAC garbage-collected (owner reference)
```

### PreRun Hooks

PreRun hooks execute as **init containers** before the agent starts. They have access to:

| Path | Description |
|------|-------------|
| `/workspace` | Shared working directory — write files here for the agent to read |
| `/ipc` | IPC bus (tool calls, task input) |
| `/tmp` | Scratch space |

**Use cases:** Fetch incident context from PagerDuty, clone a git repo, download test data, warm caches.

### PostRun Hooks

PostRun hooks execute in a **follow-up Job** after the agent completes. They receive everything preRun hooks get, plus:

| Env Var | Description |
|---------|-------------|
| `AGENT_EXIT_CODE` | `"0"` on success, non-zero on failure |
| `AGENT_RESULT` | The agent's final response text (truncated to 32Ki) |

The workspace is shared between the agent and postRun hooks via a PersistentVolumeClaim. PostRun failures are **best-effort** — they're recorded as a `PostRunFailed` Condition but don't change the agent's final phase.

**Use cases:** Upload artifacts to S3, post a summary to Slack, clean up temporary resources, trigger downstream pipelines.

### Environment Variables

All lifecycle hook containers receive these env vars:

| Env Var | Description |
|---------|-------------|
| `AGENT_RUN_ID` | Unique identifier for this agent run |
| `INSTANCE_NAME` | The SympoziumInstance this run belongs to |
| `AGENT_NAMESPACE` | Kubernetes namespace |
| Custom env vars | Any `spec.env` entries from the AgentRun |

## RBAC for Hooks

By default, lifecycle hook containers run with the `sympozium-agent` ServiceAccount, which has **no Kubernetes permissions**. If your hooks need to interact with the Kubernetes API (e.g., create or delete ConfigMaps), declare the required RBAC rules:

```yaml
spec:
  lifecycle:
    rbac:
      - apiGroups: [""]
        resources: ["configmaps"]
        verbs: ["get", "list", "create", "delete"]
    preRun:
      - name: create-context
        image: bitnami/kubectl:latest
        command: ["kubectl", "create", "configmap", "run-context",
                  "--from-literal=started=$(date)"]
    postRun:
      - name: cleanup-context
        image: bitnami/kubectl:latest
        command: ["kubectl", "delete", "configmap", "run-context"]
```

The controller creates a namespace-scoped Role and RoleBinding for the run, bound to `sympozium-agent`. These are garbage-collected when the AgentRun is deleted — no standing permissions.

## Examples

### Fetch PagerDuty incidents before the agent runs

```yaml
apiVersion: sympozium.ai/v1alpha1
kind: SympoziumInstance
metadata:
  name: oncall-agent
spec:
  agents:
    default:
      model: gpt-4o
      lifecycle:
        preRun:
          - name: fetch-incidents
            image: curlimages/curl:latest
            command: ["sh", "-c",
              "curl -s -H 'Authorization: Token token=$PD_TOKEN' \
               https://api.pagerduty.com/incidents?statuses[]=triggered \
               > /workspace/context/incidents.json"]
            env:
              - name: PD_TOKEN
                value: "your-pagerduty-token"
```

The agent's system prompt can then instruct it to read `/workspace/context/incidents.json` for current incident context.

### Upload artifacts to S3 after completion

```yaml
spec:
  lifecycle:
    postRun:
      - name: upload-report
        image: amazon/aws-cli:latest
        command: ["sh", "-c",
          "aws s3 cp /workspace/report.md s3://my-bucket/reports/$AGENT_RUN_ID.md"]
        env:
          - name: AWS_ACCESS_KEY_ID
            value: "AKIA..."
          - name: AWS_SECRET_ACCESS_KEY
            value: "..."
```

### Create and clean up a ConfigMap

```yaml
spec:
  lifecycle:
    rbac:
      - apiGroups: [""]
        resources: ["configmaps"]
        verbs: ["create", "delete", "get"]
    preRun:
      - name: create-config
        image: bitnami/kubectl:latest
        command: ["sh", "-c",
          "kubectl create configmap agent-scratch --from-literal=run=$AGENT_RUN_ID"]
    postRun:
      - name: delete-config
        image: bitnami/kubectl:latest
        command: ["kubectl", "delete", "configmap", "agent-scratch"]
```

### PersonaPack with lifecycle hooks

```yaml
apiVersion: sympozium.ai/v1alpha1
kind: PersonaPack
metadata:
  name: oncall-team
spec:
  personas:
    - name: triage-agent
      systemPrompt: "You are an SRE triage agent..."
      lifecycle:
        preRun:
          - name: fetch-alerts
            image: curlimages/curl:latest
            command: ["sh", "-c", "curl -s $ALERTMANAGER_URL/api/v2/alerts > /workspace/context/alerts.json"]
```

## Agent Sandbox Compatibility

Lifecycle hooks work with both standard Job mode and [Agent Sandbox](agent-sandbox.md) mode:

- **PreRun hooks** are injected as init containers into the Sandbox CR — they execute inside the gVisor/Kata sandbox.
- **PostRun hooks** always run as a separate follow-up Job (outside the sandbox), since the sandbox is torn down after the agent completes.
- The workspace PVC is shared between both.

## Phases

With lifecycle hooks, the AgentRun phase transitions become:

`Pending` → `Running` → `PostRunning` → `Succeeded` (or `Failed`)

The `PostRunning` phase is only entered when `postRun` hooks are defined. Without them, the flow is the standard `Pending` → `Running` → `Succeeded`/`Failed`.
