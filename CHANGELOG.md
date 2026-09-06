# Changelog

## v0.1.0-rc.6 — September 6, 2026

[Published prerelease](https://github.com/Siddhant-K-code/agent-harness/releases/tag/v0.1.0-rc.6) with project-signed native archives for macOS/Linux, amd64/arm64. [Install and start the app](docs/getting-started.md#complete-local-setup).

- Added an MIT license and included it with dependency notices in new package builds.
- Added a pinned installer that verifies a project-signed RSA-3072/SHA-256 checksum manifest, archive contents and source metadata before installation. Optional setup prepares the demo, Docker image, native AgentTrace, local BYOK and embedded web app.
- Added a release workflow that signs tested macOS/Linux amd64/arm64 archives, downloads and verifies uploaded assets, and publishes only after those checks succeed.
- Tested signature tampering, wrong keys/versions, archive path rejection, native installation and upgrades on all four targets. Removed macOS metadata files from new archives.
- Kept package checks queued separately from manual release builds, so a new run does not cancel an earlier check.
- Provided direct platform download links and a complete pinned binary setup command; separated existing-project restart commands from first-time setup.

Project archive signatures do not provide Apple Developer ID signing or notarization. Source installation and ordinary CI artifacts are not signed releases.

## v0.1.0-rc.5 — draft

Added persistent project chat to the local web app: read-only questions over a pinned Git commit, numbered source reads, model/context and spending controls, cancellation, and an editable handoff to a coding task. Duplicate submissions are rejected; interrupted requests are not silently replayed. Added README visuals and a recorded real GPT-5.4 question/coding demo with verification, cleanup and cost evidence.

## v0.1.0-rc.4 — draft

Added the embedded run dashboard, typed repository tools, a versioned system prompt/tool contract, scoped Streamable HTTP MCP, host-authenticated GitHub reads/private clones and corresponding AgentTrace evidence.

## v0.1.0-rc.3 — draft

Added budgeted context compaction, repository-scoped versioned skills, persistent observations, real candidate proposals, paired evaluations with holdouts, promotion gates and rollback. General self-improvement is not established by the small recorded evaluation.

## v0.1.0-rc.2 — draft

Added configurable models, context/output/total-token budgets, conservative long-context price admission, validated task updates and model catalog commands.

## v0.1.0-rc.1 — draft

Added native macOS/Linux packaging, local BYOK login, bundled task setup, diagnostics and optional native AgentTrace installation for the real coding runtime.

rc.1–rc.5 are historical drafts with checksums, not project signatures. Their archived files predate the current package license/notices changes; consult the current [MIT license](LICENSE) and the exact source revision recorded in each archive. Drafts need repository access and sufficient GitHub permissions.
