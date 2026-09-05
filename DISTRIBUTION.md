# Distribution status

agent-harness is currently a private evaluation preview. The repository has not yet selected an open-source license. Packaging a binary does not change that status or grant a new redistribution license. A public release requires an explicit license and visibility decision from the repository owner.

Preview archives contain the CLI, an installer, setup documentation, build metadata, checksums, and upstream dependency notices. They exclude API keys, state directories, trace exports, and private cloud artifacts. The bundled demo contains its own synthetic repository fixture and real verifier; execution uses a real model and executor.

`THIRD-PARTY-NOTICES.txt` is generated from linked Go modules when building an archive. Those notices apply to the upstream components and do not license agent-harness itself. Native AgentTrace is an optional separately installed dependency, pinned to its source revision.

Maintainers: use the **Package preview** workflow to build and test all four native targets. A manual run may create a draft prerelease after its checks pass. Inspect the draft before publishing. No public release, repository visibility change, package-registry upload, or paid model/cloud run occurs in this workflow.
