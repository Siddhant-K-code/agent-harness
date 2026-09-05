"""Independently check exact recorded runtimes, then remove their logs and image."""
import datetime
import json
import os
from pathlib import Path
import re
import subprocess

def aws(*args, absent_ok=False):
    result = subprocess.run(['aws', *args, '--output', 'json', '--no-cli-pager'], capture_output=True, text=True)
    if result.returncode:
        if absent_ok and 'ResourceNotFoundException' in result.stderr:
            return None
        raise RuntimeError(result.stderr)
    return json.loads(result.stdout or '{}')

root = Path('.harness/aws-e2e')
root.mkdir(parents=True, exist_ok=True)
audit = {'at': datetime.datetime.now(datetime.timezone.utc).isoformat(),
         'runtimes_checked': 0, 'absent': 0, 'aws_billed_usd': None}
failures = []
for file in root.glob('**/aws-executions/*.json'):
    state = json.loads(file.read_text())
    runtime = state.get('runtime_id')
    if not runtime:
        if not state.get('create_explicitly_rejected'):
            failures.append('Unresolved create: ' + state['execution_id'])
        continue
    assert re.fullmatch(r'agent_harness_probe_[0-9a-f]{24}-[A-Za-z0-9]+', runtime)
    audit['runtimes_checked'] += 1
    if aws('bedrock-agentcore-control', 'get-agent-runtime', '--agent-runtime-id', runtime, absent_ok=True) is not None:
        failures.append('Runtime remains: ' + runtime)
        continue
    audit['absent'] += 1
    prefix = '/aws/bedrock-agentcore/runtimes/' + runtime
    for group in aws('logs', 'describe-log-groups', '--log-group-name-prefix', prefix).get('logGroups', []):
        name = group['logGroupName']
        assert name == prefix or name.startswith(prefix + '/') or name.startswith(prefix + '-')
        aws('logs', 'delete-log-group', '--log-group-name', name)
    assert re.fullmatch(r'agent_harness_probe_[0-9a-f]{24}', state['name'])
    aws('bedrock-agentcore-control', 'delete-workload-identity', '--name', state['name'], absent_ok=True)
tag = os.environ.get('PROBE_TAG', '')
if tag:
    assert re.fullmatch(r'probe-[0-9]+-[0-9]+', tag)
    result = aws('ecr', 'batch-delete-image', '--repository-name', 'agent-harness-probe', '--image-ids', 'imageTag=' + tag)
    unexpected = [f for f in result.get('failures', []) if f.get('failureCode') != 'ImageNotFound']
    assert not unexpected, unexpected
audit['failures'] = failures
audit['cleanup_confirmed'] = not failures
(root / 'cleanup-audit.json').write_text(json.dumps(audit, indent=2) + '\n')
print(json.dumps(audit))
assert not failures, failures
