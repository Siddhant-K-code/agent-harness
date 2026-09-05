# Run the harness on AWS

The `agentcore` backend connects the real OpenAI controller to AWS execution.
Docker remains the default. AWS needs no Docker daemon on the controller; Docker
is used to build the ARM64 image. The manual **AWS harness** workflow builds that
image, tests transfer and interrupted-worker reconciliation, and optionally runs
GPT-5.4. The [full live validation](evidence/2026-09-05/aws-harness/README.md)
passed: real artifact transfer, killed-worker reconciliation, four GPT-5.4
requests, independent verification, native AgentTrace replay, and cleanup.

## GitHub Actions

The existing bootstrap's two repository variables and GitHub OIDC role are used.
No AWS access-key secrets are needed. For a **live model run**, configure the
encrypted repository secret `OPENAI_API_KEY`. Deterministic acceptance does not
use that secret or make model calls.

```sh
# Reads the existing local key directly into GitHub's encrypted secret store.
# Do not paste the key into a command, task JSON, or workflow file.
gh secret set OPENAI_API_KEY --repo Siddhant-K-code/agent-harness < .harness/openai-key
gh workflow run aws-harness.yml --ref main -f live_model=true
```

The live fixture uses GPT-5.4, at most four model steps, a 30-minute run deadline,
30-second commands, and a $0.50 maximum estimated model budget. AWS charges are
separate and remain unmeasured until billing evidence is available. Nothing runs
on a schedule. Ordinary CI never invokes AWS or a paid model.

Private artifacts retain the controller database, execution records, snapshots,
patch, report, native AgentTrace data and replay for seven days. They exclude the
OpenAI key. Download evidence before retention expires. Runtime, image and log
cleanup run even after a failed test.

## Local controller

To build the service image manually from the repository root:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o infra/aws/agentcore/harness-service ./cmd/agentcore-service
docker buildx build --platform linux/arm64 --provenance=false --load -t agent-harness-guard:test infra/aws/agentcore
```

The generated service binary is ignored by Git. The Docker build context admits
only the service, guard source and guard checks; it excludes controller keys and
state. The Actions workflow pushes the image and resolves its immutable digest.

Use the normal task contract with these additional fields and a prepared image:

```json
{
  "backend": "agentcore",
  "image": "ACCOUNT.dkr.ecr.us-east-1.amazonaws.com/agent-harness-probe@sha256:DIGEST",
  "aws": {
    "region": "us-east-1",
    "execution_role": "arn:aws:iam::ACCOUNT:role/agent-harness-runtime"
  }
}
```

These fields supplement the existing required task fields; this is not a full
task file. `doctor` validates the digest and role and checks the AWS identity.
Execution requires an `agent-harness-deploy` assumed-role session. The supplied
bootstrap grants that session through GitHub OIDC; it does not grant local root
credentials permission to assume the role. Use Actions unless you separately
configure an appropriate local federation path.

The controller holds the OpenAI key and repository Git database. Neither enters
the guest. Only the pinned committed workspace is sent through authenticated AWS
invocations. The artifact protocol supports archives up to **8 MiB**, 10,000
entries, regular files, directories, and relative links contained in the tree.
Archives have a SHA-256 digest, byte count and file count. Corruption, traversal,
external links, hard links, special files and protected Git metadata are refused.
No S3 bucket or new guest artifact permissions are required.

## Command lifecycle and recovery

1. Hold the run's OS lock and SQLite lease. Persist the execution reference,
   complete create parameters and idempotency token before requesting AWS work.
2. Create a runtime dedicated to this command. Import the accepted snapshot and
   execute through the mandatory network-denying syscall guard.
3. Capture bounded stdout/stderr and the real outcome. For work commands, export
   a bounded candidate archive. Treat it as untrusted input; background writers
   may still exist while it is captured.
4. Delete the **whole runtime**, then require `GetAgentRuntime` to return
   `ResourceNotFoundException`. Only then validate/extract and atomically publish
   the candidate on the controller. A fresh runtime receives that exact artifact
   for the next command or the read-only independent verifier.

The prior local workspace remains intact. A durable pointer selects the latest
accepted candidate, so an interrupted publication does not destroy the previous
one. Conversation/workspace checkpoints use that accepted artifact.

`harness reconcile RUN_ID` refuses a live owner's lock. For a dead worker it
reconciles the recorded AWS handle, deletes its runtime, preserves the accepted
workspace and reports interruption. A lost create response is resolved with the
same idempotency token and parameters; commands are never replayed. If AWS cannot
resolve an ambiguous create or confirm deletion, cleanup remains unconfirmed.
Reconciliation does not turn an unknown command outcome into success or
automatically resume the model conversation.

`agentcore-cleanup --state-dir PATH` retries recorded infrastructure cleanup
without model calls. It also refuses active controller locks. Retain the private
state directory until cleanup is confirmed. The Actions workflow additionally
audits exact runtime absence and removes their logs, identities and image.

## Limits

Each command pays runtime startup/deletion latency. The trusted artifact service
has the runtime's public network; guarded commands cannot create sockets. Work
commands have ordinary non-root filesystem permissions inside their disposable
microVM. Verification denies all filesystem writes, including `/tmp`. Prepare
dependencies in the image and use relative repository paths in portable tasks.

There is no disk quota, distributed controller takeover, automatic model resume,
or long-term object-store checkpoint service. These limits are explicit; AWS
selection does not enable an unrestricted shell fallback.
