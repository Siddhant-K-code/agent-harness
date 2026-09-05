package store

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Siddhant-K-code/agent-harness/internal/task"
	"path/filepath"
	"sync"
	"testing"
)

func spec() task.Spec {
	return task.Spec{SchemaVersion: 1, Name: "test", Goal: "fix", Repository: "/repo", Ref: "HEAD", Model: "gpt-5.4", Image: "node:22-alpine", Verifier: "/verify.sh", Limits: task.Limits{MaxSteps: 5, TimeoutMS: 10000, ToolTimeoutMS: 1000, MaxOutputTokens: 256, MaxTotalTokens: 5000, MaxUSD: 2}}
}
func TestLifecycleAtomicEventsAndCancellation(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	peer, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	r, err := s.Create(ctx, spec())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Start(ctx, r.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = peer.RequestCancel(ctx, r.ID); err != nil {
		t.Fatal(err)
	}
	ended, err := s.Finish(ctx, r.ID, Completed, "tests passed")
	if err != nil {
		t.Fatal(err)
	}
	if ended.State != Cancelled {
		t.Fatalf("completion overrode cancellation: %s", ended.State)
	}
	if _, err = peer.RequestCancel(ctx, r.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Record(ctx, r.ID, "bad", nil, true); !errors.Is(err, ErrConflict) {
		t.Fatalf("late event accepted: %v", err)
	}
	events, err := s.Events(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 4 {
		t.Fatalf("events: %d", len(events))
	}
	var final map[string]any
	if err = json.Unmarshal(events[3].Data, &final); err != nil {
		t.Fatal(err)
	}
	if final["state"] != "cancelled" {
		t.Fatalf("misleading final event: %v", final)
	}
	var outbox int
	if err = s.db.QueryRow("SELECT count(*) FROM outbox").Scan(&outbox); err != nil {
		t.Fatal(err)
	}
	if outbox != len(events) {
		t.Fatalf("outbox=%d events=%d", outbox, len(events))
	}
	for i, e := range events {
		if e.Sequence != i+1 {
			t.Fatal("non-contiguous events")
		}
	}
	if version, err := s.SQLiteVersion(ctx); err != nil {
		t.Fatal(err)
	} else {
		t.Log("SQLite", version)
	}
}
func TestConcurrentWritersAndRollback(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r, err := s.Create(ctx, spec())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Start(ctx, r.ID); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.Record(ctx, r.ID, "test", nil, true); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	// A payload that cannot be serialized must roll back the state update too.
	if _, err = s.Record(ctx, r.ID, "bad", make(chan int), true); err == nil {
		t.Fatal("invalid event accepted")
	}
	r, err = s.Get(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	events, err := s.Events(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r.Steps != 20 || r.Revision != 22 || len(events) != 22 {
		t.Fatalf("state/event divergence: %+v events=%d", r, len(events))
	}
}
