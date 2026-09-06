package agenttrace

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Siddhant-K-code/agent-harness/internal/store"
	"github.com/Siddhant-K-code/agent-harness/internal/task"
)

const canary = "ghp_0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"

func fixture(t *testing.T) (store.Run, []store.Event) {
	t.Helper()
	r := store.Run{ID: strings.Repeat("a", 32), State: store.Completed, Spec: task.Spec{Model: "gpt-5.4", Goal: canary, Repository: "/private/" + canary}, CreatedAt: time.Date(2026, 9, 5, 1, 0, 0, 0, time.UTC)}
	var events []store.Event
	add := func(kind string, data any) {
		b, err := json.Marshal(data)
		if err != nil {
			t.Fatal(err)
		}
		seq := len(events) + 1
		events = append(events, store.Event{RunID: r.ID, Sequence: seq, Type: kind, CreatedAt: r.CreatedAt.Add(time.Duration(seq) * time.Second), Data: b})
	}
	add("run.created", map[string]any{"model": "gpt-5.4"})
	add("run.started", nil)
	add("model.requested", map[string]any{"input_tokens": 10, "reserved_usd": .01})
	add("model.responded", map[string]any{"id": "resp_test", "status": "completed", "instructions": canary, "output": []any{map[string]any{"encrypted_content": canary}}, "usage": map[string]any{"input_tokens": 10, "output_tokens": 5}})
	add("tool.requested", map[string]any{"ID": "call_test", "Name": "exec", "Arguments": `{"command":"echo ` + canary + `"}`})
	add("tool.finished", map[string]any{"call_id": "call_test", "result": map[string]any{"output": canary, "stderr": "", "exit_code": 0, "truncated": true}})
	add("tool.requested", map[string]any{"ID": "call_finish", "Name": "finish", "Arguments": `{"summary":"done"}`})
	add("verification.finished", map[string]any{"passed": true, "attempt": 1, "summary": canary, "result": map[string]any{"exit_code": 0, "output": canary}})
	add("future.event", map[string]any{"private": canary})
	add("run.finished", map[string]any{"state": "completed", "reason": canary})
	r.Revision = len(events)
	r.UpdatedAt = events[len(events)-1].CreatedAt
	return r, events
}

func TestProjectionCoverageAndCorrelation(t *testing.T) {
	r, events := fixture(t)
	p, err := Build(r, events, false)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(p)
	if bytes.Contains(b, []byte(canary)) || bytes.Contains(b, []byte("encrypted_content")) {
		t.Fatal("private content leaked")
	}
	if p.Meta["total_tokens"] != float64(15) {
		t.Fatal("lost usage")
	}
	if len(p.Events) != 7 || len(p.Journal) != 10 {
		t.Fatalf("projection counts %d %d", len(p.Events), len(p.Journal))
	}
	if p.Events[2].ParentID != p.Events[1].ID || p.Events[4].ParentID != p.Events[3].ID {
		t.Fatal("lost request/result links")
	}
	if p.Events[4].Data["result"].(map[string]any)["truncated"] != true {
		t.Fatal("lost truncation")
	}
	incomplete := p.Coverage["calls_without_result"].([]map[string]any)
	if len(incomplete) != 1 || incomplete[0]["status"] != "verification_observed_without_tool_result" {
		t.Fatal("invented finish result")
	}
	p2, _ := Build(r, events, false)
	b2, _ := json.Marshal(p2)
	if !bytes.Equal(b, b2) {
		t.Fatal("unstable projection")
	}
}

func TestProjectionPreservesConfiguredSnapshotAndContext(t *testing.T) {
	r, events := fixture(t)
	r.Spec.Model = "gpt-5.4-mini-2026-03-17"
	events[2].Data = json.RawMessage(`{"input_tokens":10,"max_output_tokens":4096,"reserved_usd":0.02,"context_window_tokens":128000,"pricing_basis":"standard_uncached"}`)
	p, err := Build(r, events, false)
	if err != nil {
		t.Fatal(err)
	}
	request := p.Events[1]
	if request.Data["model"] != r.Spec.Model {
		t.Fatal("snapshot ID lost in native projection")
	}
	admission := request.Data["harness"].(map[string]any)["admission"].(map[string]any)
	if admission["context_window_tokens"] != float64(128000) || admission["pricing_basis"] != "standard_uncached" {
		t.Fatal("configuration missing from trace")
	}
}

func TestCompactionAndSkillMetadataDoNotExportTheirContent(t *testing.T) {
	r, events := fixture(t)
	events[2].Data = json.RawMessage(`{"purpose":"compaction","input_tokens":10}`)
	last := events[len(events)-1]
	events = events[:len(events)-1]
	for _, item := range []struct {
		kind string
		data any
	}{
		{"skill.loaded", map[string]any{"id": "repair", "version": strings.Repeat("b", 64), "source": "learned", "instructions": canary}},
		{"context.compacted", map[string]any{"accepted": true, "before_tokens": 100, "after_tokens": 50, "summary": canary, "source": canary}},
	} {
		b, _ := json.Marshal(item.data)
		events = append(events, store.Event{RunID: r.ID, Sequence: len(events) + 1, Type: item.kind, Data: b, CreatedAt: r.CreatedAt.Add(time.Duration(len(events)+1) * time.Second)})
	}
	last.Sequence = len(events) + 1
	last.CreatedAt = r.CreatedAt.Add(time.Duration(last.Sequence) * time.Second)
	events = append(events, last)
	r.Revision = len(events)
	r.UpdatedAt = last.CreatedAt
	p, err := Build(r, events, false)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(p)
	if bytes.Contains(b, []byte(canary)) || !bytes.Contains(b, []byte(`"purpose":"compaction"`)) || !bytes.Contains(b, []byte(`"before_tokens":100`)) {
		t.Fatal("new metadata lost or content leaked")
	}
}

func TestRejectIncompleteAndMismatchedJournal(t *testing.T) {
	r, events := fixture(t)
	if _, err := Build(r, events[:3], false); err == nil {
		t.Fatal("accepted missing events")
	}
	copy := append([]store.Event(nil), events...)
	copy[3].RunID = "wrong"
	if _, err := Build(r, copy, false); err == nil {
		t.Fatal("accepted foreign event")
	}
	r.State = store.Running
	if _, err := Build(r, events, false); err == nil {
		t.Fatal("accepted live journal")
	}
}

func TestMissingUsageAndInterruptedRequest(t *testing.T) {
	r, events := fixture(t)
	events[3].Data = json.RawMessage(`{"status":"completed"}`)
	p, err := Build(r, events, false)
	if err != nil {
		t.Fatal(err)
	}
	if p.Coverage["usage_complete"] != false {
		t.Fatal("missing usage reported complete")
	}
	if _, ok := p.Events[2].Data["input_tokens"]; ok {
		t.Fatal("invented zero usage")
	}
	// A terminal failure with an unanswered model request remains unanswered.
	events = events[:3]
	r.State = store.Failed
	r.Revision = 4
	events = append(events, store.Event{RunID: r.ID, Sequence: 4, Type: "run.finished", CreatedAt: r.UpdatedAt, Data: json.RawMessage(`{"state":"failed"}`)})
	p, err = Build(r, events, false)
	if err != nil {
		t.Fatal(err)
	}
	if p.Coverage["model_request_pending"] != true {
		t.Fatal("lost ambiguous request")
	}
}

func nativePython(t *testing.T) string {
	t.Helper()
	p := os.Getenv("HARNESS_AGENTTRACE_PYTHON")
	if p == "" {
		t.Skip("set HARNESS_AGENTTRACE_PYTHON to run against the pinned AgentTrace package")
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func TestNativeExportIdempotencyRedactionAndCorruption(t *testing.T) {
	python := nativePython(t)
	r, events := fixture(t)
	p, err := Build(r, events, true)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "traces")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// Independent processes concurrently publish the same immutable session.
	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := Export(ctx, python, root, p); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	result, err := Export(ctx, python, root, p)
	if err != nil || !result.Reused {
		t.Fatalf("retry: %+v %v", result, err)
	}
	for _, name := range []string{"meta.json", "events.ndjson", "harness.json", "manifest.json"} {
		b, err := os.ReadFile(filepath.Join(result.Path, name))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(b, []byte(canary)) {
			t.Fatalf("secret in %s", name)
		}
		info, _ := os.Stat(filepath.Join(result.Path, name))
		if info.Mode().Perm() != 0600 {
			t.Fatal("public trace file")
		}
	}
	// Exercise the real terminal renderer, without a second trace implementation.
	code := `import io,sys; from agent_trace.store import TraceStore; from agent_trace.replay import replay_session; out=io.StringIO(); replay_session(TraceStore(sys.argv[1],use_workspace_env=False),sys.argv[2],out=out); assert "tool_call" in out.getvalue()`
	if b, err := exec.CommandContext(ctx, python, "-I", "-c", code, root, r.ID).CombinedOutput(); err != nil {
		t.Fatalf("native replay: %v %s", err, b)
	}
	p.Coverage["mode"] = "different"
	if _, err := Export(ctx, python, root, p); err == nil {
		t.Fatal("overwrote conflicting export")
	}
	p.Coverage["mode"] = "selected_content"
	if err := os.WriteFile(filepath.Join(result.Path, "events.ndjson"), []byte("corrupt\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Export(ctx, python, root, p); err == nil {
		t.Fatal("accepted corrupt existing export")
	}
}

func TestNativeFailureBeforePublication(t *testing.T) {
	python := nativePython(t)
	r, events := fixture(t)
	p, _ := Build(r, events, false)
	b, _ := json.Marshal(p)
	// Replace only the final rename in a child process. The native writer and
	// reader still execute; simulate abrupt death at the publication boundary.
	code := `import os,sys,json
ns={"__name__":"bridge_test"}
exec(sys.argv[1],ns)
ns["os"].rename=lambda *args: os._exit(91)
ns["publish"](sys.argv[2],json.load(sys.stdin))`
	root := filepath.Join(t.TempDir(), "traces")
	cmd := exec.Command(python, "-I", "-c", code, bridge, root)
	cmd.Stdin = bytes.NewReader(b)
	if err := cmd.Run(); err == nil || cmd.ProcessState.ExitCode() != 91 {
		t.Fatalf("writer did not reach injected publication failure: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, r.ID)); !os.IsNotExist(err) {
		t.Fatal("partial session published")
	}
	if _, err := Export(context.Background(), python, root, p); err != nil {
		t.Fatal("could not retry interrupted export:", err)
	}
}

func TestNativeRejectsSymlinkDestination(t *testing.T) {
	python := nativePython(t)
	r, events := fixture(t)
	p, _ := Build(r, events, false)
	parent := t.TempDir()
	root := filepath.Join(parent, "traces")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	if err := os.Symlink(other, filepath.Join(root, r.ID)); err != nil {
		t.Fatal(err)
	}
	if _, err := Export(context.Background(), python, root, p); err == nil {
		t.Fatal("followed session symlink")
	}
	entries, _ := os.ReadDir(other)
	if len(entries) != 0 {
		t.Fatal("wrote through session symlink")
	}
}

func TestOutcomeValidation(t *testing.T) {
	r, events := fixture(t)
	p, _ := Build(r, events, false)
	path := filepath.Join(t.TempDir(), "report.json")
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(`{"run_id":"wrong","state":"completed"}`)
	if err := p.AttachOutcome(path); err == nil {
		t.Fatal("accepted foreign report")
	}
	write(`{"run_id":"` + r.ID + `","state":"completed","input_tokens":-1}`)
	if err := p.AttachOutcome(path); err == nil {
		t.Fatal("accepted negative usage")
	}
	write(`{"run_id":"` + r.ID + `","state":"completed","verified":true,"reason":"` + canary + `","patch":"/private/path"}`)
	if err := p.AttachOutcome(path); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(p.Outcome)
	if bytes.Contains(b, []byte(canary)) || bytes.Contains(b, []byte("/private/path")) {
		t.Fatal("report text leaked")
	}
}
