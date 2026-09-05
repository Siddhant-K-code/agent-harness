# agent-harness

A coding agent runtime with real OpenAI Responses API calls, real Docker execution, durable events, and independent verification.

The first milestone is a complete local loop: load a task, copy a committed repository snapshot, let the model inspect and edit it in Docker, run an operator-supplied verifier, and save the patch and outcome. The runtime contains no simulated model or executor. The [first real GPT-5.4 run passed](docs/validation.md), using three requests and about $0.013 in estimated token charges.

## Run it

Requires Go 1.25+, Git, a running Docker daemon, a prepared container image, and an OpenAI API key. macOS and Linux are supported. The default image for the example is `node:22-alpine`; Docker images are resolved to an immutable local image ID before execution. Pulling dependencies or images is an explicit setup step.

```sh
go build -o bin/harness ./cmd/harness
docker pull node:22-alpine
sh examples/normalize-tags/setup.sh
export OPENAI_API_KEY=...  # Prefer a secret manager or the local key file below.
bin/harness doctor --task examples/normalize-tags/task.json
bin/harness run --task examples/normalize-tags/task.json
```

Alternatively put the key in `.harness/openai-key`, with file permissions `600` and directory permissions `700`. This directory is ignored by Git. `--api-key-file` selects a different file; `OPENAI_API_KEY` takes precedence. The key stays in the controller and is never passed into containers. `doctor` checks configuration and Docker without making a model request.

The example calls GPT-5.4 to fix an actual JavaScript function, then evaluates it against separately held tests. It permits at most $2 of estimated standard text charges for one run. Review `task.json` before running: model calls incur real charges. No paid API calls run in the default test suite.

```sh
bin/harness list
bin/harness status RUN_ID
bin/harness cancel RUN_ID
bin/harness reconcile RUN_ID  # After an interrupted controller
bin/harness events RUN_ID > events.jsonl
```

Flags go before a run ID. Use `--state-dir PATH` on every command when selecting a different state directory. Progress goes to stderr; results go to stdout. A failed, cancelled, or timed-out run exits nonzero.

## Your own repository

Copy the example task and set `repository`, `ref`, `goal`, `image`, and `verifier`. Paths resolve relative to the task JSON. Only the selected Git commit is copied; local uncommitted edits and untracked files are excluded. The source repository is never edited.

The verifier is a shell script controlled by you. It is read before execution and its hash is recorded. At verification time, the controller supplies it to a fresh container over stdin; the agent's workspace is mounted read-only. Put build outputs and caches in `/tmp`. Prepare dependencies in the image because run containers have no network. Require meaningful assertions and propagate failures with a nonzero exit code.

The model has two tools:

- `exec`: execute a shell script inside a fresh container, using `/workspace` for persistent files.
- `finish`: propose completion and trigger verification. Failed verification returns feedback for a bounded repair attempt.

## Evidence and limits

Each run stores `.harness/runs/RUN_ID/report.json`, `changes.patch`, the working copy, a private Git database, and the verifier snapshot. SQLite stores ordered model/tool/lifecycle events and an outbox in the same transaction as each state change. `model.responded` includes the real API response, usage, and response ID. Treat these local artifacts as sensitive: repository content and tool output can appear in them. Export is manual; automatic redaction is not implemented.

Before every generation request, the controller calls OpenAI's input-token counting endpoint, then checks the remaining token budget and reserves the maximum possible output cost. Pricing is explicitly configured for GPT-5.4 and GPT-5.4-mini using the [official model pages](https://developers.openai.com/api/docs/models/gpt-5.4), checked September 5, 2026. Input is conservatively priced as uncached. Requests use the standard service tier and stop below long-context pricing thresholds. Unknown model prices fail closed. Cost reports are estimates based on that schedule, not invoice reconciliation or account-wide spending limits. An interrupted request can have unknown billing; it is not retried automatically.

Containers run without networking, capabilities, a Docker socket, host credentials, or the host home directory. Root filesystems are read-only; CPU, memory, PID count, runtime, and captured output are bounded. Only the disposable workspace is bind-mounted. Commands run as a non-root UID. Docker isolation shares a kernel and is not a multi-tenant security boundary. Workspace disk quotas, remote sandboxes, and stronger isolation are future work.

Durable events and [native AgentTrace export](integrations/agenttrace/README.md) are implemented. Export a terminal run with `bin/harness trace RUN_ID` after running `sh integrations/agenttrace/setup.sh`. Export defaults to metadata, records capture gaps, and uses AgentTrace's native store and replay tools. The [real-run trace evidence](docs/evidence/2026-09-05/agenttrace/manifest.json) was loaded by the pinned AgentTrace reader.

Crash reconciliation is implemented for new Docker runs: `harness reconcile RUN_ID` requires exclusive controller ownership, stops recorded containers, preserves partial work, and marks interrupted outcomes failed or cancelled. Private conversation/workspace checkpoints are captured at quiescent tool boundaries. Automatic resume and incremental outbox delivery remain pending. See [recovery behavior and limits](docs/recovery.md). A verifier passing means its checks passed; it is not a proof that arbitrary generated code is correct or resistant to evaluator tampering.

## Development

```sh
go test -race ./...
HARNESS_DOCKER_TEST=1 go test -count=1 ./internal/sandbox ./internal/runner
go vet ./...
```

The integration test launches real containers and checks writes, read-only verification, credential exclusion, disabled networking, and timeout cleanup. HTTP stubs are limited to SDK protocol tests. See [architecture](docs/architecture.md) and [implementation roadmap](docs/roadmap.md).

The [integration design](docs/native-integrations.md) connects the projects through native hooks and versioned evidence. AgentTrace batch export is implemented; Distill and the other adapters remain planned. The [remote execution plan](docs/execution-backends.md) evaluates AWS AgentCore Runtime in `us-east-1`, with Fargate and other providers behind an explicit backend contract.
