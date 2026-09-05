package store

import (
	"context"
	"errors"
	"time"
)

const LeaseDuration = 30 * time.Second

func (r Run) ownedBy(owner string) bool {
	if r.WorkerID == "" {
		return owner == ""
	}
	return owner == r.WorkerID && time.Now().Before(r.LeaseExpires)
}

func (s *Store) StartOwned(ctx context.Context, id, owner string) (Run, error) {
	if owner == "" {
		return Run{}, errors.New("worker identity required")
	}
	return s.mutate(ctx, id, "run.started", map[string]any{"worker_id": owner}, func(r *Run) (bool, error) {
		if r.State != Queued {
			return false, ErrConflict
		}
		r.State = Running
		r.WorkerID = owner
		r.Generation++
		r.LeaseExpires = time.Now().UTC().Add(LeaseDuration)
		return true, nil
	})
}

func (s *Store) Renew(ctx context.Context, id, owner string) (Run, error) {
	return s.mutate(ctx, id, "worker.renewed", nil, func(r *Run) (bool, error) {
		if (r.State != Running && r.State != Cancelling) || !r.ownedBy(owner) {
			return false, ErrConflict
		}
		r.LeaseExpires = time.Now().UTC().Add(LeaseDuration)
		return true, nil
	})
}

// ConfirmExecutionCleanup is for reconciliation while holding the run's OS
// lock. The caller must have observed the container, stopped it, and confirmed
// absence. A missing container without an acknowledgement is not sufficient.
func (s *Store) ConfirmExecutionCleanup(ctx context.Context, id, expectedOwner, reference string) (Run, error) {
	return s.mutate(ctx, id, "execution.finished", map[string]any{"execution_id": reference, "cleanup_confirmed": true, "reconciled": true}, func(r *Run) (bool, error) {
		if r.WorkerID != expectedOwner {
			return false, ErrConflict
		}
		return true, nil
	})
}

// ReconcileInterrupted is called only while the caller holds the exclusive
// local process lock and has confirmed that all recorded executions stopped.
// It fences the old identity. It never marks an interrupted run completed.
func (s *Store) ReconcileInterrupted(ctx context.Context, id string) (Run, error) {
	return s.mutate(ctx, id, "run.finished", nil, func(r *Run) (bool, error) {
		if r.State.Terminal() {
			return false, nil
		}
		if r.WorkerID == "" && r.State != Queued {
			return false, errors.New("legacy run has no durable execution ownership; reconcile manually")
		}
		r.Generation++
		r.WorkerID = ""
		r.LeaseExpires = time.Time{}
		if r.State == Cancelling {
			r.State = Cancelled
			r.Reason = "interrupted controller; recorded executions stopped after cancellation"
		} else {
			r.State = Failed
			r.Reason = "controller interrupted; recorded executions stopped; effects and billing require inspection"
		}
		return true, nil
	})
}
