"""Clean only resources identified by this workflow's durable state and tag."""
import json
import os
from pathlib import Path
import re
import subprocess


def aws(*args):
    return json.loads(subprocess.check_output(["aws", *args, "--output", "json", "--no-cli-pager"]))

state_file = Path(".harness/aws-probe.json")
if state_file.exists():
    state = json.loads(state_file.read_text())
    runtime = state.get("runtime_id", "")
    if runtime:
        assert re.fullmatch(r"agent_harness_probe_[0-9a-f]{24}-[A-Za-z0-9]+", runtime)
        prefix = "/aws/bedrock-agentcore/runtimes/" + runtime
        for group in aws("logs", "describe-log-groups", "--log-group-name-prefix", prefix).get("logGroups", []):
            name = group["logGroupName"]
            # Restrict cleanup to the exact runtime or one of its log suffixes.
            assert name == prefix or name.startswith(prefix + "-") or name.startswith(prefix + "/")
            if state.get("runtime_deletion_confirmed"):
                subprocess.run(["aws", "logs", "delete-log-group", "--log-group-name", name], check=True)
            else:
                subprocess.run(["aws", "logs", "put-retention-policy", "--log-group-name", name,
                                "--retention-in-days", "7"], check=True)
tag = os.environ.get("PROBE_TAG", "")
if tag:
    assert re.fullmatch(r"probe-[0-9]+-[0-9]+", tag)
    result = aws("ecr", "batch-delete-image", "--repository-name", "agent-harness-probe",
                 "--image-ids", "imageTag=" + tag)
    assert not result.get("failures"), result.get("failures")
