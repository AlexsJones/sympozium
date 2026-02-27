# Architecture: OpenTelemetry Instrumentation for Sympozium

**Version:** 1.0
**Date:** 2026-02-26
**GitHub Issue:** #11
**PRD Reference:** `_bmad/prd-otel-instrumentation.md`
**Debate Record:** `_bmad/party-mode-otel-debate.md`

---

## Table of Contents

1. [System Context Diagram](#1-system-context-diagram)
2. [Component Diagram — OTel SDK Placement](#2-component-diagram--otel-sdk-placement)
3. [Sequence Diagrams](#3-sequence-diagrams)
4. [Data Model](#4-data-model)
5. [Context Propagation Design](#5-context-propagation-design)
6. [pkg/telemetry Package Design](#6-pkgtelemetry-package-design)
7. [CRD Schema Changes](#7-crd-schema-changes)
8. [Architecture Decision Records](#8-architecture-decision-records)
9. [Deployment Architecture](#9-deployment-architecture)

---

## 1. System Context Diagram

### 1.1 High-Level System Boundary

```
┌─────────────────────────────── EXTERNAL ─────────────────────────────────┐
│                                                                          │
│  ┌────────────┐  ┌────────────┐  ┌────────────┐  ┌────────────┐        │
│  │  Telegram   │  │   Slack    │  │  Discord   │  │  WhatsApp  │        │
│  │  Bot API    │  │  Socket/   │  │  Gateway   │  │  Web API   │        │
│  │            │  │  Events    │  │  WebSocket │  │  (whatsmeow)│        │
│  └─────┬──────┘  └─────┬──────┘  └─────┬──────┘  └─────┬──────┘        │
│        │               │               │               │                │
│  ┌────────────┐  ┌────────────┐  ┌────────────┐  ┌────────────┐        │
│  │  Anthropic  │  │  OpenAI    │  │Azure OpenAI│  │   Ollama   │        │
│  │  API        │  │  API       │  │  API       │  │  (local)   │        │
│  └─────┬──────┘  └─────┬──────┘  └─────┬──────┘  └─────┬──────┘        │
│        │               │               │               │                │
└────────┼───────────────┼───────────────┼───────────────┼────────────────┘
         │               │               │               │
┌────────┼───────────────┼───────────────┼───────────────┼────────────────┐
│        │          KUBERNETES CLUSTER    │               │                │
│        ▼               ▼               ▼               ▼                │
│  ┌─────────────────────────────────────────────────────────────────┐    │
│  │                     SYMPOZIUM SYSTEM                             │    │
│  │                                                                  │    │
│  │  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐           │    │
│  │  │ Channel  │ │ Channel  │ │ Channel  │ │ Channel  │           │    │
│  │  │ Telegram │ │  Slack   │ │ Discord  │ │ WhatsApp │           │    │
│  │  │ (Deploy) │ │ (Deploy) │ │ (Deploy) │ │ (Deploy) │           │    │
│  │  └────┬─────┘ └────┬─────┘ └────┬─────┘ └────┬─────┘           │    │
│  │       │             │            │             │                 │    │
│  │       └──────┬──────┴─────┬──────┴──────┬──────┘                │    │
│  │              ▼            │             ▼                        │    │
│  │  ┌───────────────────┐   │  ┌────────────────────┐              │    │
│  │  │   NATS JetStream  │◄──┴─►│  Controller Manager │              │    │
│  │  │   (Event Bus)     │      │  (Deployment)       │              │    │
│  │  └────────┬──────────┘      │  ┌────────────────┐ │              │    │
│  │           │                 │  │ Channel Router  │ │              │    │
│  │           │                 │  │ Schedule Recncl │ │              │    │
│  │           │                 │  │ AgentRun Recncl │ │              │    │
│  │           │                 │  └────────────────┘ │              │    │
│  │           │                 └─────────┬──────────┘              │    │
│  │           │                           │                          │    │
│  │           │                    Creates │ K8s Jobs                 │    │
│  │           │                           ▼                          │    │
│  │           │           ┌───────────────────────────┐              │    │
│  │           │           │ Agent Pod (ephemeral Job)  │              │    │
│  │           │◄──────────┤ ┌───────────┐ ┌─────────┐ │              │    │
│  │           │           │ │ agent-    │ │ ipc-    │ │              │    │
│  │           │           │ │ runner    │ │ bridge  │ │              │    │
│  │           │           │ └───────────┘ └─────────┘ │              │    │
│  │           │           │ ┌───────────┐ ┌─────────┐ │              │    │
│  │           │           │ │ sandbox   │ │ skill   │ │              │    │
│  │           │           │ │ (optional)│ │ sidecar │ │              │    │
│  │           │           │ └───────────┘ └─────────┘ │              │    │
│  │           │           └───────────────────────────┘              │    │
│  │           │                                                      │    │
│  │  ┌───────┴───────┐  ┌────────────────────┐                     │    │
│  │  │  API Server    │  │  Webhook Server    │                     │    │
│  │  │  (Deployment)  │  │  (Deployment)      │                     │    │
│  │  └───────────────┘  └────────────────────┘                     │    │
│  └─────────────────────────────────────────────────────────────────┘    │
│                                    │                                    │
│                                    │ OTLP (gRPC :4317)                  │
│                                    ▼                                    │
│                      ┌──────────────────────────┐                      │
│                      │   OTel Collector          │  ← USER-PROVIDED    │
│                      │   (or Grafana Alloy /     │                      │
│                      │    Datadog Agent / etc.)  │                      │
│                      └─────────┬────────────────┘                      │
│                                │                                        │
└────────────────────────────────┼────────────────────────────────────────┘
                                 │
                                 ▼
                   ┌──────────────────────────┐
                   │  Observability Backend    │
                   │  Jaeger / Tempo / Datadog │
                   │  Prometheus / Mimir       │
                   │  Loki / CloudWatch        │
                   └──────────────────────────┘
```

### 1.2 Key Insight

The OTel Collector is **not part of Sympozium**. Every Sympozium component exports OTLP directly to a user-provided endpoint. Sympozium ships only the SDK instrumentation, not the collection infrastructure.

---

## 2. Component Diagram — OTel SDK Placement

### 2.1 Which Binaries Get the OTel SDK

```
┌─────────────────────────────────────────────────────────────────────────┐
│                        OTel SDK PLACEMENT MAP                           │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                         │
│  HAS OTel SDK (pkg/telemetry)         │  NO OTel SDK                   │
│  ─────────────────────────────        │  ────────────                  │
│                                        │                                │
│  ✓ cmd/controller/main.go             │  ✗ Skill Sidecars (user images)│
│    └─ internal/controller/*            │  ✗ Sandbox container           │
│                                        │  ✗ NATS server                 │
│  ✓ cmd/agent-runner/main.go           │  ✗ Webhook server              │
│    └─ cmd/agent-runner/tools.go        │                                │
│                                        │                                │
│  ✓ cmd/ipc-bridge/main.go            │                                │
│    └─ internal/ipc/bridge.go           │                                │
│                                        │                                │
│  ✓ cmd/apiserver/main.go             │                                │
│    └─ internal/apiserver/server.go     │                                │
│                                        │                                │
│  ✓ channels/telegram/main.go          │                                │
│  ✓ channels/slack/main.go             │                                │
│  ✓ channels/discord/main.go           │                                │
│  ✓ channels/whatsapp/main.go          │                                │
│                                        │                                │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                         │
│  SHARED PACKAGE: pkg/telemetry/                                        │
│  ├── telemetry.go     Init(), Shutdown(), Config, *Telemetry           │
│  ├── resource.go      K8s resource detection from env vars             │
│  └── propagation.go   TRACEPARENT env var parsing                      │
│                                                                         │
│  EVENT BUS EXTENSION: internal/eventbus/otel.go                        │
│  └── natsHeaderCarrier  TextMapCarrier for NATS message headers        │
│                                                                         │
└─────────────────────────────────────────────────────────────────────────┘
```

### 2.2 Per-Component OTel Configuration

| Component | Binary | Service Name | BatchTimeout | Lifecycle | Special |
|-----------|--------|-------------|-------------|-----------|---------|
| Controller Manager | `cmd/controller/main.go` | `sympozium-controller` | 5s | Long-lived singleton | Reconciler spans; TRACEPARENT annotation injection |
| Agent Runner | `cmd/agent-runner/main.go` | `sympozium-agent-runner` | **1s** | **Ephemeral** (Job) | GenAI spans; reads TRACEPARENT env; 10s shutdown grace |
| IPC Bridge | `cmd/ipc-bridge/main.go` | `sympozium-ipc-bridge` | 5s | Tied to agent pod | File watcher → NATS relay spans; reads TRACEPARENT env |
| API Server | `cmd/apiserver/main.go` | `sympozium-apiserver` | 5s | Long-lived | otelhttp middleware; WebSocket spans |
| Channel (Telegram) | `channels/telegram/main.go` | `sympozium-channel-telegram` | 5s | Long-lived | Inbound/outbound message spans |
| Channel (Slack) | `channels/slack/main.go` | `sympozium-channel-slack` | 5s | Long-lived | Socket Mode + Events API spans |
| Channel (Discord) | `channels/discord/main.go` | `sympozium-channel-discord` | 5s | Long-lived | Gateway WebSocket spans |
| Channel (WhatsApp) | `channels/whatsapp/main.go` | `sympozium-channel-whatsapp` | 5s | Long-lived | whatsmeow event spans |

### 2.3 Code Integration Points Per Component

```
cmd/agent-runner/main.go
│
├── L44: main()
│   ├── NEW: telemetry.Init(ctx, Config{BatchTimeout: 1s})
│   ├── NEW: defer tel.Shutdown(shutdownCtx)  // 10s deadline
│   ├── NEW: ctx = tel.ExtractParentFromEnv(ctx)
│   ├── NEW: ctx, rootSpan = tracer.Start(ctx, "agent.run")
│   │
│   ├── L137: start := time.Now()
│   ├── L147-153: switch provider {
│   │   ├── L219-326: callAnthropic()  ← NEW: gen_ai.chat spans per iteration
│   │   └── L332-431: callOpenAI()     ← NEW: gen_ai.chat spans per iteration
│   │
│   ├── L155: elapsed := time.Since(start)
│   ├── L197: write result.json
│   ├── L200: write done sentinel
│   └── NEW: rootSpan.End()
│
├── L196: executeToolCall()  ← NEW: tool.execute span wrapper
│   ├── L335: fetchURLTool()       ← NEW: tool.fetch_url child span
│   └── L620-695: executeCommand() ← NEW: ipc.exec_request child span
│       └── L666-692: polling loop
│
cmd/ipc-bridge/main.go
│
├── L19: main()
│   ├── L26-28: env vars (AGENT_RUN_ID, INSTANCE_NAME, EVENT_BUS_URL)
│   ├── NEW: telemetry.Init(ctx, Config{ServiceName: "sympozium-ipc-bridge"})
│   └── NEW: ctx = tel.ExtractParentFromEnv(ctx)
│
internal/ipc/bridge.go
│
├── L34-43: Bridge struct
├── L75-79: watcher setup
│   └── NEW: ipc.bridge.relay spans on each file event
│
internal/eventbus/nats.go
│
├── L19-23: NATSEventBus struct
├── L73: Publish()  ← MODIFY: inject traceparent into NATS headers
├── L89: Subscribe() ← MODIFY: extract traceparent from NATS headers
│
internal/controller/agentrun_controller.go
│
├── L38-46: AgentRunReconciler struct
├── L69: Reconcile()  ← NEW: agentrun.reconcile span
├── L116: reconcilePending()
│   ├── NEW: write otel.dev/traceparent annotation on AgentRun
│   └── L562-766: buildContainers()
│       └── L579-589: env vars ← NEW: inject TRACEPARENT + OTEL_EXPORTER_OTLP_ENDPOINT
├── L193: reconcileRunning()
├── L915: extractResultFromPod()
│   └── L971-985: TokenUsage extraction ← NEW: record metrics
│
internal/controller/channel_router.go
│
├── L25-29: ChannelRouter struct
├── L84: handleInbound()  ← NEW: channel.route span
│   └── L130-160: create AgentRun ← NEW: add otel.dev/traceparent annotation
│
internal/apiserver/server.go
│
├── L45-76: route registration ← NEW: wrap mux with otelhttp handler
├── L77: /metrics endpoint (existing Prometheus — unchanged)
│
channels/*/main.go
│
├── NEW: telemetry.Init with channel.type resource attribute
├── Inbound handler ← NEW: channel.message.inbound span
└── Outbound handler ← NEW: channel.message.outbound span (from NATS context)
```

---

## 3. Sequence Diagrams

### 3.1 Agent Run Trace (Channel-Triggered)

Full end-to-end trace for a user message arriving via a channel, processed by an agent, and response delivered back.

```
User          Channel Pod       NATS         Controller Manager         K8s API    Agent Pod          OTel Collector
 │               │               │          (Router + Reconciler)        │          │                    │
 │──message──►   │               │               │                      │          │                    │
 │               │               │               │                      │          │                    │
 │          ┌────┴────┐          │               │                      │          │                    │
 │          │ START   │          │               │                      │          │                    │
 │          │ span:   │          │               │                      │          │                    │
 │          │ channel.│          │               │                      │          │                    │
 │          │ message.│          │               │                      │          │                    │
 │          │ inbound │          │               │                      │          │                    │
 │          └────┬────┘          │               │                      │          │                    │
 │               │               │               │                      │          │                    │
 │               │──Publish──►   │               │                      │          │                    │
 │               │  topic:       │               │                      │          │                    │
 │               │  channel.     │               │                      │          │                    │
 │               │  message.     │               │                      │          │                    │
 │               │  received     │               │                      │          │                    │
 │               │  [traceparent │               │                      │          │                    │
 │               │   in header]  │               │                      │          │                    │
 │               │               │               │                      │          │                    │
 │               │           END │──Subscribe──► │                      │          │                    │
 │               │          span │  extract      │                      │          │                    │
 │               │               │  traceparent  │                      │          │                    │
 │               │               │               │                      │          │                    │
 │               │               │          ┌────┴────┐                 │          │                    │
 │               │               │          │ START   │                 │          │                    │
 │               │               │          │ span:   │                 │          │                    │
 │               │               │          │ channel.│                 │          │                    │
 │               │               │          │ route   │                 │          │                    │
 │               │               │          └────┬────┘                 │          │                    │
 │               │               │               │                      │          │                    │
 │               │               │               │──Create AgentRun──►  │          │                    │
 │               │               │               │  annotation:         │          │                    │
 │               │               │               │  otel.dev/           │          │                    │
 │               │               │               │  traceparent         │          │                    │
 │               │               │               │                      │          │                    │
 │               │               │          ┌────┴────┐                 │          │                    │
 │               │               │          │ START   │                 │          │                    │
 │               │               │          │ span:   │   Reconcile     │          │                    │
 │               │               │          │ agentrun│◄────event────── │          │                    │
 │               │               │          │ .recon- │                 │          │                    │
 │               │               │          │ cile    │                 │          │                    │
 │               │               │          └────┬────┘                 │          │                    │
 │               │               │               │                      │          │                    │
 │               │               │               │──Create Job──────►   │          │                    │
 │               │               │               │  env: TRACEPARENT    │──spawn──►│                    │
 │               │               │               │  env: OTEL_EXPORTER  │          │                    │
 │               │               │               │       _OTLP_ENDPOINT │          │                    │
 │               │               │               │                      │          │                    │
 │               │               │               │                      │     ┌────┴────┐              │
 │               │               │               │                      │     │ START   │              │
 │               │               │               │                      │     │ span:   │              │
 │               │               │               │                      │     │ agent.  │              │
 │               │               │               │                      │     │ run     │              │
 │               │               │               │                      │     │ (parent │              │
 │               │               │               │                      │     │  from   │              │
 │               │               │               │                      │     │  TRACE- │              │
 │               │               │               │                      │     │  PARENT)│              │
 │               │               │               │                      │     └────┬────┘              │
 │               │               │               │                      │          │                    │
 │               │               │               │                      │     ┌────┴────┐              │
 │               │               │               │                      │     │gen_ai.  │              │
 │               │               │               │                      │     │chat #1  │──export──►   │
 │               │               │               │                      │     └────┬────┘              │
 │               │               │               │                      │     ┌────┴────┐              │
 │               │               │               │                      │     │tool.    │              │
 │               │               │               │                      │     │execute  │──export──►   │
 │               │               │               │                      │     └────┬────┘              │
 │               │               │               │                      │     ┌────┴────┐              │
 │               │               │               │                      │     │gen_ai.  │              │
 │               │               │               │                      │     │chat #2  │──export──►   │
 │               │               │               │                      │     └────┬────┘              │
 │               │               │               │                      │          │                    │
 │               │               │               │                      │     write result.json        │
 │               │               │               │                      │     write done sentinel       │
 │               │               │               │                      │     END span: agent.run       │
 │               │               │               │                      │     Shutdown(10s)──export──►  │
 │               │               │               │                      │          │                    │
 │               │               │     IPC Bridge publishes             │          │                    │
 │               │  ◄────────────┤──agent.run.completed──               │          │                    │
 │               │               │  [traceparent in header]             │          │                    │
 │               │               │               │                      │          │                    │
 │               │               │          ┌────┴────┐                 │          │                    │
 │               │               │          │ START   │                 │          │                    │
 │               │               │          │ span:   │                 │          │                    │
 │               │               │          │ channel.│                 │          │                    │
 │               │               │          │ route.  │                 │          │                    │
 │               │               │          │ response│                 │          │                    │
 │               │               │          └────┬────┘                 │          │                    │
 │               │               │               │                      │          │                    │
 │               │               │◄──Publish──── │                      │          │                    │
 │               │               │  channel.     │                      │          │                    │
 │               │               │  message.send │                      │          │                    │
 │               │               │  [traceparent]│                      │          │                    │
 │               │               │               │                      │          │                    │
 │          ┌────┴────┐          │               │                      │          │                    │
 │          │ START   │◄─Subscribe                                      │          │                    │
 │          │ span:   │          │               │                      │          │                    │
 │          │ channel.│          │               │                      │          │                    │
 │          │ message.│          │               │                      │          │                    │
 │          │ outbound│          │               │                      │          │                    │
 │          └────┬────┘          │               │                      │          │                    │
 │               │               │               │                      │          │                    │
 │◄──response──  │               │               │                      │          │                    │
 │               │               │               │                      │          │                    │
```

**Trace ID** is the same across all spans in the diagram — one distributed trace.

### 3.2 Tool Call Trace (Agent Runner Detail)

Zoomed-in view of the agent-runner tool-calling loop. All spans share the `agent.run` root span.

```
agent.run (root span, from TRACEPARENT env var)
│
│  ┌── gen_ai.chat ─────────────────────────────────────────┐
│  │  kind: CLIENT                                           │
│  │  attributes:                                            │
│  │    gen_ai.system = "anthropic"                          │
│  │    gen_ai.request.model = "claude-sonnet-4-20250514"       │
│  │    gen_ai.usage.input_tokens = 1500                     │
│  │    gen_ai.usage.output_tokens = 200                     │
│  │    gen_ai.response.finish_reasons = ["tool_use"]        │
│  │    gen_ai.chat.iteration = 1                            │
│  │                                                         │
│  │  Source: callAnthropic() L219-326                       │
│  │  API call: client.Messages.New() L250                   │
│  └─────────────────────────────────────────────────────────┘
│
│  ┌── tool.execute ────────────────────────────────────────┐
│  │  kind: INTERNAL                                         │
│  │  attributes:                                            │
│  │    tool.name = "read_file"                              │
│  │    tool.success = true                                  │
│  │    tool.duration_ms = 2                                 │
│  │                                                         │
│  │  Source: executeToolCall() L196                          │
│  └─────────────────────────────────────────────────────────┘
│
│  ┌── gen_ai.chat ─────────────────────────────────────────┐
│  │  gen_ai.chat.iteration = 2                              │
│  │  gen_ai.response.finish_reasons = ["tool_use"]          │
│  └─────────────────────────────────────────────────────────┘
│
│  ┌── tool.execute ────────────────────────────────────────┐
│  │  tool.name = "execute_command"                          │
│  │  tool.success = true                                    │
│  │  tool.duration_ms = 3200                                │
│  │                                                         │
│  │  ┌── ipc.exec_request ───────────────────────────┐     │
│  │  │  kind: CLIENT                                   │     │
│  │  │  attributes:                                    │     │
│  │  │    ipc.request_id = "abc-123"                   │     │
│  │  │    ipc.timeout_s = 10                           │     │
│  │  │    ipc.wait_duration_ms = 3180                  │     │
│  │  │                                                 │     │
│  │  │  Source: executeCommand() L620                   │     │
│  │  │  Polling: L666-692                              │     │
│  │  │  Writes: /ipc/tools/exec-request-abc-123.json   │     │
│  │  │  Reads:  /ipc/tools/exec-result-abc-123.json    │     │
│  │  └─────────────────────────────────────────────────┘     │
│  └─────────────────────────────────────────────────────────┘
│
│  ┌── gen_ai.chat ─────────────────────────────────────────┐
│  │  gen_ai.chat.iteration = 3                              │
│  │  gen_ai.response.finish_reasons = ["end_turn"]          │
│  │  gen_ai.usage.input_tokens = 3500                       │
│  │  gen_ai.usage.output_tokens = 800                       │
│  └─────────────────────────────────────────────────────────┘
│
└── END agent.run
    total_input_tokens = 7000
    total_output_tokens = 1200
    total_tool_calls = 2
    duration_ms = 12500
    status = "succeeded"
```

### 3.3 Channel Message Trace (Cross-Component)

Shows how `traceparent` propagates across process boundaries.

```
 Process 1:              Process 2:                    Process 3:             Process 2:              Process 1:
 Channel Pod             Controller Manager            Agent Pod              Controller Manager      Channel Pod
 (Telegram)              (Router + Reconciler)          (ephemeral Job)        (Router)                (Telegram)

 ┌──────────┐
 │ inbound  │
 │ span     │
 │ trace:   │
 │ aaaa...  │
 │ span:    │
 │ 1111...  │
 └────┬─────┘
      │
      │ NATS header:
      │ traceparent:
      │ 00-aaaa-1111-01
      │                  ┌──────────┐
      └─────────────────►│ route    │
                         │ span     │
                         │ trace:   │
                         │ aaaa...  │
                         │ span:    │
                         │ 2222...  │
                         │ parent:  │
                         │ 1111...  │
                         └────┬─────┘
                              │
                              │ CRD annotation:
                              │ otel.dev/traceparent:
                              │ 00-aaaa-2222-01
                              │
                         ┌────┴─────┐
                         │ reconcile│
                         │ span     │
                         │ trace:   │
                         │ aaaa...  │
                         │ span:    │
                         │ 3333...  │
                         └────┬─────┘
                              │
                              │ Pod env var:
                              │ TRACEPARENT=
                              │ 00-aaaa-3333-01
                              │                   ┌──────────┐
                              └──────────────────►│ agent.run│
                                                  │ span     │
                                                  │ trace:   │
                                                  │ aaaa...  │
                                                  │ span:    │
                                                  │ 4444...  │
                                                  │ parent:  │
                                                  │ 3333...  │
                                                  └────┬─────┘
                                                       │
                                        IPC Bridge NATS header:
                                        traceparent:
                                        00-aaaa-4444-01
                                                       │
                                                  ┌────┴─────┐
                                                  └──────────┘
                                                       │
                         ┌──────────┐                  │
                         │ route.   │◄─────────────────┘
                         │ response │
                         │ trace:   │
                         │ aaaa...  │
                         │ span:    │
                         │ 5555...  │
                         └────┬─────┘
                              │
                              │ NATS header:
                              │ traceparent:
                              │ 00-aaaa-5555-01
      ┌──────────┐            │
      │ outbound │◄───────────┘
      │ span     │
      │ trace:   │
      │ aaaa...  │  ← SAME TRACE ID THROUGHOUT
      │ span:    │
      │ 6666...  │
      │ parent:  │
      │ 5555...  │
      └──────────┘
```

**Key**: All spans share trace ID `aaaa...`. The trace crosses 3 OS processes and 2 transport boundaries (NATS, K8s Job env var) while maintaining a single distributed trace.

---

## 4. Data Model

### 4.1 Span Schema

Each span conforms to the [OTel Span data model](https://opentelemetry.io/docs/specs/otel/trace/api/#span). Below are the Sympozium-specific schemas.

#### 4.1.1 GenAI Chat Span

```
Span Name:     "gen_ai.chat"
Span Kind:     CLIENT
Status:        OK | ERROR (on API failure)

Required Attributes:
  gen_ai.system              string    "anthropic" | "openai"
  gen_ai.request.model       string    "claude-sonnet-4-20250514" | "gpt-4o" | etc.

Conditional Attributes (set after response):
  gen_ai.response.model      string    Model returned by provider
  gen_ai.usage.input_tokens  int       Prompt tokens for this call
  gen_ai.usage.output_tokens int       Completion tokens for this call
  gen_ai.response.finish_reasons []string  ["end_turn"] | ["tool_use"] | ["stop"]

Sympozium Extensions:
  gen_ai.chat.iteration      int       1-based iteration in tool loop (max 25)
  sympozium.instance         string    SympoziumInstance name
  sympozium.agent.run.id     string    AgentRun K8s resource name

Events:
  gen_ai.chat.error          On API error: {error.type, error.message}

Source Locations:
  Anthropic: cmd/agent-runner/main.go L250 (client.Messages.New)
  OpenAI:    cmd/agent-runner/main.go L380 (client.Chat.Completions.New)
```

#### 4.1.2 Tool Execute Span

```
Span Name:     "tool.execute"
Span Kind:     INTERNAL
Status:        OK | ERROR

Required Attributes:
  tool.name                  string    "read_file" | "write_file" | "execute_command" |
                                       "list_directory" | "send_channel_message" |
                                       "fetch_url" | "schedule_task"
  tool.success               bool      true | false

Conditional Attributes:
  tool.duration_ms           int64     Wall-clock execution time
  tool.exit_code             int       For execute_command only (from ExecResult)
  tool.file.path             string    For read_file/write_file (sanitized — no secrets in path)
  tool.file.size_bytes       int       For read_file/write_file
  url.full                   string    For fetch_url (sanitized — query params stripped)
  http.response.status_code  int       For fetch_url
  ipc.request_id             string    For execute_command (maps to exec-request-*.json)

Source Location:
  cmd/agent-runner/tools.go L196 (executeToolCall)
```

#### 4.1.3 Agent Run Span

```
Span Name:     "agent.run"
Span Kind:     INTERNAL
Status:        OK | ERROR

Required Attributes:
  sympozium.agent.run.id     string    AgentRun K8s name (AGENT_RUN_ID env)
  sympozium.agent.id         string    Agent identifier (AGENT_ID env)
  sympozium.session.key      string    Session key (SESSION_KEY env)
  sympozium.instance         string    Instance name (INSTANCE_NAME env — via IPC bridge env)
  gen_ai.system              string    Provider name
  gen_ai.request.model       string    Model name

Set on Completion:
  sympozium.agent.status     string    "succeeded" | "failed"
  sympozium.agent.total_input_tokens   int
  sympozium.agent.total_output_tokens  int
  sympozium.agent.total_tool_calls     int
  sympozium.agent.duration_ms          int64

Source Location:
  cmd/agent-runner/main.go L44 (main)
```

### 4.2 Metric Definitions

#### 4.2.1 GenAI Semantic Convention Metrics

```go
// Registered in agent-runner at package level

// Token usage histogram — one observation per LLM API call
tokenUsageHistogram = meter.Int64Histogram(
    "gen_ai.client.token.usage",
    metric.WithUnit("{token}"),
    metric.WithDescription("Number of tokens used per GenAI API call"),
    metric.WithInt64ExplicitBucketBoundaries(
        100, 500, 1000, 2000, 5000, 10000, 50000, 100000,
    ),
)
// Dimensions: gen_ai.system, gen_ai.request.model, gen_ai.token.type (input|output),
//             instance, namespace

// Operation duration histogram — one observation per LLM API call
operationDurationHistogram = meter.Float64Histogram(
    "gen_ai.client.operation.duration",
    metric.WithUnit("s"),
    metric.WithDescription("Duration of GenAI API calls"),
    metric.WithFloat64ExplicitBucketBoundaries(
        0.1, 0.5, 1, 2, 5, 10, 30, 60, 120, 300,
    ),
)
// Dimensions: gen_ai.system, gen_ai.request.model, instance, namespace
```

#### 4.2.2 Sympozium Custom Metrics

```go
// Agent run metrics (registered in controller)
agentRunDuration = meter.Float64Histogram(
    "sympozium.agent.run.duration",
    metric.WithUnit("s"),
    metric.WithFloat64ExplicitBucketBoundaries(1, 5, 10, 30, 60, 120, 300, 600),
)
// Dimensions: model, instance, namespace, status

agentRunTotal = meter.Int64Counter(
    "sympozium.agent.run.total",
    metric.WithUnit("{run}"),
)
// Dimensions: model, instance, namespace, status, source

// Tool call metrics (registered in agent-runner)
toolCallsTotal = meter.Int64Counter(
    "sympozium.agent.tool_calls.total",
    metric.WithUnit("{call}"),
)
// Dimensions: tool_name, instance, namespace, success

// IPC metrics (registered in agent-runner + ipc-bridge)
ipcRequestDuration = meter.Float64Histogram(
    "sympozium.ipc.request.duration",
    metric.WithUnit("s"),
    metric.WithFloat64ExplicitBucketBoundaries(0.01, 0.05, 0.1, 0.5, 1, 5, 10),
)
// Dimensions: request_type, instance, namespace

// Channel metrics (registered in each channel pod)
channelMessageTotal = meter.Int64Counter(
    "sympozium.channel.message.total",
    metric.WithUnit("{message}"),
)
// Dimensions: channel, direction, instance, namespace

// Event bus metrics (registered in eventbus package)
eventbusPublishTotal = meter.Int64Counter(
    "sympozium.eventbus.publish.total",
    metric.WithUnit("{event}"),
)
// Dimensions: topic, namespace

// Controller metrics (registered in controller)
controllerReconcileDuration = meter.Float64Histogram(
    "sympozium.controller.reconcile.duration",
    metric.WithUnit("s"),
    metric.WithFloat64ExplicitBucketBoundaries(0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5),
)
// Dimensions: controller, namespace

controllerReconcileTotal = meter.Int64Counter(
    "sympozium.controller.reconcile.total",
    metric.WithUnit("{reconcile}"),
)
// Dimensions: controller, result (success|error|requeue), namespace
```

### 4.3 Log Format

OTel log bridge connects to existing logging. Every log record automatically includes trace correlation.

```json
{
  "timestamp": "2026-02-26T14:30:00.123Z",
  "severity": "INFO",
  "body": "agent run completed",
  "resource": {
    "service.name": "sympozium-agent-runner",
    "service.version": "v0.0.49",
    "k8s.namespace.name": "default",
    "k8s.pod.name": "agent-run-abc123-xyz",
    "sympozium.instance": "my-assistant",
    "sympozium.component": "agent-runner"
  },
  "attributes": {
    "agent.run.id": "run-abc123",
    "status": "succeeded",
    "tokens.input": 7000,
    "tokens.output": 1200,
    "duration_ms": 12500
  },
  "traceId": "4bf92f3577b34da6a3ce929d0e0e4736",
  "spanId": "00f067aa0ba902b7"
}
```

The `traceId` and `spanId` fields enable log→trace correlation in backends like Grafana (Loki → Tempo link).

---

## 5. Context Propagation Design

### 5.1 Propagation Mechanisms Overview

| Boundary | Mechanism | Format | Direction |
|----------|-----------|--------|-----------|
| NATS event bus | NATS message headers | W3C `traceparent` + `tracestate` | Bidirectional |
| Controller → Agent Pod | K8s env var on container | `TRACEPARENT=00-{trace}-{span}-{flags}` | Controller → Pod |
| Controller → Agent Pod | CRD annotation | `otel.dev/traceparent` on AgentRun | Controller → Controller (persisted) |
| Agent → Skill Sidecar | IPC file | `/ipc/tools/context-{call-id}.json` | Agent → Sidecar (future use) |
| HTTP API | HTTP headers | W3C `traceparent` + `tracestate` | Client → API Server |

### 5.2 NATS Header Carrier

New file: `internal/eventbus/otel.go`

```go
package eventbus

import (
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/propagation"
    "github.com/nats-io/nats.go"
)

// natsHeaderCarrier adapts nats.Header to propagation.TextMapCarrier.
// This enables transparent W3C trace context propagation through NATS messages.
type natsHeaderCarrier struct {
    header nats.Header
}

func newNATSHeaderCarrier(header nats.Header) natsHeaderCarrier {
    return natsHeaderCarrier{header: header}
}

func (c natsHeaderCarrier) Get(key string) string {
    return c.header.Get(key)
}

func (c natsHeaderCarrier) Set(key, value string) {
    c.header.Set(key, value)
}

func (c natsHeaderCarrier) Keys() []string {
    keys := make([]string, 0, len(c.header))
    for k := range c.header {
        keys = append(keys, k)
    }
    return keys
}

// InjectTraceContext adds the current span's trace context to NATS message headers.
func InjectTraceContext(ctx context.Context, header nats.Header) {
    otel.GetTextMapPropagator().Inject(ctx, newNATSHeaderCarrier(header))
}

// ExtractTraceContext reads trace context from NATS message headers into a context.
func ExtractTraceContext(ctx context.Context, header nats.Header) context.Context {
    return otel.GetTextMapPropagator().Extract(ctx, newNATSHeaderCarrier(header))
}
```

### 5.3 Modified Publish/Subscribe

Changes to `internal/eventbus/nats.go`:

```go
// Publish — MODIFIED (L73)
// Before: js.Publish(ctx, subject, data)
// After:  js.PublishMsg(ctx, msg) with headers

func (n *NATSEventBus) Publish(ctx context.Context, topic string, event *Event) error {
    subject := topicToSubject(topic)
    data, err := json.Marshal(event)
    if err != nil {
        return fmt.Errorf("marshal event: %w", err)
    }

    msg := &nats.Msg{
        Subject: subject,
        Data:    data,
        Header:  make(nats.Header),
    }

    // Inject trace context from ctx into NATS headers
    InjectTraceContext(ctx, msg.Header)

    _, err = n.js.PublishMsg(ctx, msg)
    return err
}

// Subscribe — MODIFIED (L89)
// Returned events now carry context extracted from NATS headers

// The Event struct gains a Context field (unexported, not serialized):
// type eventWithContext struct {
//     *Event
//     ctx context.Context
// }
// Or: modify the Subscribe return channel to emit context alongside events
```

### 5.4 TRACEPARENT Env Var Parsing

Implemented in `pkg/telemetry/propagation.go`:

```go
package telemetry

import (
    "context"
    "os"

    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/propagation"
)

// envCarrier reads traceparent from environment variables.
type envCarrier struct{}

func (envCarrier) Get(key string) string {
    // W3C spec header names → env var names
    switch key {
    case "traceparent":
        return os.Getenv("TRACEPARENT")
    case "tracestate":
        return os.Getenv("TRACESTATE")
    }
    return ""
}

func (envCarrier) Set(string, string) {} // no-op for extraction

func (envCarrier) Keys() []string {
    return []string{"traceparent", "tracestate"}
}

// ExtractParentFromEnv reads TRACEPARENT and TRACESTATE env vars and returns
// a context with the remote span context. If no valid trace context is found,
// returns the original context unchanged (new root trace will be created).
func ExtractParentFromEnv(ctx context.Context) context.Context {
    return otel.GetTextMapPropagator().Extract(ctx, envCarrier{})
}
```

### 5.5 File-Based Context (Agent → Skill Sidecar)

Written by agent-runner alongside `exec-request-*.json`:

```
/ipc/tools/context-<call-id>.json:
{
    "traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
    "tracestate": ""
}
```

Skill sidecars currently ignore this file. It exists for future extensibility when skill sidecars may optionally consume trace context.

---

## 6. pkg/telemetry Package Design

### 6.1 Public API

```go
package telemetry

import (
    "context"
    "log/slog"
    "time"

    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/metric"
    "go.opentelemetry.io/otel/trace"
)

// Config controls how the OTel SDK is initialized for a component.
type Config struct {
    // ServiceName identifies this component (e.g., "sympozium-agent-runner").
    ServiceName string

    // ServiceVersion is the build version (e.g., "v0.0.49").
    // Injected via -ldflags at build time.
    ServiceVersion string

    // Namespace is the Kubernetes namespace this component runs in.
    // Read from NAMESPACE env var if empty.
    Namespace string

    // BatchTimeout controls how often the batch exporter flushes.
    // Default: 5s. Agent-runner should set to 1s.
    BatchTimeout time.Duration

    // ShutdownTimeout is the maximum time to wait for flush on shutdown.
    // Default: 30s. Agent-runner should set to 10s.
    ShutdownTimeout time.Duration

    // SamplingRatio is the trace sampling probability (0.0 to 1.0).
    // Default: 1.0 (sample everything).
    // Read from OTEL_TRACES_SAMPLER_ARG env var if zero.
    SamplingRatio float64

    // ExtraResource adds additional OTel resource attributes.
    // These are merged with auto-detected K8s attributes.
    ExtraResource []attribute.KeyValue
}

// Telemetry holds initialized OTel providers and offers accessors.
type Telemetry struct {
    tracerProvider *sdktrace.TracerProvider
    meterProvider  *sdkmetric.MeterProvider
    loggerProvider *sdklog.LoggerProvider
    tracer         trace.Tracer
    meter          metric.Meter
    logger         *slog.Logger
    shutdownTimeout time.Duration
}

// Init initializes the OTel SDK for a component.
//
// If OTEL_EXPORTER_OTLP_ENDPOINT is not set, returns a Telemetry instance
// backed by noop providers (zero overhead, all operations are no-ops).
//
// Init also registers the global TextMapPropagator (W3C TraceContext).
func Init(ctx context.Context, cfg Config) (*Telemetry, error)

// Shutdown flushes all pending telemetry and shuts down providers.
// Blocks up to Config.ShutdownTimeout (default 30s).
// Must be called before process exit to avoid data loss.
func (t *Telemetry) Shutdown(ctx context.Context) error

// Tracer returns a named tracer for creating spans.
// The tracer name is the ServiceName from Config.
func (t *Telemetry) Tracer() trace.Tracer

// Meter returns a named meter for creating metric instruments.
// The meter name is the ServiceName from Config.
func (t *Telemetry) Meter() metric.Meter

// Logger returns a structured logger with OTel log bridge.
// Log records automatically include trace_id and span_id from context.
func (t *Telemetry) Logger() *slog.Logger

// ExtractParentFromEnv reads TRACEPARENT/TRACESTATE env vars and returns
// a context carrying the remote span context for trace continuation.
func ExtractParentFromEnv(ctx context.Context) context.Context

// IsEnabled returns true if OTel export is configured (not noop mode).
func (t *Telemetry) IsEnabled() bool
```

### 6.2 Init/Shutdown Flow

```
telemetry.Init(ctx, cfg)
│
├── Check OTEL_EXPORTER_OTLP_ENDPOINT env var
│   ├── Empty → return Telemetry with noop providers (zero overhead)
│   └── Set → continue initialization
│
├── Build Resource
│   ├── service.name = cfg.ServiceName
│   ├── service.version = cfg.ServiceVersion
│   ├── k8s.namespace.name = cfg.Namespace || os.Getenv("NAMESPACE")
│   ├── k8s.pod.name = os.Getenv("POD_NAME")
│   ├── k8s.node.name = os.Getenv("NODE_NAME")
│   ├── sympozium.instance = os.Getenv("INSTANCE_NAME")
│   ├── sympozium.component = cfg.ServiceName (suffix after "sympozium-")
│   └── cfg.ExtraResource (merged)
│
├── Create OTLP Trace Exporter (gRPC)
│   └── otlptracegrpc.New(ctx, otlptracegrpc.WithInsecure())
│
├── Create OTLP Metric Exporter (gRPC)
│   └── otlpmetricgrpc.New(ctx, otlpmetricgrpc.WithInsecure())
│
├── Create OTLP Log Exporter (gRPC)
│   └── otlploggrpc.New(ctx, otlploggrpc.WithInsecure())
│
├── Create TracerProvider
│   ├── WithBatcher(traceExporter, WithBatchTimeout(cfg.BatchTimeout))
│   ├── WithResource(resource)
│   └── WithSampler(TraceIDRatioBased(cfg.SamplingRatio))
│
├── Create MeterProvider
│   ├── WithReader(PeriodicReader(metricExporter, WithInterval(cfg.BatchTimeout)))
│   └── WithResource(resource)
│
├── Create LoggerProvider
│   ├── WithProcessor(BatchProcessor(logExporter))
│   └── WithResource(resource)
│
├── Register globals
│   ├── otel.SetTracerProvider(tp)
│   ├── otel.SetMeterProvider(mp)
│   ├── otel.SetTextMapPropagator(propagation.TraceContext{})
│   └── slog.SetDefault(otelslog.NewHandler(lp))
│
└── Return &Telemetry{...}


telemetry.Shutdown(ctx)
│
├── Create context with ShutdownTimeout
│   └── ctx, cancel := context.WithTimeout(ctx, t.shutdownTimeout)
│
├── Flush and shutdown (in order)
│   ├── t.tracerProvider.Shutdown(ctx)   // flushes pending spans
│   ├── t.meterProvider.Shutdown(ctx)    // flushes pending metrics
│   └── t.loggerProvider.Shutdown(ctx)   // flushes pending logs
│
└── Return first error encountered
```

### 6.3 Usage Pattern Per Component

**Agent Runner** (`cmd/agent-runner/main.go`):

```go
package main

import (
    "github.com/alexsjones/sympozium/pkg/telemetry"
    "go.opentelemetry.io/otel"
)

var tracer = otel.Tracer("sympozium.ai/agent-runner")
var meter  = otel.Meter("sympozium.ai/agent-runner")

func main() {
    ctx := context.Background()

    tel, err := telemetry.Init(ctx, telemetry.Config{
        ServiceName:     "sympozium-agent-runner",
        BatchTimeout:    1 * time.Second,
        ShutdownTimeout: 10 * time.Second,
    })
    if err != nil {
        log.Printf("warning: OTel init failed: %v", err)
        // Continue without telemetry — noop fallback
    }
    defer func() {
        shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
        defer cancel()
        tel.Shutdown(shutdownCtx)
    }()

    // Parse parent trace from TRACEPARENT env var
    ctx = telemetry.ExtractParentFromEnv(ctx)

    // Root span for entire agent run
    ctx, span := tracer.Start(ctx, "agent.run", /* attributes */)
    defer span.End()

    // ... existing main() logic, ctx threaded through ...
}
```

**Controller** (`cmd/controller/main.go`):

```go
tel, _ := telemetry.Init(ctx, telemetry.Config{
    ServiceName:  "sympozium-controller",
    BatchTimeout: 5 * time.Second,
})
defer tel.Shutdown(ctx)
```

**Channel Pod** (`channels/telegram/main.go`):

```go
tel, _ := telemetry.Init(ctx, telemetry.Config{
    ServiceName:  "sympozium-channel-telegram",
    BatchTimeout: 5 * time.Second,
    ExtraResource: []attribute.KeyValue{
        attribute.String("channel.type", "telegram"),
    },
})
defer tel.Shutdown(ctx)
```

### 6.4 File Layout

```
pkg/
└── telemetry/
    ├── telemetry.go        // Init, Shutdown, Config, Telemetry struct
    ├── resource.go         // buildResource() — K8s env detection
    ├── propagation.go      // ExtractParentFromEnv, envCarrier
    └── telemetry_test.go   // Unit tests with in-memory exporter
```

---

## 7. CRD Schema Changes

### 7.1 SympoziumInstance — ObservabilitySpec

Added to `api/v1alpha1/sympoziuminstance_types.go` after line 34 (Memory field):

```go
type SympoziumInstanceSpec struct {
    // ... existing fields (L11-34) ...

    // Observability configures OpenTelemetry for agent pods spawned by this instance.
    // When nil, inherits from Helm chart global values.
    // +optional
    Observability *ObservabilitySpec `json:"observability,omitempty"`
}

// ObservabilitySpec controls OTel instrumentation for a specific instance.
type ObservabilitySpec struct {
    // Enabled controls whether OTel instrumentation is active for this instance.
    // Overrides the global Helm value when set.
    // +optional
    Enabled *bool `json:"enabled,omitempty"`

    // Endpoint is the OTLP collector endpoint (gRPC).
    // Example: "http://otel-collector.monitoring:4317"
    // Overrides the global Helm value when set.
    // +optional
    Endpoint string `json:"endpoint,omitempty"`

    // Protocol is the OTLP transport protocol: "grpc" (default) or "http/protobuf".
    // +optional
    // +kubebuilder:validation:Enum=grpc;"http/protobuf"
    Protocol string `json:"protocol,omitempty"`

    // Headers are additional OTLP export headers (e.g., auth tokens).
    // +optional
    Headers map[string]string `json:"headers,omitempty"`

    // HeadersSecretRef references a Secret containing OTLP export headers.
    // +optional
    HeadersSecretRef string `json:"headersSecretRef,omitempty"`

    // SamplingRatio is the trace sampling probability (0.0 to 1.0).
    // +optional
    // +kubebuilder:validation:Minimum=0.0
    // +kubebuilder:validation:Maximum=1.0
    SamplingRatio *float64 `json:"samplingRatio,omitempty"`

    // ResourceAttributes are additional OTel resource attributes.
    // +optional
    ResourceAttributes map[string]string `json:"resourceAttributes,omitempty"`
}
```

**Example CRD usage:**

```yaml
apiVersion: sympozium.ai/v1alpha1
kind: SympoziumInstance
metadata:
  name: my-assistant
spec:
  agents:
    default:
      model: claude-sonnet-4-20250514
  observability:
    enabled: true
    endpoint: "http://otel-collector.monitoring:4317"
    samplingRatio: 0.5
    resourceAttributes:
      environment: production
      team: ml-platform
```

### 7.2 AgentRun — TraceID in Status

Added to `api/v1alpha1/agentrun_types.go` after line 201 (TokenUsage field):

```go
type AgentRunStatus struct {
    // ... existing fields (L166-205) ...

    // TraceID is the OTel trace ID for this agent run, if instrumentation is enabled.
    // Enables operators to look up the full distributed trace in their backend.
    // Set by the controller when creating the Job.
    // +optional
    TraceID string `json:"traceID,omitempty"`
}
```

### 7.3 AgentRun — Annotation

Not a schema change, but a convention. The controller writes:

```
metadata:
  annotations:
    otel.dev/traceparent: "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
```

This annotation is set during `reconcilePending()` (L116) and read during `buildContainers()` (L562) to inject the `TRACEPARENT` env var.

### 7.4 Resolution Order

When the controller resolves the OTel endpoint for an agent pod:

```
1. SympoziumInstance.spec.observability.endpoint  (per-instance override)
   │
   └── if empty ──►
       2. Helm values: observability.endpoint  (global default, from controller env)
          │
          └── if empty ──►
              3. Empty string → agent-runner initializes noop (no export)
```

---

## 8. Architecture Decision Records

### ADR-1: Direct OTLP Export (No Sidecar Collector)

**Status:** Accepted
**Context:** Agent pods are ephemeral K8s Jobs (seconds to minutes). Options: (a) OTel Collector sidecar per pod, (b) DaemonSet collector on each node, (c) direct OTLP export to user endpoint.
**Decision:** Direct OTLP export. No bundled sidecar or DaemonSet.
**Rationale:**
- Sidecar adds ~64Mi memory + 1 container to every agent Job (already 3-4 containers).
- DaemonSet is cluster-level infrastructure; Sympozium is a user-installed Helm chart. Many clusters already run their own collector.
- Agent-runner's LLM calls complete before writing output. With `BatchTimeout: 1s` and `ForceFlush` on shutdown with 10s deadline, span loss risk is minimal.
- If endpoint is unavailable, SDK retries with backoff. Worst case: spans dropped, not crashed.
**Trade-offs:** Operators with high-reliability requirements should deploy a DaemonSet collector themselves (documented pattern).
**Source:** Debate Topic A; `cmd/agent-runner/main.go` L197-200 (result written before exit).

### ADR-2: NATS Headers for Trace Propagation

**Status:** Accepted
**Context:** NATS JetStream is the event bus connecting all components. Options: (a) inject `traceparent` in NATS message headers, (b) add `traceparent` to `Event.Metadata` map, (c) treat NATS as opaque boundary (no propagation).
**Decision:** W3C `traceparent` + `tracestate` in NATS message headers, using a `natsHeaderCarrier` adapter.
**Rationale:**
- NATS headers are transport metadata, semantically correct for trace context (not business data).
- Existing consumers only read `msg.Data` — headers are invisible, zero impact on existing code.
- OTel's `propagation.TextMapPropagator` interface maps naturally to `nats.Header.Get/Set`.
- `Event.Metadata` mixing trace context with business data violates separation of concerns.
**Trade-offs:** Trace context is NATS-specific. If transport changes to Kafka, carrier adapter changes too (but `Event.Metadata` approach would need the same change at a different layer).
**Source:** Debate Topic B; `internal/eventbus/nats.go` L73 (Publish), `internal/eventbus/types.go` L13-24 (Event struct).

### ADR-3: Shared pkg/telemetry Package

**Status:** Accepted
**Context:** 8 binaries need OTel initialization (~50 lines boilerplate each). Options: (a) `internal/telemetry/` (Go visibility restricts to main module), (b) `pkg/telemetry/` (importable by channel modules), (c) per-component init (duplicated code).
**Decision:** `pkg/telemetry/` — shared, public package.
**Rationale:**
- Channels are separate Go modules (`channels/telegram/go.mod`, etc.) that cannot import `internal/`.
- Channels already import main module types via `replace` directives, so `pkg/telemetry/` is accessible.
- `pkg/` is the Go convention for stable library code usable by external consumers.
- Component-specific tuning via `Config.BatchTimeout` and `Config.ExtraResource`.
**Trade-offs:** `pkg/` is a public API commitment. Must maintain backward compatibility.
**Source:** Debate Topic C; channel module structure in `channels/*/go.mod`.

### ADR-4: Provider-Level GenAI Span Instrumentation

**Status:** Accepted
**Context:** Agent-runner calls LLM APIs in a tool-calling loop (up to 25 iterations per `maxIterations` at L24). Options: (a) one span per LLM API call inside `callAnthropic()`/`callOpenAI()`, (b) one span wrapping the entire provider function.
**Decision:** One `gen_ai.chat` span per LLM API call, created inside the provider functions.
**Rationale:**
- Each loop iteration is a separate API call with its own token usage and latency. A single wrapper span hides this detail.
- With per-call spans, operators can identify which iteration was slow, which failed, and how tokens accumulated.
- Tool execution spans (`tool.execute`) are siblings of `gen_ai.chat` spans, correctly modeling the sequential flow.
- Package-level `var tracer = otel.Tracer(...)` follows Go OTel conventions.
**Trade-offs:** More spans per agent run (2N+1 for N iterations: N chat + N tool + 1 root). Acceptable for a max of 51 spans.
**Source:** Debate Topic D; `cmd/agent-runner/main.go` L219-326 (callAnthropic), L332-431 (callOpenAI), L24 (maxIterations).

### ADR-5: End-to-End Trace via CRD Annotation Bridge

**Status:** Accepted
**Context:** Trace context must cross the K8s Job boundary (controller → ephemeral pod). Options: (a) env var directly from in-memory context, (b) CRD annotation as persistent intermediary, (c) ConfigMap with trace context.
**Decision:** Controller stores `traceparent` in AgentRun annotation (`otel.dev/traceparent`), then reads it back to inject as `TRACEPARENT` env var when building the Job.
**Rationale:**
- The AgentRun CRD is the single source of truth for the run's lifecycle. Storing trace context there makes it queryable (`kubectl get agentrun -o yaml`).
- The channel router creates the AgentRun (with annotation), and the same controller process reconciles it — no timing issue, annotation is present when the Job is built.
- IPC bridge reads the same `TRACEPARENT` env var from the shared pod spec.
- Amended constraint: IPC bridge gets OTel SDK (it's infrastructure, not a user skill sidecar).
**Trade-offs:** Annotation is mutable K8s metadata; if someone edits it, trace is disrupted. Acceptable operational risk.
**Source:** Debate Topic E; `internal/controller/agentrun_controller.go` L116 (reconcilePending), L562-766 (buildContainers), `internal/controller/channel_router.go` L130-160 (create AgentRun).

### ADR-6: Noop Unit Tests, In-Memory Integration Tests

**Status:** Accepted
**Context:** How to test OTel instrumentation without coupling tests to telemetry internals. Options: (a) mock OTel SDK, (b) in-memory exporter everywhere, (c) noop for unit tests + in-memory for integration.
**Decision:** Two-tier approach: noop provider for unit tests, in-memory exporter for integration tests.
**Rationale:**
- Unit tests focus on business logic. OTel code is present but produces no output — zero test maintenance burden.
- Integration tests use `tracetest.NewInMemoryExporter()` to verify span hierarchies and attributes.
- `pkg/telemetry/` package has its own unit tests with in-memory exporter (it's the SUT).
- No build tags — OTel code is always compiled. Noop mode when endpoint is unset is functionally equivalent.
- Never mock the OTel SDK itself — the real SDK with noop or in-memory backend is simpler and more accurate.
**Trade-offs:** Unit tests won't catch missing `span.End()` calls. Integration tests catch these. Acceptable gap.
**Source:** Debate Topic F.

---

## 9. Deployment Architecture

### 9.1 Helm Values Addition

Added to `charts/sympozium/values.yaml` after line 152 (current last line):

```yaml
# -------------------------------------------------------------------
# OpenTelemetry Observability
# -------------------------------------------------------------------
observability:
  # Master switch. When false, all components init noop OTel (zero overhead).
  enabled: false

  # OTLP collector endpoint. All components export here.
  # Example: "http://otel-collector.monitoring.svc:4317"
  endpoint: ""

  # OTLP transport: "grpc" (default, port 4317) or "http/protobuf" (port 4318)
  protocol: "grpc"

  # Static OTLP export headers (e.g., {"Authorization": "Bearer token"}).
  # For sensitive headers, use headersSecretRef instead.
  headers: {}

  # Secret reference for OTLP export headers.
  # Secret keys become header names; values become header values.
  headersSecretRef: ""

  # Trace sampling probability: 0.0 (none) to 1.0 (all). Default: sample everything.
  samplingRatio: 1.0

  # Additional OTel resource attributes applied to ALL components.
  # Example: {"environment": "staging", "region": "us-east-1"}
  resourceAttributes: {}

  # Prefix for service names. Component name is appended.
  # "sympozium" → "sympozium-controller", "sympozium-agent-runner", etc.
  serviceNamePrefix: "sympozium"

  # Per-component tuning
  controller:
    batchTimeout: "5s"
  agentRunner:
    batchTimeout: "1s"
    shutdownTimeout: "10s"
  apiServer:
    batchTimeout: "5s"
  ipcBridge:
    batchTimeout: "5s"
  channels:
    batchTimeout: "5s"
```

### 9.2 Helm Template Changes

#### Controller Deployment (`templates/controller-deployment.yaml`)

Add env vars to controller container:

```yaml
env:
  # ... existing env vars ...
  {{- if .Values.observability.enabled }}
  - name: OTEL_EXPORTER_OTLP_ENDPOINT
    value: {{ .Values.observability.endpoint | quote }}
  - name: OTEL_EXPORTER_OTLP_PROTOCOL
    value: {{ .Values.observability.protocol | quote }}
  - name: OTEL_SERVICE_NAME
    value: "{{ .Values.observability.serviceNamePrefix }}-controller"
  - name: OTEL_TRACES_SAMPLER
    value: "parentbased_traceidratio"
  - name: OTEL_TRACES_SAMPLER_ARG
    value: {{ .Values.observability.samplingRatio | quote }}
  - name: SYMPOZIUM_OTEL_BATCH_TIMEOUT
    value: {{ .Values.observability.controller.batchTimeout | quote }}
  {{- if .Values.observability.headers }}
  - name: OTEL_EXPORTER_OTLP_HEADERS
    value: {{ include "sympozium.otelHeaders" .Values.observability.headers | quote }}
  {{- end }}
  {{- if .Values.observability.resourceAttributes }}
  - name: OTEL_RESOURCE_ATTRIBUTES
    value: {{ include "sympozium.otelResourceAttrs" .Values.observability.resourceAttributes | quote }}
  {{- end }}
  {{- end }}
```

#### API Server Deployment (`templates/apiserver-deployment.yaml`)

Same pattern as controller, with `apiServer.batchTimeout`.

#### Channel Deployments (generated by controller, not Helm template)

The controller injects OTel env vars into channel Deployments during `SympoziumInstanceReconciler.reconcile()`. The controller reads:
1. Its own `OTEL_EXPORTER_OTLP_ENDPOINT` env var (from Helm)
2. The SympoziumInstance's `spec.observability` override

And injects the resolved values into the channel Deployment spec.

#### Agent Pod Env Vars (injected by controller at runtime)

In `agentrun_controller.go` `buildContainers()` (L562-766), the controller injects:

```go
// OTel env vars for agent container and IPC bridge container
if otelEndpoint := r.resolveOTelEndpoint(ctx, agentRun); otelEndpoint != "" {
    otelEnv := []corev1.EnvVar{
        {Name: "OTEL_EXPORTER_OTLP_ENDPOINT", Value: otelEndpoint},
        {Name: "OTEL_SERVICE_NAME", Value: "sympozium-agent-runner"}, // or "sympozium-ipc-bridge"
        {Name: "OTEL_TRACES_SAMPLER_ARG", Value: samplingRatio},
    }
    // Inject into both agent and ipc-bridge containers
}

// TRACEPARENT from annotation
if tp, ok := agentRun.Annotations["otel.dev/traceparent"]; ok {
    env = append(env, corev1.EnvVar{Name: "TRACEPARENT", Value: tp})
}
```

### 9.3 Network Policy Changes

Agent pods need egress to the OTel endpoint. Update `templates/network-policies.yaml`:

```yaml
{{- if and .Values.networkPolicies.enabled .Values.observability.enabled }}
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: {{ include "sympozium.fullname" . }}-allow-otel
spec:
  podSelector:
    matchLabels:
      app.kubernetes.io/part-of: sympozium
  policyTypes:
    - Egress
  egress:
    - to:
        - namespaceSelector: {}  # Allow to any namespace (collector may be in monitoring ns)
      ports:
        - protocol: TCP
          port: 4317  # OTLP gRPC
        - protocol: TCP
          port: 4318  # OTLP HTTP
{{- end }}
```

### 9.4 Environment Variable Reference

Complete list of OTel-related env vars across all components:

| Variable | Set By | Read By | Purpose |
|----------|--------|---------|---------|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | Helm template / Controller | `pkg/telemetry` Init() | Collector endpoint |
| `OTEL_EXPORTER_OTLP_PROTOCOL` | Helm template / Controller | `pkg/telemetry` Init() | Transport protocol |
| `OTEL_EXPORTER_OTLP_HEADERS` | Helm template / Controller | `pkg/telemetry` Init() | Auth headers |
| `OTEL_SERVICE_NAME` | Helm template / Controller | `pkg/telemetry` Init() | Service identity |
| `OTEL_TRACES_SAMPLER` | Helm template / Controller | OTel SDK | Sampler type |
| `OTEL_TRACES_SAMPLER_ARG` | Helm template / Controller | OTel SDK | Sampling ratio |
| `OTEL_RESOURCE_ATTRIBUTES` | Helm template / Controller | OTel SDK | Extra resource attrs |
| `SYMPOZIUM_OTEL_BATCH_TIMEOUT` | Helm template / Controller | `pkg/telemetry` Init() | Batch flush interval |
| `TRACEPARENT` | Controller (from CRD annotation) | Agent-runner, IPC bridge | W3C parent trace context |
| `TRACESTATE` | Controller (from CRD annotation) | Agent-runner, IPC bridge | W3C trace state (optional) |
| `NAMESPACE` | K8s downward API | `pkg/telemetry` resource | K8s namespace for resource attr |
| `POD_NAME` | K8s downward API | `pkg/telemetry` resource | K8s pod name for resource attr |
| `NODE_NAME` | K8s downward API | `pkg/telemetry` resource | K8s node name for resource attr |
| `OTEL_TRACES_EXPORTER` | Manual override | OTel SDK | Set to `logging` for debug mode |
| `OTEL_METRICS_EXPORTER` | Manual override | OTel SDK | Set to `logging` for debug mode |
| `OTEL_LOGS_EXPORTER` | Manual override | OTel SDK | Set to `logging` for debug mode |

### 9.5 Downward API Addition

Agent pods need K8s metadata for OTel resource attributes. Add to `buildContainers()`:

```go
// K8s downward API env vars for OTel resource detection
{Name: "POD_NAME", ValueFrom: &corev1.EnvVarSource{
    FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
}},
{Name: "NAMESPACE", ValueFrom: &corev1.EnvVarSource{
    FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"},
}},
{Name: "NODE_NAME", ValueFrom: &corev1.EnvVarSource{
    FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"},
}},
```

Controller and API server Helm templates get the same downward API fields.

### 9.6 Docker Image Impact

All images grow by the OTel SDK Go dependencies (~5-8 MB binary size increase). No new container images required. No sidecar containers added.

| Image | Current Size (approx) | After OTel (approx) | Delta |
|-------|----------------------|---------------------|-------|
| agent-runner | ~25 MB | ~33 MB | +8 MB |
| ipc-bridge | ~15 MB | ~23 MB | +8 MB |
| controller | ~30 MB | ~38 MB | +8 MB |
| apiserver | ~25 MB | ~33 MB | +8 MB |
| channel-telegram | ~20 MB | ~28 MB | +8 MB |
| channel-slack | ~20 MB | ~28 MB | +8 MB |
| channel-discord | ~20 MB | ~28 MB | +8 MB |
| channel-whatsapp | ~25 MB | ~33 MB | +8 MB |

---

*End of architecture document. This document should be used alongside the PRD (`_bmad/prd-otel-instrumentation.md`) for implementation. Code references point to the codebase as of commit `6d6d307`.*
