# Implementation roadmap

The product direction is a measurable coding runtime: use real execution traces to locate failures, change the harness, and evaluate whether those changes improve verified task completion.

## 1. Real local vertical slice — implemented

Go CLI, OpenAI Responses tool loop, input-token counting and budget admission, Docker execution, pinned repository/image identity, independent verifier with bounded repair, SQLite events, cancellation, patch, and report. The [first live run](validation.md) passed on September 5, 2026; broader evaluation remains work below.

## 2. Recovery and durable ownership — next

Add worker IDs, renewable leases, and a reconcile command. Persist conversation checkpoints alongside workspace snapshots. Track tool attempts as prepared, started, completed, or uncertain. After a crash, inspect container and artifact state before deciding whether a command can be retried. Treat uncertain non-idempotent actions as requiring operator review. Test by killing workers during model responses, writes, verification, and patch capture.

Acceptance: kill a worker mid-task, reconcile it, resume from a known checkpoint, and produce an honest final outcome without duplicating a completed side effect.

## 3. AgentTrace evidence adapter

Deliver committed outbox events through an adapter matching AgentTrace's published event types. Carry stable event IDs and run/call relationships; make delivery idempotent. Redact secrets before export and keep raw artifacts local by default. Preserve original provider metadata so missing usage or an uncertain tool outcome remains visible.

Acceptance: inspect one run across model calls, tool attempts, verification, and recovery; disconnect the trace sink and recover delivery without losing or duplicating events.

## 4. Evaluation corpus and comparisons

Create 20–30 real coding tasks with pinned commits, prepared images, outside-workspace evaluators, and explicit budgets. Start with bounded repository bugs, failing tests, and small features. Record verified completion rate, cost per verified success, latency, repair attempts, infrastructure failures, and recovery success. Include repeated runs and failure examples; avoid turning one successful demonstration into a benchmark claim.

Acceptance: compare two harness versions on the same task set and produce an auditable result table with patches and traces.

## 5. Trace viewer and failure analysis

Build a local viewer for the existing data: run list, chronological calls, command results, patch, verifier output, and budget consumption. Add comparisons only after run identity and metric definitions are stable. Group failures using explicit evidence such as verifier failure, dependency failure, context exhaustion, timeout, or interrupted execution.

## 6. Stronger execution and controlled improvements

Add disk quotas, prepared dependency environments, sandbox capability profiles, and a remote execution backend. Keep controller secrets outside each sandbox. Then evaluate context selection, targeted tool interfaces, recovery strategies, and repair prompts against the corpus. A harness change ships because measured outcomes improve, with regressions visible.

## Tools

Current: Go, OpenAI Go SDK / Responses API, Docker, Git, SQLite, Go tests, GitHub Actions. Next: AgentTrace's supported ingestion API, a small trace UI, and an evaluation runner. Add infrastructure only when the corresponding milestone needs it.
