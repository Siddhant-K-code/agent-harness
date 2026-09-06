# Compaction, skills, and evaluated learning

The CLI implements context compaction, explicit versioned skills, persistent observations, and a bounded skill improvement cycle. These features use the real OpenAI client and the existing Docker or AgentCore runner. There is no simulated production model or executor.

## Try the complete path

With Git, Docker, `node:22-alpine`, and your configured OpenAI key:

```sh
harness learn init learning-lab
cd learning-lab
harness skills import --id repair --repo repo --file SKILL.md
harness run --task words.task.json --skill repair
harness learn list
harness learn cycle --repo repo --skill repair --suite suite.json --max-usd 1.30
```

Setup is unpaid. The source task reserves $0.15; the cycle reserves $0.10 for its proposal and $1.20 for eight paired coding runs. Actual estimates are reported separately. These are model budgets; infrastructure costs are separate. A rejected candidate is a valid outcome, and remains available for inspection. This small lab demonstrates the mechanism, not general coding performance or statistically established improvement.

`cycle` performs one iteration and stops. Run it again after collecting new observations. It does not schedule itself, fine-tune a model, or edit its own controller, permission policy, or verifier.

## Compaction

New `harness init` tasks enable compaction and local observation capture. Existing tasks retain their previous behavior until explicitly configured:

```sh
harness config set --compaction --compact-at-percent 75 \
  --keep-recent-turns 1 --max-summary-tokens 1024 --max-compactions 4 --learn
```

The policy lives in task JSON:

```json
{
  "compaction": {
    "trigger_percent": 75,
    "keep_recent_turns": 1,
    "max_summary_tokens": 1024,
    "max_compactions": 4
  },
  "learn": true,
  "skills": [{"id": "repair"}]
}
```

Compaction runs between complete tool exchanges when measured input crosses the configured percentage of input capacity (context window minus reserved coding output). The task, selected skill instructions, latest independent verifier feedback, and the configured number of recent exchanges stay intact. Older visible exchanges and the previous summary become input to a bounded, tool-free summarization request using the task's model. Old opaque reasoning is not interpreted or summarized; recent opaque protocol items remain intact. This is lossy working memory, not lossless provider-native compaction.

Every summary request counts against the same token, time, and dollar limits as coding. The controller counts its input and reserves its output before dispatch. A summary is accepted only if it reduces measured input by at least 10% and the next coding request fits the context window. Rejected low-yield summaries keep the original history and may continue if it still fits. A growth threshold avoids repeatedly summarizing nearly identical history. Failure, insufficient budget, or an oversized pinned/recent context stops the run and preserves evidence. There is no silent truncation or automatic retry. At least one recent exchange must be retained (`--keep-recent-turns 1..10`); incomplete exchanges are never compacted. The compaction cap counts all paid attempts, including rejected summaries.

The standalone provider compaction mechanism has a different protocol; see [OpenAI's compaction documentation](https://developers.openai.com/api/docs/guides/compaction). This implementation deliberately uses ordinary Responses requests with an explicit `max_output_tokens` reservation for the summary.

Private artifacts include `runs/RUN_ID/compactions/NNN.json` (source history, summary, hashes, before/after counts), the updated checkpoint, and `report.json.compactions`. Journal model events include summary usage; `context.compacted` describes acceptance. AgentTrace exports those model calls and allowlisted compaction metadata, without exporting summary or skill text by default. Reconciliation includes summary usage and accepted compaction counts; automatic resume remains unimplemented.

## Versioned skills

Import local Markdown, including a `SKILL.md`, with explicit identity, description, and repository scope:

```sh
harness skills import --repo /path/to/repo --id repair \
  --description 'Debug parser failures' --file ./SKILL.md
harness config set --skill repair
harness skills list
harness skills show VERSION_SHA256
```

The Markdown is stored verbatim; frontmatter is not an executable configuration format. Instructions are limited to 32,000 bytes per skill and 64,000 bytes across a task's selected skills. Imports are active by default, or use `--activate=false` then `skills activate VERSION`. Active does not mean automatically loaded: tasks must select a skill explicitly. Nothing scans arbitrary repository instructions or executes skill scripts on the host.

Versions are content-addressed, validated on read, and scoped to the canonical local repository path. This scope prevents cross-repository reuse by accident; moving the checkout requires an explicit import for the new path. `--skill repair@VERSION` selects an exact revision; otherwise the active revision is resolved once before the run and pinned in the saved task, report, `skills.lock.json`, and `skill.loaded` journal records. An active revision changing later cannot change an in-flight run. Skills are advice subordinate to the task and controller policy.

`skills rollback --repo /path/to/repo --id repair` restores the previous active, unexpired revision. Promotion uses a registry lock and compares the current revision with the evaluated parent. Imported revisions can be activated by the owner; learned revisions must pass the evaluation path.

## Observations and proposals

`--learn` captures terminal run metadata and bounded failure/verifier feedback locally, with run, task, journal and verifier identities. `learn capture RUN_ID` captures an older terminal run. Capture is idempotent and does not call a model. A failed observation write is exposed as `learning_error` in the report without changing the coding outcome.

Observations expire after 30 days. They remain on disk for inspection but expired observations cannot train a new candidate. They are evidence, not automatically inserted into future task prompts. Their source journals are rechecked before proposal generation. Observations and raw proposal receipts may contain private code/output; they are not automatically safe to publish.

```sh
harness learn propose --repo /path/to/repo --skill repair \
  --observation OBSERVATION_SHA256 --max-usd 0.10
harness skills show CANDIDATE_VERSION
```

Without `--observation`, propose/cycle select up to five recent unexpired observations in that repository. Proposals use the source task model unless overridden with `--model`. Proposal context defaults to 32,768 tokens, output to 2,048, and total tokens to 50,000; the model/context/budget flags can override these. A write-ahead receipt is saved before the paid request. Interrupted requests retain `billing_unknown`; no automatic retry occurs.

A candidate has its parent revision, source observation hashes, and a 30-day expiry. It is inactive until promotion. Its instructions are model output and can be wrong; a successful source run alone does not establish that the proposed skill is helpful.

## Paired evaluation and promotion

Suite task paths are relative to the suite file. The lab provides an example:

```json
{
  "schema": 1,
  "name": "Parser repair skill",
  "repetitions": 2,
  "cases": [
    {"id": "empty-input", "task": "empty.task.json", "role": "regression"},
    {"id": "quoted-input", "task": "quoted.task.json", "role": "holdout"}
  ]
}
```

Use 2–20 distinct tasks and 2–5 repetitions, with at least one holdout whose goal was not used in the candidate's source observations. All tasks must belong to the skill's repository. Task contracts control models, contexts, compaction and per-run budgets. GPT-5.4/mini aliases are resolved to the catalog's pinned snapshots. Other selected skills, Git commits, and verifier copies are frozen. Each case's first resolved image is pinned for subsequent arms. Baseline and candidate start with fresh workspaces, and execution order alternates across trials.

```sh
harness learn evaluate --suite suite.json --max-usd 1.20 CANDIDATE_VERSION
harness learn promote EVALUATION_SHA256
```

The suite reserves the sum of every run's maximum model budget before execution. It records each pending dispatch and completed result. Unknown billing, cleanup uncertainty, cancellation, or setup failure stops evaluation without promotion. Both failures and successes count toward cost. Interrupted evaluations remain inspectable but cannot be resumed or promoted.

The fixed gate requires complete paired evidence, no per-case success regression, every candidate holdout trial passing, and no more than 10% higher total estimated model cost. It also requires strictly more successes, fewer repair attempts, or at least a 5% lower model estimate. Identical results do not qualify. This is a small-sample operational gate, not statistical proof, an adversarial verifier audit, or a general quality guarantee.

Promotion rechecks report/journal hashes, actual recorded task identities, candidate provenance, freshness (seven days), and the currently active parent. Receipts are local owner-controlled records, not signed attestations against a malicious host owner. A promotion affects future unpinned selections only. Rollback remains available.

Distill, ContextLab, ThinkBudget, LLMTraceFX adapters, distributed workers, and model weight training remain outside this implementation.
