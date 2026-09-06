Ask about your project. Turn the conversation into a verified patch.

This private preview adds project chat to the embedded local UI:

- Real OpenAI Q&A can inspect committed files through read-only list, numbered read and literal search tools. No Docker or AWS runtime is needed for questions. The project commit and prepared task's requirements stay attached to the conversation.
- Local conversation history, source reads, model/context controls, per-question spending caps, activity, cancellation and restart recovery are included. Request IDs prevent duplicate paid submissions. Interrupted requests are never replayed, and uncertain billing is labelled.
- Run task opens the coding form with the message as an editable goal. An answer can also become a task. The prepared verifier and executor remain fixed; the resulting run, status and patch are linked in the conversation.
- Existing verified coding runs, compaction, scoped skills, evaluated learning, MCP and GitHub tools, AgentTrace exports, local BYOK and checksummed macOS/Linux packaging remain included.

Download the matching archive and checksums.txt, verify/extract it, and run install.sh. Read GETTING-STARTED.md, UI-AND-INTEGRATIONS.md and COMPACTION-AND-LEARNING.md. Start with harness serve --task harness.task.json. Setup does not call a model or upload a key. Questions and retrieved context use the configured OpenAI key when submitted.

Ask mode reads a pinned commit and does not execute commands, mutate files, or invoke external integrations. Recent conversation is bounded; older questions can be omitted with a visible count. Individual text tokens are not streamed yet. Questions remain separate from coding-run/AgentTrace exports and run-spend totals. Chat attachments, UI task/verifier creation, reviewed GitHub writes, MCP OAuth/stdio, automatic crash resume, distributed workers, disk quotas and broader evaluations remain pending.

Agent-harness is licensed under MIT; each archive includes LICENSE and third-party notices. The dashboard is local and single-user. OpenAI is the implemented model provider; AWS AgentCore is an advanced coding executor. Binaries are not Apple-notarized. See workflow results for platform validation and docs/evidence for live smoke receipts; these are not quality benchmarks.
