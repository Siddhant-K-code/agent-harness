# Project chat acceptance

The September 6 smoke check used real GPT-5.4 calls and a real Docker coding executor through the embedded browser UI. No fake model or executor participated.

1. Ask about `tags.js` in the bundled committed tag-normalization project.
2. Read the cited source in the conversation.
3. Choose **Use answer as a task**, edit the goal, and retain GPT-5.4 with a $0.20 cap.
4. Start the coding run, then open its verifier results and patch from the conversation.

The question made two Responses API generation requests and cost an estimated $0.00794. Run `fc5e38518f8d09fe0f8afc83535484f8` passed the independent verifier on its first attempt, with confirmed cleanup, for $0.0208925. Total: **$0.0288325**. The source checkout remained unchanged. Usage is priced conservatively at uncached rates; this fixture is acceptance evidence, not a quality or speed benchmark.

The [sanitized receipt](summary.json) records the pinned commit, model usage, verifier/image/prompt hashes, run result and screenshot/GIF hashes. The [README GIF](../../../../assets/readme/demo.gif) contains six actual browser captures, held for 3–5 seconds each. Waits are shortened; it is a walkthrough of observed states, not a continuous wall-clock recording. No credential, access-link token or local home path appears in the frames. A [static poster](../../../../assets/readme/demo-poster.png) is also included.

Separate live questions on the words fixture verified a follow-up referencing the prior answer and retention across a server restart. Automated tests cover exclusive chat ownership, private persistence, request deduplication, interrupted/unknown billing, cancellation, budget/context admission before generation, read-only Git access, source line ranges, auth/origin protection, removed task configuration, and cross-project handoff rejection. Raw provider responses and opaque reasoning are absent from chat files.

Q&A itself does not yet export native AgentTrace sessions. The linked coding run remains exportable through the existing trace command. The existing full suite, including Docker, AgentCore container acceptance and native AgentTrace tests, passed locally. Workflow links in the repository show the current CI/platform package results.
