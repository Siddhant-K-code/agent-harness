"""Create the bounded live task from the existing real fixture and pinned image."""
import json
import os
from pathlib import Path

root = Path.cwd()
task = json.loads((root / 'examples/normalize-tags/task.json').read_text())
task.update(backend='agentcore', image=os.environ['HARNESS_AGENTCORE_IMAGE'],
            aws={'region': 'us-east-1', 'execution_role': os.environ['HARNESS_AGENTCORE_ROLE']},
            repository=str(root / '.harness/example-repo'),
            verifier=str(root / 'examples/normalize-tags/verify.sh'))
task['limits'].update(max_steps=4, timeout_ms=1800000, max_usd=0.50, max_repairs=1)
(root / '.harness/aws-e2e/task.json').write_text(json.dumps(task, indent=2) + '\n')
