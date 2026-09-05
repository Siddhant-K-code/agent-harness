# Full AWS harness: live validation

[AWS workflow 33976042955](https://github.com/Siddhant-K-code/agent-harness/actions/runs/33976042955)
passed on September 5, 2026, at implementation revision
`e4fe9c408798368fb2e6f6aba9cf49f59f647d3f`. The complete workflow took 4m30s.
[Normal CI](https://github.com/Siddhant-K-code/agent-harness/actions/runs/33976031943)
also passed, including race checks, actual Docker execution, artifact transport,
native AgentTrace, vet and builds. No simulated model or executor served the run.

## New GPT-5.4 coding run

The normal `harness run` controller used `backend: agentcore`. GPT-5.4 inspected
the repository, edited `normalizeTags`, tested it and called `finish`. A fresh
AWS runtime ran the controller's independent read-only verifier and passed six
cases plus mutation checks. The original local Docker run was not replayed.

| Measurement | Observed value |
| --- | --- |
| Run ID | `09eb2a47130c3d81d227208453965829` |
| Model requests / tool calls | 4 / 4 |
| Input / output / total tokens | 2,558 / 521 / 3,079 |
| Estimated model cost | $0.01421, within the $0.50 run limit |
| Verification attempts | 1, passed |
| Native AgentTrace events | 17 |
| Repeated native export | Reused the existing session |
| Runtime cleanup | Confirmed |

The [report](report.json) and [new patch](changes.patch) preserve those results.
The patch SHA-256 is
`eb3e2683950c8b2877824a819fe033da7b018e67372d132e8bd44b7f6082222a`.
The evaluator SHA-256 is
`03a0a88e5cc53d6a2cfbc1223e06e0d93d72e1e1305ba87ba423325fc3fa78e7`.
Token cost is an estimate from recorded usage, not an invoice. AWS billed cost
remains unknown and is represented as null.

## Real transfer and failure acceptance

Before the model run, two fresh runtimes transferred the earlier real GPT-5.4
candidate plus an untracked file and independently verified the received
artifact. Five socket family/type combinations were denied, and the verifier
rejected 14 filesystem mutation attempts. See [artifact acceptance](artifact-acceptance.json).

A third runtime exercised interruption. The test refused takeover while its
controller held the OS lock, killed that controller after the actual command
stream started, and removed the saved create acknowledgement to inject another
failure boundary. Reconciliation recovered the **same runtime** using the
original create token, deleted it, and preserved the previous local workspace.
It did not replay the command, invent a command result, accept unobserved edits,
or mark the interrupted run successful. Its intentionally failed run state is
the expected [crash-acceptance result](crash-acceptance.json).

The workflow's [independent get-runtime audit](cleanup-audit.json) confirmed all
seven runtimes absent. A subsequent [local read-only audit](independent-cleanup-audit.json)
found zero experiment runtimes, images, log groups or workload identities. The
scoped bootstrap roles, OIDC provider and empty repository remain for reuse.

## Native trace and publication

[Open the unmodified native AgentTrace replay](replay.html).
[Trace validation](trace-validation.json) records the native reader's counts and
duplicate-export result. The downloaded session was checked again locally with
AgentTrace's actual reader: all file checksums and parent-event links matched.

[The original native manifest](native-manifest.json) identifies the complete
private session files. Those files and the raw controller database/snapshots are
retained in the private workflow artifact and downloaded workspace evidence;
the manifest here does not imply that all private source files are published in
this directory. JSON summaries omit host paths and replace the cloud account ID
with `ACCOUNT`. The patch, replay and native manifest are unmodified. The selected
publication was checked for account identifiers and common credential patterns.

This validates the complete integration on one small coding task. The current
AWS backend has an 8 MiB archive limit, runtime startup/deletion latency per
command, and no disk quota or automatic model resume. See [usage and lifecycle](../../../aws-harness.md).
