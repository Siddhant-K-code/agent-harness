from agent_trace.eval.scorers import score_regex
from agent_trace.models import EventType, TraceEvent

events = [TraceEvent(event_type=EventType.ASSISTANT_RESPONSE, data={"text": "result 42"})]
for pattern in ("(", "[", "*invalid"):
    result = score_regex(events, pattern, threshold=0.7)
    assert not result.passed and result.score == 0.0
    assert result.threshold == 0.7 and "invalid" in result.reason.lower()
assert score_regex(events, r"result \d+").passed
assert not score_regex(events, "absent").passed
assert not score_regex(events, "result", event_type="tool_call").passed
assert not score_regex(events, "result", event_type="unknown").passed
assert not score_regex([], "result").passed
print("PASS: invalid-regex and matching semantics")
