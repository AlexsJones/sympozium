# Helm Chart

For production and GitOps workflows, deploy the control plane using Helm.

## Prerequisites

[cert-manager](https://cert-manager.io/) is required for webhook TLS:

```bash
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.17.1/cert-manager.yaml
```

## Install

```bash
helm install sympozium ./charts/sympozium
```

See [`charts/sympozium/values.yaml`](https://github.com/AlexsJones/sympozium/blob/main/charts/sympozium/values.yaml) for all configuration options (replicas, resources, external NATS, network policies, etc.).

## Observability

The Helm chart deploys a built-in OpenTelemetry collector by default:

```yaml
observability:
  enabled: true
  collector:
    service:
      otlpGrpcPort: 4317
      otlpHttpPort: 4318
      metricsPort: 8889
```

Disable it if you already run a shared collector:

```yaml
observability:
  enabled: false
```

## Web UI

```yaml
apiserver:
  webUI:
    enabled: true       # Serve the embedded web dashboard (default: true)
    token: ""           # Explicit token; leave blank to auto-generate a Secret
```

If `token` is left empty, Helm creates a `<release>-ui-token` Secret with a random 32-character token.

## Network Policies

```yaml
networkPolicies:
  enabled: true
  extraEgressPorts: []    # add non-standard API server ports here (e.g. [6444] for k3s)
```
