import time

from agent_trace.models import EventType, TraceEvent
from agent_trace.watch import WatcherConfig, WatchState, check_event

config = WatcherConfig(max_cost_dollars=0.05, max_retries=1)
event = TraceEvent(event_type=EventType.ASSISTANT_RESPONSE, data={})
for _ in range(2):
    state = WatchState()
    alerts = []
    for amount in (0.01, 0.06, 0.5, 1.1, 2.7, 12.2):
        state.estimated_cost = amount
        alerts.extend(v for v in check_event(event, config, state) if v.startswith("CostWatcher:"))
    assert len(alerts) == 1, f"expected one cost alert per state, got {len(alerts)}"
state.start_time = time.time() - config.max_duration_seconds - 5
assert any(v.startswith("DurationWatcher:") for v in check_event(event, config, state))
command = TraceEvent(event_type=EventType.TOOL_CALL, data={"tool_name": "bash", "arguments": {"command": "false"}})
check_event(command, config, state)
assert any(v.startswith("RetryWatcher:") for v in check_event(command, config, state))
print("PASS: once-per-session cost alerts and other watchers")
