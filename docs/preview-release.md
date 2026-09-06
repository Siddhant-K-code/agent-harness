Delegate a coding task. Review a verified patch.

This private preview adds configurable models and context windows to the packaged CLI:

- `harness models` lists configured model IDs, snapshots and capacity limits.
- `harness config show` displays or previews effective settings. `harness config set` saves validated changes to the task JSON without changing relative repository/verifier paths.
- `init`, `run`, and `doctor` accept `--model`, `--context-window`, `--max-output-tokens`, `--max-total-tokens`, and `--max-usd`. Run/doctor overrides are temporary.
- Request admission reserves output inside the selected context window and checks the separate cumulative token/dollar budgets. Context overflow stops before generation; there is no silent truncation or automatic compaction.
- GPT-5.4 configurations capable of entering the long-context pricing tier use conservative higher estimates from the first request. Reports and recovery retain the effective configuration and pricing basis.

The preview also includes local BYOK login with hidden input, a bundled real demo, diagnostics, optional native AgentTrace setup, and checksummed macOS/Linux archives for amd64/arm64. No Go compiler is needed to use a release binary. Git and a running Docker daemon are required for the local demo. Setup does not upload keys or call a model.

Download the matching archive and `checksums.txt`, verify the archive, extract it, and run its `install.sh`. See `GETTING-STARTED.md` for the full instructions. Existing tasks default to a 200,000-token combined context window; output now counts toward this limit. Newly initialized tasks retain the $0.50 estimated model budget.

Validation covers context/output boundaries, model capacities, long-context budget admission, recovery pricing, exact provider model IDs, native trace projection, and archive installation/configuration on all four native targets. No new paid model/AWS run or large-context coding benchmark is claimed.

Distribution remains private and the project license is undecided. This draft is for evaluation, not a public release. Binaries are not Apple-notarized. OpenAI is the only implemented provider; Docker is the simple default, while AWS requires the documented advanced setup.
