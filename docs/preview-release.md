Delegate a coding task. Review a verified patch.

This private preview packages the real harness as a CLI for macOS and Linux on amd64 and arm64. Each archive includes the bundled task, local installer, setup documentation, binary checksum, source commit, and third-party notices. No Go compiler is required on the user's machine.

- `harness init` creates a real demo, or a task for an existing Git repository with a pinned ref and separate verifier.
- `harness auth login` takes hidden input and stores an owner-only local key. Environment BYOK and existing task-local key files remain supported. Setup never uploads a key to GitHub.
- `harness doctor` explains missing prerequisites without calling a model. `harness run` uses `harness.task.json` by default; generated tasks start with GPT-5.4 and a $0.50 estimated model budget.
- `harness trace setup` installs the optional pinned native AgentTrace dependency without a source checkout.

Download the archive for your platform and `checksums.txt`. Verify the archive, extract it, and run the included `install.sh`; see `GETTING-STARTED.md`. Then use `harness init` to begin. Git and a running Docker daemon are required for the local demo. OpenAI API charges begin only when you run the task.

Validation: the package workflow installs and exercises the actual archive on all four native targets, including credential privacy/precedence, real Git setup, duplicate-install refusal, and corruption rejection. No model or AWS calls are part of this workflow. Previously recorded real model/AWS verification evidence is linked in the repository README; packaging does not imply a new paid run or a broader benchmark result.

Distribution remains private and the project license is undecided. This is a draft for evaluation, not a public release. Binaries are not Apple-notarized. OpenAI is the only implemented provider; Docker is the simple default, while AWS needs the documented advanced setup.
