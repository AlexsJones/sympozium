# Stories: OpenTelemetry Instrumentation for Sympozium

**GitHub Issue:** #11
**PRD:** `_bmad/prd-otel-instrumentation.md`
**Architecture:** `_bmad/architecture-otel-instrumentation.md`
**Total Story Points:** 89

---

## Epic 1: Shared Telemetry Package (`pkg/telemetry/`)

**Priority:** P0 — Foundation for all other epics
**Epic Points:** 13
**Blocked by:** Nothing
**Blocks:** Epics 2, 3, 4, 5, 6

### Story 1.1: Core Init/Shutdown/Config

**Points:** 5
**Dependencies:** None
**File:** `pkg/telemetry/telemetry.go` (new)

**Description:**
Create the shared `pkg/telemetry/` package with `Config` struct, `Init()` function, `Shutdown()` method, and `*Telemetry` accessor type. This is the entry point every component calls at startup.

**Acceptance Criteria:**
- [ ] `Config` struct with fields: `ServiceName`, `ServiceVersion`, `Namespace`, `BatchTimeout` (default 5s), `ShutdownTimeout` (default 30s), `SamplingRatio` (default 1.0), `ExtraResource`
- [ ] `Init(ctx, Config) → (*Telemetry, error)` creates TracerProvider, MeterProvider, LoggerProvider
- [ ] When `OTEL_EXPORTER_OTLP_ENDPOINT` env is empty, `Init()` returns `*Telemetry` backed by noop providers (zero overhead)
- [ ] When endpoint is set, creates OTLP gRPC exporters for all 3 signals (traces, metrics, logs)
- [ ] Batch exporter uses `Config.BatchTimeout` (not default 5s)
- [ ] Trace sampler uses `Config.SamplingRatio` via `TraceIDRatioBased`
- [ ] `Init()` registers global `TracerProvider`, `MeterProvider`, and `TextMapPropagator` (W3C TraceContext)
- [ ] `Shutdown(ctx) error` calls `ForceFlush` then `Shutdown` on all 3 providers, respects `Config.ShutdownTimeout`
- [ ] `Tracer() trace.Tracer` returns named tracer
- [ ] `Meter() metric.Meter` returns named meter
- [ ] `Logger() *slog.Logger` returns slog logger with OTel bridge
- [ ] `IsEnabled() bool` returns true when exporter is configured

**Implementation Notes:**
- Module path: `github.com/alexsjones/sympozium/pkg/telemetry`
- New Go dependencies: `go.opentelemetry.io/otel`, `go.opentelemetry.io/otel/sdk`, exporters, semconv, bridge/otelslog
- Agent-runner will call with `BatchTimeout: 1*time.Second, ShutdownTimeout: 10*time.Second`
- Controller will call with defaults (5s batch, 30s shutdown)
- Must handle `OTEL_EXPORTER_OTLP_PROTOCOL` env var for http/protobuf support

---

### Story 1.2: K8s Resource Detection

**Points:** 2
**Dependencies:** 1.1
**File:** `pkg/telemetry/resource.go` (new)

**Description:**
Build the OTel `Resource` from Kubernetes environment variables (downward API) and `Config` fields.

**Acceptance Criteria:**
- [ ] Detects `service.name` from `Config.ServiceName`
- [ ] Detects `service.version` from `Config.ServiceVersion`
- [ ] Detects `k8s.namespace.name` from `Config.Namespace` or `NAMESPACE` env var
- [ ] Detects `k8s.pod.name` from `POD_NAME` env var
- [ ] Detects `k8s.node.name` from `NODE_NAME` env var
- [ ] Sets `sympozium.instance` from `INSTANCE_NAME` env var
- [ ] Sets `sympozium.component` by stripping prefix from `ServiceName` (e.g., "sympozium-agent-runner" → "agent-runner")
- [ ] Merges `Config.ExtraResource` attributes
- [ ] Merges `OTEL_RESOURCE_ATTRIBUTES` env var (OTel SDK standard)
- [ ] Returns `*resource.Resource` for use with all providers

**Implementation Notes:**
- Use `resource.New()` with `resource.WithAttributes()`, not `resource.Default()` (avoids process/OS detection overhead in ephemeral pods)
- `INSTANCE_NAME` is already set on IPC bridge containers (`cmd/ipc-bridge/main.go` L27); agent-runner gets it from the input ConfigMap or will need it added to env in Epic 4

---

### Story 1.3: TRACEPARENT Env Var Propagation

**Points:** 3
**Dependencies:** 1.1
**File:** `pkg/telemetry/propagation.go` (new)

**Description:**
Implement `ExtractParentFromEnv(ctx) context.Context` that reads `TRACEPARENT` and `TRACESTATE` from environment variables and returns a context with the remote span context.

**Acceptance Criteria:**
- [ ] `envCarrier` type implements `propagation.TextMapCarrier` (Get, Set, Keys)
- [ ] `Get("traceparent")` reads `TRACEPARENT` env var
- [ ] `Get("tracestate")` reads `TRACESTATE` env var
- [ ] `Set()` is a no-op (extraction only)
- [ ] `ExtractParentFromEnv(ctx)` uses `otel.GetTextMapPropagator().Extract()` with `envCarrier`
- [ ] Returns original context if `TRACEPARENT` is empty or invalid (graceful degradation → new root trace)
- [ ] Valid W3C traceparent format: `00-{32-hex-trace-id}-{16-hex-span-id}-{2-hex-flags}`

**Implementation Notes:**
- Controller sets `TRACEPARENT` env var on agent pods in `agentrun_controller.go` L579-589 (Epic 4)
- Agent-runner and IPC bridge both call `ExtractParentFromEnv()` at startup
- The W3C propagator handles validation internally — no need to manually parse

---

### Story 1.4: Telemetry Package Tests

**Points:** 3
**Dependencies:** 1.1, 1.2, 1.3
**File:** `pkg/telemetry/telemetry_test.go` (new)

**Description:**
Unit tests for the telemetry package using in-memory exporter.

**Acceptance Criteria:**
- [ ] `TestInit_WithEndpoint` — Init with a (mock) endpoint returns enabled Telemetry; `IsEnabled()` returns true
- [ ] `TestInit_WithoutEndpoint` — Init without endpoint returns noop Telemetry; `IsEnabled()` returns false; `Tracer()` returns working (noop) tracer
- [ ] `TestInit_DefaultConfig` — Verify default BatchTimeout is 5s, default SamplingRatio is 1.0
- [ ] `TestResource_AllFields` — Set all env vars, verify Resource attributes
- [ ] `TestResource_MissingEnv` — Verify graceful handling of missing env vars
- [ ] `TestResource_ExtraAttributes` — Verify Config.ExtraResource merges correctly
- [ ] `TestExtractParentFromEnv_Valid` — Set valid TRACEPARENT, verify context has remote span context
- [ ] `TestExtractParentFromEnv_Invalid` — Set malformed TRACEPARENT, verify context has no parent (new root)
- [ ] `TestExtractParentFromEnv_Empty` — No TRACEPARENT set, verify context has no parent
- [ ] `TestShutdown` — Verify Shutdown flushes and returns no error
- [ ] All tests use `sdktrace.NewTracerProvider` with `tracetest.NewInMemoryExporter` — no real OTLP connection

**Implementation Notes:**
- Use `t.Setenv()` for env var manipulation (auto-cleanup)
- Use `tracetest.NewInMemoryExporter()` from `go.opentelemetry.io/otel/sdk/trace/tracetest`
- Since `Init()` registers global providers, tests should restore globals in cleanup (or use `t.Parallel()` carefully)

---

## Epic 2: Agent Runner Instrumentation

**Priority:** P0 — Highest-value telemetry
**Epic Points:** 18
**Blocked by:** Epic 1
**Blocks:** None (can ship independently after Epic 1)

### Story 2.1: Agent Runner OTel Init

**Points:** 3
**Dependencies:** 1.1, 1.3
**File:** `cmd/agent-runner/main.go` (modify L44+)

**Description:**
Initialize OTel SDK in agent-runner `main()` with ephemeral-pod-optimized settings and TRACEPARENT parsing.

**Acceptance Criteria:**
- [ ] Call `telemetry.Init()` with `BatchTimeout: 1s`, `ShutdownTimeout: 10s`
- [ ] `defer tel.Shutdown(shutdownCtx)` with 10s context deadline before `os.Exit()`
- [ ] Call `telemetry.ExtractParentFromEnv(ctx)` to get parent context from TRACEPARENT
- [ ] Package-level `var tracer = otel.Tracer("sympozium.ai/agent-runner")`
- [ ] Package-level `var meter = otel.Meter("sympozium.ai/agent-runner")`
- [ ] No crash if `OTEL_EXPORTER_OTLP_ENDPOINT` is unset (noop mode)
- [ ] Existing functionality unaffected

**Implementation Notes:**
- `cmd/agent-runner/main.go` L44: `main()` function
- Insert OTel init between env var loading (L48-68) and provider switch (L147)
- The `os.Exit(1)` at L216 needs wrapping to ensure Shutdown runs — use `defer`

---

### Story 2.2: Root agent.run Span

**Points:** 2
**Dependencies:** 2.1
**File:** `cmd/agent-runner/main.go` (modify)

**Description:**
Create the root `agent.run` span wrapping the entire agent execution.

**Acceptance Criteria:**
- [ ] Span name: `agent.run`
- [ ] Span kind: `INTERNAL`
- [ ] Parent: from TRACEPARENT env var (or root if no parent)
- [ ] Attributes set at start: `agent.run.id` (AGENT_RUN_ID), `agent.id` (AGENT_ID), `session.key` (SESSION_KEY), `gen_ai.system` (MODEL_PROVIDER), `gen_ai.request.model` (MODEL_NAME)
- [ ] Attributes set on completion: `sympozium.agent.status` (succeeded/failed), `sympozium.agent.total_input_tokens`, `sympozium.agent.total_output_tokens`, `sympozium.agent.total_tool_calls`, `sympozium.agent.duration_ms`
- [ ] On error: `span.RecordError(err)` + `span.SetStatus(codes.Error, msg)`
- [ ] `span.End()` called before Shutdown

**Implementation Notes:**
- `agentResult` struct at L26-36 has all the final metrics
- Duration computed at L155 (`time.Since(start)`)
- Error path at L214-216

---

### Story 2.3: GenAI Chat Spans (Anthropic)

**Points:** 3
**Dependencies:** 2.2
**File:** `cmd/agent-runner/main.go` (modify `callAnthropic()` L219-326)

**Description:**
Add `gen_ai.chat` span per LLM API call inside the Anthropic tool-calling loop.

**Acceptance Criteria:**
- [ ] One `gen_ai.chat` span per `client.Messages.New()` call
- [ ] Span kind: `CLIENT`
- [ ] Required attributes: `gen_ai.system` = "anthropic", `gen_ai.request.model`, `gen_ai.chat.iteration` (1-based)
- [ ] Post-response attributes: `gen_ai.usage.input_tokens`, `gen_ai.usage.output_tokens`, `gen_ai.response.finish_reasons`
- [ ] On API error: `span.RecordError(err)` + `span.SetStatus(codes.Error, msg)`
- [ ] `span.End()` called after response processing, before tool execution
- [ ] Context passed to next iteration (tool results in next API call)
- [ ] Record `gen_ai.client.token.usage` histogram (input + output separately)
- [ ] Record `gen_ai.client.operation.duration` histogram

**Implementation Notes:**
- Tool-calling loop: L254-322 (up to `maxIterations` = 25, L24)
- `client.Messages.New()` at ~L250
- Token counts from `resp.Usage.InputTokens`, `resp.Usage.OutputTokens`
- Stop reason from `resp.StopReason`

---

### Story 2.4: GenAI Chat Spans (OpenAI)

**Points:** 3
**Dependencies:** 2.2
**File:** `cmd/agent-runner/main.go` (modify `callOpenAI()` L332-431)

**Description:**
Same as 2.3 but for the OpenAI provider path.

**Acceptance Criteria:**
- [ ] One `gen_ai.chat` span per `client.Chat.Completions.New()` call
- [ ] `gen_ai.system` = "openai" (or the actual provider name from MODEL_PROVIDER)
- [ ] All same GenAI semantic convention attributes as 2.3
- [ ] Works for all OpenAI-compatible providers (azure-openai, ollama, github-copilot)
- [ ] Same histogram metrics as 2.3

**Implementation Notes:**
- Tool-calling loop: L379-427
- `client.Chat.Completions.New()` at ~L380
- Token counts from `resp.Usage.PromptTokens`, `resp.Usage.CompletionTokens`
- Finish reason from `resp.Choices[0].FinishReason`

---

### Story 2.5: Tool Execute Spans

**Points:** 3
**Dependencies:** 2.2
**File:** `cmd/agent-runner/tools.go` (modify `executeToolCall()` L196)

**Description:**
Wrap `executeToolCall()` with a `tool.execute` span for every tool invocation.

**Acceptance Criteria:**
- [ ] `tool.execute` span wraps entire `executeToolCall()` function
- [ ] Span kind: `INTERNAL`
- [ ] Attributes: `tool.name`, `tool.success` (bool)
- [ ] `tool.duration_ms` set on completion
- [ ] For `execute_command`: child `ipc.exec_request` span around the IPC polling loop (L666-692)
- [ ] `ipc.exec_request` attributes: `ipc.request_id`, `ipc.timeout_s`, `ipc.wait_duration_ms`
- [ ] For `fetch_url`: `url.full` (sanitized — strip query params), `http.response.status_code`
- [ ] Record `sympozium.agent.tool_calls.total` counter with `tool_name` dimension

**Implementation Notes:**
- `executeToolCall()` at L196 dispatches to specific handlers
- `executeCommand()` at L620-695 (IPC-based)
- `fetchURLTool()` at L335
- Tool names: `execute_command`, `read_file`, `write_file`, `list_directory`, `send_channel_message`, `fetch_url`, `schedule_task`

---

### Story 2.6: Memory Load/Save Spans

**Points:** 2
**Dependencies:** 2.2
**File:** `cmd/agent-runner/main.go` (modify)

**Description:**
Add spans for memory loading and saving operations.

**Acceptance Criteria:**
- [ ] `agent.memory.load` span when reading memory from `/skills/` or ConfigMap
- [ ] Attributes: `memory.size_bytes`, `memory.source` (configmap/file)
- [ ] `agent.memory.save` span when extracting and persisting memory updates
- [ ] Attributes: `memory.size_bytes`, `memory.updated` (bool)
- [ ] Both spans are children of `agent.run`

**Implementation Notes:**
- Memory loading is near L100-120 (reading skill files and MEMORY.md)
- Memory extraction is near L160-190 (parsing `<memory_update>` tags from response)

---

### Story 2.7: Agent Runner Metric Instruments

**Points:** 2
**Dependencies:** 2.1
**File:** `cmd/agent-runner/main.go` (modify)

**Description:**
Register all metric instruments at package level and record at appropriate points.

**Acceptance Criteria:**
- [ ] `gen_ai.client.token.usage` — Int64Histogram, recorded per LLM call
- [ ] `gen_ai.client.operation.duration` — Float64Histogram, recorded per LLM call
- [ ] `sympozium.agent.run.duration` — Float64Histogram, recorded once on completion
- [ ] `sympozium.agent.tool_calls.total` — Int64Counter, recorded per tool invocation
- [ ] `sympozium.agent.iterations.total` — Int64Histogram, recorded once with iteration count
- [ ] All metrics include dimensions: `model`, `instance`, `namespace`

**Implementation Notes:**
- Instance name available from `INSTANCE_NAME` env or IPC bridge
- Namespace available from `NAMESPACE` env (downward API — added in Epic 7)

---

## Epic 3: NATS Event Bus Propagation

**Priority:** P1 — Cross-component distributed tracing
**Epic Points:** 8
**Blocked by:** Epic 1
**Blocks:** Epics 4, 5

### Story 3.1: NATS Header Carrier

**Points:** 2
**Dependencies:** 1.1
**File:** `internal/eventbus/otel.go` (new)

**Description:**
Create `natsHeaderCarrier` type implementing `propagation.TextMapCarrier`.

**Acceptance Criteria:**
- [ ] `natsHeaderCarrier` wraps `nats.Header`
- [ ] `Get(key) string` reads from NATS header
- [ ] `Set(key, value)` writes to NATS header
- [ ] `Keys() []string` returns all header keys
- [ ] `InjectTraceContext(ctx, header)` convenience function
- [ ] `ExtractTraceContext(ctx, header) context.Context` convenience function

**Implementation Notes:**
- `nats.Header` is `type Header map[string][]string` — same as `http.Header`
- NATS header methods: `header.Get(key)`, `header.Set(key, value)`
- Uses `otel.GetTextMapPropagator()` for inject/extract

---

### Story 3.2: Modify Publish with Trace Injection

**Points:** 3
**Dependencies:** 3.1
**File:** `internal/eventbus/nats.go` (modify `Publish()` L73)

**Description:**
Modify `NATSEventBus.Publish()` to inject trace context from `context.Context` into NATS message headers.

**Acceptance Criteria:**
- [ ] Every published NATS message carries `traceparent` and `tracestate` headers
- [ ] Injection is automatic — callers don't need to change
- [ ] Use `PublishMsg()` instead of `Publish()` to support headers
- [ ] Add optional `sympozium.eventbus.publish.total` counter
- [ ] Add optional `sympozium.eventbus.publish.duration` histogram
- [ ] No change to `Event` struct or `EventBus` interface

**Implementation Notes:**
- Current code at L73: `n.js.Publish(ctx, subject, data)`
- Change to: create `nats.Msg{Subject, Data, Header}`, inject context, `n.js.PublishMsg(ctx, msg)`
- The `jetstream.JetStream` interface has `PublishMsg` — verify it accepts headers

---

### Story 3.3: Modify Subscribe with Trace Extraction

**Points:** 2
**Dependencies:** 3.1
**File:** `internal/eventbus/nats.go` (modify `Subscribe()` L89)

**Description:**
Modify subscription to extract trace context from NATS message headers and provide it to consumers.

**Acceptance Criteria:**
- [ ] Subscribed events carry trace context extracted from NATS headers
- [ ] Consumers receive a `context.Context` with the parent span from the publisher
- [ ] If no traceparent header, context has no parent (new root trace)
- [ ] Existing consumers unaffected — they can ignore the context

**Implementation Notes:**
- Current Subscribe returns `<-chan *Event` — may need to add a context field to Event or return a richer type
- Minimal approach: add `Context context.Context` field to `Event` struct (unexported or `json:"-"`)
- Alternative: create wrapper type returned from Subscribe

---

### Story 3.4: Event Bus OTel Tests

**Points:** 1
**Dependencies:** 3.1, 3.2, 3.3
**File:** `internal/eventbus/otel_test.go` (new)

**Description:**
Unit tests for the NATS header carrier and inject/extract round-trip.

**Acceptance Criteria:**
- [ ] `TestNATSHeaderCarrier_GetSet` — basic get/set operations
- [ ] `TestNATSHeaderCarrier_Keys` — returns all header keys
- [ ] `TestInjectExtract_RoundTrip` — inject traceparent, extract on other side, same trace ID
- [ ] `TestExtract_MissingHeaders` — no traceparent → no parent context
- [ ] `TestExtract_MalformedTraceparent` — invalid value → no parent context

**Implementation Notes:**
- Can test carrier without NATS connection — just use `nats.Header` directly
- Use `propagation.TraceContext{}` as propagator in tests

---

## Epic 4: Controller Instrumentation

**Priority:** P1 — Job lifecycle visibility and trace propagation hub
**Epic Points:** 18
**Blocked by:** Epics 1, 3
**Blocks:** None

### Story 4.1: Controller OTel Init

**Points:** 2
**Dependencies:** 1.1
**File:** `cmd/controller/main.go` (modify)

**Description:**
Initialize OTel SDK in the controller manager process.

**Acceptance Criteria:**
- [ ] Call `telemetry.Init()` with `ServiceName: "sympozium-controller"`, default batch timeout
- [ ] `defer tel.Shutdown(ctx)` on the manager's context
- [ ] Package-level tracer/meter declarations
- [ ] No crash if OTel endpoint unset

**Implementation Notes:**
- Controller uses `ctrl.NewManager()` and `mgr.Start()` — add Init before manager setup

---

### Story 4.2: AgentRun Reconciler Spans

**Points:** 3
**Dependencies:** 4.1
**File:** `internal/controller/agentrun_controller.go` (modify L69, L116, L193)

**Description:**
Add spans to the AgentRun reconciliation loop.

**Acceptance Criteria:**
- [ ] `agentrun.reconcile` parent span in `Reconcile()` (L69)
- [ ] `agentrun.create_job` child span in `reconcilePending()` (L116)
- [ ] `agentrun.extract_result` child span in `reconcileRunning()` (L193) when extracting pod results
- [ ] `agentrun.persist_memory` child span when persisting memory (L915+)
- [ ] Attributes: `agentrun.name`, `agentrun.phase`, `instance.name`, `namespace`
- [ ] Error recording on reconciliation failure
- [ ] Record `sympozium.controller.reconcile.duration` histogram
- [ ] Record `sympozium.controller.reconcile.total` counter

---

### Story 4.3: TRACEPARENT Annotation and Env Injection

**Points:** 5
**Dependencies:** 4.2, 3.2
**File:** `internal/controller/agentrun_controller.go` (modify L116, L562-766)

**Description:**
The critical trace propagation bridge: write traceparent to CRD annotation, read it back to inject as env var.

**Acceptance Criteria:**
- [ ] In `reconcilePending()`: extract span context, write `otel.dev/traceparent` annotation on AgentRun
- [ ] In `reconcilePending()`: set `AgentRunStatus.TraceID` to trace ID string
- [ ] In `buildContainers()` (L562): read `otel.dev/traceparent` annotation, inject as `TRACEPARENT` env var on agent container
- [ ] Also inject `TRACEPARENT` on IPC bridge container
- [ ] Inject `OTEL_EXPORTER_OTLP_ENDPOINT` from resolved OTel config
- [ ] Inject `OTEL_SERVICE_NAME` = "sympozium-agent-runner" / "sympozium-ipc-bridge"
- [ ] Add K8s downward API env vars: `POD_NAME`, `NAMESPACE`, `NODE_NAME`
- [ ] Resolve endpoint: SympoziumInstance.spec.observability.endpoint → Helm default → empty

**Implementation Notes:**
- `buildContainers()` at L562-766 builds agent + IPC bridge + sandbox + skill sidecar containers
- Agent container env vars at L579-589
- IPC bridge env vars defined separately
- The controller needs access to its own `OTEL_EXPORTER_OTLP_ENDPOINT` env to propagate to pods

---

### Story 4.4: Channel Router Spans

**Points:** 3
**Dependencies:** 4.1, 3.3
**File:** `internal/controller/channel_router.go` (modify L84, L130-160)

**Description:**
Add spans to the channel router for inbound message routing and response routing.

**Acceptance Criteria:**
- [ ] `channel.route` span in `handleInbound()` (L84) with context from NATS headers
- [ ] Attributes: `channel.type`, `instance.name`, `sender.id`, `chat.id`
- [ ] Store traceparent in AgentRun annotations during creation (L130-160)
- [ ] `channel.route.response` span in `handleCompleted()` with context from NATS headers
- [ ] Attributes: `channel.type`, `agentrun.name`, `response.length`
- [ ] Record `sympozium.channel.message.total` counter (direction=inbound)

---

### Story 4.5: Schedule Reconciler Spans

**Points:** 2
**Dependencies:** 4.1
**File:** `internal/controller/sympoziumschedule_controller.go` (modify)

**Description:**
Add spans to the schedule reconciler.

**Acceptance Criteria:**
- [ ] `schedule.reconcile` span with `WithNewRoot()` (schedules always start new traces)
- [ ] `schedule.create_run` child span when creating AgentRun
- [ ] Attributes: `schedule.name`, `schedule.type`, `schedule.cron`, `instance.name`
- [ ] Write `otel.dev/traceparent` annotation on the created AgentRun

---

### Story 4.6: Controller Metrics

**Points:** 3
**Dependencies:** 4.1
**File:** `internal/controller/agentrun_controller.go` (modify L915-985)

**Description:**
Record metrics when agent runs complete.

**Acceptance Criteria:**
- [ ] `sympozium.agent.run.total` counter incremented on each AgentRun completion
- [ ] `sympozium.agent.run.duration` histogram recorded from TokenUsage.DurationMs
- [ ] Dimensions: `model`, `instance`, `namespace`, `status` (succeeded/failed), `source` (channel/schedule/api/spawn)
- [ ] Source determined from labels: `sympozium.ai/source` on AgentRun

**Implementation Notes:**
- TokenUsage extracted at L971-985
- Labels on AgentRun set during creation (channel_router.go L130, schedule_controller.go)

---

## Epic 5: IPC Bridge Instrumentation

**Priority:** P1 — Bridges file-based IPC to NATS
**Epic Points:** 10
**Blocked by:** Epics 1, 3
**Blocks:** None

### Story 5.1: IPC Bridge OTel Init

**Points:** 2
**Dependencies:** 1.1, 1.3
**File:** `cmd/ipc-bridge/main.go` (modify L19)

**Description:**
Initialize OTel SDK in the IPC bridge sidecar.

**Acceptance Criteria:**
- [ ] Call `telemetry.Init()` with `ServiceName: "sympozium-ipc-bridge"`
- [ ] Call `telemetry.ExtractParentFromEnv(ctx)` for TRACEPARENT
- [ ] `defer tel.Shutdown(ctx)` on main context
- [ ] Package-level tracer/meter
- [ ] Noop if endpoint unset

---

### Story 5.2: File Watcher Relay Spans

**Points:** 3
**Dependencies:** 5.1
**File:** `internal/ipc/bridge.go` (modify L75-79)

**Description:**
Add spans for each file event relayed by the IPC bridge.

**Acceptance Criteria:**
- [ ] `ipc.bridge.relay` span per file event processed
- [ ] Attributes: `direction` (outbound), `event.type` (result/stream/spawn/message/schedule), `agentrun.id`
- [ ] `ipc.bridge.file_watch` child span with `file.path`, `file.type`
- [ ] Spans correctly parented to the trace from TRACEPARENT env

---

### Story 5.3: NATS Publish Spans

**Points:** 3
**Dependencies:** 5.2, 3.2
**File:** `internal/ipc/bridge.go` (modify)

**Description:**
Add spans for NATS publishes from the IPC bridge.

**Acceptance Criteria:**
- [ ] `ipc.bridge.nats_publish` child span for each NATS publish
- [ ] Attributes: `nats.subject`, `event.topic`
- [ ] Trace context automatically injected in NATS headers (via Story 3.2)
- [ ] Record `sympozium.ipc.request.duration` histogram

---

### Story 5.4: Context File for Skill Sidecars

**Points:** 2
**Dependencies:** 5.1
**File:** `internal/ipc/bridge.go` (modify) or `cmd/agent-runner/tools.go` (modify)

**Description:**
Write `context-<call-id>.json` files alongside exec requests for future sidecar use.

**Acceptance Criteria:**
- [ ] When agent-runner writes `exec-request-<call-id>.json`, also write `context-<call-id>.json`
- [ ] Context file contains `{"traceparent": "...", "tracestate": "..."}`
- [ ] Skill sidecars currently ignore this file (no behavior change)
- [ ] File cleaned up with the request file

---

## Epic 6: API Server & Channel Pod Instrumentation

**Priority:** P2 — Completes end-to-end traces
**Epic Points:** 12
**Blocked by:** Epics 1, 3
**Blocks:** None

### Story 6.1: API Server OTel Init

**Points:** 1
**Dependencies:** 1.1
**File:** `cmd/apiserver/main.go` (modify)

**Description:**
Initialize OTel SDK in the API server.

**Acceptance Criteria:**
- [ ] `telemetry.Init()` with `ServiceName: "sympozium-apiserver"`
- [ ] `defer tel.Shutdown(ctx)`
- [ ] Noop if endpoint unset

---

### Story 6.2: API Server HTTP Middleware

**Points:** 2
**Dependencies:** 6.1
**File:** `internal/apiserver/server.go` (modify L45-76)

**Description:**
Add `otelhttp` middleware for automatic HTTP span instrumentation.

**Acceptance Criteria:**
- [ ] All API endpoints auto-instrumented with `http.server.request` spans
- [ ] Span attributes: `http.request.method`, `url.path`, `http.response.status_code`, `http.route`
- [ ] Trace context extracted from HTTP `traceparent` header on incoming requests
- [ ] Existing `/metrics` endpoint unaffected

**Implementation Notes:**
- Wrap `http.ServeMux` with `otelhttp.NewHandler(mux, "sympozium-apiserver")`
- Routes registered at L48-76
- Requires `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp`

---

### Story 6.3: WebSocket Stream Span

**Points:** 2
**Dependencies:** 6.1
**File:** `internal/apiserver/server.go` (modify)

**Description:**
Add span for WebSocket streaming connections.

**Acceptance Criteria:**
- [ ] `ws.stream` span wraps WebSocket connection lifetime
- [ ] Attributes: `ws.client_id`, `agentrun.id`, `stream.chunks.count`
- [ ] Span ends when connection closes

---

### Story 6.4: Channel Pod OTel Init (All 4 Channels)

**Points:** 3
**Dependencies:** 1.1
**Files:** `channels/telegram/main.go`, `channels/slack/main.go`, `channels/discord/main.go`, `channels/whatsapp/main.go` (modify all)

**Description:**
Initialize OTel SDK in each channel pod with channel-specific resource attributes.

**Acceptance Criteria:**
- [ ] Each channel calls `telemetry.Init()` with channel-specific service name
- [ ] `ExtraResource: attribute.String("channel.type", "telegram")` (etc.)
- [ ] `defer tel.Shutdown(ctx)`
- [ ] All 4 channels instrumented consistently

---

### Story 6.5: Channel Inbound/Outbound Spans

**Points:** 3
**Dependencies:** 6.4, 3.3
**Files:** `channels/telegram/main.go`, `channels/slack/main.go`, `channels/discord/main.go`, `channels/whatsapp/main.go` (modify all)

**Description:**
Add message inbound/outbound spans in each channel pod.

**Acceptance Criteria:**
- [ ] `channel.message.inbound` span (kind: PRODUCER) when message received from external API
- [ ] Attributes: `channel.type`, `instance.name`, `sender.id`, `chat.id`, `message.length`
- [ ] `channel.message.outbound` span (kind: CONSUMER) when sending response
- [ ] Outbound span parented from NATS header trace context
- [ ] Attributes: `channel.type`, `chat.id`, `message.length`, `delivery.success`
- [ ] Record `sympozium.channel.message.total` counter

---

### Story 6.6: Channel Health Spans

**Points:** 1
**Dependencies:** 6.4
**Files:** All channel `main.go` (modify)

**Description:**
Add spans for channel health check events.

**Acceptance Criteria:**
- [ ] `channel.health.check` span when health status changes
- [ ] Attributes: `channel.type`, `channel.connected` (bool)
- [ ] Lightweight — no high cardinality

---

## Epic 7: Configuration, Helm, & Testing

**Priority:** P3 — User-facing configuration and quality assurance
**Epic Points:** 10
**Blocked by:** Epics 1-6 (partially; CRD changes can start early)
**Blocks:** None

### Story 7.1: CRD ObservabilitySpec

**Points:** 2
**Dependencies:** None (can start in parallel with Epic 1)
**File:** `api/v1alpha1/sympoziuminstance_types.go` (modify L11-35)

**Description:**
Add `ObservabilitySpec` to SympoziumInstance CRD.

**Acceptance Criteria:**
- [ ] `Observability *ObservabilitySpec` field added to `SympoziumInstanceSpec`
- [ ] `ObservabilitySpec` struct: `Enabled`, `Endpoint`, `Protocol`, `Headers`, `HeadersSecretRef`, `SamplingRatio`, `ResourceAttributes`
- [ ] Kubebuilder markers for validation (Enum on Protocol, Min/Max on SamplingRatio)
- [ ] `make generate manifests` succeeds

---

### Story 7.2: CRD TraceID in Status

**Points:** 1
**Dependencies:** None
**File:** `api/v1alpha1/agentrun_types.go` (modify L166-205)

**Description:**
Add `TraceID` field to `AgentRunStatus`.

**Acceptance Criteria:**
- [ ] `TraceID string` field added after `TokenUsage` (L201)
- [ ] `json:"traceID,omitempty"` tag
- [ ] `make generate manifests` succeeds

---

### Story 7.3: Helm Values & Templates

**Points:** 3
**Dependencies:** 7.1
**Files:** `charts/sympozium/values.yaml`, `charts/sympozium/templates/controller-deployment.yaml`, `charts/sympozium/templates/apiserver-deployment.yaml`

**Description:**
Add `observability` section to Helm values and inject env vars into deployments.

**Acceptance Criteria:**
- [ ] `observability` section in values.yaml with all fields from arch doc
- [ ] Controller deployment template injects OTel env vars when `observability.enabled=true`
- [ ] API server deployment template injects OTel env vars
- [ ] K8s downward API env vars (POD_NAME, NAMESPACE, NODE_NAME) added to both
- [ ] `helm template` renders correctly with observability enabled and disabled
- [ ] NetworkPolicy template allows OTel egress when enabled

---

### Story 7.4: Agent-Runner Integration Test

**Points:** 2
**Dependencies:** 2.3, 2.5
**File:** `cmd/agent-runner/main_test.go` (new or modify)

**Description:**
Integration test verifying the span hierarchy with a mock LLM server.

**Acceptance Criteria:**
- [ ] Test starts mock HTTP server returning tool_use then end_turn
- [ ] Runs agent-runner logic with in-memory OTel exporter
- [ ] Asserts `agent.run` root span exists
- [ ] Asserts `gen_ai.chat` child spans with correct attributes
- [ ] Asserts `tool.execute` child spans
- [ ] Asserts GenAI semantic convention attributes (system, model, tokens)

---

### Story 7.5: NATS Propagation Integration Test

**Points:** 1
**Dependencies:** 3.2, 3.3
**File:** `internal/eventbus/nats_test.go` (new or modify)

**Description:**
Integration test verifying trace context survives publish→subscribe.

**Acceptance Criteria:**
- [ ] Test publishes event with trace context
- [ ] Subscribes and verifies same trace ID received
- [ ] Verifies remote span context flag is set

---

### Story 7.6: OTel Endpoint Resolution Logic

**Points:** 1
**Dependencies:** 4.3, 7.1
**File:** `internal/controller/agentrun_controller.go` (modify)

**Description:**
Controller resolves OTel endpoint from instance CRD → Helm default → empty.

**Acceptance Criteria:**
- [ ] `resolveOTelEndpoint(ctx, agentRun)` method on reconciler
- [ ] Reads `SympoziumInstance.spec.observability.endpoint` first
- [ ] Falls back to controller's own `OTEL_EXPORTER_OTLP_ENDPOINT` env
- [ ] Returns empty string if neither set (agent-runner runs noop)

---

## Dependency Graph

```
Epic 1 (pkg/telemetry)
├──► Epic 2 (Agent Runner)
├──► Epic 3 (NATS Event Bus)
│    ├──► Epic 4 (Controller)
│    ├──► Epic 5 (IPC Bridge)
│    └──► Epic 6 (API & Channels)
└──► Epic 6 (API & Channels)

Epic 7 (Config/Helm/Tests) — can start CRD stories (7.1, 7.2) in parallel with Epic 1

Story-Level Dependencies:
1.1 ──► 1.2, 1.3, 1.4
1.1 + 1.3 ──► 2.1
2.1 ──► 2.2 ──► 2.3, 2.4, 2.5, 2.6
2.1 ──► 2.7
1.1 ──► 3.1 ──► 3.2, 3.3
3.1-3.3 ──► 3.4
1.1 ──► 4.1 ──► 4.2, 4.5, 4.6
4.2 + 3.2 ──► 4.3
4.1 + 3.3 ──► 4.4
1.1 + 1.3 ──► 5.1 ──► 5.2, 5.4
5.2 + 3.2 ──► 5.3
1.1 ──► 6.1 ──► 6.2, 6.3
1.1 ──► 6.4 ──► 6.5, 6.6
6.4 + 3.3 ──► 6.5
```

## Summary Table

| Epic | Title | Points | Stories | Priority |
|------|-------|--------|---------|----------|
| 1 | Shared Telemetry Package | 13 | 4 | P0 |
| 2 | Agent Runner Instrumentation | 18 | 7 | P0 |
| 3 | NATS Event Bus Propagation | 8 | 4 | P1 |
| 4 | Controller Instrumentation | 18 | 6 | P1 |
| 5 | IPC Bridge Instrumentation | 10 | 4 | P1 |
| 6 | API Server & Channel Pods | 12 | 6 | P2 |
| 7 | Configuration, Helm, Testing | 10 | 6 | P3 |
| **Total** | | **89** | **37** | |
