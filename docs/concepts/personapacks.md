# PersonaPacks

PersonaPacks are the **recommended way to get started** with Sympozium. A PersonaPack is a CRD that bundles multiple pre-configured agent personas — each with a system prompt, skills, tool policy, schedule, and memory seeds. Activating a pack is a single action: the PersonaPack controller stamps out all the Kubernetes resources automatically.

## Why PersonaPacks?

Without PersonaPacks, setting up even one agent requires creating a Secret, SympoziumInstance, SympoziumSchedule, and memory ConfigMap by hand. PersonaPacks collapse that into: pick a pack → enter your API key → done.

## How It Works

```
PersonaPack "platform-team" (3 personas)
  │
  ├── Activate via TUI (Enter on pack → wizard → API key → confirm)
  │
  └── Controller stamps out:
      ├── Secret: platform-team-openai-key
      ├── SympoziumInstance: platform-team-security-guardian
      │   ├── SympoziumSchedule: ...security-guardian-schedule (every 30m)
      │   └── ConfigMap: ...security-guardian-memory (seeded)
      ├── SympoziumInstance: platform-team-sre-watchdog
      │   ├── SympoziumSchedule: ...sre-watchdog-schedule (every 5m)
      │   └── ConfigMap: ...sre-watchdog-memory (seeded)
      └── SympoziumInstance: platform-team-platform-engineer
          ├── SympoziumSchedule: ...platform-engineer-schedule (weekdays 9am)
          └── ConfigMap: ...platform-engineer-memory (seeded)
```

All generated resources have `ownerReferences` pointing back to the PersonaPack — delete the pack and everything gets garbage-collected.

## Built-in Packs

| Pack | Category | Agents | Description |
|------|----------|--------|-------------|
| `platform-team` | Platform | Security Guardian, SRE Watchdog, Platform Engineer | Core platform engineering — security audits, cluster health, manifest review |
| `devops-essentials` | DevOps | Incident Responder, Cost Analyzer | DevOps workflows — incident triage, resource right-sizing |
| `developer-team` | Development | Tech Lead, Backend Dev, Frontend Dev, QA Engineer, Code Reviewer, DevOps Engineer, Docs Writer | A 2-pizza software development team that collaborates on a single GitHub repository through PRs, issues, code review, testing, and documentation |

## Activating a Pack in the TUI

<p align="center">
  <img src="../assets/personas.gif" alt="PersonaPack activation in the TUI" width="800px">
</p>

1. Launch `sympozium` — the TUI opens on the **Personas** tab (view 1)
2. Select a pack and press **Enter** to start the onboarding wizard
3. Choose your AI provider and paste an API key
4. Optionally bind channels (Telegram, Slack, Discord, WhatsApp)
5. Confirm — the controller creates all instances within seconds

## Activating via kubectl

```yaml
# 1. Create the provider secret
kubectl create secret generic my-pack-openai-key \
  --from-literal=OPENAI_API_KEY=sk-...

# 2. Patch the PersonaPack with authRefs to trigger activation
kubectl patch personapack platform-team --type=merge -p '{
  "spec": {
    "authRefs": [{"provider": "openai", "secret": "my-pack-openai-key"}]
  }
}'
```

The controller detects the `authRefs` change and reconciles — creating SympoziumInstances, Schedules, and memory ConfigMaps for each persona.

## Writing Your Own PersonaPack

```yaml
apiVersion: sympozium.ai/v1alpha1
kind: PersonaPack
metadata:
  name: my-team
spec:
  description: "My custom agent team"
  category: custom
  version: "1.0.0"
  personas:
    - name: my-agent
      displayName: "My Agent"
      systemPrompt: |
        You are a helpful assistant that monitors the cluster.
      skills:
        - k8s-ops
      toolPolicy:
        allow: [read_file, list_directory, execute_command, fetch_url]
      schedule:
        type: heartbeat
        interval: "1h"
        task: "Check cluster health and report any issues."
      memory:
        enabled: true
        seeds:
          - "Track recurring issues for trend analysis"
```

Apply it with `kubectl apply -f my-team.yaml`, then activate through the TUI.

!!! tip
    See the [Developer Team Pack](../skills/developer-team.md) for a detailed example of a complex PersonaPack with seven collaborating agents.
