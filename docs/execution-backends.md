# Execution backends and the AWS experiment

Design and implementation status, September 5, 2026. Region: `us-east-1`. Docker remains the production executor. The separately implemented AgentCore probe [passed twelve real checks, native tracing, and teardown](evidence/2026-09-05/aws-isolation/README.md). Its scoped bootstrap remains deployed. The first $50 experiment allocation was authorized; the remaining allocations below are proposals.

## Backend choice

Keep the controller, model, task contracts, verification, and evidence independent of where commands execute. Evaluate **AgentCore Runtime microVMs first on AWS**, with Fargate as the fallback for batch workers or a capability mismatch. This is a hypothesis to validate with real execution.

| Backend | Place in the plan | Main concern |
| --- | --- | --- |
| Docker / Colima | Existing local development and regression baseline. | Shares the container host kernel; current workspace lacks a disk quota. |
| AWS AgentCore Runtime microVMs | First remote experiment using the credits. | Lifecycle, artifacts, cancellation, role exposure, and independent verification. |
| ECS Fargate | Alternative managed execution for prepared worker images. | Requires our own job/control protocol and artifact handoff. |
| E2B or Modal | Later comparison behind the same contract. | Provider-specific storage, lifecycle, and billing. |
| Self-managed Firecracker | Revisit if measured requirements justify it. | Linux/KVM hosts, images, networking, quotas, scheduling, patching, and cleanup become our responsibility. |

AgentCore documents [direct shell execution](https://docs.aws.amazon.com/bedrock-agentcore/latest/devguide/runtime-execute-command.html) through `InvokeAgentRuntimeCommand`, with streamed stdout/stderr and terminal status. Its microVM runtime is [available in Northern Virginia](https://docs.aws.amazon.com/bedrock-agentcore/latest/devguide/agentcore-regions.html). Commands share guest filesystems and credentials, so our Go controller and service secrets stay outside the guest. Include build tools in a pinned image and retain serial command ownership in our controller.

Fargate provides a [hardware-virtualized isolation boundary per task](https://docs.aws.amazon.com/AmazonECS/latest/developerguide/security-shared-model.html); containers within one task are not separate trust boundaries. [E2B](https://docs.e2b.dev/), [Modal](https://modal.com/docs/guide/sandboxes), and [Firecracker](https://github.com/firecracker-microvm/firecracker/blob/main/docs/getting-started.md) provide additional options. OCI images are packaging: they do not require a local Docker daemon to be the runtime. Nix/devcontainers can describe reproducible environments but do not themselves supply isolation.

## Contract changes before a new provider

Refactor `internal/sandbox` around lifecycle and durable identity, not just `Exec`:

- `Create`: pinned environment, workspace artifact, limits, and network profile in; provider/session identity and capabilities out.
- `Exec`: immutable call ID, attempt/fencing generation, deadline, and command in; bounded streaming results and completed/failed/timed-out/unknown outcome out.
- `Inspect`, `Stop`, `Destroy`: reconcile ownership and confirm cleanup independently of a disconnected stream.
- Workspace transfer and checkpoint operations: exchange content-addressed manifests rather than host paths; declare snapshot/restore support explicitly.

Capabilities include filesystem isolation, effective CPU/memory/disk limits, egress restrictions, checkpoint support, and verifier isolation. Unsupported requirements fail admission. Never silently downgrade to host shell execution.

Add durable attempts, leases, provider handles, and fencing generations to SQLite. Persist intent before dispatch. A replacement worker must fence controller writes and reconcile or terminate the previous executor before acquiring execution authority. A database lease alone cannot stop an old remote process.

A checkpoint pairs conversation state with a workspace snapshot at a completed sequence. Include task, policy, environment, verifier, and artifact hashes. Capture modified and untracked files; a Git commit alone is insufficient. Validate worker-controlled paths, symlinks, archive sizes, and expansion limits during transfer. Do not blindly retry ambiguous external effects. On disconnect, preserve unknown billing and execution outcomes.

## AgentCore experiment

The implemented probe uses a minimal private ECR image, scoped IAM roles, short-lived logs, and the AWS SDK for Go v2 command stream. Its Go controller runs on GitHub Actions through OIDC. Known fixture files are transferred in bounded command payloads; S3 artifact transfer is still planned. The checked-in infrastructure and workflow implement runtime/image/log teardown. The client now mandates a seccomp allowlist for every command. The verifier denies filesystem writes globally in a fresh session, and actual command journals export natively to AgentTrace. The following steps describe the broader backend acceptance work.

1. Build an ARM64 environment with language tools and a small runtime endpoint. Meet the [HTTP contract](https://docs.aws.amazon.com/bedrock-agentcore/latest/devguide/runtime-http-protocol-contract.html), including `/invocations` and `/ping`. Establish the session through invocation, then use the command API for execution.
2. Transfer a pinned repository snapshot. Map a unique provider session ID to our run/attempt. Give the guest no OpenAI or GitHub credential. Treat any runtime execution-role permissions as accessible to agent code; restrict artifact access to required resources.
3. Prove streaming, output bounds, network restrictions, filesystem limits, cancellation, and cleanup on a real session. Closing a stream is not proof of process termination. If per-command termination cannot be established, stop the session and restore a validated checkpoint.
4. Export the candidate to immutable artifacts. Run the controller-owned verifier in a fresh separate environment. Do not trust an agent-modifiable server as the verifier. If a required read-only profile cannot be enforced, use another verifier backend and report the environment difference.
5. Kill the controller during commands and around checkpoint publication. Reconcile and produce an honest terminal state or resume from a validated checkpoint. Export outcomes through the same AgentTrace projection.

AgentCore's [managed session storage is in preview](https://docs.aws.amazon.com/bedrock-agentcore/latest/devguide/runtime-filesystem-configurations.html). It can preserve a session workspace across stops, subject to lifecycle and version-change constraints. Use explicit immutable S3 checkpoints as the portable recovery copy. Evaluate managed storage separately as an optimization.

Acceptance: the same small task passes locally and remotely with identified environments and real evidence; timeout leaves no continuing execution; controller loss produces validated recovery or an explicit uncertain outcome; teardown removes compute and accounts for retained storage/logs. A required capability failure selects another provider rather than weakening the contract.

## Confirmed credit and allocation

The owner-provided AWS Credits screenshot shows **$500 remaining, $0 used, active, expiring October 31, 2027**. Its associated service list explicitly includes Amazon Bedrock AgentCore, ECR, S3, CloudWatch, ECS, EC2, VPC, AWS Budgets, and Bedrock. This resolves the service-eligibility question for the proposed core resources. Credit eligibility does not itself authorize provisioning.

Account-specific source: screenshot and eligible-service list supplied September 5, 2026. The credit ID and account screenshot are intentionally not copied into this repository. To check updated values: **Billing and Cost Management → Credits → select the credit**. [AWS instructions](https://docs.aws.amazon.com/awsaccountbilling/latest/aboutv2/useconsolidatedbilling-credits.html).

| Allocation | Proposed maximum |
| --- | ---: |
| Backend experiment and teardown validation (authorized) | $50 |
| Repeated task evaluation and recovery experiments | $150 |
| Storage, image registry, logs, and networking allowance | $50 |
| Uncommitted reserve | $250 |

There is no near-term expiration pressure. Direct OpenAI API charges have separate billing. The earlier GPT-5.4 $2 authorization covered the first live run, not an ongoing evaluation campaign. Hosted E2B/Modal invoices also do not automatically consume this AWS credit.

Illustrative compute arithmetic at prices checked September 5, 2026:

| Service | Assumption for 1,000 executions | Compute estimate |
| --- | --- | ---: |
| Fargate Linux x86, us-east-1 | 2 vCPU and 4 GB allocated for 600 seconds each | $16.46 |
| AgentCore microVM | 2 vCPU consumed continuously and 4 GB billable memory for 600 seconds each | $36.13 |

Fargate uses `$0.000011244/vCPU-second` and `$0.000001235/GB-second` here. [Fargate pricing](https://aws.amazon.com/fargate/pricing/). AgentCore uses `$0.0895/vCPU-hour` and `$0.00945/GB-hour`; billing uses actual CPU and memory's running peak, so this is a consumption assumption, not a selectable instance size. [AgentCore pricing](https://aws.amazon.com/bedrock/agentcore/pricing/).

These are not equal-performance benchmarks or full project budgets. They exclude extra verifier sessions, boot/image-pull time beyond the assumed interval, system usage beyond the assumption, model calls, storage, logs, network services, and data transfer. Record architecture differences and measure actual usage before extrapolating.

The validated probe allows one concurrent workflow, up to three explicitly initialized sessions, a ten-minute maximum session lifetime, a one-minute idle expiry, and an eight-minute controller deadline. Longer model-driven runs will need separately chosen idle/lifetime limits and prompt stopping on completion. Add a separate janitor, resource tags, conservative cost reservation, and total experiment admission limit. Use [lifecycle settings](https://docs.aws.amazon.com/bedrock-agentcore/latest/devguide/runtime-lifecycle-settings.html) for provider-supported bounds. Verify storage and networking limits instead of assuming a microVM bounds all spending.

AWS Budgets alerts are secondary: [billing updates can lag by hours](https://docs.aws.amazon.com/cost-management/latest/userguide/budgets-managing-costs.html), so alerts are not a real-time cap. Avoid an always-on fleet, Kubernetes control plane, or default NAT/ALB footprint. Price required private networking explicitly. Infrastructure configuration, projected charges, and teardown should be reviewable before provisioning.
