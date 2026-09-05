package agenttrace

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func remoteFixture(t *testing.T) RemoteSource {
	t.Helper()
	src := RemoteSource{ID: RemoteID("probe-fixture")}
	at := time.Date(2026, 9, 5, 15, 0, 0, 0, time.UTC)
	add := func(kind, id string, d any) {
		b, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		src.Records = append(src.Records, RemoteRecord{Sequence: len(src.Records) + 1, Type: kind, CallID: id, At: at.Add(time.Duration(len(src.Records)) * time.Second), Data: b})
	}
	add("probe.started", "", map[string]any{"region": "us-east-1"})
	add("command.requested", src.ID+"-2", map[string]any{"command": canary, "profile": "work", "check": "command_network_isolation"})
	add("command.observed", src.ID+"-2", map[string]any{"result": map[string]any{"started": true, "finished": true, "status": "COMPLETED", "exit_code": 0, "stdout": canary, "truncated": true}})
	add("command.requested", src.ID+"-4", map[string]any{"command": "sleep 10", "profile": "work"})
	add("command.observed", src.ID+"-4", map[string]any{"result": map[string]any{"started": true, "finished": false}, "error": "context deadline exceeded"})
	add("session.stop_observed", "", map[string]any{"acknowledged": true})
	add("runtime.cleanup_observed", "", map[string]any{"runtime_deletion_confirmed": true})
	add("probe.finished", "", map[string]any{"passed": true, "runtime_deletion_confirmed": true, "stop_acknowledged": true, "new_model_calls": 0})
	return src
}
func TestRemoteUnknownOutcomeAndDeterminism(t *testing.T) {
	src := remoteFixture(t)
	p, err := BuildRemote(src, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Events) != 5 || p.Events[2].ParentID != p.Events[1].ID {
		t.Fatal("lost native call correlation")
	}
	if len(p.Coverage["calls_without_result"].([]map[string]any)) != 1 {
		t.Fatal("invented command completion after disconnect")
	}
	if p.Meta["llm_requests"] != 0 || p.Outcome["aws_billed_usd"] != nil {
		t.Fatal("invented model activity or cloud billing")
	}
	b, _ := json.Marshal(p)
	if bytes.Contains(b, []byte(canary)) {
		t.Fatal("metadata leaked command content")
	}
	p2, _ := BuildRemote(src, false)
	b2, _ := json.Marshal(p2)
	if !bytes.Equal(b, b2) {
		t.Fatal("unstable remote projection")
	}
}
func TestRemoteRejectsInvalidJournal(t *testing.T) {
	for _, kind := range []string{"partial", "sequence", "time", "duplicate", "orphan", "old-report"} {
		t.Run(kind, func(t *testing.T) {
			s := remoteFixture(t)
			switch kind {
			case "partial":
				s.Records = s.Records[:len(s.Records)-1]
			case "sequence":
				s.Records[2].Sequence = 9
			case "time":
				s.Records[2].At = s.Records[0].At.Add(-time.Second)
			case "duplicate":
				s.Records[3].CallID = s.Records[1].CallID
			case "orphan":
				s.Records[2].CallID = s.ID + "-absent"
			case "old-report":
				s.Records = nil
			}
			if _, err := BuildRemote(s, false); err == nil {
				t.Fatal("accepted invalid evidence")
			}
		})
	}
}
func TestRemoteNativeReplayAndRedaction(t *testing.T) {
	python := nativePython(t)
	src := remoteFixture(t)
	p, err := BuildRemote(src, true)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "traces")
	r, err := Export(context.Background(), python, root, p)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Export(context.Background(), python, root, p)
	if err != nil || !again.Reused {
		t.Fatal("duplicate native export failed", err)
	}
	for _, name := range []string{"meta.json", "events.ndjson", "harness.json", "manifest.json"} {
		b, err := os.ReadFile(filepath.Join(r.Path, name))
		if err != nil || bytes.Contains(b, []byte(canary)) {
			t.Fatal("native redaction failed", name, err)
		}
	}
	code := `import io,sys; from agent_trace.store import TraceStore; from agent_trace.replay import replay_session; s=TraceStore(sys.argv[1],use_workspace_env=False); m=s.load_meta(sys.argv[2]); assert m.llm_requests==0 and m.tool_calls==2; out=io.StringIO(); replay_session(s,sys.argv[2],out=out); assert 'tool_call' in out.getvalue()`
	if b, err := exec.Command(python, "-I", "-c", code, root, src.ID).CombinedOutput(); err != nil {
		t.Fatalf("native replay: %v %s", err, b)
	}
}
