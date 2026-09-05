"""Export the real model controller journal twice and replay with native APIs."""
import io
import json
from pathlib import Path
import subprocess
import sys
from agent_trace.store import TraceStore
from agent_trace.replay import replay_session, replay_to_html

root = Path('.harness/aws-e2e')
report = json.loads((root / 'model-report.json').read_text())
assert report['run_id']
command = ['bin/harness', 'trace', '--state-dir', str(root / 'model'),
           '--python', sys.executable, report['run_id']]
receipt = json.loads(subprocess.check_output(command))
repeat = json.loads(subprocess.check_output(command))
assert repeat['reused'] and repeat['session_id'] == receipt['session_id']
store = TraceStore(str(root / 'model/traces'), use_workspace_env=False)
meta = store.load_meta(receipt['session_id'])
events = store.load_events(receipt['session_id'])
assert len(events) == receipt['native_events']
assert meta.total_tokens == report['input_tokens'] + report['output_tokens']
if report['verified']:
    assert report['backend'] == 'agentcore' and report['cleanup_confirmed']
    assert meta.llm_requests > 0 and meta.tool_calls > 0
output = io.StringIO()
replay_session(store, receipt['session_id'], out=output)
(root / 'replay.txt').write_text(output.getvalue())
replay_to_html(store, receipt['session_id'], output_path=str(root / 'replay.html'))
summary = {'session_id': receipt['session_id'], 'native_events': len(events),
           'llm_requests': meta.llm_requests, 'tool_calls': meta.tool_calls,
           'total_tokens': meta.total_tokens, 'duplicate_export_reused': True}
(root / 'trace-validation.json').write_text(json.dumps(summary, indent=2) + '\n')
print(json.dumps(summary))
