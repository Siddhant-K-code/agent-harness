Delegate a coding task. Review a verified patch.

This private preview adds a local dashboard and connected tools:

- `harness serve --task harness.task.json` opens an authenticated local UI for real runs, patches, verifier results, model costs, events, skill versions and prompts. It launches prepared tasks with model/context controls and enforces the task's spending ceiling. UI assets are embedded; no Node installation is required.
- The versioned `coding-v2` prompt and tool manifest are saved per run. Typed list/read/search/write tools use the existing isolated executor, alongside exec and independent verification.
- Streamable HTTP MCP connections use the official Go SDK, explicit operator/server/tool selection, pinned schemas, bounded calls and policy rechecks. Credentials stay in host-side configuration/environment. Unknown external outcomes stop without automatic replay and survive reconciliation.
- GitHub status, private cloning, and read-only issues/PR/diff/check tools use host gh credentials. GitHub writes, MCP OAuth and stdio launch are not included.
- Native AgentTrace exports preserve the new tool names and external failure/uncertainty metadata.

Budgeted compaction, repository-scoped versioned skills, persistent observations and evaluated skill improvement remain included. Learned revisions require repeated paired evaluations with holdout success, no case regressions, bounded cost and measured improvement before promotion. Live external integrations are excluded from these comparisons until their inputs can be frozen. Local BYOK, model/context configuration, guided setup and checksummed macOS/Linux archives are included.

A real GPT-5.4 run launched through the UI read GitHub, searched/fetched public OpenAI docs via MCP, edited the fixture using typed tools and passed its independent verifier. Estimated model cost: $0.125285 within $0.30, with cleanup confirmed and 33 native AgentTrace events exported. Authenticated private cloning also passed. Race tests, real Docker checks and the installed native package/UI smoke checks passed locally. See the attached workflow results for platform build status. One smoke task is not a quality benchmark.

Download the matching archive and checksums.txt, verify/extract it, and run install.sh. Read GETTING-STARTED.md, UI-AND-INTEGRATIONS.md and COMPACTION-AND-LEARNING.md. Git and a running Docker daemon are required for the demo; GitHub integration additionally requires gh. Setup does not call a model or upload a key. OpenAI remains the implemented model provider; AWS AgentCore is an advanced execution option.

Distribution remains private and the license is undecided. The dashboard is local and single-user. Reviewed GitHub writes/draft PRs, UI-managed setup, MCP OAuth/stdio, automatic crash resume, distributed workers, disk quotas, broader evaluations and Distill/ContextLab/ThinkBudget/LLMTraceFX adapters remain pending. Binaries are not Apple-notarized.
