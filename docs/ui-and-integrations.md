# Local UI and connected tools

The preview includes a local dashboard, a versioned system prompt, typed repository tools, a Streamable HTTP MCP client, and a GitHub credential broker. Everything uses the real controller and run database. UI assets are embedded in the binary; Node, a frontend build, and a hosted account are not required to use it.

## Open the dashboard

From a prepared task directory:

```sh
harness serve --task harness.task.json
```

Open the private access link printed by the command. It listens on `127.0.0.1:8765`; use `--port 0` for an available port. Use `--state-dir PATH` to inspect an existing state directory and `--api-key-file PATH` for an explicit local key. Repeat `--task` to make several prepared tasks available. Without `--task`, the dashboard can inspect and cancel existing runs but cannot start new ones.

The dashboard shows recent runs, patches/downloads, verifier outcomes, model usage, compactions, journal events, pinned skills and prompt artifacts. Model responses are reduced to status and usage before being sent to the browser; opaque reasoning is not displayed. The Skills page lists active versions, provenance, expiry and rollback history counts. Connections shows configured capabilities, not a connectivity claim.

New run lets you choose a prepared task, edit its goal, choose a supported model, set context/output limits, lower its model spending limit, and select skills. Repository, base ref, verifier, executor and external capabilities come from the task configured at server startup. The server validates all overrides and rejects a spending limit above that task's ceiling. It runs one UI-owned task at a time. Separate CLI workers remain visible and cancellable. Clicking **Start paid run** makes real model requests. Infrastructure charges and remote MCP service fees are outside the model estimate; the dashboard does not initiate skill proposals/evaluations or include their costs in run totals.

Ctrl-C cancels runs owned by this server and waits for their cleanup. A killed server requires `harness reconcile RUN_ID`, as with a killed CLI worker. The dashboard is a single-user local tool: it does not implement remote multi-user access, task queues or automatic recovery.

The link carries a random process-lifetime access token in the URL fragment, not in a query or server log. The UI stores it for the current browser tab and sends an authorization header to the same-origin API. Host/origin checks, no CORS, a restrictive content policy, and fixed artifact names protect the local surface. Do not expose this listener with a public tunnel. OpenAI and GitHub credentials never pass through the browser.

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

After that: UI-managed task setup and integration/skill management, MCP OAuth/stdio, automatic crash resume, broader repeated evaluations, disk quotas and retention, incremental trace delivery, and the planned Distill/ContextLab/ThinkBudget/LLMTraceFX adapters. Public distribution still requires a license decision and release hardening. Additional model providers remain unimplemented. The useful distinction is still a verified, budgeted patch with inspectable evidence and evaluated improvements; the UI makes that workflow accessible.
