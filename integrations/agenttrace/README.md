# AgentTrace export

The harness exports terminal runs through AgentTrace's actual `TraceStore` API, pinned to commit `b109ec5b3714b842746e97ee8e975329d8582667` (package version 0.94.1). It produces native `meta.json` and `events.ndjson` plus `harness.json` for lifecycle/verification/coverage details and a checksummed `manifest.json`.

```sh
sh integrations/agenttrace/setup.sh
go build -o bin/harness ./cmd/harness
bin/harness trace RUN_ID
```

Default output is `.harness/traces/RUN_ID`. Python is an export dependency only; the model loop does not import or start it. No model or cloud calls are made by export. The selected Python installation must carry pip's provenance for the pinned Git revision; an unrelated AgentTrace version is rejected.

The default projection omits prompts, shell arguments, stdout/stderr text, repository paths, raw API responses, and encrypted continuation items. It retains observed usage, status, timings, call links, truncation, verification, and report estimates. Export selected command/result text only when useful:

```sh
bin/harness trace --include-content --output .harness/traces-content RUN_ID
```

AgentTrace redacts both the native records and sidecar before publication. Treat selected content as sensitive despite heuristic redaction. The original recovery journal is not changed. Flags precede the run ID; `--state-dir` and `--python` support custom paths. A pre-existing output root must have mode 700.

Native reading and replay:

```sh
.harness/agenttrace-venv/bin/python - RUN_ID <<'PY'
import sys
from agent_trace.store import TraceStore
from agent_trace.replay import replay_session
store = TraceStore('.harness/traces', use_workspace_env=False)
replay_session(store, sys.argv[1])
PY
```

Repeated identical exports validate checksums and reuse the session. Different content policies or source projections require a different output root; corrupt or conflicting exports are never overwritten. Publication occurs after native parsing and hash-chain validation, using a locked staging directory, file/directory fsync, and atomic rename. An abruptly killed writer may leave a hidden `.harness-stage-*` directory, which normal AgentTrace session discovery ignores. It is safe to remove that staging directory after confirming no exporter is running.

The sidecar reports what capture missed. A shell command is not a filesystem audit. The initial runner records successful finish verification without a separate finish-tool result; export preserves the verification and marks the absent tool result explicitly. Missing provider usage and unanswered requests stay unknown. AgentTrace's existing renderer may not display every sidecar field; inspect `harness.json` for coverage and report details.

```sh
HARNESS_AGENTTRACE_PYTHON="$PWD/.harness/agenttrace-venv/bin/python" go test -race ./internal/trace/agenttrace
```

Tests exercise the real pinned native store and renderer, duplicate concurrent exports, secret canaries, corruption, and abrupt failure before publication. No simulated model/executor is used by this integration. Exports are batch-only; incremental outbox delivery is not implemented.

## Remote AgentCore evidence

The AWS probe now writes a versioned journal before each command API call and after each observed result. It exports through the same pinned native bridge, with stable IDs, parent links, atomic publication, redaction, and checksum verification. The metadata records zero model calls because this deterministic probe reuses the existing GPT-5.4 patch. AWS billed cost remains unknown.

```sh
bin/agentcore-probe --export-trace --state .harness/aws-probe.json
```

This command reads `.harness/aws-probe.json.trace.json` and requires no AWS credentials or API calls. `--trace-output` and `--python` select other paths. An interrupted command stays without a native `tool_result`; its partial result/error remains in the sidecar. A stop acknowledgement or successful runtime teardown does not invent that missing outcome. The earlier aggregate report lacks command timestamps and is deliberately not accepted as a journal.

The AWS workflow exports twice to verify idempotency, loads the result through the real `TraceStore`, and produces native text/HTML replay. It retains those artifacts with the private report. Export requires a terminal controller journal; a worker killed before a terminal record needs explicit reconciliation before export, rather than fabricated completion.
