# Live compaction and evaluated learning — September 6, 2026

The [sanitized measurements](summary.json) come from actual OpenAI requests and Docker execution. The bundled learning lab uses three distinct buggy JavaScript functions with controller-owned verifiers. These small fixtures validate the mechanism, not general coding quality or statistical significance.

- An intentionally aggressive 4,096-token stress configuration initially allowed zero recent exchanges. It repeatedly lost useful read context and stopped at the compaction limit. That failed run cost $0.0489975 and remains included in the total.
- The corrected policy requires at least one recent exchange, useful token reduction, and context growth before another attempt. The live source task then passed: one accepted summary, two attempts, $0.0394725 estimated model cost, and confirmed container cleanup.
- The real proposal used the two source observations to suggest portable shell commands. It retained evidence and parent version identities, and stayed inactive until evaluation. Skill text remains fallible advice; the evaluation does not verify all historical or causal claims a model might make.
- Evaluation ran baseline and candidate twice on each of two other tasks, with the clamp task held out of proposal inputs. All eight runs passed. Both arms used `gpt-5.4-2026-03-05`, identical commits, images, verifiers, budgets and other settings within each case.
- Baseline estimated cost was $0.1240375; candidate cost was $0.117235 (5.48% lower in this small sample). This met the fixed 5% cost gate with no verification regressions. Promotion succeeded; rollback to the original version and re-promotion from the same revalidated evidence also succeeded.
- The corrected source run exported through the pinned native AgentTrace reader with 25 native events, including summary requests and usage. Summary text and skill instructions stay out of the default metadata projection.

Total estimated model cost: **$0.3353225**, including the failed stress run and proposal. No new AWS calls were made. Estimates use configured uncached rates, not invoice reconciliation.

Raw journals, source histories, full reports, skill versions, proposal receipts and evaluation receipts remain in the private local learning lab. The summary omits local paths and raw provider payloads. Artifacts were collected during implementation; no broad performance claim is made from this sample.
