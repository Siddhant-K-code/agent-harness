# agent-harness

**Delegate a coding task. Review a verified patch.**

A CLI for bounded coding work: give it a committed repository, a goal, an independent verifier, and a model budget. It runs a real agent in an isolated workspace and returns the patch, check results, usage estimate, and cleanup status. Your source checkout stays unchanged.

Bring your own OpenAI key. Run locally with Docker; AWS AgentCore is an advanced, explicitly configured backend. The controller calls OpenAI directly. There is no harness account, hosted credential proxy, or automatic trace upload.

**Private preview.** macOS/Linux binaries and a bundled demo are packaged for evaluation. Repository access is still required; see [installation and setup](docs/getting-started.md) and [distribution status](DISTRIBUTION.md).

## Start with a real task

After [installing the CLI](docs/getting-started.md), you need Git, a running Docker daemon, and an OpenAI API key:

```sh
harness init
cd harness-demo
harness auth login
docker pull node:22-alpine
harness doctor
harness run
```

`init` creates a real Git fixture, task configuration, and separate verifier from assets bundled in the binary. `auth login` reads the key with hidden input and saves an owner-only local file. `doctor` checks configuration without calling a model. The demo fixes JavaScript tag normalization and runs six independent cases plus input-mutation checks. The generated task defaults to GPT-5.4 and a **$0.50 estimated model budget per run**. Review `harness.task.json` before running; `run` makes real, billable API requests.

Environment-based BYOK remains available: supply `OPENAI_API_KEY` from your secret manager and skip login. Setup never uploads a key to GitHub Actions. See [key storage, precedence, and CI](docs/getting-started.md#bring-your-own-key).

```sh
harness list
harness status RUN_ID
harness events RUN_ID > events.jsonl
harness cancel RUN_ID
harness reconcile RUN_ID
harness trace setup       # Optional native AgentTrace dependency
harness trace RUN_ID
```

Results go to stdout, progress to stderr. Flags go before a run ID. State defaults to `.harness` in the current directory; use `--state-dir PATH` consistently when working elsewhere. Failed runs exit nonzero.

## Choose the model and context

```sh
harness models
harness config set --model gpt-5.4-mini --context-window 128000 \
  --max-output-tokens 4096 --max-total-tokens 250000
harness config show
harness run
```

Settings are stored in `harness.task.json`. The same flags work on `init`, `run`, and `doctor`; run/doctor overrides do not modify the task file. A context window is the **input plus reserved output for one request**. `max-total-tokens` is a separate cumulative budget across the run. Model capacity is an upper bound, and choosing a larger window does not automatically supply more context or compact the conversation.

Configured models include GPT-5.4, GPT-5.4-mini, and their pinned snapshots. Use `harness models` for IDs and capacity limits. [Model configuration](docs/getting-started.md#model-and-context-settings) explains validation, long-context pricing, and overflow behavior.

## Compaction and evaluated learning

New tasks enable budgeted context compaction and local observations. Import and explicitly select versioned skills, then improve them through real paired evaluations with a holdout, immutable verifier copies, promotion checks, and rollback.

```sh
harness learn init learning-lab
cd learning-lab
harness skills import --id repair --repo repo --file SKILL.md
harness run --task words.task.json --skill repair
harness learn cycle --repo repo --skill repair --suite suite.json --max-usd 1.30
```

Setup is unpaid; the subsequent commands use your OpenAI key. The cycle performs one bounded iteration. Candidates that fail the gate remain inactive. See [configuration, budgets, evidence, and limitations](docs/compaction-and-learning.md). This implements an improvement mechanism, not a claim of general self-improving performance.

## Use your repository

Create a separate task directory from a local repository and your own verifier:

```sh
harness init --repo /path/to/repo \
  --goal 'Fix the parser for empty input' \
  --image my-project-env:dev \
  --verifier /path/to/independent-checks.sh \
  --max-usd 0.50 parser-task
cd parser-task
harness doctor
harness run
```

The chosen Git ref is pinned at setup. Only committed files are copied. The verifier is copied into the task directory and supplied by the controller at verification time. Prepare dependencies in the image: agent commands have no network. Docker verification mounts the workspace read-only; use `/tmp` for build outputs and caches. AWS verification denies all filesystem writes. A passing verifier means its checks passed; it does not establish correctness beyond those checks.

## Why this exists

Our focus is reducing the work of reviewing a delegated coding task: a bounded attempt, independently checked changes, and an explicit record of what happened. BYOK and multiple execution environments support that workflow. They are not unique on their own. See the [product decision and next milestones](docs/product-direction.md).

The [first complete AWS run](docs/evidence/2026-09-05/aws-harness/README.md) used four real GPT-5.4 requests, passed its independent verifier, exported a native AgentTrace session, and confirmed runtime deletion. Estimated model cost was $0.01421; AWS billing was not measured. This validates one small task, not general coding performance. [Crash and artifact-transfer acceptance](docs/aws-harness.md) provide separate execution evidence.

## Evidence and limits

Each run stores `.harness/runs/RUN_ID/report.json`, `changes.patch`, the working copy, a private Git database, and the verifier snapshot. SQLite stores ordered model/tool/lifecycle events and an outbox in the same transaction as each state change. `model.responded` includes the real API response, usage, and response ID. Treat these local artifacts as sensitive: repository content and tool output can appear in them. Export is manual; automatic redaction is not implemented.

Before every generation request, the controller calls OpenAI's input-token counting endpoint, then checks the remaining token budget and reserves the maximum possible output cost. Pricing and capacity are configured for GPT-5.4, GPT-5.4-mini, and their listed snapshots using the [official model pages](https://developers.openai.com/api/docs/models/gpt-5.4), checked September 6, 2026. Input is conservatively priced as uncached. Requests use the standard service tier. A GPT-5.4 configuration that can exceed 272,000 input tokens uses the higher rates for every request from the start; this is an upper-bound estimate even if the actual run stays short. Reports record the effective settings and pricing basis, and reconciliation uses the recorded task configuration. Unknown model prices fail closed. Cost reports are estimates based on that schedule, not invoice reconciliation or account-wide spending limits. An interrupted request can have unknown billing; it is not retried automatically.

Containers run without networking, capabilities, a Docker socket, host credentials, or the host home directory. Root filesystems are read-only; CPU, memory, PID count, runtime, and captured output are bounded. Only the disposable workspace is bind-mounted. Commands run as a non-root UID. Docker isolation shares a kernel and is not a multi-tenant security boundary. Workspace disk quotas remain future work. AWS microVM execution is available through the explicit agentcore backend.

Durable events and [native AgentTrace export](integrations/agenttrace/README.md) are implemented. Export a terminal run with `bin/harness trace RUN_ID` after running `harness trace setup`. Export defaults to metadata, records capture gaps, and uses AgentTrace's native store and replay tools. The [real-run trace evidence](docs/evidence/2026-09-05/agenttrace/manifest.json) was loaded by the pinned AgentTrace reader.

Crash reconciliation is implemented for Docker and AgentCore runs: `harness reconcile RUN_ID` requires exclusive controller ownership, stops recorded executors, preserves accepted work, and marks interrupted outcomes failed or cancelled. AWS confirms whole-runtime deletion and preserves the last accepted artifact. Private conversation/workspace checkpoints are captured at quiescent tool boundaries. Automatic resume and incremental outbox delivery remain pending. See [recovery behavior and limits](docs/recovery.md). A verifier passing means its checks passed; it is not a proof that arbitrary generated code is correct or resistant to evaluator tampering.

## Development

```sh
go test -race ./...
HARNESS_DOCKER_TEST=1 go test -count=1 ./internal/sandbox ./internal/runner
go vet ./...
```

The integration test launches real containers and checks writes, read-only verification, credential exclusion, disabled networking, and timeout cleanup. HTTP stubs are limited to SDK protocol tests. See [architecture](docs/architecture.md) and [implementation roadmap](docs/roadmap.md).

The [integration design](docs/native-integrations.md) connects the projects through native hooks and versioned evidence. AgentTrace batch export is implemented; Distill and the other adapters remain planned. The [remote execution plan](docs/execution-backends.md) evaluates AWS AgentCore Runtime in `us-east-1`, with Fargate and other providers behind an explicit backend contract.

The [AWS AgentCore capability probe](infra/aws/agentcore/README.md) passed twelve real cloud checks in `us-east-1`. Commands use a mandatory syscall allowlist that blocks networking; the verifier runs in a fresh session with filesystem writes denied. Native AgentTrace export produced 25 events from 31 controller records, preserving the disconnected command as incomplete. The real GPT-5.4 patch passed verification with zero new model calls, and teardown was independently confirmed. See the [live evidence and replay](docs/evidence/2026-09-05/aws-isolation/README.md). The new [full AWS backend](docs/aws-harness.md) adds artifact transfer and interrupted-worker reconciliation to the model controller. GitHub Actions uses OIDC and two repository variables for AWS. The optional live model workflow additionally uses the encrypted OPENAI_API_KEY repository secret.
