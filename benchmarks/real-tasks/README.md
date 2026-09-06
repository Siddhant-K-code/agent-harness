# Five real regression tasks

This corpus reproduces five historical bugs from [AgentTrace](https://github.com/Siddhant-K-code/agent-trace) and [LLMTraceFX](https://github.com/Siddhant-K-code/LLMTraceFX). The [manifest](manifest.json) pins each buggy base and upstream reference fix. Checks are written independently and stored outside the agent's writable checkout. Two AgentTrace cases share one historical fix, so these are five behavior checks across two repositories, not five independent project-level samples or a general benchmark score.

| Case | Required behavior |
| --- | --- |
| `trace-invalid-regex` | Invalid patterns return a failed scorer result; valid matching stays intact. |
| `trace-report-counts` | Report counts follow mutations to scorer results. |
| `trace-cost-alert` | A cost limit alerts once per watch state, across whole-dollar increases. |
| `trace-hook-workspace` | Hook payload workspace selection, explicit override and invocation isolation. Reserved as a skill-evaluation holdout. |
| `llm-portable-verifier` | Generated offline verifier finds matching parent source and retains digest checks. |

## Prepare and validate without model charges

From this repository, with Python 3.12+, Git and a running Docker daemon:

```sh
docker pull python:3.12-slim
python3.12 scripts/prepare-real-tasks.py --output .harness/real-tasks
```

The script clones the public repositories, pins the local image ID, generates task/verifier files, and runs each independent check on both revisions in a read-only, network-disabled container. Preparation succeeds only if every buggy base fails and every upstream fix passes. It emits `prepared.json`, ten check logs and `agent-trace.suite.json`. These are verifier validation results, **not agent performance results**. Reusing an existing output is refused; select a new directory. `--sources PATH` can reuse existing clones after origin checks. Select a different prepared Python 3.12 image with `--image`; Docker must share the output directory with its VM on macOS.

## Run a budgeted baseline

Build the development CLI with `go build -o bin/harness ./cmd/harness`, or set `--harness` to your installed executable. Configure your own key with `harness auth login`. Run only when ready for real API charges:

```sh
python3.12 scripts/run-real-tasks.py \
  --prepared .harness/real-tasks --output .harness/baseline-1 \
  --harness ./bin/harness --max-usd 0.75
```

Defaults use GPT-5.4, a 32,768-token context, 2,048 reserved output tokens and $0.15 per case. The script reserves the sum of all five caps before dispatch, snapshots the tasks/verifiers, and writes `baseline.json` before each run. Every attempt, failure, independent-verifier outcome, run ID, observation and uncached cost estimate is retained. A timeout, missing report, unknown billing or unconfirmed cleanup stops the batch. Existing journals are never replayed; inspect the recorded state with the CLI and reconcile interrupted runs before another experiment. `--api-key-file PATH` supports an existing owner-only key without reading it in the script.

## Test one improvement from a real failure

Use only a failed **non-holdout AgentTrace** observation from `baseline.json`. If all eligible tasks pass, there is no observed failure here to justify an improvement claim: collect a new real regression instead. Never feed the holdout observation, goal, solution, or reference fix into proposal generation. The model receives only its historical task checkout; Git metadata and the corpus reference fixes are outside that workspace.

1. Inspect the run's events, failed verifier output and native AgentTrace projection. Export with `harness trace --state-dir .harness/baseline-1/state RUN_ID` after [native AgentTrace setup](../../docs/getting-started.md#optional-native-replay).
2. Import a small repository-scoped parent skill with `harness skills import --state-dir .harness/baseline-1/state --repo ABSOLUTE_AGENT_TRACE_CLONE --id repair --file SKILL.md`. Describe a repair procedure; do not embed solutions or weaken the verifier.
3. Propose from an explicit regression observation: `harness learn propose --state-dir .harness/baseline-1/state --repo ABSOLUTE_AGENT_TRACE_CLONE --skill repair --observation OBSERVATION_HASH --max-usd 0.10`. This is a separate paid request.
4. Compare the returned candidate using `harness learn evaluate --state-dir .harness/baseline-1/state --suite .harness/real-tasks/agent-trace.suite.json --max-usd 2.40 CANDIDATE_VERSION`. The four AgentTrace cases run twice per arm with fresh workspaces: 16 real coding runs, including the reserved holdout. Base revisions, checks and runtime are pinned; only the selected skill differs.
5. Inspect all trials and the eligibility decision. Promote only with `harness learn promote --state-dir .harness/baseline-1/state EVALUATION_HASH` if it passes the existing gates. Rejecting a candidate is a valid outcome. Do not claim a reliable success-rate improvement from this small corpus.

The default complete experiment reserves **$3.25**: $0.75 baseline, $0.10 proposal and $2.40 comparison. Preparation makes no model requests. These are model cost reservations, not invoice reconciliation or cloud infrastructure budgets. Preparation and baseline scripts never launch the proposal/comparison automatically. See [compaction, observations and evaluation gates](../../docs/compaction-and-learning.md).
