Delegate a coding task. Review a verified patch.

This private preview adds budgeted context compaction, versioned skills, persistent observations, and an evaluated skill improvement cycle:

- Compaction preserves the original task, selected skills, recent complete tool exchanges and latest verifier feedback. Summary requests share the task's token, time and dollar budgets. The controller rejects low-yield summaries, waits for context growth, and records source/summary hashes and usage.
- Skills are imported explicitly, scoped to a repository, content-addressed, and pinned per run. Rollback restores a previous active revision.
- Learning captures terminal observations with provenance and expiry. Real OpenAI requests propose inactive skill revisions. Paired real coding runs, including a holdout, gate promotion on independent checks and measured improvement. Unknown billing, cleanup uncertainty, stale evidence, and changed task/verifier identities prevent promotion.
- `harness learn init learning-lab` creates real tasks, independent verifiers, a seed skill and an evaluation suite. `harness learn cycle` performs one bounded proposal/evaluation/promotion iteration within an explicit model budget.
- AgentTrace exports compaction usage and selected skill/compaction metadata through its native reader. Raw histories and skill instructions remain private by default.

Local BYOK login, configurable models/context, guided setup, diagnostics and checksummed macOS/Linux archives remain included. No Go compiler is needed to use a release binary. Git and a running Docker daemon are required for the learning lab. Setup does not upload keys or call a model. OpenAI is the implemented model provider; AWS remains an advanced execution option.

Download the matching archive and `checksums.txt`, verify the archive, extract it, and run `install.sh`. Read `GETTING-STARTED.md` and `COMPACTION-AND-LEARNING.md`. New tasks enable compaction and local observation capture; existing tasks retain their previous behavior until configured. Skill selection and paid learning cycles remain explicit.

Validation includes race tests, real Docker isolation/artifact checks, native AgentTrace export, and installation/setup on four native targets. Live GPT-5.4 validation covered a corrected compaction run, a real skill proposal, eight passing paired evaluations, promotion, rollback and re-promotion. Total estimated model cost was $0.3353225 including the initial failed stress run; no new AWS calls were made. The observed 5.48% evaluation cost reduction is a small-sample result, not statistical proof of general improvement. Model-generated skill advice remains fallible.

Distribution remains private and the project license is undecided. This is a draft evaluation preview. Automatic crash resume, model weight training, distributed workers and the planned Distill/ContextLab/ThinkBudget/LLMTraceFX adapters are not included. Binaries are not Apple-notarized.
