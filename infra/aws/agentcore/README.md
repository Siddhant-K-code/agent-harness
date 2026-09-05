# AWS AgentCore capability probe

This experiment uses the real AWS SDK and an ARM64 container in `us-east-1`. It is deliberately a separate executable from `harness run`: the current AgentCore command API integration does not enforce the production executor's disabled network, read-only verifier mount, or inspectable session cleanup guarantees. No model client or key is present in the guest or the workflow.

The probe initializes real sessions through `InvokeAgentRuntime`, executes commands through `InvokeAgentRuntimeCommand`, and checks the original bug fails, the saved GPT-5.4 patch applies, files persist, a separate session starts clean, independent assertions pass, exit codes/stderr arrive, output is bounded, and a server timeout is reported. Disconnecting the controller is an uncertain command outcome. `StopRuntimeSession` is reported as an acknowledgement, not proof of an absent microVM. The probe deletes the entire runtime and waits until `GetAgentRuntime` returns `ResourceNotFoundException`.

## Bootstrap once

Use a locally authenticated account administrator for the bootstrap only. The probe refuses root and IAM-user credentials and requires the dedicated assumed role. Never paste keys into the repository or GitHub secrets.

```sh
aws cloudformation validate-template --template-body file://infra/aws/agentcore/bootstrap.json --profile agent-harness --region us-east-1
aws cloudformation deploy --stack-name agent-harness-bootstrap --template-file infra/aws/agentcore/bootstrap.json --capabilities CAPABILITY_NAMED_IAM --profile agent-harness --region us-east-1
```

The template creates one ECR repository, a GitHub OIDC provider, an AgentCore identity service-linked role, an execution role, and a deployment role. Inspect existing account resources first: this minimal template expects those names and the GitHub OIDC provider to be absent. Adapt it to reference preexisting resources rather than replacing resources belonging to another project.

For a different repository, read its `sub_claim_prefix` with `gh api repos/OWNER/REPO/actions/oidc/customization/sub` and pass it as `GitHubSubjectPrefix`. GitHub requires immutable IDs for newly created repositories after July 15, 2026; do not substitute the older name-only subject. The checked-in default identifies this exact repository.

Set repository variables `AGENT_HARNESS_AWS_ROLE_ARN` and `AGENT_HARNESS_AWS_RUNTIME_ROLE_ARN` from stack outputs `DeploymentRoleArn` and `RuntimeRoleArn`. These are identifiers, not secrets. The OIDC trust admits only this repository's immutable owner/repository IDs and `main` branch; the workflow runs on manual dispatch with concurrency one. It obtains short-lived credentials and builds/pushes an immutable digest to ECR. The bootstrap contains no model permissions, NAT gateway, database, cluster, or always-on compute.

```sh
gh workflow run aws-probe.yml --ref main
```

The role allows image operations only on `agent-harness-probe`, passing only the runtime execution role, and runtime operations only on `agent_harness_probe_*`. AWS's `CreateAgentRuntime` action lacks resource-level scoping, and its dependent default-endpoint creation, workload-identity creation, and tagging are authorized against `runtime/*` and `workload-identity/*` before the new IDs exist. Identity creation, dependent tagging, and deletion also require authorization on the exact `workload-identity-directory/default` parent; deletion additionally requires the probe-prefixed identity resource. These creation statements require the two experiment request tags and `us-east-1`, and dependent tagging accepts only the `Project` and `Purpose` keys on runtime resources and the automatically created workload identity. No workload-token or credential access is granted; subsequent get/invoke/stop/delete operations remain restricted to the probe name prefix; list/authentication operations also require `Resource: "*"`. The workflow controls runtime names and invocation counts. The runtime role can pull the prepared image and write its logs. AWS may expose this limited role inside the session; do not treat guest AWS credentials as secret from executed commands.

## Diagnosing a failed workflow

No Actions secrets are required for either workflow. The normal `test` workflow uses local protocol fixtures, real Docker, and native AgentTrace without a paid model call. The AWS probe needs the two repository **variables** above and obtains temporary AWS credentials through OIDC; it never reads an OpenAI API key.

If `Obtain short-lived scoped credentials` fails with `AssumeRoleWithWebIdentity`, check the exact repository subject, branch, and deployed trust policy. If that step and ECR authentication succeed but `CreateAgentRuntime` returns `AccessDeniedException`, check the action and resource named in the error against the deployed bootstrap policy. Updating the JSON in Git does not update AWS: deploy the changed stack before rerunning. Errors in command checks require inspecting the private report, not adding credentials. Cleanup steps run even when a check fails.

## Bounds and evidence

There are at most three explicitly initialized sessions, serial bounded checks, an eight-minute controller deadline, 60-second idle expiry, and a 600-second maximum microVM lifetime. Lifecycle expiry terminates an instance; subsequent invocations can start another instance for the same session ID. The probe does not use expiry as proof of cleanup. Separate cleanup has an eight-minute deadline, is repeated in the workflow's `always()` step, and retains uncertain state for manual reconciliation. No runtime capacity is provisioned until invocations occur. Cloud costs and GitHub hosted-runner minutes are real. These operation limits are not an account-wide USD hard cap or a billing measurement.

The CLI journals the intended runtime name before creation and sessions before initialization. An ambiguous create can be found by exact name using `--cleanup`; absence from a list alone is not reported as confirmed deletion. Actual command output is limited to 64 KiB per stream and drained after truncation. Automatic SDK retries are disabled for commands. The guest is configured with public outbound networking for these known fixture commands. Do not dispatch arbitrary agent commands until required capabilities are implemented and tested.

Private workflow artifacts retain the runtime identity, command checks, patch/verifier hashes, progress, and teardown result for seven days. The workflow deletes its runtime logs after confirmed teardown, or applies seven-day retention if teardown remains uncertain. It removes its ECR image; a one-day ECR lifecycle rule is a fallback. The empty bootstrap roles/repository remain for future runs. AgentTrace export for remote command evidence is still pending; the reused patch's original local model trace is already available.

If a job is killed before cleanup, download its state artifact to a private local directory and run the following with an authenticated scoped role. If the artifact was never uploaded, an account operator must list runtimes, match the exact workflow-created name, stop/delete it, and verify absence; do not delete by broad prefix alone.

```sh
bin/agentcore-probe --cleanup --state PATH_TO_STATE_JSON
```

After all experiments and runtime cleanup, remove the bootstrap with `aws cloudformation delete-stack --stack-name agent-harness-bootstrap --profile agent-harness --region us-east-1`, then wait for `stack-delete-complete`. This removes only the resources managed by this stack. Do not delete an account-level OIDC provider or service-linked role still used by another project; detach shared resources from the stack first if you have since reused them.

References: [command API](https://docs.aws.amazon.com/bedrock-agentcore/latest/devguide/runtime-execute-command.html), [lifecycle](https://docs.aws.amazon.com/bedrock-agentcore/latest/devguide/runtime-lifecycle-settings.html), [IAM actions and scopes](https://docs.aws.amazon.com/service-authorization/latest/reference/list_bedrock-agentcore.html), [identity service-linked role](https://docs.aws.amazon.com/bedrock-agentcore/latest/devguide/runtime-oauth.html), [HTTP contract](https://docs.aws.amazon.com/bedrock-agentcore/latest/devguide/runtime-http-protocol-contract.html).

OIDC reference: [GitHub immutable subject claims](https://github.blog/changelog/2026-04-23-immutable-subject-claims-for-github-actions-oidc-tokens/).
