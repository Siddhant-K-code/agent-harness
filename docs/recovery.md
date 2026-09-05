# Controller recovery

New Docker and AWS runs acquire an exclusive OS file lock for the lifetime of the controller and persist a random worker identity with a renewable 30-second lease. Owned journal writes require that identity and a valid lease. This is a local single-controller guarantee, not a distributed lock; keep state and locks on a local filesystem. Legacy running records lack the required ownership evidence and are refused.

Before each dispatch the controller commits an execution reference. Docker names the actual container with that reference. Normal execution cleans it with a separate deadline; reconciliation can inspect and stop it after a controller crash. Cleanup errors remain visible.

```sh
bin/harness reconcile RUN_ID
```

The command obtains the process lock, refuses a live owner, inspects each recorded execution without confirmed cleanup, stops observable containers, confirms their absence, and journals cleanup acknowledgements. If a container is already missing without a durable acknowledgement, dispatch remains uncertain: an orphaned client or in-flight Docker request may still create it. Reconciliation fails explicitly and requires operator inspection; it does not convert that absence into cleanup success. It then preserves the candidate patch, validates the checkpoint, and fences the old worker identity. An interrupted run becomes failed or cancelled. A previously committed terminal outcome is retained, and a missing final report is reconstructed from committed evidence. Unknown model usage stays unknown. No command or model request is replayed. A cleanup or artifact error leaves reconciliation incomplete. Confirmed cleanup acknowledgements make later retries safe; a crash between removal and its acknowledgement remains uncertain and needs inspection.

At initial setup and after acknowledged tool results, the controller atomically publishes `checkpoint.json` containing the provider conversation (including opaque continuation items), task hash, journal sequence, report, and workspace archive identity. Snapshots include untracked files and internal relative symlinks. Each snapshot is limited to 64 MiB of content and 10,000 entries; special files and external symlinks are rejected. Archives are hashed and fsynced before checkpoint publication. The restore primitive accepts a new destination only and rejects traversal or checksum mismatch. Checkpoints are private, ignored by Git, and may contain sensitive source and model content.

Automatic resume is not implemented: the workspace at an interruption may have effects newer than the last checkpoint, and an unanswered API request may still be billable. Snapshot limits are per artifact, not a total workspace or state-directory disk quota. Old snapshots are not yet garbage-collected. The journal and checkpoint are not one transaction, so a crash may leave an unused complete archive or a checkpoint with no subsequent `checkpoint.saved` event; their recorded source sequence and task identity are validated independently.

The real Docker acceptance test kills a controller during a mutating command, rejects concurrent takeover, preserves partial work, verifies container removal, and repeats reconciliation. Additional fault injection before dispatch, during verifier execution, and across every checkpoint publication step is still needed before automatic resume or remote worker ownership.

## AWS runtime reconciliation

The [AgentCore backend](aws-harness.md) persists complete create parameters and an idempotency token before dispatch. Each command owns a separate runtime. Reconciliation uses that same token if the create acknowledgement was lost, then deletes the whole runtime and confirms absence. It never repeats the command. A missing remote execution record is a pre-dispatch failure because no AWS request can precede its durable publication.

Candidate archives are validated and published only after runtime deletion. The controller retains the previous directory and atomically switches a workspace pointer; a fresh verifier receives the accepted artifact. On a worker crash, reconciliation keeps that accepted workspace and reports the interrupted outcome. The OS lock still limits ownership to one local controller; this does not implement distributed takeover or automatic model resume.

The [real AWS acceptance](evidence/2026-09-05/aws-harness/README.md) killed a worker after command-start observation, injected a lost create acknowledgement, recovered the same runtime, and confirmed deletion without replay or acceptance of unknown edits.
