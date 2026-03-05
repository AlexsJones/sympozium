# Testing

This document is the quick entry point for running Sympozium tests locally and in CI.

## Local test commands

```bash
# Unit tests
make test

# Existing end-to-end tool/channel integration tests (LLM-dependent)
make test-integration

# API-first integration regression suite
make integration-tests
```

## API integration suite notes

`make integration-tests` runs the API-focused smoke and behavior checks under `test/integration/`, including:

- API smoke coverage for namespaces/skills/policies/personapacks/instances/schedules
- PersonaPack provisioning and provider switch propagation
- PersonaPack vs ad-hoc correctness checks
- Schedule dispatch behavior
- AgentRun pod container shape checks
- Observability API checks
- Web-endpoint skill enable/disable/status API checks
- Serving-mode AgentRun shape (Deployment + Service creation)
- Optional capability checks (`CLAUDE_TOKEN`, `GITHUB_TOKEN`)

Optional secrets can be passed locally:

```bash
CLAUDE_TOKEN=... GITHUB_TOKEN=... make integration-tests
```

## Scheduled GitHub Actions workflow

The repository includes a scheduled Kind-based workflow:

- Workflow file: `.github/workflows/integration-kind.yaml`
- Name: `Integration Tests (Kind)`
- Triggers:
  - Daily schedule (`0 6 * * *`, UTC)
  - Manual run via `workflow_dispatch`

### What the workflow does

1. Checks out the repository
2. Sets up Go
3. Creates a Kind cluster
4. Builds Sympozium images (`make docker-build`)
5. Loads images into Kind
6. Installs CRDs and built-ins (`make install`)
7. Applies control-plane manifests
8. Waits for core deployments
9. Runs `make integration-tests`
10. On failure, dumps key cluster diagnostics and logs

### Repository secrets

The workflow passes these optional secrets to tests:

- `CLAUDE_TOKEN`
- `GITHUB_TOKEN`

If unset, the related capability checks are skipped by design.

## Running the workflow manually

In GitHub:

1. Open **Actions**
2. Select **Integration Tests (Kind)**
3. Click **Run workflow**
