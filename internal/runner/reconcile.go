package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Siddhant-K-code/agent-harness/internal/model"
	"github.com/Siddhant-K-code/agent-harness/internal/ownership"
	"github.com/Siddhant-K-code/agent-harness/internal/sandbox"
	"github.com/Siddhant-K-code/agent-harness/internal/store"
	"github.com/Siddhant-K-code/agent-harness/internal/workspace"
)

var ErrUncertainDispatch = errors.New("recorded execution is absent but dispatch completion is unknown; inspect the Docker daemon and client before manual reconciliation")

// Reconcile stops known executions only after obtaining the dead controller's
// OS lock. It preserves candidate work and reports uncertainty, never success.
func Reconcile(ctx context.Context, db *store.Store, root, id string) (Report, error) {
	var report Report
	r, err := db.Get(ctx, id)
	if err != nil {
		return report, err
	}
	if _, err := sandbox.Reference(r.ID, 1); err != nil {
		return report, err
	}
	directory := filepath.Join(root, "runs", r.ID)
	lock, err := ownership.Acquire(directory)
	if err != nil {
		return report, err
	}
	defer lock.Close()
	r, err = db.Get(ctx, id)
	if err != nil {
		return report, err
	}
	if r.WorkerID == "" && !r.State.Terminal() && r.State != store.Queued {
		return report, errors.New("legacy active run lacks durable ownership")
	}
	events, err := db.Events(ctx, id)
	if err != nil {
		return report, err
	}
	prepared := map[string]bool{}
	var pending bool
	var lastVerificationPassed bool
	price, priceErr := model.Pricing(r.Spec.Model)
	report = Report{RunID: r.ID, Model: r.Spec.Model, BillingUnknown: priceErr != nil, Reconciled: true}
	for _, event := range events {
		switch event.Type {
		case "execution.prepared":
			var d struct {
				ExecutionID string `json:"execution_id"`
				Backend     string `json:"backend"`
			}
			if err := json.Unmarshal(event.Data, &d); err != nil {
				return report, err
			}
			if d.Backend != "docker" {
				return report, errors.New("reconciliation requires the recorded backend adapter")
			}
			// The execution must belong to this run, not merely match the general name format.
			if len(d.ExecutionID) < len("agent-harness-"+r.ID+"-") || d.ExecutionID[:len("agent-harness-"+r.ID+"-")] != "agent-harness-"+r.ID+"-" {
				return report, errors.New("foreign execution reference")
			}
			prepared[d.ExecutionID] = true
		case "execution.finished":
			var d struct {
				ExecutionID      string `json:"execution_id"`
				CleanupConfirmed bool   `json:"cleanup_confirmed"`
			}
			if err := json.Unmarshal(event.Data, &d); err != nil {
				return report, err
			}
			if d.CleanupConfirmed {
				delete(prepared, d.ExecutionID)
			}
		case "workspace.ready":
			var d struct {
				Base     string `json:"base_commit"`
				Image    string `json:"image_id"`
				Verifier string `json:"verifier_sha256"`
			}
			if err := json.Unmarshal(event.Data, &d); err != nil {
				return report, err
			}
			report.BaseCommit = d.Base
			report.ImageID = d.Image
			report.VerifierSHA256 = d.Verifier
		case "model.requested":
			pending = true
		case "model.responded":
			var d struct {
				Usage *struct {
					Input  *int64 `json:"input_tokens"`
					Output *int64 `json:"output_tokens"`
				} `json:"usage"`
			}
			if err := json.Unmarshal(event.Data, &d); err != nil {
				return report, err
			}
			pending = false
			if d.Usage == nil || d.Usage.Input == nil || d.Usage.Output == nil || *d.Usage.Input < 0 || *d.Usage.Output < 0 {
				report.BillingUnknown = true
				continue
			}
			report.InputTokens += *d.Usage.Input
			report.OutputTokens += *d.Usage.Output
		case "verification.finished":
			var d struct {
				Passed bool `json:"passed"`
			}
			if err := json.Unmarshal(event.Data, &d); err != nil {
				return report, err
			}
			lastVerificationPassed = d.Passed
			report.VerificationAttempts++
		}
	}
	report.BillingUnknown = report.BillingUnknown || pending
	if priceErr == nil {
		report.EstimatedUSD = price.Cost(report.InputTokens, report.OutputTokens)
	}
	for reference := range prepared {
		docker := sandbox.Docker{}
		observed, err := docker.Inspect(ctx, reference)
		if err != nil {
			return report, err
		}
		if !observed.Exists {
			// An orphaned Docker client or an in-flight daemon request could
			// create the container after this lookup. Do not infer cleanup.
			return report, fmt.Errorf("%w: %s", ErrUncertainDispatch, reference)
		}
		if err := docker.Stop(ctx, reference); err != nil {
			return report, fmt.Errorf("stop recorded execution: %w", err)
		}
		if _, err := db.ConfirmExecutionCleanup(ctx, id, r.WorkerID, reference); err != nil {
			return report, err
		}
	}
	report.CleanupConfirmed = true
	// A terminal run's existing outcome remains authoritative. Reconciliation
	// can still retry cleanup after a previous cleanup failure.
	if r.State.Terminal() {
		var existing Report
		if err := readJSON(filepath.Join(directory, "report.json"), &existing, 1<<20); err == nil {
			if existing.RunID != r.ID || existing.State != r.State {
				return report, errors.New("report identity or state mismatch")
			}
			existing.CleanupConfirmed = true
			return existing, writeJSON(filepath.Join(directory, "report.json"), existing)
		} else if !errors.Is(err, os.ErrNotExist) {
			return report, err
		}
		// The controller may have died after committing run.finished but before
		// publishing report.json. Reconstruct only from committed evidence.
		if r.State == store.Completed && !lastVerificationPassed {
			return report, errors.New("completed run lacks verifier evidence")
		}
		report.Verified = r.State == store.Completed && lastVerificationPassed
	}
	if report.BaseCommit != "" {
		w := workspace.Workspace{Root: directory, Path: filepath.Join(directory, "workspace"), GitDir: filepath.Join(directory, "git"), Base: report.BaseCommit}
		patch, err := w.Patch(ctx)
		if err != nil {
			return report, fmt.Errorf("preserve interrupted patch: %w", err)
		}
		if err := os.WriteFile(filepath.Join(directory, "changes.patch"), []byte(patch), 0600); err != nil {
			return report, err
		}
		report.Patch = filepath.Join(directory, "changes.patch")
	}
	var cp Checkpoint
	if err := readJSON(filepath.Join(directory, "checkpoint.json"), &cp, 32<<20); err == nil {
		if cp.Version != 1 || cp.RunID != r.ID || cp.Sequence > r.Revision {
			return report, errors.New("invalid recovery checkpoint")
		}
		spec, e := json.Marshal(r.Spec)
		if e != nil {
			return report, e
		}
		sum := sha256.Sum256(spec)
		if cp.TaskSHA256 != hex.EncodeToString(sum[:]) {
			return report, errors.New("checkpoint task identity mismatch")
		}
		if err := workspace.VerifySnapshot(filepath.Join(directory, "snapshots", cp.Snapshot.SHA256+".tar"), cp.Snapshot); err != nil {
			return report, err
		}
		report.CheckpointAvailable = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return report, err
	}
	ended := r
	if !r.State.Terminal() {
		ended, err = db.ReconcileInterrupted(ctx, id)
		if err != nil {
			return report, err
		}
	}
	report.State = ended.State
	report.Reason = ended.Reason
	return report, writeJSON(filepath.Join(directory, "report.json"), report)
}

func readJSON(path string, dst any, max int64) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > max {
		return errors.New("invalid artifact file")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, max+1))
	if err := d.Decode(dst); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("invalid trailing artifact data")
	}
	return nil
}
