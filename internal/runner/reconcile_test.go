package runner

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Siddhant-K-code/agent-harness/internal/ownership"
	"github.com/Siddhant-K-code/agent-harness/internal/sandbox"
	"github.com/Siddhant-K-code/agent-harness/internal/store"
	"github.com/Siddhant-K-code/agent-harness/internal/task"
	"github.com/Siddhant-K-code/agent-harness/internal/workspace"
	"github.com/openai/openai-go/v3/responses"
)

func crashSpec(source, verifier string) task.Spec {
	return task.Spec{SchemaVersion: 1, Name: "real crash reconciliation", Goal: "exercise a real interrupted executor", Repository: source, Ref: "HEAD", Model: "gpt-5.4", Image: "node:22-alpine", Verifier: verifier,
		Limits: task.Limits{MaxSteps: 3, TimeoutMS: 120000, ToolTimeoutMS: 60000, MaxOutputTokens: 256, MaxTotalTokens: 5000, MaxUSD: 2}}
}

func TestCrashWorkerHelper(t *testing.T) {
	if os.Getenv("HARNESS_CRASH_HELPER") != "1" {
		return
	}
	root := os.Getenv("HARNESS_CRASH_ROOT")
	ctx := context.Background()
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = source
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git: %v %s", err, b)
		}
	}
	git("init", "-q")
	os.WriteFile(filepath.Join(source, "code.txt"), []byte("original\n"), 0644)
	git("add", ".")
	git("-c", "user.name=Harness Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "fixture")
	verifier := filepath.Join(root, "verify.sh")
	os.WriteFile(verifier, []byte("exit 0\n"), 0600)
	db, err := store.Open(filepath.Join(root, "harness.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	r, err := db.Create(ctx, crashSpec(source, verifier))
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(root, "runs", r.ID)
	lock, err := ownership.Acquire(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	r, err = db.StartOwned(ctx, r.ID, "crash-worker")
	if err != nil {
		t.Fatal(err)
	}
	w, err := workspace.Prepare(ctx, directory, source, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	image, err := sandbox.Check(ctx, "node:22-alpine")
	if err != nil {
		t.Fatal(err)
	}
	r, err = db.RecordOwned(ctx, r.ID, "crash-worker", "workspace.ready", map[string]any{"base_commit": w.Base, "image_id": image, "verifier_sha256": "test"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = saveCheckpoint(ctx, directory, r, w, Report{RunID: r.ID, BaseCommit: w.Base, ImageID: image}, []responses.ResponseInputItemUnionParam{responses.ResponseInputItemParamOfMessage("exercise recovery", "user")}); err != nil {
		t.Fatal(err)
	}
	id, _ := sandbox.Reference(r.ID, 1)
	if _, err = db.RecordOwned(ctx, r.ID, "crash-worker", "execution.prepared", map[string]any{"backend": "docker", "execution_id": id}, false); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "run-id"), []byte(r.ID), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := (sandbox.Docker{Workspace: w.Path, Image: image}).Execute(ctx, sandbox.Request{ID: id, Command: "set -eu; echo changed > code.txt; touch running; sleep 60"})
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("container failed: %s %s", result.Output, result.Stderr)
	}
}

func TestReconcileRealKilledController(t *testing.T) {
	if os.Getenv("HARNESS_DOCKER_TEST") != "1" {
		t.Skip("set HARNESS_DOCKER_TEST=1 for real controller-crash recovery")
	}
	// Colima shares the checkout, but may not mount macOS's system temp tree.
	base, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp(base, ".docker-test-crash-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	cmd := exec.Command(os.Args[0], "-test.run=^TestCrashWorkerHelper$")
	cmd.Env = append(os.Environ(), "HARNESS_CRASH_HELPER=1", "HARNESS_CRASH_ROOT="+root)
	log, err := os.Create(filepath.Join(root, "worker.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	cmd.Stdout = log
	cmd.Stderr = log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var id string
	defer func() {
		_ = cmd.Process.Kill()
		if id != "" {
			ref, _ := sandbox.Reference(id, 1)
			ctx, stop := context.WithTimeout(context.Background(), 10*time.Second)
			defer stop()
			_ = (sandbox.Docker{}).Stop(ctx, ref)
		}
	}()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		b, _ := os.ReadFile(filepath.Join(root, "run-id"))
		id = string(b)
		if len(id) == 32 {
			if _, err := os.Stat(filepath.Join(root, "runs", id, "workspace", "running")); err == nil {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, err := os.Stat(filepath.Join(root, "runs", id, "workspace", "running")); err != nil {
		b, _ := os.ReadFile(filepath.Join(root, "worker.log"))
		t.Fatalf("worker did not start: %s", b)
	}
	db, err := store.Open(filepath.Join(root, "harness.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, stop := context.WithTimeout(context.Background(), 20*time.Second)
	defer stop()
	if _, err := Reconcile(ctx, db, root, id); !errors.Is(err, ownership.ErrActive) {
		t.Fatalf("took over active controller: %v", err)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	report, err := Reconcile(ctx, db, root, id)
	if err != nil {
		t.Fatal(err)
	}
	if report.State != store.Failed || !report.CleanupConfirmed || !report.CheckpointAvailable || report.Verified {
		t.Fatalf("dishonest recovery outcome: %+v", report)
	}
	patch, err := os.ReadFile(report.Patch)
	if err != nil || !strings.Contains(string(patch), "+changed") {
		t.Fatalf("lost partial work: %v %s", err, patch)
	}
	ref, _ := sandbox.Reference(id, 1)
	state, err := (sandbox.Docker{}).Inspect(ctx, ref)
	if err != nil || state.Exists {
		t.Fatalf("orphan remains: %+v %v", state, err)
	}
	if _, err := Reconcile(ctx, db, root, id); err != nil {
		t.Fatal("non-idempotent reconcile:", err)
	}
}

func TestReconcileTerminalWithoutReport(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db, err := store.Open(filepath.Join(root, "harness.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	r, err := db.Create(ctx, crashSpec(root, filepath.Join(root, "verify.sh")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.StartOwned(ctx, r.ID, "dead-worker"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.RecordOwned(ctx, r.ID, "dead-worker", "verification.finished", map[string]any{"passed": true}, false); err != nil {
		t.Fatal(err)
	}
	if _, err = db.FinishOwned(ctx, r.ID, "dead-worker", store.Completed, "independent verifier passed"); err != nil {
		t.Fatal(err)
	}
	report, err := Reconcile(ctx, db, root, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if report.State != store.Completed || !report.Verified || !report.Reconciled {
		t.Fatalf("lost committed outcome: %+v", report)
	}
	if _, err = Reconcile(ctx, db, root, r.ID); err != nil {
		t.Fatal(err)
	}
}
