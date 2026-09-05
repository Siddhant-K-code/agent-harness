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

The full race suite and vet passed with Docker and native AgentTrace enabled. A real Docker lookup of an unacknowledged missing execution is reported as uncertain, not cleaned up. Other checks reject stale worker writes, corrupt snapshots, traversal, external symlinks, special files, and oversized payloads, and recover a report after the terminal database write. These checks do not establish automatic resume, distributed ownership, every crash window, or cloud cleanup guarantees.

## AWS AgentCore live probe — September 5, 2026

[Workflow 33971620876](https://github.com/Siddhant-K-code/agent-harness/actions/runs/33971620876) passed on commit `65f0ff51c58bb2a348b4e0e58afa1fcec2ae0e4a`. It created a real ARM64 AgentCore runtime in `us-east-1`, initialized three sessions, and ran nine checks through the AWS command event stream. The saved GPT-5.4 patch passed six independent cases plus mutation checks in a fresh session. No new model call was made. The [published evidence](evidence/2026-09-05/aws-agentcore/README.md) includes each observed outcome, image/patch/verifier identities, and an independent teardown audit.

The service reported `TIMED_OUT` with exit code `-1` for the two-second command deadline. A one-second controller disconnect returned `context deadline exceeded` after command start, without a terminal command outcome. The stop API acknowledged the request; whole-runtime deletion was separately confirmed with `ResourceNotFoundException`. Subsequent AWS CLI queries found no probe runtimes, ECR images, log groups, or workload identities. The empty bootstrap repository and scoped IAM/OIDC resources remain.

Seven earlier attempts failed during OIDC authentication or runtime creation. The fixes matched GitHub's immutable repository subject and added the specific endpoint, identity, parent-directory, and tagging permissions that AWS creation requires. No runtime commands ran in those failed attempts. The workflow now checks the two repository variables early; neither workflow requires Actions secrets. The regular race/Docker/native AgentTrace CI also [passed on the tested code revision](https://github.com/Siddhant-K-code/agent-harness/actions/runs/33971574923).

This validates the bounded command experiment, not a production remote backend. Public networking remains enabled; read-only verification, disk quotas, individual session-absence inspection, remote checkpoint recovery, and remote AgentTrace export are not implemented. AWS billed cost has not been reconciled. Controller elapsed time is not a billing measurement or a coding benchmark.

## Remote AgentTrace and command isolation — September 5, 2026

[Workflow 33974407163](https://github.com/Siddhant-K-code/agent-harness/actions/runs/33974407163) passed twelve real AWS checks on commit `f1989f5008fa07ed6fabf133e795897dfd81b840`. A mandatory seccomp allowlist blocked TCP, UDP, IPv6, Unix sockets, io_uring, and socket creation in a child process. The fresh verifier session denied fourteen mutation attempts, including direct writes, truncation, writable shared mappings, creation flags, symlink/proc-fd aliases, hard links, rename/unlink, metadata changes, a nested launcher, and scratch writes. The saved GPT-5.4 patch then passed six independent cases plus mutation checks under the same read-only profile.

Native AgentTrace loaded 25 events from 31 actual controller records. Twelve command requests have eleven terminal results; the deadline-interrupted command retains an unknown completion. The stop acknowledgement and whole-runtime teardown are recorded separately. Repeated export reused the same session, and native text/HTML replay succeeded. The [evidence bundle](evidence/2026-09-05/aws-isolation/README.md) publishes the unmodified checksummed native files, sanitized report, replay, and independent cleanup audit. Zero new model calls were made; AWS billed cost remains unknown.

The first isolation attempt correctly refused its payload because the AWS runtime lacked the required Landlock ABI. That [observed refusal](evidence/2026-09-05/aws-isolation/landlock-refusal.json) informed the final portable syscall allowlist. The verifier now denies filesystem writes globally, including scratch space. Both x86 CI and ARM64 container tests include positive controls, inherited-descriptor closure, environment clearing, and the real patch/verifier. The full race suite, real Docker checks, native AgentTrace tests, and vet pass.

The trusted runtime service still has platform networking. These are restrictions on commands and their descendants. A read-only filesystem does not establish immunity to evaluator tampering inside a language process or prove arbitrary candidate correctness. The backend still lacks production artifact handoff, distributed ownership, automatic remote recovery, individual execution-absence inspection, and disk quotas. It remains separate from `harness run`.
