# Historical regression verifier validation

All five independent checks failed on their pinned buggy base and passed on the corresponding upstream fix in actual local Docker containers. [Machine-readable receipt](validation.json) records the image ID, revisions, verifier hashes and logs. Preparation made **zero model requests**. This validates the tasks' checks; it is not an agent success-rate result or evidence of skill improvement.

| Case | Buggy base | Reference fix |
| --- | --- | --- |
| Invalid regex | [Failed](trace-invalid-regex.base.log) | [Passed](trace-invalid-regex.reference.log) |
| Mutable report counts | [Failed](trace-report-counts.base.log) | [Passed](trace-report-counts.reference.log) |
| Repeated cost alert | [Failed](trace-cost-alert.base.log) | [Passed](trace-cost-alert.reference.log) |
| Hook workspace | [Failed](trace-hook-workspace.base.log) | [Passed](trace-hook-workspace.reference.log) |
| Portable verifier source discovery | [Failed](llm-portable-verifier.base.log) | [Passed](llm-portable-verifier.reference.log) |

Each check ran with a read-only source checkout, a separately mounted read-only verifier, network disabled, an unprivileged UID, dropped capabilities and bounded CPU/memory/time. Reference fixes were used only to validate the external checks; the eventual model tasks start at the buggy bases with Git metadata outside their workspace.

[Reproduce preparation and run an explicitly budgeted baseline](../../../../benchmarks/real-tasks/README.md). Real model baseline, failure selection and paired skill comparison have not yet been run on this corpus.
