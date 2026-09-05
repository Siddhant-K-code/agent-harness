# Real isolated AWS commands and native AgentTrace

[Workflow 33974407163](https://github.com/Siddhant-K-code/agent-harness/actions/runs/33974407163) passed on `f1989f5008fa07ed6fabf133e795897dfd81b840` in `us-east-1`. It reused the [actual GPT-5.4 patch](../changes.patch), initialized three real AgentCore sessions, and made **zero new model calls**.

| Result | Observed evidence |
| --- | --- |
| Live checks | 12 passed, including original-bug rejection, patch application, persistence, exit/stderr, truncation, timeout, and disconnect |
| Command networking | Five socket family/type combinations denied, io_uring denied, child inherited restrictions |
| Verifier writes | 14 mutation attempts denied; candidate bytes/mode unchanged; scratch also read-only |
| Actual candidate | Six independent cases plus mutation checks passed in the fresh, restricted verifier session |
| Native AgentTrace | 25 events from 31 controller records; 12 command requests, 11 terminal results |
| Controller disconnect | Remote start observed; completion unknown; no fabricated native result |
| Duplicate export | Existing session reused; native reader and HTML/text replay passed |
| Teardown | Exact runtime returned ResourceNotFoundException; no probe runtime, image, logs, or workload identity remained |

Read the [sanitized report](report.json), [independent cleanup audit](cleanup-audit.json), and [native manifest](agenttrace/c9a78c542e28448636c0dca02d88d1cf/manifest.json). The [HTML replay](replay.html) is the unmodified native AgentTrace output; open the downloaded file in a browser. Native files live under `agenttrace/c9a78c542e28448636c0dca02d88d1cf/` and can be read directly with AgentTrace's `TraceStore` rooted at `agenttrace`.

The native export is metadata-only and retains its original checksums. Publication omits the private raw command journal, replaces the AWS account ID in the report's image URI, and replaces repeated 64 KiB stdout with its original retained length and hash. The raw workflow artifact remains private with seven-day retention. The report's patch and verifier hashes match the existing source artifacts.

The [first isolation attempt](landlock-refusal.json) refused the payload because AWS lacked the required Landlock ABI. The final implementation uses a mandatory syscall allowlist and a fully read-only verifier filesystem instead. Its real container tests on ARM64 and x86 include unrestricted positive controls, inherited socket closure, environment clearing, and the actual patch/verifier.

The controller ran from 15:19:56 to 15:25:25 UTC, including asynchronous runtime deletion. Elapsed time is not CPU/memory usage or a billing measurement; AWS billed cost remains unknown. The scoped IAM/OIDC bootstrap and empty ECR repository remain for reuse.

The trusted HTTP service retains platform networking; the restrictions apply to command processes and descendants. These checks do not prove arbitrary workload compatibility, immunity to evaluator monkeypatching, complete remote recovery, or individual session absence. Durable artifact handoff, controller ownership/reconciliation, and disk quotas remain pending, so the production model runner stays on Docker.
