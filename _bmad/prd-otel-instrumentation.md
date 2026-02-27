# PRD: OpenTelemetry Instrumentation for Sympozium

**Version:** 1.0
**Date:** 2026-02-26
**GitHub Issue:** #11
**Status:** Draft
**Author:** Generated from BMAD Party Mode debate decisions

---

## 1. Overview

### 1.1 Problem Statement

Sympozium is a Kubernetes-native AI agent orchestrator with distributed execution across ephemeral Job pods, long-lived controller processes, NATS event bus messaging, and per-channel Deployments. Operators currently have no visibility into:

- End-to-end latency of a user message through the system (channel → agent → response)
- Per-LLM-call token consumption and latency broken down by model and provider
- Tool execution performance and failure rates
- Cross-component trace correlation (which agent run served which channel message)
- IPC bridge relay timing and reliability

### 1.2 Solution

Full OpenTelemetry instrumentation across all Sympozium components, emitting traces, metrics, and structured logs via OTLP to any user-configured collector backend.

### 1.3 Scope

**In scope:** All Sympozium-owned components (controller, API server, agent-runner, IPC bridge, channel pods).
**Out of scope:** User-provided skill sidecars, MCP bridge (#10), OTel Collector deployment (user responsibility).

### 1.4 Design Authority

All architectural decisions in this PRD derive from the BMAD Party Mode debate record (`_bmad/party-mode-otel-debate.md`). Decisions are final unless reopened by the team.

---

## 2. Architectural Decisions (from Debate)

| ID | Decision | Debate Topic |
|----|----------|--------------|
| AD-1 | Direct OTLP export from each component; no sidecar collector, no bundled DaemonSet | Topic A |
| AD-2 | Agent-runner uses batch exporter with `BatchTimeout: 1s` + 10s shutdown grace | Topic A |
| AD-3 | Noop exporter when `OTEL_EXPORTER_OTLP_ENDPOINT` is unset (zero overhead) | Topic A |
| AD-4 | W3C `traceparent`/`tracestate` propagated in NATS message headers (not Event.Metadata) | Topic B |
| AD-5 | `natsHeaderCarrier` adapter in `internal/eventbus/otel.go` | Topic B |
| AD-6 | Shared `pkg/telemetry/` package (not `internal/`) for cross-module visibility | Topic C |
| AD-7 | GenAI spans created at provider level (inside `callAnthropic`/`callOpenAI`), one span per API call | Topic D |
| AD-8 | `executeToolCall()` creates its own `tool.execute` span as a sibling to `gen_ai.chat` | Topic D |
| AD-9 | Package-level `var tracer` / `var meter` in each component | Topic D |
| AD-10 | End-to-end trace via: NATS headers → CRD annotation `otel.dev/traceparent` → `TRACEPARENT` env var | Topic E |
| AD-11 | OTel SDK added to IPC bridge (infrastructure sidecar, not a user skill sidecar) | Topic E |
| AD-12 | Unit tests use noop provider; integration tests use in-memory exporter; no mocking OTel SDK | Topic F |
| AD-13 | No build tags — OTel code always compiled, noop when unconfigured | Topic F |

---

## 3. Functional Requirements

### 3.1 Traces — Span Catalog

Every span follows OpenTelemetry semantic conventions. GenAI spans follow the [OTel GenAI semantic conventions](https://opentelemetry.io/docs/specs/semconv/gen-ai/).

#### 3.1.1 Agent Runner Spans

| Span Name | Kind | Parent | Attributes | Source File |
|-----------|------|--------|------------|-------------|
| `agent.run` | `INTERNAL` | From `TRACEPARENT` env var or root | `agent.run.id`, `agent.id`, `session.key`, `model.provider`, `model.name`, `instance.name`, `namespace` | `cmd/agent-runner/main.go` |
| `gen_ai.chat` | `CLIENT` | `agent.run` | `gen_ai.system` (anthropic\|openai), `gen_ai.request.model`, `gen_ai.response.model`, `gen_ai.usage.input_tokens`, `gen_ai.usage.output_tokens`, `gen_ai.response.finish_reasons`, `gen_ai.chat.iteration` | `cmd/agent-runner/main.go` (inside `callAnthropic`/`callOpenAI`) |
| `tool.execute` | `INTERNAL` | `agent.run` | `tool.name`, `tool.success` (bool), `tool.duration_ms`, `tool.exit_code` (for execute_command) | `cmd/agent-runner/tools.go` |
| `ipc.exec_request` | `CLIENT` | `tool.execute` | `ipc.request_id`, `ipc.timeout_s`, `ipc.wait_duration_ms` | `cmd/agent-runner/tools.go` (inside `executeCommand`) |
| `tool.fetch_url` | `CLIENT` | `tool.execute` | `url.full` (sanitized), `http.response.status_code`, `http.response.body.size` | `cmd/agent-runner/tools.go` |
| `agent.memory.load` | `INTERNAL` | `agent.run` | `memory.size_bytes`, `memory.source` | `cmd/agent-runner/main.go` |
| `agent.memory.save` | `INTERNAL` | `agent.run` | `memory.size_bytes`, `memory.updated` (bool) | `cmd/agent-runner/main.go` |

**Span hierarchy (agent-runner):**

```
agent.run
├── agent.memory.load
├── gen_ai.chat (iteration 1)
├── tool.execute (read_file)
├── gen_ai.chat (iteration 2)
├── tool.execute (execute_command)
│   └── ipc.exec_request
├── gen_ai.chat (iteration 3)
├── tool.execute (send_channel_message)
├── gen_ai.chat (iteration 4, final)
└── agent.memory.save
```

#### 3.1.2 Controller Spans

| Span Name | Kind | Parent | Attributes | Source File |
|-----------|------|--------|------------|-------------|
| `agentrun.reconcile` | `INTERNAL` | From NATS header context or root | `agentrun.name`, `agentrun.phase`, `instance.name`, `namespace` | `internal/controller/agentrun_controller.go` |
| `agentrun.create_job` | `INTERNAL` | `agentrun.reconcile` | `job.name`, `pod.containers.count`, `skills.count` | `internal/controller/agentrun_controller.go` |
| `agentrun.extract_result` | `INTERNAL` | `agentrun.reconcile` | `agentrun.status`, `tokens.input`, `tokens.output`, `duration_ms` | `internal/controller/agentrun_controller.go` |
| `agentrun.persist_memory` | `INTERNAL` | `agentrun.reconcile` | `memory.updated` (bool), `memory.size_bytes` | `internal/controller/agentrun_controller.go` |
| `channel.route` | `INTERNAL` | From NATS header context | `channel.type`, `instance.name`, `sender.id`, `chat.id` | `internal/controller/channel_router.go` |
| `channel.route.response` | `INTERNAL` | From NATS header context | `channel.type`, `agentrun.name`, `response.length` | `internal/controller/channel_router.go` |
| `schedule.reconcile` | `INTERNAL` | Root | `schedule.name`, `schedule.type`, `schedule.cron`, `instance.name` | `internal/controller/sympoziumschedule_controller.go` |
| `schedule.create_run` | `INTERNAL` | `schedule.reconcile` | `agentrun.name`, `schedule.include_memory` | `internal/controller/sympoziumschedule_controller.go` |

#### 3.1.3 IPC Bridge Spans

| Span Name | Kind | Parent | Attributes | Source File |
|-----------|------|--------|------------|-------------|
| `ipc.bridge.relay` | `INTERNAL` | From agent trace context | `direction` (inbound\|outbound), `event.type`, `agentrun.id` | `cmd/ipc-bridge/main.go` |
| `ipc.bridge.file_watch` | `INTERNAL` | `ipc.bridge.relay` | `file.path`, `file.type` (result\|stream\|spawn\|message) | `internal/ipc/bridge.go` |
| `ipc.bridge.nats_publish` | `CLIENT` | `ipc.bridge.relay` | `nats.subject`, `event.topic` | `internal/ipc/bridge.go` |

#### 3.1.4 API Server Spans

| Span Name | Kind | Parent | Attributes | Source File |
|-----------|------|--------|------------|-------------|
| `http.server.request` | `SERVER` | From HTTP `traceparent` header | `http.request.method`, `url.path`, `http.response.status_code`, `http.route` | `internal/apiserver/server.go` (via `otelhttp` middleware) |
| `ws.stream` | `SERVER` | From WebSocket upgrade request | `ws.client_id`, `agentrun.id`, `stream.chunks.count` | `internal/apiserver/server.go` |

#### 3.1.5 Channel Pod Spans

| Span Name | Kind | Parent | Attributes | Source File |
|-----------|------|--------|------------|-------------|
| `channel.message.inbound` | `PRODUCER` | Root (new trace per user message) | `channel.type`, `instance.name`, `sender.id`, `chat.id`, `message.length` | `channels/*/main.go` |
| `channel.message.outbound` | `CONSUMER` | From NATS header context | `channel.type`, `chat.id`, `message.length`, `delivery.success` | `channels/*/main.go` |
| `channel.health.check` | `INTERNAL` | Root | `channel.type`, `channel.connected` (bool) | `channels/*/main.go` |

### 3.2 Metrics — Instrument Catalog

All metrics use OTLP export. Dimensions follow the debate decision: `model`, `tool_name`, `instance`, `namespace`.

#### 3.2.1 GenAI Semantic Convention Metrics

| Metric Name | Type | Unit | Dimensions | Description |
|-------------|------|------|------------|-------------|
| `gen_ai.client.token.usage` | Histogram | `{token}` | `gen_ai.system`, `gen_ai.request.model`, `gen_ai.token.type` (input\|output), `instance`, `namespace` | Token consumption per LLM API call |
| `gen_ai.client.operation.duration` | Histogram | `s` | `gen_ai.system`, `gen_ai.request.model`, `instance`, `namespace` | Latency per LLM API call |

#### 3.2.2 Sympozium Custom Metrics

| Metric Name | Type | Unit | Dimensions | Description |
|-------------|------|------|------------|-------------|
| `sympozium.agent.run.duration` | Histogram | `s` | `model`, `instance`, `namespace`, `status` (succeeded\|failed) | Total wall-clock time per agent run |
| `sympozium.agent.run.total` | Counter | `{run}` | `model`, `instance`, `namespace`, `status`, `source` (channel\|schedule\|api\|spawn) | Agent runs created |
| `sympozium.agent.tool_calls.total` | Counter | `{call}` | `tool_name`, `instance`, `namespace`, `success` (true\|false) | Tool invocations |
| `sympozium.agent.iterations.total` | Histogram | `{iteration}` | `model`, `instance`, `namespace` | Tool-calling loop iterations per run |
| `sympozium.ipc.request.duration` | Histogram | `s` | `request_type` (exec\|spawn\|message\|schedule), `instance`, `namespace` | IPC round-trip latency |
| `sympozium.channel.message.total` | Counter | `{message}` | `channel` (telegram\|slack\|discord\|whatsapp), `direction` (inbound\|outbound), `instance`, `namespace` | Channel messages processed |
| `sympozium.channel.message.duration` | Histogram | `s` | `channel`, `direction`, `instance`, `namespace` | Time from receipt to send of channel messages |
| `sympozium.eventbus.publish.total` | Counter | `{event}` | `topic`, `namespace` | NATS events published |
| `sympozium.eventbus.publish.duration` | Histogram | `s` | `topic`, `namespace` | NATS publish latency |
| `sympozium.controller.reconcile.duration` | Histogram | `s` | `controller` (agentrun\|instance\|schedule\|skillpack\|personapack\|policy), `namespace` | Reconciliation loop duration |
| `sympozium.controller.reconcile.total` | Counter | `{reconcile}` | `controller`, `result` (success\|error\|requeue), `namespace` | Reconciliation outcomes |

#### 3.2.3 Histogram Bucket Boundaries

| Metric Category | Buckets |
|----------------|---------|
| LLM API call duration | 0.1, 0.5, 1, 2, 5, 10, 30, 60, 120, 300 seconds |
| Agent run duration | 1, 5, 10, 30, 60, 120, 300, 600 seconds |
| Token counts | 100, 500, 1000, 2000, 5000, 10000, 50000, 100000 |
| IPC request duration | 0.01, 0.05, 0.1, 0.5, 1, 5, 10 seconds |
| Controller reconcile | 0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5 seconds |

### 3.3 Logs — Structured Log Events

OTel log bridge connects existing `log.Printf` / `slog` / `zap` output to OTel log pipeline with trace correlation.

#### 3.3.1 Log Events

| Log Event | Level | Component | Attributes | When |
|-----------|-------|-----------|------------|------|
| `agent.run.started` | INFO | agent-runner | `agent.run.id`, `model`, `provider`, `task.length` | Agent begins execution |
| `agent.run.completed` | INFO | agent-runner | `agent.run.id`, `status`, `tokens.input`, `tokens.output`, `duration_ms` | Agent finishes |
| `agent.run.failed` | ERROR | agent-runner | `agent.run.id`, `error.message`, `error.type` | Agent errors |
| `gen_ai.chat.request` | DEBUG | agent-runner | `gen_ai.system`, `model`, `iteration`, `messages.count` | Each LLM API call |
| `gen_ai.chat.response` | DEBUG | agent-runner | `gen_ai.system`, `model`, `tokens.input`, `tokens.output`, `finish_reason` | Each LLM API response |
| `tool.execute.started` | DEBUG | agent-runner | `tool.name`, `args.summary` | Tool invocation begins |
| `tool.execute.completed` | DEBUG | agent-runner | `tool.name`, `success`, `duration_ms` | Tool invocation ends |
| `ipc.bridge.event` | DEBUG | ipc-bridge | `direction`, `event.type`, `file.path` | IPC bridge relays an event |
| `channel.message.received` | INFO | channel | `channel.type`, `sender.id`, `chat.id` | Inbound message from user |
| `channel.message.sent` | INFO | channel | `channel.type`, `chat.id`, `delivery.success` | Outbound message delivered |
| `controller.reconcile.error` | ERROR | controller | `controller`, `resource.name`, `error.message` | Reconciliation failure |

All log events automatically include `trace_id` and `span_id` from the active span context for correlation.

---

## 4. CRD Configuration Schema

### 4.1 SympoziumInstance CRD Addition

Add `Observability` field to `SympoziumInstanceSpec`:

```go
// In api/v1alpha1/sympoziuminstance_types.go

type SympoziumInstanceSpec struct {
    Channels []ChannelSpec  `json:"channels,omitempty"`
    Agents   AgentsSpec     `json:"agents"`
    Skills   []SkillRef     `json:"skills,omitempty"`
    PolicyRef string        `json:"policyRef,omitempty"`
    AuthRefs []SecretRef    `json:"authRefs,omitempty"`
    Memory   *MemorySpec    `json:"memory,omitempty"`

    // NEW: OpenTelemetry configuration for this instance
    Observability *ObservabilitySpec `json:"observability,omitempty"`
}

// ObservabilitySpec configures OpenTelemetry for agent pods spawned by this instance.
// When unset, inherits from Helm chart global values.
// When set, overrides Helm defaults for this instance's agent pods.
type ObservabilitySpec struct {
    // Enabled controls whether OTel instrumentation is active.
    // Default: inherits from Helm value observability.enabled
    Enabled *bool `json:"enabled,omitempty"`

    // Endpoint is the OTLP collector endpoint (gRPC).
    // Example: "http://otel-collector.monitoring:4317"
    // Default: inherits from Helm value observability.endpoint
    Endpoint string `json:"endpoint,omitempty"`

    // Protocol is the OTLP transport protocol.
    // Valid values: "grpc" (default), "http/protobuf"
    Protocol string `json:"protocol,omitempty"`

    // Headers are additional headers sent with OTLP export requests.
    // Useful for authentication tokens.
    // Example: {"Authorization": "Bearer <token>"}
    Headers map[string]string `json:"headers,omitempty"`

    // HeadersSecretRef references a Secret containing OTLP headers.
    // Secret keys become header names; values become header values.
    // Merged with (and overridden by) inline Headers.
    HeadersSecretRef string `json:"headersSecretRef,omitempty"`

    // SamplingRatio is the trace sampling probability (0.0 to 1.0).
    // 1.0 = sample all traces, 0.1 = sample 10%.
    // Default: 1.0
    SamplingRatio *float64 `json:"samplingRatio,omitempty"`

    // ResourceAttributes are additional OTel resource attributes
    // added to all telemetry from this instance's agent pods.
    // Example: {"environment": "production", "team": "ml-platform"}
    ResourceAttributes map[string]string `json:"resourceAttributes,omitempty"`
}
```

### 4.2 AgentRun Status Addition

Extend `AgentRunStatus` with trace correlation:

```go
// In api/v1alpha1/agentrun_types.go

type AgentRunStatus struct {
    Phase       AgentRunPhase      `json:"phase,omitempty"`
    PodName     string             `json:"podName,omitempty"`
    JobName     string             `json:"jobName,omitempty"`
    StartedAt   *metav1.Time       `json:"startedAt,omitempty"`
    CompletedAt *metav1.Time       `json:"completedAt,omitempty"`
    Result      string             `json:"result,omitempty"`
    Error       string             `json:"error,omitempty"`
    ExitCode    *int32             `json:"exitCode,omitempty"`
    TokenUsage  *TokenUsage        `json:"tokenUsage,omitempty"`
    Conditions  []metav1.Condition `json:"conditions,omitempty"`

    // NEW: TraceID is the OTel trace ID for this agent run.
    // Set by the controller when creating the Job.
    // Enables operators to look up the full trace in their backend.
    TraceID string `json:"traceID,omitempty"`
}
```

### 4.3 AgentRun Annotation

The controller stores the full W3C traceparent on the AgentRun CRD for cross-boundary propagation:

```
Annotation key: otel.dev/traceparent
Annotation value: 00-<trace-id>-<span-id>-<trace-flags>
Example: 00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01
```

### 4.4 Helm Values Schema

```yaml
# charts/sympozium/values.yaml — new section

# OpenTelemetry observability configuration
observability:
  # Enable OTel instrumentation across all components.
  # When false, OTel SDK initializes with noop providers (zero overhead).
  enabled: false

  # OTLP collector endpoint (gRPC).
  # This is injected as OTEL_EXPORTER_OTLP_ENDPOINT into all pods.
  # Example: "http://otel-collector.monitoring.svc:4317"
  endpoint: ""

  # OTLP transport protocol.
  # Valid values: "grpc", "http/protobuf"
  protocol: "grpc"

  # Additional OTLP export headers (e.g., auth tokens).
  # Injected as OTEL_EXPORTER_OTLP_HEADERS.
  headers: {}

  # Reference to a Secret containing OTLP headers.
  # Secret keys = header names, values = header values.
  headersSecretRef: ""

  # Trace sampling ratio (0.0 to 1.0). Default: sample everything.
  samplingRatio: 1.0

  # Additional resource attributes applied to all components.
  # Injected as OTEL_RESOURCE_ATTRIBUTES.
  resourceAttributes: {}

  # Service name prefix. Each component appends its role.
  # e.g., "sympozium" → "sympozium-controller", "sympozium-agent-runner"
  serviceNamePrefix: "sympozium"

  # Per-component overrides
  controller:
    # Override batch timeout for the controller (default: 5s)
    batchTimeout: "5s"

  agentRunner:
    # Aggressive batch timeout for ephemeral pods (default: 1s)
    batchTimeout: "1s"
    # Shutdown grace period for telemetry flush (default: 10s)
    shutdownTimeout: "10s"

  apiServer:
    batchTimeout: "5s"

  ipcBridge:
    batchTimeout: "5s"

  channels:
    batchTimeout: "5s"
```

---

## 5. Context Propagation Design

### 5.1 Propagation Map

```
┌─────────────────────────────────────────────────────────────────────┐
│                     CONTEXT PROPAGATION BOUNDARIES                   │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  ┌──────────┐    NATS Headers     ┌──────────────────────┐          │
│  │ Channel  │ ──────────────────→ │ Controller Manager   │          │
│  │ Pod      │    traceparent      │ (Channel Router +    │          │
│  │          │    tracestate       │  AgentRun Reconciler)│          │
│  └──────────┘                     └──────────┬───────────┘          │
│                                              │                       │
│                              CRD Annotation  │  otel.dev/traceparent │
│                                              │                       │
│                                              ▼                       │
│                                   ┌──────────────────────┐          │
│                                   │ AgentRun CRD         │          │
│                                   │ (K8s API Server)     │          │
│                                   └──────────┬───────────┘          │
│                                              │                       │
│                              Env Var         │  TRACEPARENT           │
│                                              │                       │
│                                              ▼                       │
│                              ┌───────────────────────────┐          │
│                              │ Agent Pod                  │          │
│                              │ ┌───────────┐ ┌─────────┐ │          │
│                              │ │ agent-    │ │ ipc-    │ │          │
│                              │ │ runner    │ │ bridge  │ │          │
│                              │ │           │ │         │ │          │
│                              │ │ Reads     │ │ Reads   │ │          │
│                              │ │ TRACEPARENT│ │ context │ │          │
│                              │ │ env var   │ │ from    │ │          │
│                              │ │           │ │ agent   │ │          │
│                              │ └───────────┘ └────┬────┘ │          │
│                              └────────────────────┼──────┘          │
│                                                   │                  │
│                                     NATS Headers  │  traceparent     │
│                                                   │                  │
│                                                   ▼                  │
│  ┌──────────┐    NATS Headers     ┌──────────────────────┐          │
│  │ Channel  │ ←────────────────── │ Controller Manager   │          │
│  │ Pod      │    traceparent      │ (Channel Router)     │          │
│  │ (outbound)│                    └──────────────────────┘          │
│  └──────────┘                                                        │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘

LEGEND:
  NATS Headers    = W3C traceparent in nats.Header via natsHeaderCarrier
  CRD Annotation  = otel.dev/traceparent annotation on AgentRun resource
  Env Var         = TRACEPARENT environment variable on agent container
  /ipc/ context   = context-<call-id>.json file (agent → skill sidecar)
```

### 5.2 Boundary Crossing Details

#### Boundary 1: Channel Pod → NATS → Controller

**Publisher (channel pod):**
```go
ctx, span := tracer.Start(ctx, "channel.message.inbound",
    trace.WithSpanKind(trace.SpanKindProducer))
defer span.End()

// BaseChannel.PublishInbound already calls eventbus.Publish
// eventbus.Publish injects traceparent into NATS headers automatically
eb.Publish(ctx, TopicChannelMessageRecv, event)
```

**Subscriber (channel router in controller):**
```go
// eventbus.Subscribe extracts traceparent from NATS headers
// returns ctx with parent span context
for event := range events {
    ctx := extractTraceContext(event.NATSMsg)
    ctx, span := tracer.Start(ctx, "channel.route")
    // ... create AgentRun with annotation ...
    span.End()
}
```

#### Boundary 2: Controller → AgentRun CRD → Agent Pod

**Controller writes annotation:**
```go
func (r *AgentRunReconciler) reconcilePending(ctx context.Context, run *v1alpha1.AgentRun) {
    // Extract current trace context
    sc := trace.SpanContextFromContext(ctx)
    if sc.IsValid() {
        tp := fmt.Sprintf("00-%s-%s-%s",
            sc.TraceID().String(),
            sc.SpanID().String(),
            sc.TraceFlags().String())
        run.Annotations["otel.dev/traceparent"] = tp
        // Also store in status for operator visibility
        run.Status.TraceID = sc.TraceID().String()
    }
}
```

**Controller injects env var into Job:**
```go
func (r *AgentRunReconciler) buildContainers(...) []corev1.Container {
    env := []corev1.EnvVar{
        // ... existing env vars ...
    }

    // Inject TRACEPARENT from annotation
    if tp, ok := run.Annotations["otel.dev/traceparent"]; ok {
        env = append(env, corev1.EnvVar{
            Name:  "TRACEPARENT",
            Value: tp,
        })
    }

    // Inject OTEL endpoint from instance or Helm defaults
    if endpoint := r.resolveOTelEndpoint(run); endpoint != "" {
        env = append(env, corev1.EnvVar{
            Name:  "OTEL_EXPORTER_OTLP_ENDPOINT",
            Value: endpoint,
        })
    }
}
```

**Agent-runner parses TRACEPARENT:**
```go
func main() {
    // pkg/telemetry handles TRACEPARENT parsing
    tel, err := telemetry.Init(ctx, telemetry.Config{
        ServiceName:  "sympozium-agent-runner",
        BatchTimeout: 1 * time.Second,
    })
    defer tel.Shutdown(shutdownCtx) // 10s deadline

    // Start root span with parent from TRACEPARENT
    ctx = tel.ExtractParentFromEnv(ctx)
    ctx, span := tracer.Start(ctx, "agent.run")
    defer span.End()
}
```

#### Boundary 3: Agent → IPC Files → IPC Bridge → NATS

**Agent writes trace context to IPC:**
```go
// In agent-runner, when writing to /ipc/output/result.json
// The IPC bridge shares the same pod and reads TRACEPARENT from env
// No file-based context needed for agent→bridge (same pod)
```

**IPC bridge inherits trace from env:**
```go
func main() {
    tel, _ := telemetry.Init(ctx, telemetry.Config{
        ServiceName: "sympozium-ipc-bridge",
    })
    // Parse TRACEPARENT env var (same as agent, same pod env)
    parentCtx := tel.ExtractParentFromEnv(ctx)

    // All NATS publishes from bridge carry this trace context
    // via natsHeaderCarrier injection
}
```

#### Boundary 4: Agent → Skill Sidecar (file-based)

Per the agreed constraints, skill sidecars do NOT have OTel SDK. Context is passed for potential future use:

```
/ipc/tools/context-<call-id>.json:
{
    "traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
    "tracestate": ""
}
```

The agent-runner creates this file alongside `exec-request-<call-id>.json`. Skill sidecars ignore it. The agent-runner uses the same context when creating the `ipc.exec_request` child span.

### 5.3 Sub-Agent Propagation

When an agent spawns a sub-agent via `/ipc/spawn/request-*.json`:

1. IPC bridge reads spawn request and publishes to `agent.spawn.request` NATS topic with traceparent in headers
2. Controller's spawner creates a child AgentRun with `otel.dev/traceparent` annotation from the parent trace
3. Child agent receives `TRACEPARENT` env var and creates a child span
4. Result: parent trace contains both parent and child agent execution as a single distributed trace

### 5.4 Schedule-Triggered Runs

Scheduled runs have no inbound trace context. The controller creates a new root trace:

```go
func (r *SympoziumScheduleReconciler) createAgentRun(ctx context.Context, ...) {
    ctx, span := tracer.Start(ctx, "schedule.create_run",
        trace.WithNewRoot(), // Always a new trace for scheduled runs
    )
    defer span.End()
    // Annotation set from this new span's context
}
```

---

## 6. Epic Breakdown

### Epic 1: Shared Telemetry Package (`pkg/telemetry/`)

**Priority:** P0 — Foundation for all other epics
**Estimated files:** 4 new files

| Story | Description | Acceptance Criteria |
|-------|-------------|---------------------|
| 1.1 | Create `pkg/telemetry/telemetry.go` with `Init()`, `Shutdown()`, `Config` struct | `Init()` returns `*Telemetry` with tracer, meter, logger accessors; noop when endpoint unset |
| 1.2 | Create `pkg/telemetry/resource.go` with K8s resource detection | Detects `service.name`, `service.version`, `k8s.namespace.name`, `k8s.pod.name` from env |
| 1.3 | Create `pkg/telemetry/propagation.go` with `TRACEPARENT` env var parsing | `ExtractParentFromEnv(ctx)` returns ctx with remote span context |
| 1.4 | Create `pkg/telemetry/telemetry_test.go` with in-memory exporter tests | Tests: init with endpoint, init without endpoint (noop), resource attributes, TRACEPARENT parsing |

**Config struct:**

```go
type Config struct {
    ServiceName    string
    ServiceVersion string
    Namespace      string
    BatchTimeout   time.Duration        // default 5s
    ShutdownTimeout time.Duration       // default 30s, agent-runner uses 10s
    SamplingRatio  float64              // default 1.0
    ExtraResource  []attribute.KeyValue // additional resource attributes
}
```

**Go dependencies to add:**
```
go.opentelemetry.io/otel v1.34+
go.opentelemetry.io/otel/sdk v1.34+
go.opentelemetry.io/otel/trace
go.opentelemetry.io/otel/metric
go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc
go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc
go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc
go.opentelemetry.io/otel/semconv v1.28+
go.opentelemetry.io/otel/bridge/otelslog
```

---

### Epic 2: Agent Runner Instrumentation

**Priority:** P0 — Highest-value telemetry
**Estimated files:** 3 modified files

| Story | Description | Acceptance Criteria |
|-------|-------------|---------------------|
| 2.1 | Initialize OTel in `cmd/agent-runner/main.go` with `BatchTimeout: 1s` and TRACEPARENT parsing | Agent starts with parent context from env; `defer Shutdown()` with 10s deadline |
| 2.2 | Add `agent.run` root span wrapping entire execution in `main()` | Span includes all required attributes (run ID, model, provider, instance) |
| 2.3 | Add `gen_ai.chat` spans inside `callAnthropic()` | One span per API call in tool loop; GenAI semantic convention attributes |
| 2.4 | Add `gen_ai.chat` spans inside `callOpenAI()` | One span per API call in tool loop; GenAI semantic convention attributes |
| 2.5 | Add `tool.execute` span in `executeToolCall()` | Span with tool.name, success, duration; `ipc.exec_request` child span for command execution |
| 2.6 | Register metric instruments and record on each LLM call and tool call | `gen_ai.client.token.usage`, `gen_ai.client.operation.duration`, `sympozium.agent.tool_calls.total` |
| 2.7 | Add `agent.memory.load` and `agent.memory.save` spans | Span memory operations with size tracking |

**Package-level declarations (agent-runner):**

```go
var tracer = otel.Tracer("sympozium.ai/agent-runner")
var meter  = otel.Meter("sympozium.ai/agent-runner")
```

---

### Epic 3: NATS Event Bus Propagation

**Priority:** P1 — Enables cross-component distributed tracing
**Estimated files:** 2 new + 1 modified

| Story | Description | Acceptance Criteria |
|-------|-------------|---------------------|
| 3.1 | Create `internal/eventbus/otel.go` with `natsHeaderCarrier` type | Implements `propagation.TextMapCarrier` interface wrapping `nats.Header` |
| 3.2 | Modify `NATSEventBus.Publish()` to inject trace context into NATS headers | Every published message carries `traceparent` and `tracestate` headers automatically |
| 3.3 | Modify `NATSEventBus.Subscribe()` to extract trace context from NATS headers | Returned events carry `context.Context` with parent span from headers |
| 3.4 | Add `sympozium.eventbus.publish.total` and `sympozium.eventbus.publish.duration` metrics | Metrics recorded on every Publish call |
| 3.5 | Create `internal/eventbus/otel_test.go` with carrier tests | Tests: inject/extract round-trip, missing headers, malformed traceparent |

**natsHeaderCarrier implementation:**

```go
type natsHeaderCarrier struct {
    msg *nats.Msg
}

func (c natsHeaderCarrier) Get(key string) string {
    return c.msg.Header.Get(key)
}

func (c natsHeaderCarrier) Set(key, value string) {
    c.msg.Header.Set(key, value)
}

func (c natsHeaderCarrier) Keys() []string {
    keys := make([]string, 0, len(c.msg.Header))
    for k := range c.msg.Header {
        keys = append(keys, k)
    }
    return keys
}
```

---

### Epic 4: Controller Instrumentation

**Priority:** P1 — Job lifecycle visibility
**Estimated files:** 5 modified files

| Story | Description | Acceptance Criteria |
|-------|-------------|---------------------|
| 4.1 | Initialize OTel in `cmd/controller/main.go` | Controller starts with OTel SDK; integrates with controller-runtime metrics registry |
| 4.2 | Add spans to `AgentRunReconciler.Reconcile()` | `agentrun.reconcile` parent span with phase-specific child spans |
| 4.3 | Propagate traceparent: write CRD annotation, inject TRACEPARENT env var into agent pod | Controller reads context from NATS event → writes to annotation → injects into Job env |
| 4.4 | Store `traceID` in `AgentRunStatus.TraceID` | Operators can query trace ID from `kubectl get agentrun -o yaml` |
| 4.5 | Add spans to `ChannelRouter.handleInbound()` and `handleCompleted()` | `channel.route` and `channel.route.response` spans with correct parent context from NATS |
| 4.6 | Add spans to `SympoziumScheduleReconciler` | `schedule.reconcile` and `schedule.create_run` spans (always new root traces) |
| 4.7 | Add `sympozium.controller.reconcile.duration` and `.total` metrics | Per-controller metrics with success/error/requeue dimensions |

---

### Epic 5: IPC Bridge Instrumentation

**Priority:** P1 — Bridges file-based IPC to NATS
**Estimated files:** 2 modified files

| Story | Description | Acceptance Criteria |
|-------|-------------|---------------------|
| 5.1 | Initialize OTel in `cmd/ipc-bridge/main.go` with TRACEPARENT from env | Bridge shares trace context with agent container via same TRACEPARENT env var |
| 5.2 | Add `ipc.bridge.relay` span for each file event relayed | Spans for result, stream, spawn, message file events |
| 5.3 | Add `ipc.bridge.nats_publish` child span for each NATS publish | Captures NATS subject and publish latency |
| 5.4 | Write `context-<call-id>.json` files for skill sidecar context (future use) | File created alongside `exec-request-<call-id>.json`; contains traceparent |
| 5.5 | Add `sympozium.ipc.request.duration` metric | Histogram of IPC relay latency by request type |

---

### Epic 6: API Server & Channel Pod Instrumentation

**Priority:** P2 — Completes end-to-end trace
**Estimated files:** 5 modified files

| Story | Description | Acceptance Criteria |
|-------|-------------|---------------------|
| 6.1 | Initialize OTel in `cmd/apiserver/main.go` | API server starts with OTel SDK |
| 6.2 | Add `otelhttp` middleware to API server | All HTTP endpoints auto-instrumented with `http.server.request` spans |
| 6.3 | Add `ws.stream` span for WebSocket connections | Tracks streaming duration and chunk count |
| 6.4 | Initialize OTel in each channel pod `main.go` (Telegram, Slack, Discord, WhatsApp) | All 4 channels init with `channel.type` resource attribute |
| 6.5 | Add `channel.message.inbound` span in each channel's message handler | Span starts when message is received from external API |
| 6.6 | Add `channel.message.outbound` span in each channel's send handler | Span created from NATS header context; tracks delivery success |
| 6.7 | Add `sympozium.channel.message.total` and `.duration` metrics | Counter and histogram per channel and direction |

---

### Epic 7: Configuration, Helm, & Testing

**Priority:** P3 — User-facing configuration and confidence
**Estimated files:** 6 modified + 3 new

| Story | Description | Acceptance Criteria |
|-------|-------------|---------------------|
| 7.1 | Add `ObservabilitySpec` to `SympoziumInstanceSpec` in CRD types | CRD validates; `kubectl apply` works with observability config |
| 7.2 | Add `TraceID` to `AgentRunStatus` in CRD types | `kubectl get agentrun -o yaml` shows traceID |
| 7.3 | Regenerate CRD manifests (`make generate manifests`) | Updated CRD YAML in `config/crd/` |
| 7.4 | Add `observability` section to Helm `values.yaml` | All observability values documented with defaults |
| 7.5 | Update Helm templates to inject OTel env vars into controller, API server, channel deployments | Templates read from `.Values.observability` and inject env vars |
| 7.6 | Update controller to resolve OTel endpoint: Helm default → SympoziumInstance override | Per-instance override works; falls back to global |
| 7.7 | Add `pkg/telemetry/` unit tests (init, resource, propagation, carrier) | 90%+ coverage of telemetry package |
| 7.8 | Add agent-runner integration test with mock LLM and in-memory exporter | Verifies `agent.run` → `gen_ai.chat` → `tool.execute` span hierarchy |
| 7.9 | Add NATS propagation integration test | Verifies traceparent survives publish → subscribe round-trip |
| 7.10 | Document OTel setup in user docs | Setup guide: Helm values, collector configuration, Grafana/Jaeger examples |

---

## 7. Testing Strategy

### 7.1 Test Pyramid

```
                    ┌─────────────────┐
                    │  E2E / Smoke    │  ← Manual or CI: full cluster with collector
                    │  (not automated)│
                    ├─────────────────┤
                    │  Integration    │  ← In-memory exporter, mock LLM server
                    │  Tests          │     Verify span hierarchies and attributes
                    │  (CI pipeline)  │
                    ├─────────────────┤
                    │  Unit Tests     │  ← Noop provider for business logic tests
                    │  (every PR)     │     In-memory exporter for pkg/telemetry/ tests
                    │                 │     Carrier tests for natsHeaderCarrier
                    └─────────────────┘
```

### 7.2 Unit Tests (run on every PR)

**Business logic tests** (`cmd/agent-runner/`, `internal/controller/`, etc.):
- OTel code is present but global tracer returns noop
- No assertions on telemetry
- No additional test setup needed
- Tests focus purely on functional correctness

**Telemetry package tests** (`pkg/telemetry/`):
- Use `tracetest.NewInMemoryExporter()` and `sdktrace.NewTracerProvider()`
- Test `Init()` with endpoint → working provider
- Test `Init()` without endpoint → noop provider
- Test `ExtractParentFromEnv()` with valid/invalid/missing TRACEPARENT
- Test resource attribute detection

**Carrier tests** (`internal/eventbus/otel_test.go`):
- Test `natsHeaderCarrier.Get/Set/Keys` methods
- Test `Inject` → `Extract` round-trip with real propagator
- Test missing headers → no parent context (graceful degradation)
- Test malformed traceparent → no parent context

### 7.3 Integration Tests (run in CI)

**Agent-runner span hierarchy test:**
```go
func TestAgentRunSpanHierarchy(t *testing.T) {
    exporter := tracetest.NewInMemoryExporter()
    tp := sdktrace.NewTracerProvider(
        sdktrace.WithSyncer(exporter),
    )
    otel.SetTracerProvider(tp)

    // Start mock LLM server that returns tool_use then end_turn
    mockServer := startMockAnthropicServer(t)
    defer mockServer.Close()

    // Run agent-runner main logic with test env vars
    os.Setenv("MODEL_PROVIDER", "anthropic")
    os.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "noop") // triggers init
    // ... run agent ...

    tp.ForceFlush(context.Background())
    spans := exporter.GetSpans()

    // Assert span hierarchy
    root := findSpanByName(spans, "agent.run")
    require.NotNil(t, root)

    genAISpans := findChildSpansByName(spans, root.SpanContext.SpanID(), "gen_ai.chat")
    require.GreaterOrEqual(t, len(genAISpans), 1)

    // Assert GenAI attributes
    assertAttribute(t, genAISpans[0], "gen_ai.system", "anthropic")
    assertAttribute(t, genAISpans[0], "gen_ai.request.model", "claude-sonnet-4-20250514")

    toolSpans := findChildSpansByName(spans, root.SpanContext.SpanID(), "tool.execute")
    require.GreaterOrEqual(t, len(toolSpans), 1)
}
```

**NATS propagation test:**
```go
func TestNATSTracePropagation(t *testing.T) {
    exporter := tracetest.NewInMemoryExporter()
    tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
    otel.SetTracerProvider(tp)
    otel.SetTextMapPropagator(propagation.TraceContext{})

    eb := setupTestNATSEventBus(t)

    // Publisher creates span
    pubCtx, pubSpan := tp.Tracer("test").Start(context.Background(), "publish")
    eb.Publish(pubCtx, "test.topic", &Event{Topic: "test"})
    pubSpan.End()

    // Subscriber receives span context
    events, _ := eb.Subscribe(context.Background(), "test.topic")
    event := <-events

    subCtx := event.Context // extracted by Subscribe
    subSpanCtx := trace.SpanContextFromContext(subCtx)

    // Assert same trace ID
    assert.Equal(t, pubSpan.SpanContext().TraceID(), subSpanCtx.TraceID())
    assert.True(t, subSpanCtx.IsRemote())
}
```

### 7.4 Live Debugging

For verifying OTel works in a deployed cluster without a full collector:

```bash
# Set exporter to console logging (debug mode)
helm upgrade sympozium ./charts/sympozium \
  --set observability.enabled=true \
  --set observability.endpoint="none"

# Then in any pod:
OTEL_TRACES_EXPORTER=logging  # prints spans to stderr
OTEL_METRICS_EXPORTER=logging # prints metrics to stderr
OTEL_LOGS_EXPORTER=logging    # prints logs to stderr
```

Document this pattern in user docs as `oteldbg` mode.

### 7.5 What is NOT Tested

- End-to-end traces across full cluster (manual verification only)
- OTel Collector configuration (user responsibility)
- Backend-specific behavior (Jaeger, Grafana Tempo, Datadog)
- Performance impact of OTel SDK (negligible per benchmarks; not worth testing)

---

## 8. Non-Functional Requirements

### 8.1 Performance

| Requirement | Target |
|-------------|--------|
| OTel SDK memory overhead per component | < 10 MB |
| Span export latency (batch, not blocking) | < 1s p99 |
| Agent-runner startup latency increase | < 50ms |
| Noop mode overhead (OTel disabled) | Zero measurable impact |
| NATS header size increase per message | < 100 bytes |

### 8.2 Reliability

| Requirement | Approach |
|-------------|----------|
| Agent pod dies before flush | BatchTimeout: 1s ensures most spans exported; ForceFlush on shutdown |
| Collector unavailable | SDK retries with exponential backoff; falls back to dropping spans (no crash) |
| NATS unavailable | Event bus already handles reconnection; trace context propagation degrades gracefully |
| Malformed TRACEPARENT | Parsed with error handling; starts new trace on parse failure |

### 8.3 Security

| Requirement | Approach |
|-------------|----------|
| No secrets in spans | Sanitize: no API keys, no message content, no file contents in span attributes |
| OTLP auth tokens | Stored in K8s Secret, referenced via `headersSecretRef` |
| Network access | Agent pods need egress to OTel endpoint; update NetworkPolicy templates |

### 8.4 Compatibility

| Requirement | Detail |
|-------------|--------|
| OTel SDK version | go.opentelemetry.io/otel v1.34+ (stable API) |
| OTLP protocol | gRPC (default) and HTTP/protobuf |
| Collector compatibility | Any OTLP-compatible: OTel Collector, Grafana Alloy, Datadog Agent, etc. |
| Backward compatibility | Zero breaking changes; observability is opt-in; no CRD field removals |
| GenAI semantic conventions | v0.31+ (experimental but stable subset) |

---

## 9. Rollout Plan

### Phase 1: Foundation (Epics 1 + 2)
- Ship `pkg/telemetry/` and agent-runner instrumentation
- Users can see GenAI spans and token metrics immediately
- No cross-component tracing yet

### Phase 2: Distributed Tracing (Epics 3 + 4 + 5)
- Ship NATS propagation, controller spans, IPC bridge spans
- End-to-end traces for channel → agent → response flow
- TraceID visible in AgentRun status

### Phase 3: Full Coverage (Epics 6 + 7)
- Ship API server, channel pod instrumentation, Helm config
- Complete end-to-end traces including external channel hops
- User-facing documentation

### Feature Flag

OTel is **always compiled in** (no build tags). Activation is controlled by:
- `observability.enabled: true` in Helm values (global)
- `spec.observability.enabled: true` in SympoziumInstance CRD (per-instance)
- Presence of `OTEL_EXPORTER_OTLP_ENDPOINT` env var (runtime)

When disabled, all OTel SDK operations are noop with zero overhead.

---

## 10. Dependencies & Go Modules

### New Go Dependencies (main module)

```
go.opentelemetry.io/otel                              v1.34.0
go.opentelemetry.io/otel/sdk                           v1.34.0
go.opentelemetry.io/otel/sdk/metric                    v1.34.0
go.opentelemetry.io/otel/sdk/log                       v0.10.0
go.opentelemetry.io/otel/trace                         v1.34.0
go.opentelemetry.io/otel/metric                        v1.34.0
go.opentelemetry.io/otel/log                           v0.10.0
go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc   v1.34.0
go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc v1.34.0
go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc       v0.10.0
go.opentelemetry.io/otel/propagation                   v1.34.0
go.opentelemetry.io/otel/semconv                       v1.28.0
go.opentelemetry.io/otel/bridge/otelslog               v0.10.0
go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.58.0
```

### Transitive Dependencies

The OTel SDK pulls in gRPC (already a dependency via controller-runtime and NATS). No new major transitive dependencies expected.

---

## 11. Risk Register

| Risk | Probability | Impact | Mitigation |
|------|------------|--------|------------|
| Agent pod OOMKilled before span flush | Low | Medium | BatchTimeout: 1s ensures most data exported before completion; acceptable loss |
| OTel SDK version conflicts with controller-runtime | Low | High | Pin OTel versions; test with current controller-runtime version |
| GenAI semantic conventions change (experimental) | Medium | Low | Use stable subset only; attribute names are strings, easy to update |
| Performance regression from span creation | Very Low | Medium | Noop mode benchmarks show zero overhead; batch export is non-blocking |
| NATS header propagation breaks existing consumers | Very Low | High | NATS headers are invisible to consumers using `msg.Data` only; tested |
| CRD schema migration for ObservabilitySpec | Low | Low | New optional field; no migration needed; backward compatible |
| Channel modules can't import pkg/telemetry/ | Low | Medium | Verify go.mod replace directives work; fallback: duplicate init code |

---

## Appendix A: OTel Resource Attributes

All components emit these resource attributes:

| Attribute | Source | Example |
|-----------|--------|---------|
| `service.name` | `Config.ServiceName` | `sympozium-agent-runner` |
| `service.version` | Build-time `-ldflags` | `v0.0.49` |
| `k8s.namespace.name` | `NAMESPACE` env var (from downward API) | `default` |
| `k8s.pod.name` | `POD_NAME` env var (from downward API) | `agent-run-abc123-xyz` |
| `k8s.node.name` | `NODE_NAME` env var (from downward API) | `worker-1` |
| `sympozium.instance` | `INSTANCE_NAME` env var | `my-assistant` |
| `sympozium.component` | Hardcoded per binary | `agent-runner`, `controller`, `ipc-bridge`, `apiserver`, `channel-telegram` |

## Appendix B: Environment Variables

| Variable | Set By | Read By | Purpose |
|----------|--------|---------|---------|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | Helm → controller → pod env | All components | Collector endpoint |
| `OTEL_EXPORTER_OTLP_HEADERS` | Helm → controller → pod env | All components | Auth headers |
| `OTEL_TRACES_SAMPLER_ARG` | Helm → controller → pod env | All components | Sampling ratio |
| `OTEL_RESOURCE_ATTRIBUTES` | Helm → controller → pod env | All components | Extra resource attrs |
| `OTEL_SERVICE_NAME` | Set by each component | pkg/telemetry | Service identity |
| `TRACEPARENT` | Controller → pod env | Agent-runner, IPC bridge | W3C trace context from parent |
| `OTEL_TRACES_EXPORTER` | Manual debug override | pkg/telemetry | Set to `logging` for debug |
| `OTEL_METRICS_EXPORTER` | Manual debug override | pkg/telemetry | Set to `logging` for debug |

---

*End of PRD. Implementation should follow the epic ordering (1→2→3+4+5→6+7) for incremental value delivery.*
