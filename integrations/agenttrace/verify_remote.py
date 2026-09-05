"""Validate/replay the actual exported remote journal using pinned AgentTrace."""
import io
import json
from pathlib import Path
from agent_trace.store import TraceStore
from agent_trace.replay import replay_session, replay_to_html

receipt = json.loads(Path('.harness/aws-trace-export.json').read_text())
repeat = json.loads(Path('.harness/aws-trace-export-repeat.json').read_text())
assert repeat['reused'] and repeat['session_id'] == receipt['session_id']
store = TraceStore('.harness/aws-traces', use_workspace_env=False)
meta = store.load_meta(receipt['session_id'])
events = store.load_events(receipt['session_id'])
sidecar = json.loads((Path(receipt['path']) / 'harness.json').read_text())
assert meta.llm_requests == 0 and meta.total_tokens == 0
assert all(e.event_type not in ('llm_request', 'llm_response') for e in events)
assert len(events) == receipt['native_events']
if sidecar['outcome']['passed']:
    assert len(sidecar['coverage']['calls_without_result']) == 1
    assert sidecar['outcome']['runtime_deletion_confirmed']
output = io.StringIO()
replay_session(store, receipt['session_id'], out=output)
if meta.tool_calls:
    assert 'tool_call' in output.getvalue()
Path('.harness/aws-trace-replay.txt').write_text(output.getvalue())
replay_to_html(store, receipt['session_id'], output_path='.harness/aws-trace-replay.html')
print(json.dumps({'native_events': len(events), 'tool_calls': meta.tool_calls,
                  'llm_requests': meta.llm_requests, 'duplicate_export_reused': repeat['reused'],
                  'calls_without_result': len(sidecar['coverage']['calls_without_result'])}))
