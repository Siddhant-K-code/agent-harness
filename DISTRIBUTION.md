# Distribution status

agent-harness is licensed under the [MIT License](LICENSE), copyright 2026 Siddhant Khare. Preview downloads currently require repository/draft-release access.

Preview archives contain the CLI, the project `LICENSE`, an installer, setup documentation, build metadata, checksums, and upstream dependency notices. They exclude API keys, state directories, trace exports, and private cloud artifacts. The bundled demo contains its own synthetic repository fixture and real verifier; execution uses a real model and executor.

`THIRD-PARTY-NOTICES.txt` is generated from linked Go modules when building an archive. Upstream components retain their own licenses and notices. Native AgentTrace is an optional separately installed dependency, pinned to its source revision.

Maintainers: use the **Package preview** workflow to build and test all four native targets. A manual run may create a draft prerelease after its checks pass. Inspect the draft before publishing. No public release, repository visibility change, package-registry upload, or paid model/cloud run occurs in this workflow.
