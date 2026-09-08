# Live catalogue-backed Harness proof

`test/integration/test-celln-catalogue-harness.sh` exercises the actual Sympozium
CLI and controller binaries against an isolated Kind API, a TLS-authenticated
managed issuer, a TLS-fronted actual Celln router, a host KVM dispatcher and
DeepSeek. The model calls `uppercase` and then `length`, returning
`CELLN has length 5`.

This differs from the earlier explicit-artifact controller proof: runtime/tool
catalogue objects and independent grant documents use Kubernetes-assigned UIDs
and persisted specs. Composition is derived from those approved objects. The
actual `celln-tool issue-run` command freezes issuance and serving route in
AgentRun status before the actual controller is started.

## Explicit prerequisites

- Existing `kind-celln-deployed` kubeconfig; no default or Framework context.
- The isolated `harness-proof-controller` deployment at one replica with its
  local proof image, and no unfinished AgentRuns. The script temporarily pauses
  that deployment and restores its original UID at one replica on exit.
- The current AgentRun, AgentRuntime and CellnTool CRDs installed in that cluster.
- Go, kubectl, jq, real `/dev/kvm`, the Celln artifact toolchain and readable kernel.
- The public signed catalogue fixture, native JSON Harness package and explicit
  public-fixture materializer used by `TestComposeRealCellnArtifacts`.
- Permission for a billable DeepSeek test, limited to three requests and 1,536
  total output tokens per run. A failure must be investigated before rerunning.

```sh
export CELLN_PAUSE_TEST_CONTROLLER=1
export CELLN_CONTROLLER_KUBECONFIG=/absolute/isolated-kubeconfig
export CELLN_COMPOSITION_FIXTURE=/absolute/public-catalogue-fixture
export CELLN_COMPOSITION_BINARY=/absolute/celln
export CELLN_ISSUANCE_MATERIALIZER=/absolute/prepare_issuance_fixture
export CELLN_HARNESS_PACKAGE=/absolute/native-json-harness-package
export CELLN_LIVE_CREDENTIAL_FILE=/absolute/host-only-deepseek-key
export CELLN_LIVE_EVIDENCE_PARENT=/absolute/existing-evidence-directory
test/integration/test-celln-catalogue-harness.sh
```

Alternatively, explicitly set `CELLN_LIVE_DEEPSEEK_ZSHRC` instead of
`CELLN_LIVE_CREDENTIAL_FILE` to a shell configuration containing exactly one
literal `DEEPSEEK_API_KEY=sk-...` assignment. The test parses only that literal;
it never sources the shell configuration or evaluates expansions. The extracted
key is written to a private temporary host file and removed with test cleanup.
No key contents enter Kubernetes, command-line arguments, the guest, or evidence.

## Assertions and cleanup

The test verifies durable `Issued` status before dispatch, three model events,
the two ordered tool results, the final answer, terminal status/receipt, zero
Jobs, the matching grant and receipt in the execution audit, `Dissolved`, and
zero live cells. It deletes the independent model policy, waits for managed
host-profile withdrawal, requires host grant reissuance to refuse, and retrieves
the same successful owner receipt after withdrawal.

The test owns and joins all host processes and TLS listeners. Run deletion is
attempted while the controller can still reconcile cancellation/finalizers;
namespace deletion is UID-scoped. The outer script then restores the isolated
deployment. Cleanup failures fail the test; they do not authorize stripping
finalizers or deleting unrelated resources. The newly generated evidence
directory retains AgentRun status, audit, node state, Job list and summary.

Portable `go test` skips this billable test unless `CELLN_LIVE_CATALOGUE=1` is
explicitly supplied. The setup helper also has non-network tests rejecting
implicit paths and non-proof contexts.

## What this does not prove

The controller, issuer and router run as real host processes, not deployed
production pods. Both tools come from an operator-selected public signed fixture;
the materializer is not a production admission/distribution service. TLS uses
public test certificates restricted to loopback. This is one bounded one-shot
run, not UI/YAML selection, arbitrary OCI Harness support, conversations,
multi-host availability, fleet revocation, or full epic/release acceptance.
