# Durable catalogue issuance from the operator CLI

`sympozium celln-tool issue-run AGENT` persists the verified issuer result on
an existing AgentRun. Unlike `issue-remote`, this is an **execution hand-off**:
the catalogue controller can submit the run as soon as issuance is committed.
The command itself does not call the router or assert readiness.

Use an explicit kubeconfig and namespace. Only operators with trusted grant
source configuration and AgentRun status write permission should use this
command; source and route flags are not tenant-controlled inputs.

```sh
sympozium --kubeconfig /absolute/isolated-kubeconfig --namespace tenant \
  celln-tool issue-run assistant --run task-1 \
  --grant-namespace operator \
  --operator-grants operator-grants --runtime-grants runtime-grants \
  --agent-grants assistant-grants --model-policy model-policy \
  --tool uppercase@v1 --tool length@v1 \
  --execution-mote blake3:REPLACE_WITH_ADMITTED_MOTE_HASH \
  --execution-closure blake3:REPLACE_WITH_COMPOSED_CLOSURE_HASH \
  --issuer-url https://issuer.example \
  --issuer-token-file /absolute/issuer-token \
  --issuer-ca-file /absolute/issuer-ca.pem \
  --router-url https://router.example --backend http://dispatcher.internal:9400
```

The names, revisions and hashes above are placeholders, not runnable admitted
artifacts. Composition and host admission must already be complete. The
controller's catalogue binding must match the Agent, authority sources, issuer
and frozen serving route. Its router credential stays in controller configuration;
the issuance command does not need it.

Until high-level catalogue run creation exists, isolate or pause the intended
controller **before creating the pending run**, create the run and its independent
approvals, issue it, then resume the configured controller. An unissued pending
run is not a holding queue: an active legacy controller can process it through
the ordinary Celln path before this command records issuance. Do not pause a
shared production controller to run a demonstration.

The CLI saves `Prepared` before contacting the issuer and `Issued` only after
independent result validation. A lost response, status acknowledgement or output
does not authorize changing identity, deleting the journal or switching hosts.
Retry with the identical command inputs while the run is still pending; changed
approvals, identity or route refuse. Once dispatch starts, inspect AgentRun status
and let the controller recover the saved router owner rather than reissuing.
Output failure does not revoke a committed grant that may already be in use.

This is an operator bridge to the real catalogue-backed proof, not the final
UI/YAML selection workflow, automatic admission, conversation support or a
production-readiness claim.
