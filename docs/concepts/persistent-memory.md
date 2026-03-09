# Persistent Memory

Each `SympoziumInstance` can enable **persistent memory** — a ConfigMap (`<instance>-memory`) containing `MEMORY.md` that is:

- Mounted read-only into every agent pod at `/memory/MEMORY.md`
- Prepended as context so the agent knows what it has learned
- Updated after each run — the controller extracts memory markers from pod logs and patches the ConfigMap

This gives agents **continuity across runs** without external databases or file systems. Memory lives in etcd alongside all other cluster state.

## How It Works

1. During onboarding (or PersonaPack activation), a ConfigMap is created with optional seed memories
2. Before each AgentRun, the controller mounts the ConfigMap into the agent pod at `/memory/MEMORY.md`
3. The agent reads its memory at the start of every run for context
4. During execution, the agent can emit memory markers in its output
5. After the run completes, the controller extracts these markers and patches the ConfigMap

## Enabling Memory

Memory is enabled by default when using PersonaPacks. For manual instances:

```yaml
apiVersion: sympozium.ai/v1alpha1
kind: SympoziumInstance
metadata:
  name: my-agent
spec:
  memory:
    enabled: true
```

## Memory Seeds

PersonaPacks can pre-populate agent memory with seed values:

```yaml
personas:
  - name: sre-watchdog
    memory:
      enabled: true
      seeds:
        - "Track recurring issues for trend analysis"
        - "Note any nodes that frequently report NotReady"
```

## Viewing Memory

View an agent's current memory through the TUI:

```
/memory <instance-name>
```

Or directly with kubectl:

```bash
kubectl get configmap <instance>-memory -o jsonpath='{.data.MEMORY\.md}'
```
