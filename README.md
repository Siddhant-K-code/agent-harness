<p align="center">
  <img src="assets/readme/hero.svg" width="100%" alt="agent harness — ask about a project, run an isolated coding task, verify it independently, and review the patch">
</p>


**Ask about your code. Delegate a task. Review a verified patch.**

A local CLI and project chat for coding work with a budget, an independent verifier, and a record of what happened. Bring your own OpenAI key. Run questions against committed project files, then turn the conversation into an isolated coding run. Your source checkout stays unchanged.


[Get started](docs/getting-started.md) · [Chat & integrations](docs/ui-and-integrations.md) · [How it works](docs/architecture.md) · [Changelog](CHANGELOG.md)

## Install and open the app

**Binary downloads — v0.1.0-rc.5 (unsigned draft)**

| Your computer | Download |
| --- | --- |
| macOS · Apple Silicon | [Download `.tar.gz`](https://github.com/Siddhant-K-code/agent-harness/releases/download/untagged-8aa7d8190d6cdd424351/agent-harness_v0.1.0-rc.5_darwin_arm64.tar.gz) |
| macOS · Intel | [Download `.tar.gz`](https://github.com/Siddhant-K-code/agent-harness/releases/download/untagged-8aa7d8190d6cdd424351/agent-harness_v0.1.0-rc.5_darwin_amd64.tar.gz) |
| Linux · Intel/AMD 64-bit | [Download `.tar.gz`](https://github.com/Siddhant-K-code/agent-harness/releases/download/untagged-8aa7d8190d6cdd424351/agent-harness_v0.1.0-rc.5_linux_amd64.tar.gz) |
| Linux · ARM64 | [Download `.tar.gz`](https://github.com/Siddhant-K-code/agent-harness/releases/download/untagged-8aa7d8190d6cdd424351/agent-harness_v0.1.0-rc.5_linux_arm64.tar.gz) |

[SHA-256 checksums](https://github.com/Siddhant-K-code/agent-harness/releases/download/untagged-8aa7d8190d6cdd424351/checksums.txt) · [Draft installation instructions and release notes](https://github.com/Siddhant-K-code/agent-harness/releases/tag/untagged-8aa7d8190d6cdd424351) · [All releases](https://github.com/Siddhant-K-code/agent-harness/releases)

Sign in to GitHub with collaborator access to use these draft links. Verify the archive against `checksums.txt` before extracting and running its `install.sh`. These are older, unsigned binaries; draft URLs can change if the draft is edited. They do not include the newer package-license and signing changes.

**Current source:** [Download ZIP](https://github.com/Siddhant-K-code/agent-harness/archive/ae422133ae5f081ad35e356f6d94a83077f0b0c2.zip) · [Browse source](https://github.com/Siddhant-K-code/agent-harness/tree/ae422133ae5f081ad35e356f6d94a83077f0b0c2) · [Minimal installation / Linux](docs/getting-started.md#install-from-source). The ZIP contains source code, not an app binary. With Go 1.25+ installed, run `go build -o harness ./cmd/harness` from the extracted directory, then `./harness --help`. ZIP builds report `dev` without a source commit. The full setup below uses a Git clone, records the source commit, and installs the CLI on PATH.

**Complete source setup (macOS).** Signed `v0.1.0-rc.6` binaries are not published yet. This command uses [Homebrew](https://brew.sh/) and repository access to build the current pinned source revision, prepare Docker and AgentTrace, configure local BYOK, and start the web app:

```sh
(
  set -eu
  brew install git gh go python@3.12 docker colima
  export PATH="$HOME/.local/bin:$PATH"
  gh auth status --hostname github.com >/dev/null 2>&1 || gh auth login --hostname github.com --web
  colima start
  source_dir=$(mktemp -d)
  trap 'rm -rf "$source_dir"' EXIT
  gh repo clone Siddhant-K-code/agent-harness "$source_dir"
  git -C "$source_dir" checkout --detach 8ac4a146a83b94c69f1737b00d0d7a59d11acdd9
  make -C "$source_dir" install
  harness version
  harness init "$HOME/harness-demo"
  cd "$HOME/harness-demo"
  docker pull node:22-alpine
  harness trace setup --python python3.12
  harness auth status >/dev/null 2>&1 || harness auth login
  harness doctor
  harness serve --task harness.task.json
)
```

Open the complete local link printed by the server. Setup downloads dependencies and prompts for your key locally; **no paid model request is submitted**. Choose a new setup directory if `~/harness-demo` exists. `make install` installs or replaces `~/.local/bin/harness`; keep that directory on your shell's PATH for later use. If Docker Desktop is already running, omit `docker colima` from the Homebrew command and omit `colima start`.

The signed-binary installer is implemented and tested; its download command becomes usable when rc.6 is published. It verifies **project-signed archives before extraction**. Apple notarization is not provided. [Linux and minimal source installation](docs/getting-started.md#install-from-source) · [Signed binary installation status](docs/getting-started.md#install-a-binary) · [Signature verification](docs/release-verification.md).

## See it work

<p align="center">
  <img src="assets/readme/demo.gif" width="100%" alt="Real demo: GPT-5.4 reads tags.js, explains the bug with source references, hands an edited task to Docker, passes the independent verifier, and returns a patch">
</p>

A real GPT-5.4 question and coding run on the bundled tag-normalization project. The agent reads the file, proposes a change, and produces a patch that passes the independent verifier. **$0.0288 estimated model cost**, including the question; sandbox cleanup confirmed. Selected UI captures with shortened pauses, not a timing benchmark. [Full-size GIF](assets/readme/demo.gif) · [Still image](assets/readme/demo-poster.png) · [Run receipt](docs/evidence/2026-09-06/project-chat/summary.json).

## What makes this a harness

The useful output is a patch with evidence you can inspect. The controller owns the limits and verification around the model.

| Capability | What you get |
| --- | --- |
| **Project chat** | Persistent conversations, read-only code questions, numbered source reads, and an editable handoff to coding tasks. |
| **Bounded execution** | Model/context controls, output reservations, dollar and token limits, timeouts, and cancellation. |
| **Independent verification** | A prepared verifier runs outside the agent's control. A coding task completes only when its checks pass. |
| **Isolated workspaces** | Docker locally or explicitly configured AWS AgentCore. Agent commands have no network or host credentials. |
| **Inspectable evidence** | Patches, usage, ordered events, prompt/tool manifests, cleanup status, and native AgentTrace exports for coding runs. |
| **Evaluated improvement** | Budgeted compaction, scoped versioned skills, observations, paired evaluations with holdouts, promotion gates, and rollback. |

The improvement loop proposes skill revisions and tests them before activation. It does not establish general self-improving performance. See the [learning mechanism and live evidence](docs/compaction-and-learning.md).

## Use the web app

The web app runs on **your computer**: `harness serve` starts the local server and you use it in your browser. There is no hosted sign-up URL or shared team server in this preview. The UI ships inside the CLI binary.

| You need | When |
| --- | --- |
| macOS or Linux, Git, and a browser | Required; native archives cover Intel/AMD and ARM. Windows binaries are not shipped. |
| The installed `harness` CLI | Required. [Build from source now](docs/getting-started.md#install-from-source); [signed binary downloads are pending](docs/getting-started.md#install-a-binary). |
| Your OpenAI API key, API billing/model access, and internet access | To submit questions or coding tasks. Inference runs through OpenAI. |
| A running Docker daemon and the task's image | For local coding runs. Read-only project questions do not need Docker. |

The signed installer needs Python 3.9+, OpenSSL and authenticated `gh` with repository access. Once installed, the core CLI/web app needs no Python, Node, npm or Go runtime. Native AgentTrace additionally uses Python 3.12+. MCP and AWS are separately configured integrations.

If you used the complete setup command above, the app is already running. For a minimal CLI-only installation, prepare a new project with:

```sh
harness auth status >/dev/null 2>&1 || harness auth login
harness init harness-demo
cd harness-demo
harness serve --task harness.task.json
```

Keep the terminal running and open the **complete private link** it prints, including its `#token=…` fragment. The default address is `http://127.0.0.1:8765/`; the bare address alone does not authenticate a new browser tab. This access token is separate from your OpenAI key.

1. In **Chat**, select the demo project and start a conversation. Try: “Read tags.js and explain the normalization bug, with source references.” **Model & budget** controls the question's model, context, output and spending cap.
2. For a coding task, start Docker and run `docker pull node:22-alpine` in another terminal. In Chat, describe the fix and choose **Run task…**, or use **Use answer as a task…**. Review the goal, model, skills and budget, then select **Start paid run**.
3. Follow the run link to inspect its verifier result, cost and patch. A passing run produces a patch for review; it does not apply it to your checkout or open a GitHub PR.

Setup is unpaid. **Ask project** and **Start paid run** make real API requests. The generated demo caps each question or coding run at **$0.50 estimated model spend**; the UI can lower that ceiling. These are per-operation limits, not an account-wide budget.

Ctrl-C stops the server and cancels work it owns. To return later, run the same `serve` command from `harness-demo` and open the newly printed link; history and artifacts remain in `.harness/`. Port busy? Add `--port 0`. [Complete browser workflow, project setup and troubleshooting](docs/ui-and-integrations.md#open-the-dashboard).

`auth login` uses hidden input and an owner-only local key file. You can instead supply `OPENAI_API_KEY` through your secret manager. Keys never pass through the browser or upload to GitHub during setup. [BYOK storage and precedence](docs/getting-started.md#bring-your-own-key).

Prefer the terminal?

```sh
harness doctor                 # Unpaid configuration checks
harness run                    # Real, billable coding run
harness list
harness status RUN_ID
harness trace setup --python python3.12  # Once per task; skip if complete setup installed it
harness trace RUN_ID
```

State and patches live under `.harness/`. Use `--state-dir PATH` consistently when working elsewhere. [CLI and setup guide](docs/getting-started.md).

<details>
<summary><strong>Use your own repository and verifier</strong></summary>

```sh
harness init --repo /path/to/repo \
  --goal 'Fix the parser for empty input' \
  --image my-project-env:dev \
  --verifier /path/to/independent-checks.sh \
  --max-usd 0.50 parser-task
cd parser-task
harness doctor
harness serve --task harness.task.json
```

The repository, prepared image and verifier must already exist for coding. The current UI uses prepared task files; it cannot yet open an arbitrary folder or create the verifier for you. Only committed files are used. Prepare dependencies in the image because agent commands have no network. Choose checks that actually establish your task's requirements. A passing verifier means those checks passed, not that arbitrary generated code is correct. [Task setup](docs/getting-started.md#your-own-task).

</details>

## Configure and connect

```sh
harness models
harness config set --model gpt-5.4 --context-window 32768 \
  --max-output-tokens 2048 --max-usd 0.20
harness config show
```

The context window covers input plus reserved output for one request; the total-token budget covers the whole run. GPT-5.4, GPT-5.4-mini and their configured snapshots are supported. Unknown model prices fail closed. [Model and context settings](docs/getting-started.md#model-and-context-settings).

- **MCP:** Streamable HTTP with explicit server/tool selection, pinned schemas, bounded calls and policy rechecks. [Connect an MCP server](docs/ui-and-integrations.md#mcp).
- **GitHub:** Authenticate with host `gh`, clone private repositories, and grant tasks read access to selected issues, PRs, diffs and checks. Credentials stay outside the sandbox. [GitHub setup](docs/ui-and-integrations.md#github-and-git-credentials).
- **Skills and learning:** Import repository-scoped guidance, pin versions, evaluate proposed revisions, then promote or roll back. [Compaction and learning](docs/compaction-and-learning.md).
- **AgentTrace:** Export real coding-run events through its native reader and replay tools. [Trace integration](integrations/agenttrace/README.md).
- **AWS AgentCore:** Run coding commands in fresh microVM sessions with artifact transfer and confirmed teardown. [AWS setup and evidence](docs/aws-harness.md).

## Current boundaries

This is a local, single-user preview. Ask mode reads a pinned Git commit; it does not see dirty/untracked files or call external integrations. Conversations use recent bounded history, display omission counts, and survive restarts. Interrupted model calls are never silently replayed; billing can be unknown. [Chat behavior and limits](docs/ui-and-integrations.md#project-chat).

Coding runs retain private repository content and tool output in their evidence. Exports are manual. Model cost is an estimate from configured prices and reported usage, separate from infrastructure and external-service charges. Docker shares a kernel and is not a multi-tenant security boundary.

Token-by-token chat streaming, attachments, UI task/verifier creation, reviewed GitHub writes, MCP OAuth/stdio, automatic crash resume, distributed workers and disk quotas are not supported in this version. OpenAI is the implemented model provider.

## Development

```sh
go test -race ./...
HARNESS_DOCKER_TEST=1 go test -count=1 ./internal/sandbox ./internal/runner
go vet ./...
```

[CI tests](https://github.com/Siddhant-K-code/agent-harness/actions/workflows/ci.yml) · [Platform package builds](https://github.com/Siddhant-K-code/agent-harness/actions/workflows/package.yml)

Tests cover real Git boundaries, Docker isolation, SDK/MCP protocols, budget admission, cancellation, persistence and recovery. Test doubles are confined to tests; production uses real model and executor paths. [Architecture](docs/architecture.md) · [Recovery](docs/recovery.md) · [Evidence](docs/evidence/2026-09-06/project-chat/README.md).

Licensed under the [MIT License](LICENSE).
