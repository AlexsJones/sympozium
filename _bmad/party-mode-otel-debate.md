# BMAD Party Mode: OpenTelemetry Instrumentation Architecture Debate

**Date:** 2026-02-26
**Topic:** Best architecture for adding OpenTelemetry instrumentation to Sympozium
**GitHub Issue:** #11
**Participants:**

- **ARCH** (Architect) — Systems design, distributed tracing patterns, OTel SDK expertise
- **PM** (Product Manager) — User experience, operational burden, adoption, scope control
- **DEV** (Senior Developer) — Implementation feasibility, Go OTel SDK, testing, code impact

---

## Pre-Debate: Agreed Constraints

These decisions are **already locked** and not up for debate:

1. All 3 signals: traces + metrics + logs
2. Instrument ALL components (controller, API server, agent pods, sidecars, channels)
3. MCP bridge (#10) is separate from OTel (#11)
4. Config via SympoziumInstance CRD spec + Helm values
5. Context propagation: `TRACEPARENT` env var (controller→agent), file-based `context-<call-id>.json` (agent→sidecar)
6. OTel SDK in controller + agent container only, NOT in skill sidecars
7. Metric dimensions: `model`, `tool_name`, `instance`, `namespace`
8. Follow OTel GenAI semantic conventions strictly

---

## Topic A: OTel Collector Sidecar vs Direct Export from Agent Pods

**The question:** Should we add an OTel collector sidecar to agent pods, or have agent-runner export directly to an external collector?

### ARCH opens:

The fundamental tension here is that agent pods are **ephemeral Kubernetes Jobs**. They run for seconds to minutes, then terminate. The OTel SDK's batch exporter has a default 5-second export interval, and on shutdown it calls `ForceFlush()` — but there's a race. If the pod is killed before the flush completes, you lose telemetry for the most important part: the final result.

I propose a **DaemonSet OTel Collector** on each node, not a sidecar per pod. Here's why:

1. **Sidecar per pod** adds ~64Mi memory and a container to every agent Job. We already have 3-4 containers per pod (agent, ipc-bridge, sandbox, skill sidecars). A 5th container for OTel is wasteful.
2. **Direct export to external collector** (e.g., a central OTel Collector Deployment) means every agent pod needs network access to the collector endpoint. With our `NetworkPolicy` deny-all default, that's another egress rule per namespace. And if the collector is briefly unavailable, the ephemeral pod can't retry — it's already dead.
3. **DaemonSet Collector** runs one collector per node. Agent pods export to `localhost:4317` (OTLP gRPC) via hostNetwork or a well-known service. The DaemonSet handles buffering, retry, and export to the final backend. Pod death doesn't lose data because the DaemonSet persists.

However — I know PM will push back on operational complexity.

### PM responds:

Correct. A DaemonSet is a **cluster-level infrastructure concern**. Sympozium is a Helm chart that users install into their cluster. Many users will already have an OTel Collector running (via `opentelemetry-operator`, Datadog agent, Grafana Alloy, etc.). We should not force our own DaemonSet.

My position: **direct export to a user-configured endpoint** as the default, with documentation for the DaemonSet pattern for production deployments.

The `OTEL_EXPORTER_OTLP_ENDPOINT` env var is the standard. Users set it in Helm values, it flows into pods. Simple. If they lose a few spans from pod crashes, that's an acceptable trade-off for most users. The GenAI spans are the high-value telemetry, and those complete before the agent writes `result.json`.

### DEV weighs in:

Let me ground this in the actual code. Looking at `cmd/agent-runner/main.go`, the execution flow is:

```
main() → callAnthropic()/callOpenAI() → write result.json → write done sentinel → os.Exit()
```

The LLM calls (the expensive spans) complete *before* the agent writes output. So if we initialize the OTel SDK at startup and call `tp.Shutdown(ctx)` right before `os.Exit()`, we get a clean flush window. The risk is only if the pod is OOMKilled or the node crashes mid-execution.

For the IPC bridge sidecar (`cmd/ipc-bridge/main.go`), it runs as a long-lived process watching fsnotify events. It doesn't have the ephemeral problem.

**My recommendation:** Direct export with a **sync exporter option** for agent-runner specifically. The Go OTel SDK supports `otlptracegrpc` with synchronous mode. For a pod that runs 25 LLM iterations max, the overhead of sync export is negligible compared to LLM latency. This guarantees every span is exported before the process exits.

### ARCH counter:

Sync export blocks on every span completion — that adds network latency to the hot path. If the collector is slow or has a blip, your agent run takes longer. For production, that's a non-starter.

Better compromise: **batch exporter with aggressive flush**. Set `BatchTimeout` to 1 second (not 5), and call `ForceFlush` in a deferred shutdown with a 10-second context. The agent-runner already has a timeout mechanism (`TIMEOUT` env var, default 5m). We add a 10-second grace period after the main loop for telemetry flush.

### PM mediates:

I like DEV's point about sync being negligible relative to LLM latency, but ARCH is right that it's a bad pattern to encode. Let's go with ARCH's batch-with-aggressive-flush approach, and document the DaemonSet pattern.

### DECISION A:

| Aspect | Decision |
|--------|----------|
| **Export strategy** | Direct OTLP export from agent-runner to user-configured endpoint |
| **Exporter type** | Batch exporter with `BatchTimeout: 1s` (not default 5s) |
| **Shutdown** | `defer tp.Shutdown(ctx)` with 10-second deadline before `os.Exit()` |
| **No sidecar** | No OTel Collector sidecar in agent pods |
| **No DaemonSet** | Not bundled in Helm chart; documented as recommended production pattern |
| **Env var** | `OTEL_EXPORTER_OTLP_ENDPOINT` injected from Helm values → CRD spec → pod env |
| **Fallback** | If endpoint not configured, OTel SDK initializes with noop exporter (zero overhead) |

---

## Topic B: NATS Event Bus Instrumentation

**The question:** Inject traceparent in NATS message headers? Or treat NATS as an opaque boundary and start new trace segments?

### ARCH opens:

NATS JetStream supports message headers natively. The W3C Trace Context spec defines `traceparent` and `tracestate` headers. The OTel NATS instrumentation pattern is well-established:

```go
// Publisher side
msg := nats.NewMsg(subject)
msg.Header.Set("traceparent", spanContext.TraceParent())
msg.Data = eventJSON

// Subscriber side
traceparent := msg.Header.Get("traceparent")
ctx = otel.GetTextMapPropagator().Extract(ctx, carrier)
```

This gives us **end-to-end distributed traces** across the entire message flow:

```
Channel Pod → [NATS] → Channel Router → AgentRun creation → [K8s Job] → Agent → [NATS] → Channel Router → [NATS] → Channel Pod
```

One trace, multiple spans, full visibility. This is the architecturally correct approach.

### DEV pushes back:

Hold on. Let's look at the actual NATS code. In `internal/eventbus/nats.go`, the `Publish` method is:

```go
func (n *NATSEventBus) Publish(ctx context.Context, topic string, event Event) error {
    subject := topicToSubject(topic)
    data, _ := json.Marshal(event)
    _, err := n.js.Publish(ctx, subject, data)
    return err
}
```

The `Event` struct already has a `Metadata map[string]string` field. We have two options:

1. **NATS headers** — transport-level, invisible to application code
2. **Event.Metadata** — application-level, visible and portable

If we use NATS headers, the trace context is coupled to NATS. If we ever switch to Kafka, Redis Streams, or any other transport, we lose it. If we use `Event.Metadata`, it's transport-agnostic and already serialized in every event.

I lean toward **putting traceparent in Event.Metadata**, not NATS headers.

### ARCH responds:

That violates separation of concerns. Trace context is transport metadata, not business data. Putting it in `Event.Metadata` means every consumer has to know about OTel to not choke on unexpected metadata keys. NATS headers are designed exactly for this — they're invisible to consumers that don't care.

Also, the OTel `propagation.TextMapPropagator` interface expects a carrier with `Get(key)` and `Set(key, value)`. NATS headers implement this naturally. Event.Metadata would work too, but it's the wrong semantic layer.

### PM intervenes:

What about both? NATS headers for the transport layer (so standard OTel NATS instrumentation works), and **also** copy `traceparent` into `Event.Metadata` so it survives serialization for debugging and logging.

But actually — let me ask the real question: **does any consumer currently fail on unknown metadata keys?**

### DEV checks:

No. The `Metadata` map is used for routing (`instanceName`, `agentRunID`, `channel`) and consumers only read keys they need. Unknown keys are ignored. So putting `traceparent` in Metadata is safe.

But ARCH's point about semantic correctness is valid. Let's use NATS headers as the primary mechanism, and have the event bus `Publish` method automatically propagate the span context from the Go `context.Context`:

```go
func (n *NATSEventBus) Publish(ctx context.Context, topic string, event Event) error {
    subject := topicToSubject(topic)
    data, _ := json.Marshal(event)
    msg := &nats.Msg{Subject: subject, Data: data}
    // Inject trace context into NATS headers
    otel.GetTextMapPropagator().Inject(ctx, natsHeaderCarrier(msg))
    _, err := n.js.PublishMsg(ctx, msg)
    return err
}
```

On the subscribe side, we extract before processing. This is transparent to all existing consumers.

### ARCH agrees:

Yes. And for the cases where trace context needs to survive beyond NATS (like being stored in AgentRun annotations for the controller→pod hop), we explicitly extract and propagate. The NATS layer handles NATS-to-NATS hops; the controller handles NATS-to-K8s-Job hops.

### DECISION B:

| Aspect | Decision |
|--------|----------|
| **Primary mechanism** | W3C `traceparent` + `tracestate` in NATS message headers |
| **Implementation** | `otel.GetTextMapPropagator().Inject/Extract` with a `natsHeaderCarrier` adapter |
| **Transparency** | Existing consumers unaffected — headers are invisible unless explicitly read |
| **Not in Event.Metadata** | Trace context stays at transport layer, not mixed into business metadata |
| **Extraction** | Every subscriber wraps handler in `Extract(ctx, carrier)` to continue the trace |
| **Cross-boundary** | When trace crosses non-NATS boundary (K8s Job), controller explicitly propagates via `TRACEPARENT` env var |
| **New file** | `internal/eventbus/otel.go` — `natsHeaderCarrier` type implementing `propagation.TextMapCarrier` |

---

## Topic C: Shared Telemetry Package vs Per-Component Init

**The question:** Should the OTel SDK be a shared Go package (`internal/telemetry/`) imported by all components, or should each component init its own?

### DEV opens:

I've looked at every `main.go` in the repo:

- `cmd/agent-runner/main.go` — agent process (ephemeral)
- `cmd/apiserver/main.go` — API server (long-lived)
- `cmd/ipc-bridge/main.go` — IPC bridge sidecar (medium-lived, tied to agent pod)
- `cmd/controller/main.go` — controller manager (long-lived, singleton)
- `channels/telegram/main.go` — channel pod (long-lived)
- `channels/slack/main.go` — channel pod (long-lived)
- `channels/discord/main.go` — channel pod (long-lived)
- `channels/whatsapp/main.go` — channel pod (long-lived)

Every single one of these needs:
1. Resource detection (`service.name`, `service.version`, `k8s.namespace.name`)
2. Trace provider init with OTLP exporter
3. Meter provider init
4. Logger provider init (bridging to slog/zap)
5. Shutdown handling

That's ~50 lines of boilerplate per component. A shared package is a no-brainer.

### ARCH agrees but with conditions:

Yes, shared package, but with **component-specific configuration**. The agent-runner needs:
- `BatchTimeout: 1s` (aggressive flush for ephemeral pods)
- `TRACEPARENT` env var parsing for incoming context
- GenAI-specific metric instruments pre-registered

The controller needs:
- Standard `BatchTimeout: 5s`
- Reconciler-scoped spans
- Controller-runtime metrics integration

The channel pods need:
- Channel-type as a resource attribute
- Health status spans

So the shared package should provide:

```go
package telemetry

type Config struct {
    ServiceName    string
    ServiceVersion string
    Namespace      string
    BatchTimeout   time.Duration  // default 5s, agent-runner uses 1s
    ExtraResource  []attribute.KeyValue
}

func Init(ctx context.Context, cfg Config) (*Telemetry, error)
func (t *Telemetry) Shutdown(ctx context.Context) error
func (t *Telemetry) Tracer() trace.Tracer
func (t *Telemetry) Meter() metric.Meter
func (t *Telemetry) Logger() *slog.Logger
```

### PM asks:

What about the channel pods? They're separate Go modules in `channels/`. Do they import from `internal/`?

### DEV clarifies:

Good catch. Looking at the repo structure:

```
channels/
├── telegram/
│   ├── main.go
│   └── go.mod  ← separate module
├── slack/
│   ├── main.go
│   └── go.mod  ← separate module
...
```

Each channel is its own Go module. They can't import `internal/` — that's Go's visibility rule.

Options:
1. **Move telemetry to `pkg/telemetry/`** — exportable, any module can import
2. **Keep `internal/telemetry/`** for core components, duplicate init in channels
3. **Create a `go.opentelemetry.io`-style thin wrapper** as a separate module

### ARCH decides:

Option 1. `pkg/telemetry/` is the Go convention for shared library code. The telemetry package is stable infrastructure — it won't change based on business logic. It's the right candidate for `pkg/`.

The channel modules add a `replace` directive in their `go.mod` to reference the parent module (they likely already do this for `internal/channel/types.go` imports).

### DEV confirms:

Actually, looking at the channel code, channels already import `internal/channel` and `internal/eventbus` — they use the `BaseChannel` type. So they're either in the same module or already have replace directives. Let me check...

The channels import paths show they reference the main module's types. So `pkg/telemetry/` would be accessible the same way.

### DECISION C:

| Aspect | Decision |
|--------|----------|
| **Package location** | `pkg/telemetry/` (not `internal/`) for cross-module visibility |
| **Shared init** | `telemetry.Init(ctx, Config)` returns `*Telemetry` with tracer, meter, logger accessors |
| **Component-specific config** | `Config.BatchTimeout`, `Config.ExtraResource` for per-component tuning |
| **Agent-runner** | `BatchTimeout: 1s`, parses `TRACEPARENT` env for incoming context |
| **Controller** | `BatchTimeout: 5s`, integrates with controller-runtime metrics |
| **Channels** | `BatchTimeout: 5s`, adds `channel.type` resource attribute |
| **Noop fallback** | If `OTEL_EXPORTER_OTLP_ENDPOINT` is empty, returns noop providers (zero overhead) |
| **Files** | `pkg/telemetry/telemetry.go`, `pkg/telemetry/resource.go`, `pkg/telemetry/propagation.go` |

---

## Topic D: LLM Provider Instrumentation Strategy

**The question:** Both Anthropic and OpenAI calls need `gen_ai.chat` spans. Wrap at the provider level or add spans in the main tool loop?

### ARCH opens:

The OTel GenAI semantic conventions (v0.31+) define:

- Span name: `gen_ai.chat` (or `gen_ai.chat {model}`)
- Span kind: `CLIENT`
- Required attributes: `gen_ai.system`, `gen_ai.request.model`, `gen_ai.response.model`
- Recommended: `gen_ai.usage.input_tokens`, `gen_ai.usage.output_tokens`, `gen_ai.response.finish_reasons`

The question is **where** to create these spans. Looking at the code:

```go
// main.go line 147-153
switch provider {
case "anthropic":
    responseText, inputTokens, outputTokens, toolCalls, err = callAnthropic(...)
default:
    responseText, inputTokens, outputTokens, toolCalls, err = callOpenAI(...)
}
```

Option 1: **Wrap at provider level** — add spans inside `callAnthropic()` and `callOpenAI()`
Option 2: **Wrap in main loop** — add a span around the switch statement

I strongly advocate **Option 1: provider level**. Here's why:

Each provider function contains a **tool-calling loop** (up to 25 iterations). Each iteration makes a separate API call. With provider-level instrumentation, we get:

```
agent.run (root span)
└── gen_ai.chat iteration=1 (anthropic)
    ├── gen_ai.usage.input_tokens = 1500
    ├── gen_ai.usage.output_tokens = 200
    └── gen_ai.response.finish_reasons = ["tool_use"]
└── tool.execute name=read_file
└── gen_ai.chat iteration=2 (anthropic)
    ├── gen_ai.usage.input_tokens = 2000
    └── gen_ai.response.finish_reasons = ["end_turn"]
```

With main-loop wrapping, we'd get one fat span covering all iterations — useless for debugging which iteration was slow or which API call failed.

### DEV agrees, adds detail:

Yes, and looking at the actual code structure:

**`callAnthropic()`** (main.go lines 219-326):
- Lines 254-322: tool-calling loop
- Each iteration: `client.Messages.New()` → check stop_reason → execute tools
- Accumulates `totalInputTokens`, `totalOutputTokens`

**`callOpenAI()`** (main.go lines 328-431):
- Lines 379-427: tool-calling loop
- Each iteration: `client.Chat.Completions.New()` → check finish_reason → execute tools

I'd instrument like this:

```go
func callAnthropic(ctx context.Context, ...) (...) {
    for i := 0; i < maxIterations; i++ {
        ctx, span := tracer.Start(ctx, "gen_ai.chat",
            trace.WithSpanKind(trace.SpanKindClient),
            trace.WithAttributes(
                semconv.GenAISystemAnthropic,
                semconv.GenAIRequestModel(modelName),
                attribute.Int("gen_ai.chat.iteration", i+1),
            ),
        )

        resp, err := client.Messages.New(ctx, params)

        span.SetAttributes(
            semconv.GenAIUsageInputTokens(int(resp.Usage.InputTokens)),
            semconv.GenAIUsageOutputTokens(int(resp.Usage.OutputTokens)),
            semconv.GenAIResponseFinishReasons(string(resp.StopReason)),
        )
        span.End()
        // ... tool execution with its own span ...
    }
}
```

### PM asks:

What about the tool execution spans? Should `executeToolCall()` create its own span, or should the provider function wrap it?

### ARCH responds:

`executeToolCall()` should create its own span. It's called from both providers identically. The span hierarchy becomes:

```
agent.run
├── gen_ai.chat (iteration 1)
├── tool.execute (read_file)
├── gen_ai.chat (iteration 2)
├── tool.execute (execute_command)
│   └── ipc.exec_request (child span for IPC wait)
├── gen_ai.chat (iteration 3)
└── ...
```

Tool spans are siblings of gen_ai.chat spans, not children. This correctly models the sequential flow: LLM responds → tool executes → LLM called again.

### DEV raises a concern:

One issue: `callAnthropic` and `callOpenAI` currently don't take a `tracer` parameter. We need to either:

1. Pass `tracer` as a parameter (clutters the signature)
2. Use a package-level tracer `var tracer = otel.Tracer("sympozium/agent-runner")`
3. Extract tracer from context

Option 2 is the OTel Go convention. Declare at package level, use everywhere.

### ARCH agrees:

Package-level tracer. Standard Go OTel pattern:

```go
var tracer = otel.Tracer("sympozium.ai/agent-runner")
var meter  = otel.Meter("sympozium.ai/agent-runner")
```

### DECISION D:

| Aspect | Decision |
|--------|----------|
| **Instrumentation level** | Provider-level (inside `callAnthropic()` and `callOpenAI()`) |
| **Span per API call** | Each LLM API call in the tool loop gets its own `gen_ai.chat` span |
| **Tool spans** | `executeToolCall()` creates its own `tool.execute` span as a sibling |
| **Span hierarchy** | `agent.run` → flat sequence of `gen_ai.chat` + `tool.execute` spans |
| **Attributes** | Full GenAI semantic conventions: system, model, tokens, finish_reasons, iteration number |
| **Tracer init** | Package-level `var tracer = otel.Tracer("sympozium.ai/agent-runner")` |
| **Meter init** | Package-level `var meter = otel.Meter("sympozium.ai/agent-runner")` |
| **Metrics** | Histograms for token counts and duration, counters for tool calls, per-model and per-tool breakdowns |
| **Error recording** | `span.RecordError(err)` + `span.SetStatus(codes.Error, msg)` on API failures |

---

## Topic E: Channel Pod Trace Context Propagation

**The question:** Each channel is a separate Deployment. How to propagate trace context from channel → NATS → agent → response → channel?

### ARCH opens:

This is the hardest problem. The full message lifecycle crosses **5 process boundaries**:

```
1. Channel Pod (inbound)     — receives user message
2. NATS                       — transport
3. Channel Router (controller) — creates AgentRun
4. Agent Pod (K8s Job)        — processes task
5. Channel Pod (outbound)     — sends response
```

With our decisions from Topic B (NATS headers carry traceparent), the flow becomes:

```
Channel Pod: start span "channel.message.inbound"
  → inject traceparent into NATS header
  → publish to channel.message.received

Channel Router: extract traceparent from NATS header
  → continue trace with span "channel.route"
  → create AgentRun CRD
  → store traceparent in AgentRun annotation: otel.dev/traceparent

AgentRun Controller: read traceparent from annotation
  → span "agentrun.reconcile"
  → inject TRACEPARENT env var into agent pod (Decision #5 from constraints)

Agent Runner: parse TRACEPARENT env var
  → start child span "agent.run"
  → LLM calls, tool calls, etc.
  → write result.json
  → IPC bridge publishes agent.run.completed to NATS with traceparent in header

Channel Router: extract traceparent from NATS header
  → span "channel.route.response"
  → publish OutboundMessage to NATS with traceparent in header

Channel Pod: extract traceparent from NATS header
  → span "channel.message.outbound"
  → send to external service
```

**One trace, end-to-end.** The key insight is that the AgentRun CRD annotation bridges the K8s Job boundary.

### DEV raises concerns:

This is elegant but has a timing issue. The Channel Router creates the AgentRun CRD, but the AgentRun Controller reconciles it asynchronously. The controller might not see the annotation immediately. Let me trace the code:

In `channel_router.go:handleInbound()`, the AgentRun is created with annotations. In `agentrun_controller.go:reconcilePending()`, the controller reads the AgentRun and builds the Job. The annotation is on the AgentRun object — it'll be there when the controller reads it. No timing issue.

But there's another concern: **the controller's reconcile span and the channel router's span are in different processes.** The controller runs as a Deployment; the channel router runs *inside* the controller process (it's a goroutine that subscribes to NATS).

### ARCH clarifies:

Right — the Channel Router is part of the controller manager process. So the flow is actually:

```
Channel Pod (Process 1)
  → NATS
Channel Router (Process 2 = Controller Manager)
  → creates AgentRun CRD
  → same process reconciles it
  → creates Job
Agent Pod (Process 3 = ephemeral)
  → processes
  → IPC bridge publishes to NATS
Channel Router (Process 2 again)
  → reads completion
  → publishes outbound to NATS
Channel Pod (Process 1 again)
  → sends response
```

Only 3 processes. The channel router and controller share the same OTel SDK instance. Context flows naturally within the controller manager.

### PM asks:

What about multi-channel scenarios? A user messages on Telegram, and the agent's response includes a `send_channel_message` tool call to Discord. That's two different channel pods.

### DEV answers:

The `send_channel_message` tool writes to `/ipc/messages/send-*.json`. The IPC bridge picks it up and publishes to `channel.message.send` on NATS. The Discord channel pod receives it.

The trace context flows: agent span → IPC bridge span (same pod, shared /ipc volume) → NATS header → Discord channel pod span. It works with the same mechanism.

The only gap is that the IPC bridge doesn't currently have OTel SDK. But per our constraints, the OTel SDK is in the controller + agent container. The IPC bridge is a sidecar.

### ARCH proposes:

The IPC bridge is a special case. It's not a skill sidecar — it's infrastructure. I think we should add OTel SDK to the IPC bridge too. It's the **critical relay** between the agent's file-based IPC and the NATS event bus. Without instrumenting it, we have a black hole in our traces.

### PM pushes back:

The constraint says "OTel SDK in controller + agent container only, NOT in skill sidecars." The IPC bridge is technically a sidecar. But it's *our* sidecar — we control it, it's part of the Sympozium infrastructure. I think the intent was to avoid requiring *user-provided* skill sidecars to have OTel.

Let's amend the constraint: OTel SDK in controller + agent container + IPC bridge. Not in user-provided skill sidecars.

### ARCH and DEV agree.

### DECISION E:

| Aspect | Decision |
|--------|----------|
| **End-to-end trace** | Single trace spans the full lifecycle: channel → router → agent → response → channel |
| **Channel Pod → NATS** | Channel pod starts root span, injects `traceparent` into NATS message headers |
| **NATS → Channel Router** | Router extracts `traceparent`, continues trace with child span |
| **Router → AgentRun CRD** | `traceparent` stored in annotation `otel.dev/traceparent` on the AgentRun resource |
| **Controller → Agent Pod** | Controller reads annotation, injects `TRACEPARENT` env var into agent container |
| **Agent → NATS (via IPC bridge)** | Agent creates child spans; IPC bridge propagates `traceparent` in NATS headers |
| **NATS → Channel Pod (response)** | Channel pod extracts context, creates `channel.message.outbound` span |
| **IPC Bridge amendment** | Add OTel SDK to IPC bridge sidecar (it's infrastructure, not a user skill sidecar) |
| **Cross-channel** | Tool-initiated messages (send_channel_message) carry trace context through same NATS mechanism |
| **Annotation key** | `otel.dev/traceparent` on AgentRun CRD |

---

## Topic F: Testing Strategy for OTel Instrumentation

**The question:** Mock the OTel SDK in tests, use in-memory exporter, or skip OTel in unit tests?

### DEV opens:

I've seen three patterns in Go OTel projects:

**Pattern 1: In-memory exporter (integration tests)**
```go
exporter := tracetest.NewInMemoryExporter()
tp := trace.NewTracerProvider(trace.WithBatcher(exporter))
// ... run code ...
spans := exporter.GetSpans()
assert.Equal(t, "gen_ai.chat", spans[0].Name)
```

**Pattern 2: Noop provider (unit tests)**
```go
// Don't init OTel at all — global tracer returns noop
// Spans are created but never exported
// Zero assertions on telemetry
```

**Pattern 3: Mock tracer (brittle, avoid)**
```go
// Don't do this — couples tests to OTel internals
```

I advocate a **two-tier approach**:

- **Unit tests**: Noop provider. The telemetry code is there but produces no output. Tests focus on business logic. If someone accidentally removes a `span.End()` call, unit tests don't catch it — but that's fine.
- **Integration tests**: In-memory exporter. Verify that the correct spans are produced with correct attributes. These run in CI but not on every `go test ./...`.

### ARCH adds nuance:

Agreed on noop for unit tests. But for the `pkg/telemetry/` package itself, we need real tests:

1. **Init tests**: Verify that `telemetry.Init()` produces a working provider
2. **Resource tests**: Verify resource attributes are correctly set
3. **Propagation tests**: Verify `TRACEPARENT` parsing and injection
4. **Carrier tests**: Verify `natsHeaderCarrier` correctly implements `TextMapCarrier`

These use in-memory exporters and are true unit tests of the telemetry package.

For the agent-runner, I'd add **one integration test** that verifies the span hierarchy:

```go
func TestAgentRunSpanHierarchy(t *testing.T) {
    exporter := tracetest.NewInMemoryExporter()
    // ... init with in-memory ...
    // ... run agent with mock LLM server ...
    spans := exporter.GetSpans()

    // Verify: agent.run contains gen_ai.chat children
    root := findSpan(spans, "agent.run")
    genAI := findChildSpans(spans, root, "gen_ai.chat")
    assert.GreaterOrEqual(t, len(genAI), 1)
    assert.Equal(t, "anthropic", genAI[0].Attributes["gen_ai.system"])
}
```

### PM adds:

From an operational perspective, we also need a way to **verify OTel is working in a live cluster** without reading code. I want a Helm test or smoke test:

```yaml
# templates/tests/test-otel.yaml
apiVersion: v1
kind: Pod
metadata:
  name: {{ .Release.Name }}-otel-test
  annotations:
    "helm.sh/hook": test
spec:
  containers:
  - name: test
    image: curlimages/curl
    command: ['curl', '-sf', 'http://{{ .Release.Name }}-apiserver:8080/healthz']
```

Actually, that's just a health check. For OTel specifically, we should verify spans reach the collector. But that's a deployment concern, not a unit test concern.

### DEV summarizes:

Let me also address: **should we gate OTel code behind build tags?**

No. The OTel SDK with noop providers has zero overhead. There's no reason to conditionally compile it. The `OTEL_EXPORTER_OTLP_ENDPOINT` env var being empty triggers noop mode. Simple, no build complexity.

### DECISION F:

| Aspect | Decision |
|--------|----------|
| **Unit tests** | Noop provider — OTel code present but no export, no assertions on spans |
| **Integration tests** | In-memory exporter via `tracetest.NewInMemoryExporter()` for span hierarchy validation |
| **Telemetry package tests** | Full unit tests with in-memory exporter for `pkg/telemetry/` |
| **Carrier tests** | Unit tests for `natsHeaderCarrier` implementing `TextMapCarrier` |
| **Propagation tests** | Test `TRACEPARENT` env var parsing and context extraction |
| **Agent integration test** | One test verifying `agent.run` → `gen_ai.chat` span hierarchy with mock LLM |
| **Build tags** | None — no conditional compilation. Noop mode when endpoint not configured. |
| **CI** | Integration tests run in CI pipeline; unit tests run on every PR |
| **No mocking** | Never mock the OTel SDK itself — use noop or in-memory exporters |
| **Live verification** | Document `oteldbg` pattern: set `OTEL_TRACES_EXPORTER=logging` for debug output |

---

## Consolidated Architecture Decision Record

### Summary of All Decisions

```
┌─────────────────────────────────────────────────────────────────┐
│                    SYMPOZIUM + OTEL ARCHITECTURE                 │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  pkg/telemetry/          ← Shared OTel init package              │
│  ├── telemetry.go        ← Init(), Shutdown(), Config            │
│  ├── resource.go         ← K8s resource detection                │
│  └── propagation.go      ← TRACEPARENT parsing                   │
│                                                                  │
│  internal/eventbus/                                              │
│  └── otel.go             ← natsHeaderCarrier for NATS propagation│
│                                                                  │
│  cmd/agent-runner/                                               │
│  └── main.go             ← gen_ai.chat spans per API call        │
│                          ← tool.execute spans                    │
│                          ← BatchTimeout: 1s                      │
│                          ← TRACEPARENT env var → parent context   │
│                                                                  │
│  cmd/controller/                                                 │
│  └── main.go             ← Reconciler spans                      │
│                          ← TRACEPARENT → AgentRun annotation      │
│                          ← Annotation → TRACEPARENT env var       │
│                                                                  │
│  cmd/ipc-bridge/                                                 │
│  └── main.go             ← File watch spans                      │
│                          ← NATS publish/subscribe spans           │
│                          ← Propagates traceparent in NATS headers │
│                                                                  │
│  cmd/apiserver/                                                  │
│  └── main.go             ← HTTP handler spans                    │
│                          ← WebSocket streaming spans              │
│                                                                  │
│  channels/*/                                                     │
│  └── main.go             ← channel.message.inbound spans         │
│                          ← channel.message.outbound spans         │
│                                                                  │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  TRACE FLOW (end-to-end):                                        │
│                                                                  │
│  Channel Pod                                                     │
│    │ span: channel.message.inbound                               │
│    ▼                                                             │
│  NATS [traceparent in headers]                                   │
│    │                                                             │
│    ▼                                                             │
│  Channel Router (in controller process)                          │
│    │ span: channel.route                                         │
│    │ → creates AgentRun with annotation otel.dev/traceparent     │
│    ▼                                                             │
│  AgentRun Controller                                             │
│    │ span: agentrun.reconcile                                    │
│    │ → reads annotation → injects TRACEPARENT env var            │
│    ▼                                                             │
│  Agent Pod                                                       │
│    │ span: agent.run (parent from TRACEPARENT)                   │
│    │ ├── gen_ai.chat (per LLM API call)                          │
│    │ ├── tool.execute (per tool invocation)                      │
│    │ └── gen_ai.chat (next iteration)                            │
│    ▼                                                             │
│  IPC Bridge → NATS [traceparent in headers]                      │
│    │                                                             │
│    ▼                                                             │
│  Channel Router                                                  │
│    │ span: channel.route.response                                │
│    ▼                                                             │
│  NATS [traceparent in headers]                                   │
│    │                                                             │
│    ▼                                                             │
│  Channel Pod                                                     │
│    │ span: channel.message.outbound                              │
│    ▼                                                             │
│  External Service (Telegram/Slack/Discord/WhatsApp)              │
│                                                                  │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  CONFIGURATION FLOW:                                             │
│                                                                  │
│  Helm values.yaml                                                │
│    observability:                                                 │
│      enabled: true                                               │
│      endpoint: "http://otel-collector:4317"                      │
│      sampling: 1.0                                               │
│    │                                                             │
│    ▼                                                             │
│  Controller/API Server env vars (from Helm)                      │
│    OTEL_EXPORTER_OTLP_ENDPOINT                                   │
│    OTEL_TRACES_SAMPLER_ARG                                       │
│    │                                                             │
│    ▼                                                             │
│  SympoziumInstance CRD (optional override per instance)           │
│    spec.observability.endpoint                                   │
│    │                                                             │
│    ▼                                                             │
│  Agent Pod env vars (from controller)                            │
│    OTEL_EXPORTER_OTLP_ENDPOINT                                   │
│    TRACEPARENT                                                    │
│    OTEL_SERVICE_NAME=sympozium-agent-runner                       │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### Implementation Priority Order

| Priority | Component | Effort | Impact |
|----------|-----------|--------|--------|
| P0 | `pkg/telemetry/` shared package | Medium | Foundation for everything |
| P0 | Agent-runner GenAI spans | Medium | Highest-value telemetry |
| P1 | Controller reconciler spans | Low | Job lifecycle visibility |
| P1 | NATS header propagation (`internal/eventbus/otel.go`) | Low | Cross-component tracing |
| P1 | IPC bridge spans | Medium | Bridges the file↔NATS gap |
| P2 | API server HTTP spans | Low | Standard otelhttp middleware |
| P2 | Channel pod spans | Low | End-to-end trace completion |
| P3 | Helm values + CRD config | Low | User-facing configuration |
| P3 | Integration tests | Medium | Confidence in span hierarchy |

### Key Go Dependencies to Add

```
go.opentelemetry.io/otel v1.34+
go.opentelemetry.io/otel/sdk v1.34+
go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc
go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc
go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc
go.opentelemetry.io/otel/semconv v1.28+ (for GenAI conventions)
go.opentelemetry.io/otel/bridge/otelslog (for slog integration)
```

### Metric Instruments (GenAI Semantic Conventions)

```go
// Counters
gen_ai.client.token.usage          {tokens}     // input_tokens, output_tokens breakdown
gen_ai.client.operation.duration    {s}          // per-call latency histogram

// Custom Sympozium metrics
sympozium.agent.run.duration        {s}          // total agent run time
sympozium.agent.run.total           {runs}       // counter by status
sympozium.agent.tool_calls.total    {calls}      // counter by tool_name
sympozium.ipc.request.duration      {s}          // IPC round-trip time
sympozium.channel.message.total     {messages}   // counter by channel, direction
```

### Risk Register

| Risk | Mitigation |
|------|------------|
| Agent pod dies before flush | Batch exporter with 1s timeout + 10s shutdown grace |
| NATS header size limits | W3C traceparent is 55 bytes — well within NATS limits |
| OTel SDK init adds startup latency | Async exporter connection; noop fallback if endpoint unreachable |
| Channel pod restarts lose in-flight spans | Channel pods are long-lived; standard batch export is fine |
| Collector unavailable | SDK retries with backoff; falls back to noop after repeated failures |
| Per-pod memory overhead | OTel SDK ~5-10MB; acceptable for all components |
| Test flakiness from timing | In-memory exporter is synchronous; no timing issues in tests |

---

*This document was generated by a BMAD Party Mode debate session. All decisions should be reviewed by the team before implementation begins.*
