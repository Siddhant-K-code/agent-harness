# Real AWS AgentCore capability evidence

[Workflow 33971620876](https://github.com/Siddhant-K-code/agent-harness/actions/runs/33971620876) passed on commit `65f0ff51c58bb2a348b4e0e58afa1fcec2ae0e4a`, in `us-east-1`, using a real ARM64 image and three AgentCore sessions. The controller ran from 14:23:34 to 14:24:07 UTC on September 5, 2026, including runtime creation and deletion. It reused the [actual GPT-5.4 patch](../changes.patch) and made **zero new model calls**.

| Check | Observed outcome |
| --- | --- |
| Non-root execution and tools | Passed; Node 22.23.2, Python 3.14.7, Git 2.54.0 |
| Original faulty implementation | Independent verifier rejected it, exit 1 |
| Apply the saved GPT-5.4 patch | Patch applied; generated tests passed |
| Workspace persistence | Marker and patched file persisted across commands |
| Fresh-session verification | Original marker absent; six cases plus mutation checks passed after patch transfer |
| Nonzero exit and stderr | Exit 7 and exact `deliberate-error` line preserved |
| Output bound | 65,536 bytes retained from oversized output; truncation reported; stream completed |
| Server deadline | `TIMED_OUT`, exit -1 |
| Controller disconnect | Started command; `context deadline exceeded`; completion remained unknown; stop acknowledged |

The [report](report.json) preserves real command results, configured limits, and immutable patch/verifier/image digests. Publication replaces the AWS account ID in the image URI and omits the repeated 64 KiB stdout payload while preserving its length and SHA-256. All other command output is retained. Raw state, report, and workflow logs remain private; GitHub's artifact retention is seven days.

The [cleanup audit](cleanup-audit.json) independently queried AWS after workflow completion. The exact runtime returned `ResourceNotFoundException`; zero probe runtimes, images, log groups, or workload identities remained. The GitHub OIDC provider, two scoped IAM roles, AgentCore identity service-linked role, and empty ECR repository remain for subsequent experiments.

The stop acknowledgement alone does not prove an individual microVM is absent. The evidence establishes whole-runtime deletion separately. It does not establish disabled egress, an enforced read-only verifier workspace, disk quotas, remote checkpoint recovery, or native remote AgentTrace export. Production model execution remains on Docker. AWS billed cost is unknown; these elapsed times are not CPU/memory usage measurements. This is one deterministic capability probe, not an evaluation of model quality.
