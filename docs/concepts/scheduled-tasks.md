# Scheduled Tasks

`SympoziumSchedule` resources define cron-based recurring agent runs — perfect for automated cluster health checks, overnight alert reviews, resource right-sizing sweeps, or any domain-specific task.

## Example

```yaml
apiVersion: sympozium.ai/v1alpha1
kind: SympoziumSchedule
metadata:
  name: daily-standup
spec:
  instanceRef: alice
  schedule: "0 9 * * *"        # every day at 9am
  type: heartbeat
  task: "Review overnight alerts and summarize status"
  includeMemory: true           # inject persistent memory
  concurrencyPolicy: Forbid     # skip if previous run still active
```

## Concurrency Policies

Concurrency policies work like `CronJob.spec.concurrencyPolicy` — a natural extension of Kubernetes semantics:

| Policy | Behaviour |
|--------|-----------|
| `Allow` | Multiple runs can execute concurrently |
| `Forbid` | Skip the scheduled run if the previous one is still active |
| `Replace` | Cancel the active run and start a new one |

## Heartbeat Presets

| Preset | Cron | Good for |
|--------|------|----------|
| Every 30 min | `*/30 * * * *` | Active incident monitoring, SRE on-call |
| Every hour | `0 * * * *` | General ops, default for most users |
| Every 6 hours | `0 */6 * * *` | Light-touch monitoring, cost-sensitive setups |
| Daily at 9 AM | `0 9 * * *` | Daily audits, reports, security scans |
| Disabled | — | On-demand only, no background activity |

## Managing Schedules

Change the heartbeat at any time through the TUI edit modal or by editing the CR:

```bash
kubectl edit sympoziumschedule <instance>-heartbeat
```

Or use the TUI slash command:

```
/schedule <instance> "*/30 * * * *" "Check cluster health every 30 minutes"
```
