# Implementation roadmap

Build a measurable coding runtime: use real evidence to locate failures, change one part of the harness, and evaluate verified outcomes. Detailed proposals: [native integrations](native-integrations.md) and [execution backends / AWS](execution-backends.md). Status is recorded per milestone.

## Baseline — implemented

Go CLI, real OpenAI Responses loop, token counting and budget admission, Docker execution, pinned repository/image identity, independent verifier with bounded repair, SQLite events/outbox, cancellation, patch, and report. The [first live GPT-5.4 run](validation.md) passed on September 5, 2026. One successful task is not a benchmark.

## 1. Native AgentTrace export — implemented; corpus expansion — pending

`internal/trace/agenttrace` and `harness trace` export terminal runs through the pinned native Python API. Stable IDs, linked calls, coverage, redaction, checksums, and atomic publication are tested against AgentTrace's reader and renderer. The existing GPT-5.4 run exported successfully without new model calls; see [evidence](validation.md).

Prepare five real task cases with pinned commits and controller-owned verifiers: ordinary bug, small feature, plausible wrong fix, dependency failure, and cancellation/recovery. Separate deterministic infrastructure checks from paid evaluation.

Acceptance: AgentTrace's reader accepts our evidence; duplicate export and interrupted publication are safe; secret markers and omissions are accounted for. Corpus manifests identify task, environment, evaluator, and allowed spend.

## 2. Durable attempts and an execution backend contract — local reconciliation implemented

Docker now implements an executor lifecycle/capability interface. New runs hold a local OS lock, renew a lease, reject stale writers, journal execution references before dispatch, and save paired private conversation/workspace checkpoints. `harness reconcile` stops orphaned executions and preserves partial work without replay. See [recovery](recovery.md). Distributed takeover, automatic resume, provider-neutral artifact transfer, and the complete fault-injection matrix remain pending.

Acceptance: kill workers before dispatch, during commands, after effects but before acknowledgement, during verification, and during snapshot publication. Reconcile without silently repeating uncertain external effects or granting two workers execution ownership.

## 3. AWS AgentCore Runtime experiment in us-east-1 — isolated commands and native traces validated

`internal/sandbox/agentcore` implements the AWS command stream, and `cmd/agentcore-probe` passed twelve real cloud checks using the actual GPT-5.4 patch. The ARM64 image, scoped IAM/OIDC bootstrap, manual GitHub workflow, private evidence artifacts, and teardown are in [infra/aws/agentcore](../infra/aws/agentcore/README.md). [Evidence](evidence/2026-09-05/aws-isolation/README.md) confirms command network denial, read-only verification, native AgentTrace export, bounded output, timeout, disconnect, and whole-runtime deletion, with zero new model calls. The first experiment was authorized within the $50 allocation; billed cost remains unmeasured.

Next: test durable artifact transfer, controller ownership, and interrupted-worker reconciliation. The remote client already mandates the network-denying and read-only verifier profiles; native trace export preserves incomplete commands. Production model execution remains on Docker until those guarantees pass acceptance.

Acceptance: a real task produces a verified patch and AgentTrace evidence remotely; interrupted runs and cleanup are accounted for. If required capabilities cannot be enforced, evaluate Fargate rather than reducing guarantees silently.

## 4. Distill and ContextLab experiments

Add `internal/context` with source manifests and an optional Distill Go adapter. Protect task requirements and provider protocol items. Compare unchanged retrieval, exact deduplication, and compression; add semantic embeddings with explicit configuration and cost accounting. ContextLab consumes manifests offline.

Acceptance: token savings are reported with verified completion, latency, and failures. A smaller prompt alone is insufficient to enable a policy by default.

## 5. Typed tools, authorization, and incremental evidence

Add repository tools and task-selected external tools behind a broker. Implement sponsor authentication and per-dispatch OpenFGA checks before external mutations. Begin GitHub integration with read-only issue/diff/check access. Add actionsec for workflow changes and task-specific diagnostics/browser checks when needed.

Deliver the outbox incrementally with per-sink cursors, idempotency, partial-write recovery, and visible exporter failures. Keep control state independent of sink availability.

Acceptance: forged/expired authority, revocation, out-of-scope resources, broker bypass, and sink outages have tested outcomes.

## 6. Repeated evaluation and LLMTraceFX comparison

Expand to 20–30 real tasks and repeated runs within a separately approved budget. Implement a whole-workflow evidence adapter; keep inference benchmarks separate. Record verified success, cost including failures, latency, repairs, recovery, and missing measurements. Consider ThinkBudget experiments where providers support the necessary control.

Acceptance: compare harness revisions on identical task/evaluator identities and disclose environment/model differences. Publish selected patches, traces, and regressions after artifact review. Promote configurations because verified outcomes improve.

## Tools by milestone

Current: Go, OpenAI Go SDK / Responses, Git, Docker/Colima, SQLite, Go tests, GitHub Actions. Native export: pinned AgentTrace Python package. Remote experiment: AWS SDK for Go v2, AgentCore Runtime, ECR, IAM, short-lived logs, infrastructure as code; S3 artifact transfer remains planned. Later: Distill Go packages, OpenFGA SDK, selective MCP connectors, ContextLab and LLMTraceFX adapters. E2B, Modal, and self-managed Firecracker remain options when experiments need them.
