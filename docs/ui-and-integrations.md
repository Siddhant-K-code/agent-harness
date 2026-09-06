# Local UI and connected tools

The preview includes a local project chat and dashboard, a versioned system prompt, typed repository tools, a Streamable HTTP MCP client, and a GitHub credential broker. Everything uses the real controller and run database. UI assets are embedded in the binary; Node, a frontend build, and a hosted account are not required to use it.

## Open the dashboard

First [install the CLI](getting-started.md#install-a-binary) on macOS or Linux, then run `harness auth login` to configure your OpenAI key locally. New users can run `harness init harness-demo` and `cd harness-demo` to prepare the bundled project. The core browser app needs Git and the installed binary; OpenAI API access is needed to ask questions. Docker is needed only when starting local coding tasks. There is no hosted account or public app URL in this preview.

From a prepared task directory:

```sh
harness serve --task harness.task.json
```

Open the private access link printed by the command. It listens on `127.0.0.1:8765`; use `--port 0` for an available port. Use `--state-dir PATH` to inspect an existing state directory and `--api-key-file PATH` for an explicit local key. Repeat `--task` to make several prepared tasks available. Without `--task`, the dashboard can inspect existing runs and chat history and cancel runs, but cannot start new paid work.

Keep this terminal running while using the browser. Copy the **whole** link, including `#token=…`; entering the bare host/port in a new tab is not enough. Each server restart creates a new link. Use the same state directory to keep your history, and open the fresh link after restarting. Closing a browser tab does not stop a running question or coding task.

For several prepared projects in one workspace:

```sh
harness serve --state-dir ./workspace-state \
  --task /path/to/project-a/harness.task.json \
  --task /path/to/project-b/harness.task.json
```

The default state directory is `.harness` relative to the terminal's current directory; task repository/verifier paths resolve relative to their task file. Keep `--state-dir` consistent with other harness commands to use the same runs, skills and integration policy. Tasks and their ceilings are loaded at server startup. Restart after changing a task file, then create a new conversation for that configuration. [Prepare your own repository, image and verifier](getting-started.md#your-own-task); the UI cannot yet create a task or select an arbitrary project folder.

| In the browser | What to do |
| --- | --- |
| Chat | Choose a project, start a conversation, ask a code question, or review a task handoff. |
| Runs | Follow progress, inspect the independent verifier, review/download the patch and inspect recorded usage. |
| Skills | Inspect imported skills and their active versions; use the CLI to import, evaluate or promote them. |
| Connections | Inspect selected MCP/GitHub capabilities; configure them with the CLI on the host. |
| System prompt | Inspect the coding controller's prompt/tool contract. |

Create projects/verifiers, log in, configure integrations, and manage skills through the CLI. See [troubleshooting](getting-started.md#troubleshooting) for missing credentials, disabled actions, busy ports and lost history.

The dashboard shows recent runs, patches/downloads, verifier outcomes, model usage, compactions, journal events, pinned skills and prompt artifacts. Model responses are reduced to status and usage before being sent to the browser; opaque reasoning is not displayed. The Skills page lists active versions, provenance, expiry and rollback history counts. Connections shows configured capabilities, not a connectivity claim.

New run lets you choose a prepared task, edit its goal, choose a supported model, set context/output limits, lower its model spending limit, and select skills. Repository, base ref, verifier, executor and external capabilities come from the task configured at server startup. The server validates all overrides and rejects a spending limit above that task's ceiling. It runs one UI-owned question or coding task at a time. Separate CLI workers remain visible and cancellable. Clicking **Start paid run** makes real model requests. Infrastructure charges and remote MCP service fees are outside the model estimate; the dashboard does not initiate skill proposals/evaluations or include their costs in run totals.

Ctrl-C cancels runs owned by this server and waits for their cleanup. A killed server requires `harness reconcile RUN_ID`, as with a killed CLI worker. The dashboard is a single-user local tool: it does not implement remote multi-user access, task queues or automatic recovery.

The link carries a random process-lifetime access token in the URL fragment, not in a query or server log. The UI stores it for the current browser tab and sends an authorization header to the same-origin API. Host/origin checks, no CORS, a restrictive content policy, and fixed artifact names protect the local surface. Do not expose this listener with a public tunnel. OpenAI and GitHub credentials never pass through the browser.

## Project chat

**Chat** is the default page. Choose a prepared project under **Project for new chat**, then start a conversation. A conversation pins that task's committed Git ref when it starts. Ask about the code, follow up, or describe an implementation task in the composer. Model, context window, reserved output, and maximum spend per question are editable under **Model & budget**. Enter sends a question; Shift+Enter adds a line.

- **Ask project** calls the real OpenAI Responses API using the configured key. It receives the project/task goal, the current question, and up to six recent completed question/answer pairs. It can list, read numbered lines, and search regular tracked files. Answers include model-generated path/line references; expandable source reads let you check the evidence.
- **Run task…** opens the existing coding form with your message as its goal. **Use answer as a task…** includes the question and proposed answer as editable context. Review the goal, model, spending cap and selected skills, then click **Start paid run**. The project, pinned commit, verifier, executor and selected integrations remain those of the prepared task. The conversation links to the real run's status, verifier results and patch. If a new task requires different checks, prepare a matching task/verifier with the CLI first.
- **Stop** cancels the question or coding run owned by this server. Navigating away or refreshing does not cancel it. The source project stays unchanged; a coding run produces a separate patch. Questions continue to refer to the pinned commit, not that patch. To discuss newer committed changes, first prepare/update the task to use the newer ref, restart the server with that task, and create a new conversation. Tasks initialized with `--repo` pin a commit during setup, so starting a conversation alone does not advance that ref.

Q&A reads Git objects through fixed `rev-parse`, `ls-tree` and `cat-file` argument lists. It does not check out or execute repository code, invoke filters/hooks, run a model-supplied shell command, read host credentials, follow symlinks, or expose untracked/dirty files. It needs Git and API access, but no Docker daemon or AWS runtime. Files are limited to 256 KiB of UTF-8 text; reads to 200 lines/32 KiB; searches to 200 files/8 MiB/50 matches. Large files, binary files, symlinks, submodules and uncommitted changes are outside the current reader. These are separate read-only tools; the coding tools below still use their isolated executor.

Each question has its own budget, at most eight model responses (or the task's smaller step limit), its configured total-token limit, and a deadline of at most three minutes. The controller counts input and reserves full output cost before generation; it disables API retries. It drops complete older Q&A pairs when necessary and records how many were omitted. It never silently truncates the current question or in-flight tool exchanges. Chat does not yet summarize old conversations; the coding runner's existing compaction policy still applies to coding runs. A question can stop without a final answer if its output, context, response count or spending cap is exhausted.

Conversations are private, owner-only JSON under `<state-dir>/chats/`, capped at 40 turns per conversation and 200 conversations. The server holds an exclusive process lock for that chat store. Message request IDs deduplicate retries, including task handoffs. A request is recorded before paid generation, and returned usage is saved afterwards. On restart, unfinished turns become **interrupted** and are never replayed. A pending request is labelled **billing unknown**. An interrupted coding turn retains its run link where available; inspect/reconcile the run before deciding what to do next. History remains readable if its prepared task is removed or changed, but further work requires a new conversation.

Conversation history and source excerpts stay on this machine; the question, selected history and retrieved content are sent to OpenAI as needed. Raw provider responses and opaque reasoning are not persisted in chat files or sent to the UI. Within a question, opaque continuation items remain in memory for the tool loop. The implementation manually supplies context with `store:false`, following the [Responses conversation-state guide](https://developers.openai.com/api/docs/guides/conversation-state). This is not a claim about provider retention policies.

Chat currently shows activity and the final answer, rather than streaming individual text tokens. Ask mode does not receive MCP, GitHub or skill tools. Task handoffs retain the prepared task's integrations and selected skills. Q&A usage is shown per message and is separate from the Runs page's cost total; chat conversations do not yet have native AgentTrace exports. Linked coding runs retain their existing trace exports. Attachments, cross-project search, inline patch approval, UI task/verifier creation and chat-managed skill promotion remain future work.

## System prompt and native tools

```sh
harness prompt
harness prompt --json
```

`coding-v2` defines evidence-led inspection/edit/check behavior, project conventions, trust boundaries, compaction continuity, credential boundaries, and independent verification. A run assembles it with its actual backend and available tool names. `prompt.json` stores the text, tool definitions, version and a hash covering the complete request contract. Token counting and generation receive the same prompt and tool catalog. This prompt passed the integration smoke task; comparative quality across a broader corpus is still unmeasured.

| Tool | Purpose |
|---|---|
| `list_files` | List files below a repository-relative directory |
| `read_file` | Read a text file; use `exec` for ranged reads of large files |
| `search_text` | Search for literal text and return line numbers |
| `write_file` | Write one small text file without shell interpolation |
| `exec` | Run shell commands, targeted edits, tests and diagnostics |
| `finish` | Submit changes to the independent verifier |

Typed repository tools run through the same Docker/AgentCore executor lifecycle as `exec`. They never execute on the controller host. Read tools request a read-only workspace. Paths are relative; traversal, `.git` paths and symlink components are rejected. Writes are limited to 32,000 bytes and require existing parent directories. Output and time remain bounded by the executor. The baseline images need `/bin/sh` and ordinary Unix utilities; no extra host runtime is required.

## GitHub and Git credentials

Install the GitHub CLI and authenticate on the host:

```sh
gh auth login --hostname github.com --web
harness github status
harness github clone OWNER/REPO ./repo
harness github read --repo OWNER/REPO --resource pulls
harness github read --repo OWNER/REPO --resource diff --number 123
```

`gh` manages the credential; its storage behavior is documented in [GitHub CLI login](https://cli.github.com/manual/gh_auth_login). On systems without a credential store, `gh` can fall back to a plaintext file. The harness does not request or print the token. Clone invokes Git with an isolated command configuration, disabled hooks/filter configuration, a fixed GitHub HTTPS URL and `gh auth git-credential` as its helper. It does not change global Git configuration. Existing destinations are rejected; a failed clone leaves any partial checkout for inspection. Submodules and automatic LFS downloads are not implemented.

To let a coding task read GitHub, enable a repository in the operator policy and select it for that task:

```sh
harness integrations allow-github --repo OWNER/REPO
harness integrations select --github-repo OWNER/REPO
harness integrations check
```

The model receives `github_read` only when a repository is selected. Supported resources are `issues`, `issue`, `pulls`, `pull`, `diff`, and `checks`; `number` is an issue/PR number, or `0` for lists. Routes and HTTP GET are chosen by the broker, not by arbitrary model-supplied URLs. Checks resolve the PR head inside the selected repository. Results are bounded; lists return the first page. Credentials and Git metadata never enter the sandbox. This version does not push branches, create PRs, post comments, merge, or integrate GitHub Apps/enterprise hosts.

## MCP

The controller uses the [official MCP Go SDK](https://go.sdk.modelcontextprotocol.io/) with Streamable HTTP. For example, connect the [public OpenAI documentation server](https://developers.openai.com/learn/docs-mcp):

```sh
harness integrations add-mcp --name openai-docs \
  --url https://developers.openai.com/mcp \
  --allow-tool search_openai_docs --allow-tool fetch_openai_doc
harness integrations select \
  --mcp-tool openai-docs/search_openai_docs \
  --mcp-tool openai-docs/fetch_openai_doc
harness integrations check
```

For a bearer-authenticated server, add `--token-env MY_MCP_TOKEN` and set that variable through your local secret manager before starting the controller. The file stores only the variable name. `integrations list` inspects operator configuration; `select --clear` removes task selections. Repeat flags for several tools/repositories. All commands accept `--state-dir`; `select` and `check` accept `--task`.

Operator policy lives in owner-only `<state-dir>/integrations.json`. Task JSON can only select an allowed subset; it cannot supply a server URL, host command or credential. `add-mcp` replaces the named server's configuration, including its tool allowlist. Enable only tools whose effects and data access you intend to grant. A server's `readOnlyHint`, instructions or description is not authorization: tools can have external side effects regardless of annotations. The current operator allowlist is a local capability boundary, not per-user OpenFGA authorization or an interactive approval queue.

Before model generation, the controller connects, discovers the selected tools, validates their schemas locally, and saves `integrations.lock.json` with a hash. The model sees a catalog plus a strict `mcp_call(server, tool, arguments_json)` gateway. Each dispatch rechecks operator policy, exact selection and pinned argument schema. A policy change requires a new run. MCP descriptions/results remain untrusted context; the client offers no sampling, roots, elicitation or automatic resource reads. Remote schema references are not fetched.

Endpoints require HTTPS, except literal loopback HTTP for local servers. Redirects are refused. Credentials are sent only to the configured endpoint; known credential values are redacted from results and rejected in tool arguments. Discovery, response sizes and time are bounded. Non-text content is counted and omitted. Tools are dispatched once; an uncertain transport result stops the run, records uncertainty, and is never automatically replayed. Reconciliation also preserves a pending MCP call as an uncertain outcome. Native AgentTrace exports preserve generic tool names, paired results, errors, uncertainty and prompt/catalog hashes.

MCP OAuth login/refresh, stdio process launch, server installation/discovery UI, resource/prompt browsing and durable external-effect receipts remain pending. Learning evaluations currently reject tasks with live external integrations because those responses cannot yet be frozen for comparable trials.

## What remains

The next product milestone should be **review a patch, approve a scoped GitHub action, and open a draft PR**. That needs branch/PR policy, explicit approval bound to the patch, short-lived credentials, durable effect receipts, and recovery that can distinguish an applied action from an unknown one. Adding a write-capable tool alone would not complete that workflow.

After that: UI-managed task setup and integration/skill management, MCP OAuth/stdio, automatic crash resume, broader repeated evaluations, disk quotas and retention, incremental trace delivery, and the planned Distill/ContextLab/ThinkBudget/LLMTraceFX adapters. The project is MIT licensed; public release hardening remains pending. Additional model providers remain unimplemented. The useful distinction is still a verified, budgeted patch with inspectable evidence and evaluated improvements; the UI makes that workflow accessible.
