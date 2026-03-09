# Architecture

## System Overview

```mermaid
graph TB
    subgraph K8S["Kubernetes Cluster"]
        direction TB

        subgraph CP["Control Plane"]
            CM["Controller Manager<br/><small>SympoziumInstance · AgentRun · PersonaPack<br/>SympoziumPolicy · SkillPack · SympoziumSchedule</small>"]
            API["API Server<br/><small>HTTP + WebSocket</small>"]
            WH["Admission Webhook<br/><small>Policy enforcement</small>"]
            NATS[("NATS JetStream<br/><small>Event bus</small>")]
            CM --- NATS
            API --- NATS
            WH -.- CM
        end

        subgraph SCHED["Scheduled Tasks"]
            CS["SympoziumSchedule Controller<br/><small>Cron-based reconciler</small>"]
            SROUTER["Schedule Router<br/><small>NATS → SympoziumSchedule CRD</small>"]
            CS -- "creates AgentRuns<br/>on schedule" --> CM
            SROUTER -- "creates / updates<br/>SympoziumSchedule CRDs" --> CS
        end

        subgraph CH["Channel Pods  ·  one Deployment per type"]
            TG["Telegram"]
            SL["Slack"]
            DC["Discord"]
            WA["WhatsApp"]
        end

        subgraph WE["Web Endpoints  ·  long-lived Deployments"]
            WP["Web Proxy<br/><small>OpenAI-compat API + MCP</small>"]
            GW["Envoy Gateway<br/><small>HTTPRoute per instance</small>"]
            GW -- "routes traffic" --> WP
        end

        subgraph AP["Agent Pod  ·  ephemeral K8s Job"]
            direction LR
            A1["Agent Container<br/><small>LLM provider agnostic</small>"]
            IPC["IPC Bridge<br/><small>fsnotify → NATS</small>"]
            SB["Sandbox<br/><small>optional sidecar</small>"]
            SKS["Skill Sidecars<br/><small>kubectl, helm, etc.<br/>auto-RBAC</small>"]
            A1 -. "/ipc volume" .- IPC
            A1 -. optional .- SB
            A1 -. "/workspace" .- SKS
        end

        subgraph SEC["Skill RBAC  ·  ephemeral, least-privilege"]
            SR["Role + RoleBinding<br/><small>namespace-scoped<br/>ownerRef → AgentRun</small>"]
            SCR["ClusterRole + Binding<br/><small>cluster-scoped<br/>label-based cleanup</small>"]
        end

        SKS -- "uses" --> SR
        SKS -- "uses" --> SCR
        CM -- "creates / deletes" --> SEC

        WP -- "creates per-request<br/>AgentRun Jobs" --> CM
        WP --- NATS
        CM -- "creates Deployment<br/>+ Service + HTTPRoute" --> WE

        subgraph MEM["Persistent Memory"]
            MCM[("ConfigMap<br/><small>&lt;instance&gt;-memory</small>")]
            A1 -- "reads /memory<br/>MEMORY.md" --> MCM
            CM -- "extracts & patches<br/>memory markers" --> MCM
        end

        subgraph DATA["Data Layer"]
            ETCD[("etcd<br/><small>CRDs, state</small>")]
            PG[("PostgreSQL<br/><small>sessions, history</small>")]
            SK[("SkillPack ConfigMaps<br/><small>mounted at /skills</small>")]
        end

        TG & SL & DC & WA -- "messages" --> NATS
        NATS -- "tasks" --> IPC
        IPC -- "channel msgs<br/>schedule requests" --> NATS
        NATS -- "schedule.upsert" --> SROUTER
    end

    USER(["User / Chat Client"]) -- "Telegram · Slack<br/>Discord · WhatsApp" --> CH
    HTTPUSER(["HTTP / API Client"]) -- "REST · MCP<br/>OpenAI-compat" --> GW
    ADMIN(["Operator / SRE"]) -- "sympozium TUI · Web UI<br/>kubectl · k9s" --> CP

    style K8S fill:#0d1117,stroke:#30363d,color:#c9d1d9
    style CP fill:#1a1a2e,stroke:#e94560,color:#fff
    style SCHED fill:#1a1a2e,stroke:#f5a623,color:#fff
    style CH fill:#16213e,stroke:#0f3460,color:#fff
    style WE fill:#16213e,stroke:#f5a623,color:#fff
    style AP fill:#0f3460,stroke:#53354a,color:#fff
    style MEM fill:#1c2333,stroke:#7c3aed,color:#fff
    style SEC fill:#1c2333,stroke:#238636,color:#fff
    style DATA fill:#161b22,stroke:#30363d,color:#c9d1d9
    style NATS fill:#e94560,stroke:#fff,color:#fff
    style USER fill:#238636,stroke:#fff,color:#fff
    style HTTPUSER fill:#f5a623,stroke:#fff,color:#000
    style ADMIN fill:#1f6feb,stroke:#fff,color:#fff
```

## How It Works

1. **A message arrives** via a channel pod (Telegram, Slack, etc.) and is published to the NATS event bus.
2. **The controller creates an AgentRun CR**, which reconciles into an ephemeral K8s Job — an agent container + IPC bridge sidecar + optional sandbox + skill sidecars (with auto-provisioned RBAC).
3. **The agent container** calls the configured LLM provider (OpenAI, Anthropic, Azure, Ollama, or any OpenAI-compatible endpoint), with skills mounted as files, persistent memory injected from a ConfigMap, and tool sidecars providing runtime capabilities like `kubectl`.
4. **Results flow back** through the IPC bridge → NATS → channel pod → user. The controller extracts structured results and memory updates from pod logs.
5. **Web endpoints** expose agents as HTTP APIs. When an instance has the `web-endpoint` skill, the controller creates a long-lived Deployment (serving mode) with a web-proxy sidecar. The proxy accepts OpenAI-compatible (`/v1/chat/completions`) and MCP (`/sse`, `/message`) requests, creating per-request AgentRun Jobs. An Envoy Gateway with per-instance HTTPRoutes provides external access with TLS.
6. **Everything is a Kubernetes resource** — instances, runs, policies, skills, and schedules are all CRDs. Lifecycle is managed by controllers. Access is gated by admission webhooks. Network isolation is enforced by NetworkPolicy. The TUI and web dashboard give you full visibility into the entire system.

---

## How It Compares

| Concern | In-process frameworks | Sympozium (Kubernetes-native) |
|---------|----------------------|----------------------------|
| **Agent execution** | Shared memory, single process | Ephemeral **Pod** per invocation (K8s Job) |
| **Orchestration** | In-process registry + lane queue | **CRD-based** registry with controller reconciliation |
| **Sandbox isolation** | Long-lived Docker sidecar | Pod **SecurityContext** + PodSecurity admission |
| **IPC** | In-process EventEmitter | Filesystem sidecar + **NATS JetStream** |
| **Tool/feature gating** | In-process pipeline | **Admission webhooks** + `SympoziumPolicy` CRD |
| **Persistent memory** | Files on disk | **ConfigMap** per instance, controller-managed |
| **Scheduled tasks** | Cron jobs / external scripts | **SympoziumSchedule CRD** with cron controller |
| **State** | SQLite + flat files | **etcd** (CRDs) + PostgreSQL + object storage |
| **Multi-tenancy** | Single-instance file lock | **Namespaced CRDs**, RBAC, NetworkPolicy |
| **Scaling** | Vertical only | **Horizontal** — stateless control plane, HPA |
| **Channel connections** | In-process per channel | Dedicated **Deployment** per channel type |
| **Observability** | Application logs | `kubectl logs`, events, conditions, **OpenTelemetry traces/metrics**, **k9s-style TUI**, **web dashboard** |

---

## Key Design Decisions

| Decision | Kubernetes Primitive | Rationale |
|----------|---------------------|-----------|
| **One Pod per agent run** | Job | Blast-radius isolation, resource limits, automatic cleanup — each agent is as ephemeral as a CronJob pod |
| **Filesystem IPC** | emptyDir volume | Agent writes to `/ipc/`, bridge sidecar watches via fsnotify and publishes to NATS — language-agnostic, zero dependencies in agent container |
| **NATS JetStream** | StatefulSet | Durable pub/sub with replay — channels and control plane communicate without direct coupling |
| **NetworkPolicy isolation** | NetworkPolicy | Agent pods get deny-all egress; only the IPC bridge connects to the event bus — agents cannot reach the internet or other pods |
| **Policy-as-CRD** | Admission Webhook | `SympoziumPolicy` resources gate tools, sandboxes, and features — enforced at admission time, not at runtime |
| **Memory-as-ConfigMap** | ConfigMap | Persistent agent memory lives in etcd — no external database, no file system, fully declarative and backed up with cluster state |
| **Schedule-as-CRD** | CronJob analogy | `SympoziumSchedule` resources define recurring tasks with cron expressions — the controller creates AgentRuns, not the user |
| **Skills-as-ConfigMap** | ConfigMap volume | SkillPacks generate ConfigMaps mounted into agent pods — portable, versionable, namespace-scoped |
| **Skill sidecars with auto-RBAC** | Role / ClusterRole | SkillPacks can declare sidecar containers with RBAC rules — the controller injects the container and provisions ephemeral, least-privilege RBAC per run |
| **PersonaPacks** | Operator Bundle | Pre-configured agent bundles — the controller stamps out SympoziumInstances, Schedules, and memory ConfigMaps. Activating a pack is a single TUI action |

---

## Project Structure

```
sympozium/
├── api/v1alpha1/           # CRD type definitions
├── cmd/                    # Binary entry points
│   ├── agent-runner/       # LLM agent runner (runs inside agent pods)
│   ├── controller/         # Controller manager (reconciles all CRDs)
│   ├── apiserver/          # HTTP + WebSocket API server (+ embedded web UI)
│   ├── ipc-bridge/         # IPC bridge sidecar (fsnotify → NATS)
│   ├── web-proxy/          # Web proxy (OpenAI-compat API + MCP gateway)
│   ├── webhook/            # Admission webhook (policy enforcement)
│   └── sympozium/          # CLI + interactive TUI
├── web/                    # Web dashboard (React + TypeScript + Vite)
├── internal/               # Internal packages
│   ├── controller/         # Kubernetes controllers (6 reconcilers)
│   ├── orchestrator/       # Agent pod builder & spawner
│   ├── apiserver/          # API server handlers
│   ├── eventbus/           # NATS JetStream event bus
│   ├── ipc/                # IPC bridge (fsnotify + NATS)
│   ├── webhook/            # Policy enforcement webhooks
│   ├── webproxy/           # Web proxy handlers (OpenAI, MCP, rate limiting)
│   ├── session/            # Session persistence (PostgreSQL)
│   └── channel/            # Channel base types
├── channels/               # Channel pod implementations
├── images/                 # Dockerfiles for all components
├── config/                 # Kubernetes manifests
│   ├── crd/bases/          # CRD YAML definitions
│   ├── manager/            # Controller deployment
│   ├── rbac/               # ClusterRole, bindings
│   ├── webhook/            # Webhook configuration
│   ├── network/            # NetworkPolicy for agent isolation
│   ├── nats/               # NATS JetStream deployment
│   ├── cert/               # TLS certificate resources
│   ├── personas/           # Built-in PersonaPack definitions
│   ├── skills/             # Built-in SkillPack definitions
│   ├── policies/           # Default SympoziumPolicy presets
│   └── samples/            # Example CRs
├── migrations/             # PostgreSQL schema migrations
├── docs/                   # Documentation (this site)
├── Makefile
└── README.md
```
