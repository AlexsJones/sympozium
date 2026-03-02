# feat: OpenTelemetry instrumentation for end-to-end agent observability

Closes #11

## Summary

This PR adds comprehensive OpenTelemetry instrumentation to Sympozium, enabling end-to-end distributed tracing, metrics, and structured logging across the entire agent execution pipeline: **Channel → NATS → Controller → Agent**.

### Changes per epic

**Epic 1 — `pkg/telemetry` package**
- Shared OTel SDK initialization (traces, metrics, logs via OTLP gRPC/HTTP)
- Automatic noop mode when no collector is configured (zero overhead)
- W3C TraceContext propagation registered globally
- Configurable batch timing, sampling ratio, and shutdown timeout

**Epic 2 — Controller OTel instrumentation + CRD `ObservabilitySpec`**
- Extended `SympoziumInstanceSpec` with per-instance `ObservabilitySpec`
- `agentrun.reconcile` span with attributes: agentrun name, phase, instance, namespace
- Trace context injection into agent pods via `otel.dev/traceparent` annotation
- `buildObservabilityEnv()` injects OTel env vars into agent and IPC bridge containers

**Epic 4 — API server OTel instrumentation**
- `otelhttp` middleware on API server HTTP handlers
- NATS event bus trace context propagation (`InjectTraceContext` / `ExtractTraceContext`)

**Epic 5 — Metrics instrumentation**
- `sympozium.agent.runs` counter (by status and instance)
- `sympozium.agent.duration_ms` histogram
- `sympozium.errors` counter (by error type and instance)

**Epic 6 — Structured logging with trace correlation**
- OTel slog bridge (`otelslog`) for log-trace correlation
- OTLP log exporter via gRPC
- Logs automatically include `trace_id` and `span_id` from context

**Epic 7 — Configuration, Helm, and testing**
- Helm `values.yaml`: global `observability` section with per-component tuning
- Helm templates: OTel env vars injected into controller and apiserver deployments
- Helper templates for OTel headers and resource attributes
- Network policies for OTLP collector traffic
- Unit tests for telemetry package, event bus propagation, and agent-runner OTel

**End-to-end trace propagation**
- Slack channel creates root span on message receipt, injects into NATS headers
- Controller extracts context, creates child `agentrun.reconcile` span
- Agent pod receives traceparent and joins the distributed trace
- Channel router propagates trace context when creating AgentRun resources

**Channel pod OTel injection**
- Controller injects `OTEL_EXPORTER_OTLP_ENDPOINT` into channel pod deployments
- `SYMPOZIUM_IMAGE_REGISTRY` env var support for configurable image registry (defaults to `ghcr.io/alexsjones/sympozium`)

**Bug fix (included)**
- Slack channel: add WebSocket ping/pong handlers and increase read deadline to 120s to prevent disconnects

## Configuration example

### Helm values
```yaml
observability:
  enabled: true
  endpoint: "http://otel-collector.monitoring.svc:4317"
  protocol: "grpc"
  samplingRatio: 1.0
  serviceNamePrefix: "sympozium"
  headers:
    Authorization: "Api-Token dt0c01.XXXXX"
  controller:
    batchTimeout: "5s"
  agentRunner:
    batchTimeout: "1s"
    shutdownTimeout: "10s"
```

### SympoziumInstance CR (per-instance override)
```yaml
apiVersion: sympozium.ai/v1alpha1
kind: SympoziumInstance
metadata:
  name: my-instance
spec:
  agents:
    default:
      model:
        provider: anthropic
        model: claude-sonnet-4-20250514
  observability:
    enabled: true
    endpoint: "http://otel-collector.monitoring.svc:4317"
    protocol: "grpc"
    samplingRatio: 0.5
    resourceAttributes:
      deployment.environment: "production"
      team.name: "platform"
```

## What traces look like

### Span names
| Component | Span name | Kind |
|---|---|---|
| Slack channel | `slack.message.received` | Server |
| Controller | `agentrun.reconcile` | Internal |
| Agent runner | `agent.run` | Internal |
| Agent runner | `agent.chat.turn` | Internal |
| API server | HTTP handler spans via `otelhttp` | Server |

### Key attributes
- `sympozium.channel`: channel type (slack, telegram, etc.)
- `sympozium.sender.id`: user identifier from channel
- `agentrun.name`: AgentRun resource name
- `agentrun.phase`: Pending → Running → Succeeded/Failed
- `instance.name`: SympoziumInstance name
- `k8s.namespace.name`: Kubernetes namespace

### Metrics
- `sympozium.agent.runs` — counter by status and instance
- `sympozium.agent.duration_ms` — histogram of run durations
- `sympozium.errors` — counter by error type and instance

## Screenshots

We have end-to-end traces working in Dynatrace, showing the full Channel → NATS → Controller → Agent flow. Screenshots available on request.

## Testing done

- `go build ./...` passes
- `go vet ./...` passes
- Unit tests added for:
  - `pkg/telemetry` — initialization, noop mode, shutdown, sampling
  - `internal/eventbus` — NATS header trace context inject/extract round-trip
  - `cmd/agent-runner` — OTel environment variable parsing and configuration
  - `internal/controller` — `buildContainers` with observability env injection
- Manual end-to-end testing with Dynatrace backend confirming traces propagate across all components

## Notes

- BMAD architecture docs (PRD, stories, architecture decision records) are available in the fork's `_bmad/` directory but excluded from this PR to keep it focused on code
- When `observability.enabled` is `false` (default), all OTel instrumentation uses noop providers with zero performance overhead
- The `SYMPOZIUM_IMAGE_REGISTRY` env var allows any fork to override the default image registry without code changes
