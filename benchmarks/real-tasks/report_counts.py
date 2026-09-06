import io
import json
import tempfile

from agent_trace.eval.config import EvalConfig
from agent_trace.eval.runner import EvalReport, format_report_json, run_eval
from agent_trace.eval.scorers import ScoreResult
from agent_trace.models import SessionMeta
from agent_trace.store import TraceStore

good = ScoreResult("good", 1.0, 1.0, True)
bad = ScoreResult("bad", 0.0, 1.0, False)
report = EvalReport(session_id="counts", results=[good], config=EvalConfig())
assert (report.passed, report.failed, report.overall_passed) == (1, 0, True)
report.results.append(bad)
assert (report.passed, report.failed, report.overall_passed) == (1, 1, False)
report.results[0] = bad
assert (report.passed, report.failed) == (0, 2)
report.results.clear()
assert (report.passed, report.failed, report.overall_passed) == (0, 0, True)
report.results.extend([good, good, bad])
output = io.StringIO()
format_report_json(report, out=output)
data = json.loads(output.getvalue())
assert (data["pass_count"], data["fail_count"], data["passed"]) == (2, 1, False)
with tempfile.TemporaryDirectory() as directory:
    trace = TraceStore(directory)
    trace.create_session(SessionMeta(session_id="empty-session"))
    result = run_eval(trace, "empty-session", EvalConfig())
    assert result.passed == result.failed == 0
print("PASS: mutable report counts and runner compatibility")
