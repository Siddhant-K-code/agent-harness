# Local UI, MCP, GitHub and native-tool validation

A real GPT-5.4 run launched from the embedded dashboard completed on September 6, 2026. It read the private repository's PR list through the host GitHub broker, searched and fetched documentation through OpenAI's public MCP server, used native file tools to fix the bundled tag-normalization task, ran a check, and passed the independent verifier on its first attempt.

Estimated model cost: **$0.125285**, within the **$0.30** per-run ceiling. The run used 45,992 input and 687 output tokens across eight model turns, a 32,768-token context window and 1,024-token output reservation. Cleanup was confirmed. No AWS resources or new credentials were created. A separate authenticated clone of the private harness repository succeeded with a plain HTTPS remote and host-side `gh` credential helper.

The [sanitized receipt](summary.json) records the actual tool sequence, prompt/catalog identities, base/image/verifier hashes, outcome and usage. Native AgentTrace accepted 33 events from the terminal journal. Raw model requests, MCP/GitHub content, local paths, the dashboard access token and credentials are not included here.

Validation also covered:

- `go test -race ./...` with real Docker isolation, AgentCore artifact-service checks, crash reconciliation and the pinned native AgentTrace reader enabled; `go vet ./...` passed.
- Official MCP SDK client/server HTTP tests for selected-tool/schema enforcement, token redaction, policy revocation, redirects and a timed-out call with one dispatch.
- Native shell operations preserving literal file content and rejecting traversal/symlink components; recovery retaining unknown MCP outcomes and prompt identity.
- Dashboard authentication, origin/host checks, spending ceilings, cancellation, artifact containment, key-file resolution and omission of raw provider responses.
- The installed macOS ARM64 archive served the embedded UI and passed installation/upgrade, corruption rejection, hidden-input key privacy, model/config, skills and learning-lab smoke checks without model requests.

This is an integration smoke result, not a comparative prompt or harness quality benchmark. MCP results were live and the model was an alias. Broader controlled evaluations remain necessary. The UI's run cost total excludes separate learning proposal requests and infrastructure.
