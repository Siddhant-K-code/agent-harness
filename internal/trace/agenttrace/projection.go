// Package agenttrace projects the private journal into AgentTrace's native schema.
// It never exports the raw Responses object or reconstructs unobserved events.
package agenttrace

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/Siddhant-K-code/agent-harness/internal/store"
)

const Revision = "b109ec5b3714b842746e97ee8e975329d8582667"
const Schema = "agent-harness.agenttrace.v1"

var runIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

type Event struct {
	Type       string         `json:"event_type"`
	Timestamp  float64        `json:"timestamp"`
	ID         string         `json:"event_id"`
	SessionID  string         `json:"session_id"`
	ParentID   string         `json:"parent_id,omitempty"`
	DurationMS *float64       `json:"duration_ms,omitempty"`
	Data       map[string]any `json:"data"`
}

type Projection struct {
	Schema    string           `json:"schema"`
	Revision  string           `json:"agenttrace_revision"`
	SessionID string           `json:"session_id"`
	Meta      map[string]any   `json:"meta"`
	Events    []Event          `json:"events"`
	Journal   []map[string]any `json:"journal"`
	Coverage  map[string]any   `json:"coverage"`
	Outcome   map[string]any   `json:"outcome,omitempty"`
}

func eventID(run string, seq int) string {
	s := sha256.Sum256([]byte(fmt.Sprintf("%s:%s:%d", Schema, run, seq)))
	return hex.EncodeToString(s[:16])
}

func object(raw json.RawMessage) (map[string]any, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return map[string]any{}, nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil || m == nil {
		return nil, errors.New("event data must be an object")
	}
	return m, nil
}

// Only explicitly selected primitives reach the bridge. Text is opt-in and is
// redacted there, using the pinned AgentTrace implementation before any write.
func fields(src map[string]any, keys ...string) map[string]any {
	out := map[string]any{}
	for _, k := range keys {
		if v, ok := src[k]; ok {
			switch v.(type) {
			case string, bool, float64:
				out[k] = v
			}
		}
	}
	return out
}

func resultSummary(v any, content bool) map[string]any {
	m, _ := v.(map[string]any)
	out := fields(m, "exit_code", "truncated", "verified")
	if _, ok := m["error"]; ok {
		out["has_error"] = true
	}
	if content {
		for k, v := range fields(m, "output", "stderr", "error", "instruction") {
			out[k] = v
		}
	}
	for _, key := range []string{"result", "feedback"} {
		if nested, ok := m[key]; ok {
			out[key] = resultSummary(nested, content)
		}
	}
	return out
}

// Build accepts only a complete, contiguous terminal journal. Source types with
// no native equivalent retain an ordered, metadata-only sidecar entry.
func Build(run store.Run, events []store.Event, content bool) (Projection, error) {
	p := Projection{Schema: Schema, Revision: Revision, SessionID: run.ID,
		Events: []Event{}, Journal: []map[string]any{},
		Coverage: map[string]any{
			"mode": "metadata", "source_high_water_mark": run.Revision,
			"file_access": "not_captured", "syscalls": "not_captured", "hidden_reasoning": "not_captured",
			"raw_provider_payload": "omitted", "repository_paths": "omitted", "prompts": "omitted",
			"source_payloads": "allowlisted_projection", "redaction": "agenttrace_pinned",
			"tool_dispatch": "requests_and_results_only_no_separate_start_event",
		},
	}
	if content {
		p.Coverage["mode"] = "selected_content"
	}
	if !runIDPattern.MatchString(run.ID) || !run.State.Terminal() {
		return p, errors.New("trace export requires a valid terminal run")
	}
	if len(events) != run.Revision || len(events) == 0 {
		return p, errors.New("incomplete journal")
	}
	var modelPending *Event
	type pendingCall struct {
		event    Event
		name     string
		verified bool
	}
	calls := map[string]pendingCall{}
	var lastCall string
	var started *time.Time
	var inputTokens, outputTokens float64
	var requests, toolCount, missingUsage int
	knownModels := map[string]bool{"gpt-5.4": true, "gpt-5.4-mini": true}
	modelName := "unrecognized"
	if knownModels[run.Spec.Model] {
		modelName = run.Spec.Model
	}
	for i, source := range events {
		if source.RunID != run.ID || source.Sequence != i+1 || source.CreatedAt.IsZero() {
			return p, errors.New("invalid journal identity or sequence")
		}
		d, err := object(source.Data)
		if err != nil {
			return p, fmt.Errorf("event %d: %w", source.Sequence, err)
		}
		h := map[string]any{"run_id": run.ID, "sequence": source.Sequence, "source_type": source.Type}
		e := Event{Timestamp: float64(source.CreatedAt.UnixNano()) / 1e9, ID: eventID(run.ID, source.Sequence), SessionID: run.ID, Data: map[string]any{"harness": h}}
		side := map[string]any{"sequence": source.Sequence, "source_type": source.Type, "timestamp": source.CreatedAt, "data": map[string]any{}}
		switch source.Type {
		case "run.created":
			side["data"] = map[string]any{"model": modelName}
		case "run.started":
			if started != nil {
				return p, errors.New("multiple attempts require a newer trace projection")
			}
			t := source.CreatedAt
			started = &t
			e.Type = "session_start"
			e.Data["agent_name"] = "agent-harness"
		case "workspace.ready":
			side["data"] = fields(d, "base_commit", "image_id", "verifier_sha256")
		case "executor.ready":
			side["data"] = fields(d, "backend", "isolation", "network_disabled", "read_only_workspace", "durable_execution_id", "disk_quota")
		case "execution.prepared", "execution.finished":
			v := fields(d, "execution_id", "backend", "readonly", "cleanup_confirmed")
			if call, ok := calls[lastCall]; ok {
				v["parent_event_id"] = call.event.ID
			}
			side["data"] = v
		case "checkpoint.saved":
			side["data"] = fields(d, "source_sequence", "workspace_sha256")
		case "worker.renewed":
			side["data"] = map[string]any{"renewal_recorded": true}
		case "model.requested":
			if modelPending != nil {
				return p, errors.New("overlapping model requests")
			}
			e.Type = "llm_request"
			e.Data["model"] = modelName
			h["admission"] = fields(d, "input_tokens", "max_output_tokens", "reserved_usd")
			copy := e
			modelPending = &copy
			requests++
		case "model.responded":
			if modelPending == nil {
				return p, errors.New("model response without request")
			}
			e.Type = "llm_response"
			e.ParentID = modelPending.ID
			e.Data["model"] = modelName
			duration := (e.Timestamp - modelPending.Timestamp) * 1000
			e.DurationMS = &duration
			modelPending = nil
			u, _ := d["usage"].(map[string]any)
			in, inOK := u["input_tokens"].(float64)
			out, outOK := u["output_tokens"].(float64)
			if inOK && outOK && in >= 0 && out >= 0 && in == float64(int64(in)) && out == float64(int64(out)) && in+out < 1<<53 {
				e.Data["input_tokens"] = in
				e.Data["output_tokens"] = out
				e.Data["total_tokens"] = in + out
				inputTokens += in
				outputTokens += out
			} else {
				missingUsage++
				h["usage"] = "unknown"
			}
			h["provider"] = fields(d, "id", "status")
		case "tool.requested":
			var call struct{ ID, Name, Arguments string }
			if err := json.Unmarshal(source.Data, &call); err != nil || call.ID == "" {
				return p, errors.New("invalid tool request")
			}
			if _, exists := calls[call.ID]; exists {
				return p, errors.New("duplicate tool call ID")
			}
			name := call.Name
			if name != "exec" && name != "finish" {
				name = "unknown"
			}
			e.Type = "tool_call"
			e.Data["tool_name"] = name
			h["call_id"] = call.ID
			if content {
				args, err := object(json.RawMessage(call.Arguments))
				if err == nil {
					e.Data["arguments"] = fields(args, "command", "summary")
				} else {
					h["arguments"] = "invalid_json_omitted"
				}
			} else {
				h["arguments"] = "omitted"
			}
			calls[call.ID] = pendingCall{event: e, name: name}
			lastCall = call.ID
			toolCount++
		case "tool.finished":
			id, _ := d["call_id"].(string)
			call, ok := calls[id]
			if !ok {
				return p, errors.New("tool result without request")
			}
			e.Type = "tool_result"
			e.ParentID = call.event.ID
			e.Data["tool_name"] = call.name
			duration := (e.Timestamp - call.event.Timestamp) * 1000
			e.DurationMS = &duration
			e.Data["result"] = resultSummary(d["result"], content)
			h["call_id"] = id
			delete(calls, id)
		case "verification.finished":
			v := fields(d, "passed", "attempt")
			v["result"] = resultSummary(d["result"], content)
			if content {
				for k, value := range fields(d, "summary") {
					v[k] = value
				}
			}
			if call, ok := calls[lastCall]; ok && call.name == "finish" {
				v["parent_event_id"] = call.event.ID
				call.verified = true
				calls[lastCall] = call
			}
			side["data"] = v
		case "run.finished":
			if d["state"] != string(run.State) || i != len(events)-1 {
				return p, errors.New("terminal state disagrees with journal")
			}
			e.Type = "session_end"
			e.Data["state"] = string(run.State)
			if content {
				e.Data["reason"] = d["reason"]
			}
		case "run.cancel_requested":
			side["data"] = map[string]any{"cancellation_requested": true}
		default:
			// Payloads from future event types have no export policy yet.
			side["payload_omitted"] = true
		}
		if e.Type != "" {
			p.Events = append(p.Events, e)
			side["native_event_id"] = e.ID
		}
		p.Journal = append(p.Journal, side)
	}
	incomplete := []map[string]any{}
	// Iterate source order rather than a map so exports are deterministic.
	for _, event := range p.Events {
		if event.Type != "tool_call" {
			continue
		}
		for _, call := range calls {
			if call.event.ID == event.ID {
				status := "outcome_unknown"
				if call.verified {
					status = "verification_observed_without_tool_result"
				}
				incomplete = append(incomplete, map[string]any{"event_id": event.ID, "status": status})
			}
		}
	}
	p.Coverage["calls_without_result"] = incomplete
	p.Coverage["usage_complete"] = missingUsage == 0 && modelPending == nil
	p.Coverage["model_request_pending"] = modelPending != nil
	p.Coverage["source_events"] = len(events)
	p.Coverage["native_events"] = len(p.Events)
	p.Meta = map[string]any{"session_id": run.ID, "agent_name": "agent-harness", "started_at": float64(run.CreatedAt.UnixNano()) / 1e9,
		"ended_at": float64(run.UpdatedAt.UnixNano()) / 1e9, "tool_calls": toolCount, "llm_requests": requests, "total_tokens": inputTokens + outputTokens,
		"total_duration_ms": run.UpdatedAt.Sub(run.CreatedAt).Seconds() * 1000,
		"attribution":       map[string]any{"harness_schema": Schema, "run_id": run.ID, "state": string(run.State)},
	}
	if started != nil {
		p.Meta["started_at"] = float64(started.UnixNano()) / 1e9
		p.Meta["total_duration_ms"] = run.UpdatedAt.Sub(*started).Seconds() * 1000
	}
	return p, nil
}
