package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestOwnershipAndFencing(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	r, err := db.Create(ctx, spec())
	if err != nil {
		t.Fatal(err)
	}
	r, err = db.StartOwned(ctx, r.ID, "worker-one")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Record(ctx, r.ID, "stale", nil, false); !errors.Is(err, ErrConflict) {
		t.Fatal("unowned writer accepted")
	}
	if _, err := db.Renew(ctx, r.ID, "worker-two"); !errors.Is(err, ErrConflict) {
		t.Fatal("foreign renewal accepted")
	}
	if _, err := db.RecordOwned(ctx, r.ID, "worker-one", "ok", nil, false); err != nil {
		t.Fatal(err)
	}
	if _, err := db.RequestCancel(ctx, r.ID); err != nil {
		t.Fatal(err)
	}
	ended, err := db.ReconcileInterrupted(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ended.State != Cancelled || ended.Generation != 2 || ended.WorkerID != "" {
		t.Fatalf("invalid fenced outcome: %+v", ended)
	}
	if _, err := db.RecordOwned(ctx, r.ID, "worker-one", "late", nil, false); !errors.Is(err, ErrConflict) {
		t.Fatal("fenced writer accepted")
	}
}
