# Implementation roadmap

Build a measurable coding runtime: use real evidence to locate failures, change one part of the harness, and evaluate verified outcomes. Detailed proposals: [native integrations](native-integrations.md) and [execution backends / AWS](execution-backends.md). Status is recorded per milestone.

## Baseline — implemented

Go CLI, real OpenAI Responses loop, token counting and budget admission, Docker execution, pinned repository/image identity, independent verifier with bounded repair, SQLite events/outbox, cancellation, patch, and report. The [first live GPT-5.4 run](validation.md) passed on September 5, 2026. One successful task is not a benchmark.

## 1. Native AgentTrace export — implemented; corpus expansion — pending

`internal/trace/agenttrace` and `harness trace` export terminal runs through the pinned native Python API. Stable IDs, linked calls, coverage, redaction, checksums, and atomic publication are tested against AgentTrace's reader and renderer. The existing GPT-5.4 run exported successfully without new model calls; see [evidence](validation.md).

Prepare five real task cases with pinned commits and controller-owned verifiers: ordinary bug, small feature, plausible wrong fix, dependency failure, and cancellation/recovery. Separate deterministic infrastructure checks from paid evaluation.

Acceptance: AgentTrace's reader accepts our evidence; duplicate export and interrupted publication are safe; secret markers and omissions are accounted for. Corpus manifests identify task, environment, evaluator, and allowed spend.

## 2. Durable attempts and an execution backend contract — local reconciliation implemented

Docker now implements an executor lifecycle/capability interface. New runs hold a local OS lock, renew a lease, reject stale writers, journal execution references before dispatch, and save paired private conversation/workspace checkpoints. `harness reconcile` stops orphaned executions and preserves partial work without replay. See [recovery](recovery.md). The AWS adapter adds bounded artifact transfer and interrupted-worker cleanup. Distributed takeover, automatic resume, and the complete fault-injection matrix remain pending.

Acceptance: kill workers before dispatch, during commands, after effects but before acknowledgement, during verification, and during snapshot publication. Reconcile without silently repeating uncertain external effects or granting two workers execution ownership.

## 3. AWS AgentCore Runtime experiment in us-east-1 — isolated commands and native traces validated

`internal/sandbox/agentcore` implements the AWS command stream, and `cmd/agentcore-probe` passed twelve real cloud checks using the actual GPT-5.4 patch. The ARM64 image, scoped IAM/OIDC bootstrap, manual GitHub workflow, private evidence artifacts, and teardown are in [infra/aws/agentcore](../infra/aws/agentcore/README.md). [Evidence](evidence/2026-09-05/aws-isolation/README.md) confirms command network denial, read-only verification, native AgentTrace export, bounded output, timeout, disconnect, and whole-runtime deletion, with zero new model calls. The first experiment was authorized within the $50 allocation; billed cost remains unmeasured.

The [full AWS adapter](aws-harness.md) now implements durable artifact transfer, controller ownership and interrupted-worker reconciliation. Its [real transfer/crash acceptance and GPT-5.4 run passed](evidence/2026-09-05/aws-harness/README.md), with native tracing and all seven runtimes confirmed absent. The remote client mandates the network-denying and read-only verifier profiles; native trace export preserves incomplete commands. Docker remains the default.

Acceptance: a real task produces a verified patch and AgentTrace evidence remotely; interrupted runs and cleanup are accounted for. If required capabilities cannot be enforced, evaluate Fargate rather than reducing guarantees silently.

## 4. Compaction, skills, and evaluated learning — implemented

Budgeted working-memory compaction preserves task/skills, recent tool continuity and verifier feedback, with source manifests and checkpoints. Explicit repository-scoped skills are content-addressed and pinned per run. Persistent observations feed real model-generated candidates. Paired repeated runs with a holdout gate promotion and permit rollback; `harness learn cycle` performs one bounded iteration. See [the implementation guide](compaction-and-learning.md). A three-task installable lab exercises the full path; broader evaluation is still required before claiming general gains.

## 4b. Distill and ContextLab experiments — pending

Add `internal/context` with source manifests and an optional Distill Go adapter. Protect task requirements and provider protocol items. Compare unchanged retrieval, exact deduplication, and compression; add semantic embeddings with explicit configuration and cost accounting. ContextLab consumes manifests offline.

Acceptance: token savings are reported with verified completion, latency, and failures. A smaller prompt alone is insufficient to enable a policy by default.

## 5a. Local UI, typed tools, MCP and GitHub reads — implemented

The embedded `harness serve` dashboard displays real run evidence and launches prepared tasks with bounded model/context settings. `coding-v2` records a prompt/tool contract per run. Typed file/list/search/write tools share the existing executor lifecycle. The operator-scoped Streamable HTTP MCP broker discovers and pins selected tool schemas, rechecks policy, and stops on uncertain effects. GitHub reads and authenticated private clones use host `gh` credentials. See [setup and limits](ui-and-integrations.md) and [live validation](evidence/2026-09-06/ui-integrations/README.md).

## 5b. Project conversations — implemented

The local UI now supports persistent project chat, real read-only Q&A over a pinned Git commit, numbered source reads, configurable model/context and per-question budget, cancellation, and an editable message-to-coding-task handoff. Request IDs deduplicate paid submissions; interrupted questions are not replayed. Coding results link back to their run evidence. Q&A needs no sandbox runtime because it only reads committed blobs through fixed Git commands. See [chat setup, privacy and limits](ui-and-integrations.md#project-chat).


## 5c. GitHub review/approval, distributed authorization, and incremental evidence — pending

Extend the local broker with patch-bound approval for GitHub writes. Implement sponsor authentication and per-dispatch OpenFGA checks before external mutations. Read-only issue/diff/check access is available; scoped branch pushes and draft PR creation remain pending. Add actionsec for workflow changes and task-specific diagnostics/browser checks when needed.

Deliver the outbox incrementally with per-sink cursors, idempotency, partial-write recovery, and visible exporter failures. Keep control state independent of sink availability.

Acceptance: forged/expired authority, revocation, out-of-scope resources, broker bypass, and sink outages have tested outcomes.

## 6. Repeated evaluation — implemented; broader corpus / LLMTraceFX adapter — pending

Expand to 20–30 real tasks and repeated runs within a separately approved budget. Implement a whole-workflow evidence adapter; keep inference benchmarks separate. Record verified success, cost including failures, latency, repairs, recovery, and missing measurements. Consider ThinkBudget experiments where providers support the necessary control.

Acceptance: compare harness revisions on identical task/evaluator identities and disclose environment/model differences. Publish selected patches, traces, and regressions after artifact review. Promote configurations because verified outcomes improve.

## Tools by milestone

Current: Go, OpenAI Go SDK / Responses, Git, Docker/Colima, SQLite, Go tests, GitHub Actions. Native export: pinned AgentTrace Python package. Remote experiment: AWS SDK for Go v2, AgentCore Runtime, ECR, IAM, short-lived logs, infrastructure as code; bounded artifact transfer uses authenticated invocations; S3 remains optional future storage. Later: Distill Go packages, OpenFGA SDK, selective MCP connectors, ContextLab and LLMTraceFX adapters. E2B, Modal, and self-managed Firecracker remain options when experiments need them.
