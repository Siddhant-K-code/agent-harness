package learning

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Siddhant-K-code/agent-harness/internal/store"
	"github.com/Siddhant-K-code/agent-harness/internal/task"
)

func TestObservationUsesTerminalJournalAndKeepsFailuresAsEvidence(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	db, err := store.Open(filepath.Join(root, "harness.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	spec, err := task.Load("../../examples/normalize-tags/task.json")
	if err != nil {
		t.Fatal(err)
	}
	spec.Repository = t.TempDir()
	r, err := db.Create(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.StartOwned(ctx, r.ID, "learner-test"); err != nil {
		t.Fatal(err)
	}
	if _, err := Capture(ctx, root, db, r.ID); err == nil {
		t.Fatal("active partial run became an observation")
	}
	if _, err := db.RecordOwned(ctx, r.ID, "learner-test", "workspace.ready", map[string]any{"base_commit": "commit", "verifier_sha256": "verifier"}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := db.RecordOwned(ctx, r.ID, "learner-test", "verification.finished", map[string]any{"passed": false, "result": map[string]any{"output": strings.Repeat("failure ", 1000), "exit_code": 1}}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := db.FinishOwned(ctx, r.ID, "learner-test", store.Failed, "repair limit reached"); err != nil {
		t.Fatal(err)
	}
	id, err := Capture(ctx, root, db, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Capture(ctx, root, db, r.ID)
	if err != nil || again != id {
		t.Fatal("capture not idempotent", err)
	}
	o, err := Load(root, id)
	if err != nil {
		t.Fatal(err)
	}
	if o.State != store.Failed || len(o.Feedback) != 1 || o.Feedback[0].Passed || len(o.Feedback[0].Output) > 3020 || !o.ExpiresAt.After(o.CreatedAt) {
		t.Fatal("observation invented success or lost bounds", o)
	}
	path := filepath.Join(root, "learning", "observations", id+".json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(b), "repair limit reached", "success", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root, id); err == nil {
		t.Fatal("tampered observation accepted")
	}
}
