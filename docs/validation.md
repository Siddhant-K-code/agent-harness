# Validation evidence

## Live run — September 5, 2026

The real CLI used GPT-5.4 through the OpenAI Responses API and executed commands in Docker through Colima on macOS. The task repaired the deliberately faulty tag-normalization function in the example repository.

| Measurement | Observed result |
|---|---|
| Run | `a5fc5db3af9719003429189a5c31d5d8` |
| Model requests | 3 |
| Input tokens | 1,901 |
| Output tokens | 537 |
| Estimated USD, using uncached input pricing | $0.0128075 |
| Authorized run cap | $2 |
| Independent verification | 6 cases and input-mutation checks passed |
| Verification attempts | 1 |
| Final state | `completed` |

The model read the source, changed the implementation, added and ran its own tests, then requested completion. The controller independently ran the held-out verifier and captured the resulting patch. The local source fixture remained unchanged.

The [report](evidence/2026-09-05/report.json) retains the actual commit, image, and verifier identities; its local absolute patch path is replaced with a relative path. The [patch](evidence/2026-09-05/changes.patch) is the unmodified generated artifact. Raw Responses events remain local in SQLite. This is one smoke test, not a general coding benchmark or a recovery demonstration.

## Automated checks

`go test -race ./...`, `go vet ./...`, and a CLI build were run locally. With `HARNESS_DOCKER_TEST=1`, the test suite also executes real Docker containers. Coverage includes state/event/outbox consistency, transactional rollback, cancellation winning over completion, concurrent writers, response continuation and tool call IDs, strict configuration, bounded subprocess output, source-repository isolation, new-file and symlink patch capture, and Docker restrictions and timeout cleanup.

The Docker integration suite also runs the example verifier against the unfixed source and requires it to fail. SDK protocol tests use a local HTTP server; production never selects that implementation. No paid API calls run in CI.

Docker crash reconciliation is now tested as described below. Automatic resume remains unimplemented. Failure-repair quality, broader repository performance, and cost comparisons need their own acceptance evidence.

## Native AgentTrace export — September 5, 2026

The same live run was exported through AgentTrace 0.94.1 at pinned commit `b109ec5b3714b842746e97ee8e975329d8582667`, then loaded by its real `TraceStore` and HTML replay renderer. The projection has 13 native events from 16 source events, with the remaining lifecycle/verification information in the [harness sidecar](evidence/2026-09-05/agenttrace/harness.json). The [manifest](evidence/2026-09-05/agenttrace/manifest.json) authenticates the exported file bytes against recorded checksums; it is not a signature or proof of complete capture.

The reader reported 2,438 total tokens, matching the original 1,901 input and 537 output tokens. The report estimate remains $0.0128075, and verification passed once. Repeating the export returned `reused: true`. No new model or cloud calls were made.

The full race suite and vet passed with real AgentTrace integration enabled. Native tests cover concurrent duplicate publication, secret canaries, parent links, missing usage, corrupted exports, symlink destinations, and a child writer terminated with exit code 91 immediately before the final rename. The interrupted export did not appear as a session and a retry succeeded. A successful finish without a separate tool-result event is explicitly recorded as such.

## Crash reconciliation — September 5, 2026

A real child controller started a Docker command that wrote a file and then slept. Reconciliation rejected takeover while the controller held its process lock. The test killed that process with SIGKILL, reconciled its recorded container, verified the container was absent, preserved the changed file in a patch, and recovered the paired checkpoint. The outcome was `failed`, never verified success. Repeating reconciliation succeeded. No model request was involved.

The full race suite and vet passed with Docker and native AgentTrace enabled. Other checks reject stale worker writes, corrupt snapshots, traversal, external symlinks, special files, and oversized payloads, and recover a report after the terminal database write. These checks do not establish automatic resume, distributed ownership, every crash window, or cloud cleanup guarantees.

## AWS probe preparation — September 5, 2026

The real AWS Go SDK command client builds and its protocol-state tests pass. The ARM64 image was built and run locally: its non-root UID, `/ping`, and `/invocations` endpoints passed. AWS CloudFormation validated the bootstrap template. The full Go race suite and vet passed with the new code, real Docker, and native AgentTrace enabled.

No AWS runtime has been deployed or invoked. The bootstrap requires explicit approval because it creates persistent GitHub OIDC trust and IAM roles. Command streaming behavior in AWS, resource permissions, session isolation, remote patch verification, cancellation, teardown, actual costs, and remote AgentTrace evidence remain unvalidated. The prepared workflow records these outcomes when executed; local checks are not cloud acceptance evidence.
