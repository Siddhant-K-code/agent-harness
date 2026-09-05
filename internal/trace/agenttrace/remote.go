package agenttrace

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// RemoteRecord is written by the controller before dispatch and after observation.
// A response without a terminal command event is an observation, not completion.
type RemoteRecord struct {
	Sequence int             `json:"sequence"`
	Type     string          `json:"type"`
	At       time.Time       `json:"at"`
	CallID   string          `json:"call_id,omitempty"`
	Data     json.RawMessage `json:"data"`
}

type RemoteSource struct {
	ID      string         `json:"id"`
	Records []RemoteRecord `json:"records"`
}

func RemoteID(name string) string {
	sum := sha256.Sum256([]byte("agent-harness.remote.v1:" + name))
	return hex.EncodeToString(sum[:16])
}

// BuildRemote requires observed, ordered records. It cannot import the old
// aggregate probe report, which did not capture command dispatch timestamps.
func BuildRemote(src RemoteSource, content bool) (Projection, error) {
	p := Projection{Schema: Schema, Revision: Revision, SessionID: src.ID, Events: []Event{}, Journal: []map[string]any{}, Coverage: map[string]any{
		"mode": "metadata", "source_format": "agent-harness.remote.v1", "source_high_water_mark": len(src.Records),
		"file_access": "not_captured", "syscalls": "not_captured", "hidden_reasoning": "not_captured",
		"model_calls": "none_in_deterministic_probe", "source_payloads": "allowlisted_projection", "redaction": "agenttrace_pinned",
		"command_timestamps": "controller_intent_and_observation",
		"tool_dispatch":      "intent_precedes_api_call; remote_start_requires_observation", "individual_session_absence": "not_inspectable",
	}}
	if content {
		p.Coverage["mode"] = "selected_content"
	}
	if !runIDPattern.MatchString(src.ID) || len(src.Records) < 2 || src.Records[0].Type != "probe.started" || src.Records[len(src.Records)-1].Type != "probe.finished" {
		return p, errors.New("remote trace requires a valid terminal recorded probe")
	}
	pending := map[string]Event{}
	seen := map[string]bool{}
	observed := map[string]bool{}
	var prior time.Time
	calls := 0
	for i, r := range src.Records {
		if r.Sequence != i+1 || r.At.IsZero() || r.At.Before(prior) {
			return p, errors.New("invalid remote sequence or timestamp")
		}
		prior = r.At
		d, err := object(r.Data)
		if err != nil {
			return p, err
		}
		h := map[string]any{"source_sequence": r.Sequence, "source_type": r.Type, "origin": "controller", "backend": "aws-agentcore"}
		e := Event{ID: eventID(src.ID, r.Sequence), SessionID: src.ID, Timestamp: float64(r.At.UnixNano()) / 1e9, Data: map[string]any{"harness": h}}
		side := map[string]any{"sequence": r.Sequence, "source_type": r.Type, "timestamp": r.At, "data": map[string]any{}}
		switch r.Type {
		case "probe.started":
			if i != 0 {
				return p, errors.New("duplicate remote start")
			}
			e.Type = "session_start"
			e.Data["agent_name"] = "agent-harness-aws-probe"
			side["data"] = fields(d, "region", "image_digest", "patch_sha256", "verifier_sha256")
		case "command.requested":
			if !strings.HasPrefix(r.CallID, src.ID+"-") || seen[r.CallID] {
				return p, errors.New("duplicate or empty remote call ID")
			}
			seen[r.CallID] = true
			calls++
			e.Type = "tool_call"
			e.Data["tool_name"] = "aws_agentcore_command"
			h["call_id"] = r.CallID
			h["request"] = fields(d, "check", "profile", "timeout_seconds", "command_sha256", "session_label")
			if content {
				e.Data["arguments"] = fields(d, "command")
			}
			pending[r.CallID] = e
		case "command.observed":
			call, ok := pending[r.CallID]
			if !ok || observed[r.CallID] {
				return p, errors.New("remote observation without pending dispatch")
			}
			observed[r.CallID] = true
			result, _ := d["result"].(map[string]any)
			v := fields(result, "started", "finished", "status", "exit_code", "truncated")
			if _, ok := d["error"]; ok {
				v["has_error"] = true
			}
			if content {
				for k, val := range fields(result, "stdout", "stderr") {
					v[k] = val
				}
				for k, val := range fields(d, "error") {
					v[k] = val
				}
			}
			v["parent_event_id"] = call.ID
			side["data"] = v
			if result["finished"] == true {
				if code, ok := result["exit_code"].(float64); !ok || code != float64(int32(code)) || (result["status"] != "COMPLETED" && result["status"] != "TIMED_OUT") {
					return p, errors.New("remote completion missing exit code")
				}
				e.Type = "tool_result"
				e.ParentID = call.ID
				e.Data["tool_name"] = "aws_agentcore_command"
				e.Data["result"] = v
				elapsed := (e.Timestamp - call.Timestamp) * 1000
				e.DurationMS = &elapsed
				delete(pending, r.CallID)
			}
		case "artifacts.observed":
			side["data"] = fields(d, "patch_sha256", "verifier_sha256")
		case "session.stop_observed", "runtime.cleanup_observed", "isolation.checked":
			side["data"] = fields(d, "acknowledged", "runtime_deletion_confirmed", "passed", "profile", "check", "command_network_disabled", "read_only_workspace_enforced")
		case "probe.finished":
			if i != len(src.Records)-1 {
				return p, errors.New("premature remote terminal record")
			}
			passed, ok := d["passed"].(bool)
			if !ok {
				return p, errors.New("missing probe outcome")
			}
			state := "failed"
			if passed {
				state = "completed"
			}
			e.Type = "session_end"
			e.Data["state"] = state
			p.Outcome = fields(d, "passed", "runtime_deletion_confirmed", "stop_acknowledged", "new_model_calls")
			p.Outcome["aws_billed_usd"] = nil
			p.Outcome["measurement_source"] = "remote_controller_journal"
			p.Meta = map[string]any{"session_id": src.ID, "agent_name": "agent-harness-aws-probe", "started_at": float64(src.Records[0].At.UnixNano()) / 1e9,
				"ended_at": e.Timestamp, "tool_calls": calls, "llm_requests": 0, "total_tokens": 0, "total_duration_ms": r.At.Sub(src.Records[0].At).Seconds() * 1000,
				"attribution": map[string]any{"state": state, "harness_schema": Schema, "backend": "aws-agentcore", "source_format": "agent-harness.remote.v1"}}
		default:
			return p, fmt.Errorf("unsupported remote record %q", r.Type)
		}
		if e.Type != "" {
			p.Events = append(p.Events, e)
			side["native_event_id"] = e.ID
		}
		p.Journal = append(p.Journal, side)
	}
	incomplete := []map[string]any{}
	for _, e := range p.Events {
		if e.Type == "tool_call" {
			for _, call := range pending {
				if call.ID == e.ID {
					incomplete = append(incomplete, map[string]any{"event_id": e.ID, "status": "outcome_unknown"})
				}
			}
		}
	}
	p.Coverage["calls_without_result"] = incomplete
	p.Coverage["source_events"] = len(src.Records)
	p.Coverage["native_events"] = len(p.Events)
	return p, nil
}
