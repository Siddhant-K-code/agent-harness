# Product direction: reviewable delegated coding work

Decision, September 5, 2026: optimize the first preview for developers and maintainers delegating small, testable repository tasks. Platform teams are a later distribution path for the same run contract. Validate this audience choice with actual users.

**Product promise: Delegate a coding task. Review a verified patch.**

The initial differentiator is an opinionated completion contract: an isolated attempt with explicit limits, an operator-owned verifier, the resulting patch, and evidence of the outcome and cleanup. The benefit to validate is less effort deciding whether delegated work is ready for human review. Do not claim that a passing verifier proves arbitrary correctness.

## What supports the promise today

| User question | Current behavior |
| --- | --- |
| What changed? | Committed baseline, isolated workspace, saved patch and verifier hash. |
| Why is it marked successful? | A controller-supplied check passes against the candidate in a separate verification execution. |
| How far can the agent go? | Step, token, time, output, repair, and estimated model-cost limits. |
| Did execution stop? | Cleanup confirmation; AWS runtime deletion before accepting a candidate; explicit unresolved outcomes. |
| Can I inspect a failure? | Durable events, artifacts, native AgentTrace export, and interrupted-run reconciliation. |
| Who holds the key and code? | User-operated controller, direct OpenAI requests, isolated guest, local state; explicit manual exports. |

The [recorded AWS demo](evidence/2026-09-05/aws-harness/README.md) and crash/transfer tests demonstrate this on a small fixture. They do not establish broad task success rates, reduced review time, or lower total cost. Measure those before making stronger claims.

## Where it fits

Current primary documentation shows considerable overlap:

- [Aider](https://aider.chat/docs/config/api-keys.html) already supports user-supplied provider keys. BYOK is expected functionality.
- [OpenHands SDK](https://docs.openhands.dev/sdk/index) offers coding agents, tools, and local/cloud execution. Adding execution backends alone is not a distinctive product.
- [Inspect](https://inspect.aisi.org.uk/sandboxing.html) supplies isolated environments for model evaluation; its [scoring workflow](https://inspect.aisi.org.uk/scoring.html) covers evaluation outcomes. Sandboxing and evaluation are established building blocks.

Our positioning is a narrower workflow choice, not a claim that competitors lack these mechanisms: package delegated repository work around the patch and the evidence a reviewer needs. Start with one useful path and make it easy to repeat. A generic agent framework, a chat UI, and an infrastructure catalog would dilute that focus at this stage.

## BYOK and integrations

BYOK is part of the trust model: no harness account or credential proxy, direct provider billing, explicit key precedence, no automatic secret uploads. Current support is OpenAI only. Add a provider only when its token accounting, continuation protocol, failure states, and budget admission are implemented and tested.

AgentTrace is the native inspection layer already integrated. The evaluated skill loop now uses selected observations to propose and test procedural revisions. The broader next step is to use failed runs to identify concrete harness changes: better task context, prepared dependencies, clearer verifier feedback, or tool behavior. Turn those failures into repeatable evaluation tasks; compare a harness revision with the previous one. Traces alone do not create an improvement loop.

Distill can later prepare smaller, relevant repository context; ThinkBudget can inform budget policy if its contract adds value beyond existing admission checks. These adapters remain planned. Keep integration code behind the task/executor/evidence contracts. Do not add an MCP server with unrestricted network or credentials to the guest to make a capability list longer.

## Next milestones

1. **Private installable preview:** versioned macOS/Linux binaries, checksums, local BYOK login, bundled real demo, own-repo setup, actionable doctor, optional native trace setup. Test actual archive installation and key handling on clean native runners. No paid calls during setup or packaging CI.
2. **Review experience:** a compact run summary tying patch, verifier failures, budget consumption, and cleanup into one inspection flow. Preserve links to raw evidence and clearly distinguish verified, failed, and unknown outcomes.
3. **Small evaluation corpus:** at least ten bounded tasks across three real repositories, with independent checks. Include task failures, budget stops, and interrupted executors. Fix the model, image, task, verifier and budget when comparing harness revisions. Any new model/cloud spend needs an explicit experiment budget.
4. **User validation:** ask three consenting developers to install without walkthroughs and run their own task. Measure setup completion, time to the first reviewed patch, manual recovery steps, and reviewer effort. These are experiments, not current metrics. Contact/invitations require the owner's explicit instruction.
5. **Public release hardening:** the project uses the MIT license. Resolve public distribution policy and notarization/signing, and verify release checksums. Add Homebrew distribution after this path is stable. Broaden providers and infrastructure based on observed task requirements.

Success for the next iteration: a new user can install, understand where their key goes, complete one bounded task, and inspect a failure without the author guiding each step. A polished installer with an unclear completion contract would not be enough.
