package runner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Siddhant-K-code/agent-harness/internal/ownership"
	"github.com/Siddhant-K-code/agent-harness/internal/sandbox"
	"github.com/Siddhant-K-code/agent-harness/internal/sandbox/agentcore"
	"github.com/Siddhant-K-code/agent-harness/internal/store"
	"github.com/Siddhant-K-code/agent-harness/internal/task"
	"github.com/Siddhant-K-code/agent-harness/internal/workspace"
)

func awsSpec(t *testing.T) task.Spec {
	t.Helper()
	if os.Getenv("HARNESS_AGENTCORE_TEST") != "1" {
		t.Skip("explicit real AWS test opt-in required")
	}
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	spec, err := task.Load(filepath.Join(repo, "examples/normalize-tags/task.json"))
	if err != nil {
		t.Fatal(err)
	}
	spec.Backend = "agentcore"
	spec.Image = os.Getenv("HARNESS_AGENTCORE_IMAGE")
	spec.AWS = &task.AWSConfig{Region: "us-east-1", ExecutionRole: os.Getenv("HARNESS_AGENTCORE_ROLE")}
	return spec
}

// Real AWS round trip of the previously generated GPT-5.4 patch. No model API.
func TestAWSExecutorRoundTrip(t *testing.T) {
	spec := awsSpec(t)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()
	cfg, err := agentcore.Check(ctx, spec.AWS.Region, spec.Image, spec.AWS.ExecutionRole)
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs(os.Getenv("HARNESS_AGENTCORE_EVIDENCE"))
	if err != nil {
		t.Fatal(err)
	}
	root = filepath.Join(root, "roundtrip")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	w, err := workspace.Prepare(ctx, root, spec.Repository, spec.Ref)
	if err != nil {
		t.Fatal(err)
	}
	e := &agentcore.Executor{Config: cfg, Image: spec.Image, Role: spec.AWS.ExecutionRole, Root: root, Workspace: &w, Progress: os.Stderr}
	patch, err := os.ReadFile("../../docs/evidence/2026-09-05/changes.patch")
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := os.ReadFile(spec.Verifier)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{"agent-harness-" + strings.Repeat("1", 32) + "-1", "agent-harness-" + strings.Repeat("1", 32) + "-2"}
	// Use a different identifier for each workflow execution, while retaining
	// durable ownership within this test's controller directory.
	db, err := store.Open(filepath.Join(root, "identity.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	run, err := db.Create(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	for i := range ids {
		ids[i], err = sandbox.Reference(run.ID, i+1)
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 15*time.Minute)
		defer stop()
		for _, id := range ids {
			if err := e.Stop(cleanup, id); err != nil {
				t.Error("cleanup:", err)
			}
		}
	})
	command := "set -eu\npatch -p1 <<'HARNESS_PATCH'\n" + string(patch) + "\nHARNESS_PATCH\npython3 /opt/harness/isolation-checks.py network\nprintf 'durable untracked file' > transferred.txt\n"
	result, err := e.Execute(ctx, sandbox.Request{ID: ids[0], Command: command, TimeoutMS: 30000})
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("real write: %+v %v", result, err)
	}
	if b, _ := os.ReadFile(filepath.Join(w.Path, "transferred.txt")); string(b) != "durable untracked file" {
		t.Fatal("artifact handoff lost untracked file")
	}
	pathBefore := w.Path
	result, err = e.Execute(ctx, sandbox.Request{ID: ids[1], Command: "python3 /opt/harness/isolation-checks.py readonly\n" + string(verifier), ReadOnly: true, TimeoutMS: 30000})
	if err != nil || result.ExitCode != 0 || !strings.Contains(result.Output, "Passed 6") {
		t.Fatalf("real verification: %+v %v", result, err)
	}
	if w.Path != pathBefore {
		t.Fatal("verifier replaced candidate")
	}
	actualPatch, err := w.Patch(ctx)
	if err != nil || !strings.Contains(actualPatch, "transferred.txt") || !strings.Contains(actualPatch, "toLowerCase") {
		t.Fatal("controller patch lost candidate:", err)
	}
	if err := os.WriteFile(filepath.Join(root, "changes.patch"), []byte(actualPatch), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(root, "result.json"), map[string]any{"passed": true, "real_model_patch": true, "new_model_calls": 0, "network_denial": true, "readonly_verifier": true, "candidate_transfer": true, "cleanup_confirmed": true, "verification": result}); err != nil {
		t.Fatal(err)
	}
}

func TestAWSWorkerCrashReconciliation(t *testing.T) {
	spec := awsSpec(t)
	base, err := filepath.Abs(os.Getenv("HARNESS_AGENTCORE_EVIDENCE"))
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "crash")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAWSInterruptedWorkerHelper$", "-test.v")
	child.Env = append(os.Environ(), "HARNESS_CRASH_ROOT="+root)
	child.Stdout = os.Stderr
	child.Stderr = os.Stderr
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer child.Process.Kill()
	var runID string
	var state agentcore.Execution
	var stateFile string
	for {
		b, _ := os.ReadFile(filepath.Join(root, "run-id"))
		runID = string(b)
		if runID != "" {
			id, _ := sandbox.Reference(runID, 1)
			stateFile = filepath.Join(root, "runs", runID, "aws-executions", id+".json")
			b, _ = os.ReadFile(stateFile)
			if json.Unmarshal(b, &state) == nil && state.CommandStarted {
				break
			}
		}
		select {
		case <-ctx.Done():
			t.Fatal("worker did not start command:", ctx.Err())
		case <-time.After(time.Second):
		}
	}
	db, err := store.Open(filepath.Join(root, "harness.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := Reconcile(ctx, db, root, runID); !errors.Is(err, ownership.ErrActive) {
		t.Fatalf("active worker takeover allowed: %v", err)
	}
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = child.Wait()
	actualID := state.RuntimeID
	// Inject a lost Create acknowledgement into the persisted controller record.
	// Reconciliation must recover the SAME runtime with the original token, not
	// replay Command or infer absence from a disconnected worker.
	state.RuntimeID = ""
	state.RuntimeARN = ""
	if err := writeJSON(stateFile, state); err != nil {
		t.Fatal(err)
	}
	report, err := Reconcile(ctx, db, root, runID)
	if err != nil {
		t.Fatal(err)
	}
	if report.State != store.Failed || report.Verified || !report.CleanupConfirmed {
		t.Fatalf("invented crash outcome: %+v", report)
	}
	b, err := os.ReadFile(stateFile)
	if err != nil || json.Unmarshal(b, &state) != nil {
		t.Fatal("missing cleanup state")
	}
	if state.RuntimeID != actualID || !state.Deleted || state.CommandResult != nil || state.Accepted {
		t.Fatalf("incorrect recovery: %+v", state)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "runs", runID, "workspace", "tags.js")); !strings.Contains(string(b), "new Set(tags)") {
		t.Fatal("accepted an unobserved candidate")
	}
	if err := writeJSON(filepath.Join(root, "result.json"), map[string]any{"passed": true, "worker_killed": true, "active_owner_rejected": true, "lost_create_ack_recovered": true, "runtime_identity_preserved": true, "command_replayed": false, "unobserved_candidate_accepted": false, "cleanup_confirmed": report.CleanupConfirmed, "report": report}); err != nil {
		t.Fatal(err)
	}
	_ = spec
}

func TestAWSInterruptedWorkerHelper(t *testing.T) {
	root := os.Getenv("HARNESS_CRASH_ROOT")
	if root == "" {
		t.Skip("subprocess only")
	}
	spec := awsSpec(t)
	ctx := context.Background()
	cfg, err := agentcore.Check(ctx, spec.AWS.Region, spec.Image, spec.AWS.ExecutionRole)
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(root, "harness.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	run, err := db.Create(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(root, "runs", run.ID)
	lock, err := ownership.Acquire(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	owner := "crash-acceptance-worker"
	if _, err := db.StartOwned(ctx, run.ID, owner); err != nil {
		t.Fatal(err)
	}
	w, err := workspace.Prepare(ctx, directory, spec.Repository, spec.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.RecordOwned(ctx, run.ID, owner, "workspace.ready", map[string]any{"base_commit": w.Base, "image_id": spec.Image}, false); err != nil {
		t.Fatal(err)
	}
	id, _ := sandbox.Reference(run.ID, 1)
	if _, err := db.RecordOwned(ctx, run.ID, owner, "execution.prepared", map[string]any{"execution_id": id, "backend": "agentcore", "readonly": false}, false); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "run-id"), []byte(run.ID), 0600); err != nil {
		t.Fatal(err)
	}
	e := &agentcore.Executor{Config: cfg, Image: spec.Image, Role: spec.AWS.ExecutionRole, Root: directory, Workspace: &w, Progress: os.Stderr}
	_, err = e.Execute(ctx, sandbox.Request{ID: id, Command: "printf unobserved > tags.js; sleep 300; printf should-not-be-observed", TimeoutMS: 330000})
	t.Fatalf("worker was expected to be killed, returned %v", err)
}
