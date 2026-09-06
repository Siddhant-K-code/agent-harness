# Install and run agent-harness

Binary archives are built for macOS and Linux, on Intel/AMD (`amd64`) and ARM (`arm64`); Windows binaries are not shipped. Git and a browser are enough to open the installed app and prepare the bundled project. Questions require an OpenAI API key, model access, API billing and internet access. Local coding tasks additionally require Docker. The signature-verifying installer uses Python 3.9+ and OpenSSL; the installed core CLI/web app needs no Python or Go runtime. Optional native AgentTrace uses Python 3.12+.

The web app runs on your machine through `harness serve`. There is no hosted sign-up URL, shared team server, or separate frontend installation.

## Install a binary

**[v0.1.0-rc.6](https://github.com/Siddhant-K-code/agent-harness/releases/tag/v0.1.0-rc.6) is published with project-signed archives** for all four platforms. [Direct platform downloads](../README.md#install-and-open-the-app) are also available. rc.1–rc.5 are historical unsigned drafts; ordinary CI artifacts are also unsigned.

This command downloads the bootstrap from an immutable source commit and installs the matching signed archive. It requires Python 3.9+, OpenSSL and authenticated GitHub CLI with repository access:

```sh
(
  set -eu
  installer=$(mktemp)
  trap 'rm -f "$installer"' EXIT
  gh api -H 'Accept: application/vnd.github.raw+json' \
    'repos/Siddhant-K-code/agent-harness/contents/scripts/install-release.py?ref=bb5a4695f5f2acddd8cc799e67ed7410c06995f8' > "$installer"
  python3 "$installer" --version v0.1.0-rc.6
)
export PATH="$HOME/.local/bin:$PATH"
harness version
```

The installer verifies the RSA-3072/SHA-256 project signature using its embedded public key, then checks the archive hash and signed version/source metadata **before extraction**. It uses `~/.local/bin` without sudo, refuses an existing installation unless given `--force`, and accepts `--bin-dir PATH`. Add `--verify-only` to authenticate without installing. A failed signature never falls back to an unsigned install. See [trust root, manual verification and release workflow](release-verification.md).

This verification path applies to project-signed archives; older drafts have checksums only. Apple Developer ID signing and notarization are not provided. Platform trust policy can still require approval for a downloaded executable. Download access follows the repository's visibility; a private repository needs authenticated access. The bootstrap's `--public` mode works only when the release and repository are publicly accessible.

To upgrade, repeat the pinned bootstrap command with the desired signed version and `--force`. To uninstall, remove the installed `harness` binary. Credentials and task artifacts remain until you explicitly remove them.

## Complete local setup

On macOS with [Homebrew](https://brew.sh/), this installs prerequisites, starts [Colima](https://formulae.brew.sh/formula/colima), verifies and installs the released binary, prepares the demo and starts the local server:

```sh
(
  set -eu
  brew install git gh python@3.12 openssl@3 docker colima
  export PATH="$HOME/.local/bin:$(brew --prefix openssl@3)/bin:$PATH"
  gh auth status --hostname github.com >/dev/null 2>&1 || gh auth login --hostname github.com --web
  colima start
  installer=$(mktemp)
  trap 'rm -f "$installer"' EXIT
  gh api -H 'Accept: application/vnd.github.raw+json' \
    'repos/Siddhant-K-code/agent-harness/contents/scripts/install-release.py?ref=bb5a4695f5f2acddd8cc799e67ed7410c06995f8' > "$installer"
  python3.12 "$installer" --version v0.1.0-rc.6 \
    --setup "$HOME/harness-demo" --with-docker --with-trace --login --serve
)
```

If Docker Desktop or another daemon is already running, omit `docker colima` from the Homebrew install and omit `colima start`. The script uses your current Docker context. Python is explicitly [3.12](https://formulae.brew.sh/formula/python@3.12) for AgentTrace.

On Linux, install Git, GitHub CLI, OpenSSL, Python 3.12+ with `venv`, and a working Docker daemon through your distribution's supported installation method. [Official Docker Engine installation](https://docs.docker.com/engine/install/) includes distribution-specific setup. Check that `docker info` works as your regular user. Use the command above without `brew install` and `colima start`, set `PATH="$HOME/.local/bin:$PATH"`, and replace `python3.12` with the path to your Python 3.12+ interpreter if necessary. Binary installation needs no Go compiler or Make.

The command creates a new project and installs `~/.local/bin/harness`. If the project already exists, choose a different directory or restart it with `harness serve --task harness.task.json` from the project directory. Login reuses a configured local credential without testing API access. For chat-only setup, omit Docker/Colima from the prerequisite commands and remove `--with-docker --with-trace` from the installer invocation.

These setup flags perform the following steps:

1. Install the verified native CLI, create a new real Git demo and independent verifier.
2. With `--with-docker`, check the daemon and pull `node:22-alpine`.
3. With `--with-trace`, install pinned AgentTrace into the task's private virtual environment. `--python PATH` selects its Python 3.12+ interpreter.
4. With `--login`, reuse a configured OpenAI key or prompt with hidden input and save it locally. No key is uploaded during setup.
5. With `--with-docker`, run unpaid readiness checks; with `--serve`, start the embedded web app and print its private access link.

No paid model request is submitted. Package/image downloads need internet access and disk space. For the signed bootstrap, choose a new `--setup` directory and add `--force` only when replacing the installed CLI. If a dependency/login check fails after installation, the binary and any new task remain available; finish setup from that directory using the individual commands below. Skills, compaction and evaluated learning ship in the binary; MCP servers, GitHub grants and AWS access require your own project-specific configuration.

Add `~/.local/bin` to your shell's PATH for future sessions. Ctrl-C stops the web server. Return later with `cd ~/harness-demo` and `harness serve --task harness.task.json`, then open the newly printed complete link.

## Install from source

To build the released source yourself, you need Go 1.25+, Make, Git, GitHub CLI and repository access. Choose an unused checkout directory:

```sh
gh auth status --hostname github.com >/dev/null 2>&1 || gh auth login --hostname github.com --web
gh repo clone Siddhant-K-code/agent-harness
cd agent-harness
git checkout --detach bb5a4695f5f2acddd8cc799e67ed7410c06995f8
make install
export PATH="$HOME/.local/bin:$PATH"
harness version
```

`make install` installs or replaces `~/.local/bin/harness`; override with `make install PREFIX=/your/prefix`. Add that prefix's `bin` directory to PATH. The pinned revision has passed full CI and native package checks. Source builds report `dev` and their commit; they are locally built executables, not signed release archives. No image pull, model call, credential upload, or cloud deployment happens during this minimal installation.

## First task in the web app

```sh
harness init
cd harness-demo
harness auth status >/dev/null 2>&1 || harness auth login
harness serve --task harness.task.json
```

The default directory must not already exist. Choose another with `harness init my-demo`. Setup embeds a real buggy JavaScript fixture and an independent verifier. It does not use a simulated model or executor. Keep the server running and open its complete private link, including `#token=…`, in your browser. A bare `http://127.0.0.1:8765/` URL does not authenticate a new tab.

In **Chat**, choose the project and start a conversation. Try “Read tags.js and explain the normalization bug, with source references.” Review **Model & budget**, then choose **Ask project**. This makes a real GPT-5.4 request and may retrieve committed project files; no Docker daemon is needed for questions.

For a coding task, start your Docker daemon. In another terminal, from `harness-demo`:

```sh
docker pull node:22-alpine
harness doctor
```

Use **Run task…** in Chat to describe the fix, or **Use answer as a task…** to carry over the discussion. Review the form and click **Start paid run**. Follow the linked run to review verification, usage and the patch. The generated demo permits at most **$0.50 of estimated model usage per question or coding run**. Each operation has a separate cap. Change the task ceiling with `harness init --max-usd 1 my-demo`, or edit `limits.max_usd` in `harness.task.json` before starting the server. The UI can lower that ceiling.

For terminal-only coding, run `harness run` from the task directory instead of submitting through the UI. This is also a real, billable model run.

`doctor` checks the selected credential source, priced model, Git commit, verifier file, Docker image/backend, and writable state directory. `--json` produces structured diagnostics. It does not validate the key against OpenAI, execute the verifier, or prove dependencies are complete. A valid key with access to the selected model and API billing is required for `run`.

Successful CLI output includes `run_id`, `verified`, `cleanup_confirmed`, `estimated_usd_uncached`, and `patch`. Open the patch path to review the changes, or inspect it on the UI's Runs page. Reports, conversations and events remain under `.harness`; your source checkout is unchanged. A failed verifier can trigger a bounded repair attempt. Failure, cancellation, exhausted budgets, or unconfirmed cleanup exit nonzero in the CLI.

Ctrl-C stops the server and cancels its active work. Restart with the same task and state directory to retain history, then open the newly printed link. Closing the browser tab alone does not stop the server or cancel work. [Browser controls, multiple projects and recovery](ui-and-integrations.md).

## Local dashboard and integrations

Run `harness serve --task harness.task.json` and open the private local link. Use Chat to ask about committed project files and turn a message into a reviewed coding task. Conversation history and source reads persist locally. Use the dashboard to review runs and patches, inspect skills and prompts, and launch prepared tasks with model/context settings. The task's spending limit is the UI's ceiling. `harness serve` without a task enables inspection/cancellation only.

The UI is embedded in the binary. GitHub integration additionally requires `gh`; MCP uses Streamable HTTP and explicit server/tool selection. See [the complete setup guide](ui-and-integrations.md) or `UI-AND-INTEGRATIONS.md` in the archive for credentials, supported operations and current limits.

## Model and context settings

Inspect the configured OpenAI models and set values for the current task:

```sh
harness models
harness config set --model gpt-5.4-mini \
  --context-window 128000 --max-output-tokens 4096 \
  --max-total-tokens 250000 --max-usd 0.50
harness config show
```

`config set` validates and atomically updates `harness.task.json`, preserving repository/verifier paths and other task fields. `config show` prints the task and effective limits/pricing without accessing your key or calling an API. Adding flags to `config show` previews a change without saving it. Invalid combinations fail without changing the file. `--task PATH` selects a different task.

The same settings can be chosen during setup or overridden for one run:

```sh
harness init --model gpt-5.4 --context-window 128000 my-task
harness doctor --model gpt-5.4-mini --context-window 64000
harness run --model gpt-5.4-mini --context-window 64000
```

Explicit run/doctor flags override task values for that invocation only. Unspecified flags retain the task's settings. Changing models does not silently enlarge or clamp the context; choose a compatible window explicitly if the new model has a smaller capacity.

| Setting | Meaning | New-task default |
| --- | --- | --- |
| `--model` | Exact provider model ID; aliases and listed snapshots are accepted. | `gpt-5.4` |
| `--context-window` | Total tokens for one request: complete counted input plus the maximum reserved output. | 200,000 |
| `--max-output-tokens` | Maximum output per request, including reasoning tokens. | 2,048 |
| `--max-total-tokens` | Cumulative input and output across all requests, including repeated history. | 50,000 |
| `--max-usd` | Estimated model cost budget across the run. | $0.50 |

For a 128,000-token window with 4,096 output tokens, at most 123,904 input tokens can be admitted. The API token counter includes the submitted conversation, instructions and tool definitions. If that count no longer fits, the runner stops before generation and keeps its recorded artifacts. Tasks with compaction enabled first attempt bounded summarization of older complete exchanges. Existing tasks without a compaction policy retain the stop-on-overflow behavior. See [compaction, skills, and evaluated learning](compaction-and-learning.md). A larger window is a ceiling, not a request to fill the window. The total-token and dollar budgets can stop a run before it reaches that ceiling.

The optional task field is `limits.context_window_tokens`. Existing tasks without it (or with `0`) use a 200,000-token **combined** window. This is slightly stricter than the old hardcoded 200,000-input limit because output now also consumes window space. Context must be 1,024 tokens or more and fit the chosen model. Output must be 256..128,000 and smaller than the window. The cumulative token ceiling is 10,000,000; this is a harness bound, separate from model capacity.

Configured capacities, checked September 6, 2026:

| Model / snapshot | Maximum context | Maximum output |
| --- | --- | --- |
| `gpt-5.4`, `gpt-5.4-2026-03-05` | 1,050,000 | 128,000 |
| `gpt-5.4-mini`, `gpt-5.4-mini-2026-03-17` | 400,000 | 128,000 |

These are OpenAI's documented capacities, not guarantees of access through a particular API key. The catalog does not query your account. Sources: [GPT-5.4](https://developers.openai.com/api/docs/models/gpt-5.4), [GPT-5.4-mini](https://developers.openai.com/api/docs/models/gpt-5.4-mini). Live coding evidence currently covers GPT-5.4 on the small recorded fixture; other IDs and large windows have configuration/protocol tests, not new paid benchmark results.

GPT-5.4 applies higher session rates when input exceeds 272,000 tokens. If `context-window - max-output-tokens` can exceed that threshold, the harness estimates **every request from the start** at $5 input / $22.50 output per million tokens, conservatively covering a later threshold crossing. Otherwise it uses $2.50 / $15; mini uses $0.75 / $4.50. This may overestimate actual charges for a short run with a large configured window. `doctor`, `config show`, reports, and request events expose the pricing basis. Reconciliation retains the selected schedule through the recorded task configuration. These remain estimates, not invoice reconciliation.

For other model families/providers, a configured protocol, capacity and price schedule is required. Unknown IDs fail rather than applying GPT-5.4's assumptions to a different model. Arbitrary provider URLs or user-defined prices are not supported in this preview.

## Compaction, skills, and learning

New tasks enable budgeted compaction and local observation capture. Use `harness config set --compaction=false --learn=false` to disable them. Skills require explicit import and selection. `harness learn init learning-lab` creates a complete unpaid lab; its subsequent coding and evaluation commands use your API key. See [the complete workflow and limits](compaction-and-learning.md). The release archive also includes `COMPACTION-AND-LEARNING.md`.

## Bring your own key

The current provider is **OpenAI Responses**, with explicit price schedules for `gpt-5.4`, `gpt-5.4-mini`, and the listed pinned snapshots. There is no Anthropic, local-model, or arbitrary OpenAI-compatible endpoint support yet. BYOK means your OpenAI account is billed directly, without a harness proxy.

Choose either:

- `harness auth login`: hidden terminal input, then an owner-only **plaintext** local file. It is not an OS keychain. The command prints its destination before reading the key, makes no API call, and does not upload it anywhere. `--replace` explicitly replaces a saved key.
- `OPENAI_API_KEY` supplied by a secret manager: used in memory, with no need to save a key. For a secret manager that prints one credential to stdout, pipe it to `harness auth login --stdin` only when you want persistent local storage. Never put the key in a task JSON or a command-line flag.

Default login locations:

| System | Saved key |
| --- | --- |
| macOS | `~/Library/Application Support/agent-harness/openai-key` |
| Linux | `$XDG_CONFIG_HOME/agent-harness/openai-key`, or `~/.config/agent-harness/openai-key` |

The key file uses `0600`; its directory must be private (`0700`). The controller resolves credentials in this order:

1. Nonempty `OPENAI_API_KEY`.
2. Explicit `--api-key-file PATH`.
3. Existing `<state-dir>/openai-key`, retained for existing setups.
4. The user-config key saved by `harness auth login`.

An invalid explicit or existing file produces an error instead of silently falling back. Use `harness auth status` from the task directory to see the effective source without revealing the key. `harness auth logout` deletes only the user-config key; it does not unset environment variables, remove legacy task files, or revoke a key at OpenAI.

The controller sends task instructions and selected repository/tool context to OpenAI using that key. Agent commands do not receive the key. Reports and event journals remain local unless you export/share them; they can contain source code and tool output. BYOK does not mean inference runs locally. See [OpenAI's authentication guidance](https://developers.openai.com/api/reference/overview#authentication).

### CI is a separate setup

Local login never writes GitHub Actions secrets. If you deliberately enable a live model workflow, configure its `OPENAI_API_KEY` secret in that repository/environment yourself. Expose it only to trusted controller steps, not untrusted PRs or guest commands. Ordinary test and packaging workflows require no OpenAI or AWS secrets and do not make paid model calls.

The existing AWS workflow uses GitHub OIDC for AWS credentials and a separate OpenAI secret only for its optional live-model step. AWS credits do not pay for direct OpenAI API usage. Model budgets are per-run estimates, not account-wide limits or invoice reconciliation; infrastructure charges are separate.

## Your own task

```sh
harness init --repo /absolute/path/to/repo \
  --goal 'Fix empty-input handling in the parser' \
  --ref HEAD --image my-project-env:dev \
  --verifier /absolute/path/to/independent-checks.sh \
  --model gpt-5.4 --max-usd 0.50 parser-task
cd parser-task
harness doctor
harness run
```

The ref is resolved and pinned when creating the task. The repository path is absolute; edit it if moving the task to another machine. The verifier is copied into the new task directory. Keep it outside the agent workspace, require meaningful assertions, and propagate failures. Only committed source files are copied; uncommitted/untracked work, submodules, and runtime network dependency installation are unsupported. Build a prepared image for the language and dependencies you need.

Task files remain plain versioned JSON. `harness run --task PATH` supports existing task files. The original source example retains its $2 limit; the new bundled setup defaults to $0.50. Docker is the default. AWS is an advanced path with account-specific IAM/ECR/OIDC setup, not one-command cloud provisioning; follow [AWS setup](https://github.com/Siddhant-K-code/agent-harness/blob/main/docs/aws-harness.md).

## Optional native replay

With Python 3.12+, Git, and network access available:

```sh
harness trace setup
harness trace RUN_ID
```

This installs the pinned AgentTrace dependency into the task's `.harness/agenttrace-venv`. It is optional and performs a package download. Export defaults to metadata and records capture gaps; `--include-content` opts into selected content with AgentTrace redaction. Existing native exports and the journal are not a promise of full model-conversation reconstruction or automatic resume. Follow [native replay usage](https://github.com/Siddhant-K-code/agent-harness/blob/main/integrations/agenttrace/README.md).

## Troubleshooting

| Symptom | Next step |
| --- | --- |
| `harness: command not found` | Add the installation's bin directory to PATH, or invoke its absolute path. |
| Release download is unavailable | Confirm your authenticated GitHub account has repository and draft-release access. The preview is private. |
| Browser cannot connect | Keep `harness serve` running on the same computer as the browser; open its printed URL. |
| Browser says unauthorized after restart | Open the newly printed complete access link, including `#token=…`. The token changes with each server process. |
| Port 8765 is busy | Start with `harness serve --task harness.task.json --port 0` and use the chosen port in its output. |
| Chat/task actions are disabled | Pass a prepared `--task`, check `harness auth status`, and ensure another UI operation is not active. Without a task, the server is for inspection/cancellation only. |
| History appears empty | Restart from the same task directory, or pass the original `--state-dir PATH`; a different directory selects different local state. |
| Task changed or is no longer configured | Restart with the intended task file and create a new conversation. Old history remains readable. |
| Docker daemon unavailable | Start Docker Desktop, Colima, or your Docker daemon; check `docker info`. |
| Image unavailable | Pull/build the exact image named in the task before running. |
| Saved key is not being used | Check `harness auth status`; environment and legacy task files take precedence. |
| HTTP 401/403 or model access error | Check the selected key and model permissions in your OpenAI project. Login does not test API access. |
| Budget cannot cover next request | Inspect the task and partial result, then choose a smaller task/output budget or explicitly raise `max_usd`. |
| Interrupted controller | Use `harness reconcile RUN_ID` from the same state directory to resolve cleanup. |
| Verifier fails | Inspect the verifier output and saved patch; checks must run with prepared dependencies and a read-only candidate. |
