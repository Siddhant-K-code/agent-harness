// Package store persists lifecycle changes and their events in one transaction.
package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Siddhant-K-code/agent-harness/internal/task"
	_ "modernc.org/sqlite"
)

type State string

const (
	Queued     State = "queued"
	Running    State = "running"
	Cancelling State = "cancelling"
	Completed  State = "completed"
	Failed     State = "failed"
	Cancelled  State = "cancelled"
	TimedOut   State = "timed_out"
)

var (
	ErrNotFound = errors.New("run not found")
	ErrConflict = errors.New("run state does not allow this operation")
)

func (s State) Terminal() bool {
	return s == Completed || s == Failed || s == Cancelled || s == TimedOut
}

type Run struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Backend      string    `json:"backend"`
	State        State     `json:"state"`
	Revision     int       `json:"revision"`
	Steps        int       `json:"steps"`
	Reason       string    `json:"reason,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	Spec         task.Spec `json:"task"`
	WorkerID     string    `json:"worker_id,omitempty"`
	Generation   int       `json:"generation,omitempty"`
	LeaseExpires time.Time `json:"lease_expires,omitempty"`
}

type Event struct {
	RunID     string          `json:"run_id"`
	Sequence  int             `json:"sequence"`
	Type      string          `json:"type"`
	CreatedAt time.Time       `json:"created_at"`
	Data      json.RawMessage `json:"data"`
}

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(abs, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", abs)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	s := &Store{db: db}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, pragma := range []string{
		"PRAGMA busy_timeout=5000", "PRAGMA foreign_keys=ON",
		"PRAGMA journal_mode=WAL", "PRAGMA synchronous=FULL",
	} {
		if _, err := db.ExecContext(ctx, pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("configure database: %w", err)
		}
	}
	err = s.write(ctx, func(conn *sql.Conn) error {
		var version int
		if err := conn.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
			return err
		}
		if version > 1 {
			return fmt.Errorf("unsupported database schema %d", version)
		}
		if version == 1 {
			return nil
		}
		_, err := conn.ExecContext(ctx, `
CREATE TABLE runs (id TEXT PRIMARY KEY, record TEXT NOT NULL);
CREATE TABLE events (
 run_id TEXT NOT NULL REFERENCES runs(id), sequence INTEGER NOT NULL,
 event TEXT NOT NULL, PRIMARY KEY(run_id, sequence)
);
CREATE TABLE outbox (
 run_id TEXT NOT NULL, sequence INTEGER NOT NULL,
 delivered INTEGER NOT NULL DEFAULT 0,
 PRIMARY KEY(run_id, sequence),
 FOREIGN KEY(run_id, sequence) REFERENCES events(run_id, sequence)
);
PRAGMA user_version=1;`)
		return err
	})
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate database: %w", err)
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) SQLiteVersion(ctx context.Context) (string, error) {
	var version string
	err := s.db.QueryRowContext(ctx, "SELECT sqlite_version()").Scan(&version)
	return version, err
}

// BEGIN IMMEDIATE serializes writers before reading the current run. This also
// serializes cancellation against completion across separate CLI processes.
func (s *Store) write(ctx context.Context, fn func(*sql.Conn) error) error {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}
	defer conn.ExecContext(context.Background(), "ROLLBACK")
	if err := fn(conn); err != nil {
		return err
	}
	_, err = conn.ExecContext(ctx, "COMMIT")
	return err
}

func (s *Store) Create(ctx context.Context, spec task.Spec) (Run, error) {
	if err := spec.Validate(); err != nil {
		return Run{}, err
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return Run{}, err
	}
	now := time.Now().UTC()
	r := Run{ID: hex.EncodeToString(id[:]), Name: spec.Name, Backend: "openai-responses/docker",
		State: Queued, Revision: 1, CreatedAt: now, UpdatedAt: now, Spec: spec}
	err := s.write(ctx, func(conn *sql.Conn) error {
		record, err := json.Marshal(r)
		if err != nil {
			return err
		}
		if _, err := conn.ExecContext(ctx, "INSERT INTO runs(id,record) VALUES(?,?)", r.ID, string(record)); err != nil {
			return err
		}
		return insertEvent(ctx, conn, r, "run.created", map[string]any{"model": spec.Model})
	})
	return r, err
}

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func get(ctx context.Context, q queryer, id string) (Run, error) {
	var record string
	if err := q.QueryRowContext(ctx, "SELECT record FROM runs WHERE id=?", id).Scan(&record); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Run{}, ErrNotFound
		}
		return Run{}, err
	}
	var r Run
	if err := json.Unmarshal([]byte(record), &r); err != nil {
		return Run{}, fmt.Errorf("corrupt run record: %w", err)
	}
	return r, nil
}

func (s *Store) Get(ctx context.Context, id string) (Run, error) { return get(ctx, s.db, id) }

func (s *Store) List(ctx context.Context) ([]Run, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT record FROM runs ORDER BY rowid DESC LIMIT 100")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	runs := []Run{}
	for rows.Next() {
		var record string
		if err := rows.Scan(&record); err != nil {
			return nil, err
		}
		var r Run
		if err := json.Unmarshal([]byte(record), &r); err != nil {
			return nil, err
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

func insertEvent(ctx context.Context, conn *sql.Conn, r Run, kind string, data any) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	e := Event{RunID: r.ID, Sequence: r.Revision, Type: kind, CreatedAt: r.UpdatedAt, Data: payload}
	record, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO events(run_id,sequence,event) VALUES(?,?,?)", r.ID, r.Revision, string(record)); err != nil {
		return err
	}
	_, err = conn.ExecContext(ctx, "INSERT INTO outbox(run_id,sequence) VALUES(?,?)", r.ID, r.Revision)
	return err
}

// mutate's callback returns false for an idempotent no-op. State, event and
// outbox insertion either all commit or all roll back.
func (s *Store) mutate(ctx context.Context, id, kind string, data any, fn func(*Run) (bool, error)) (Run, error) {
	var r Run
	err := s.write(ctx, func(conn *sql.Conn) error {
		var err error
		r, err = get(ctx, conn, id)
		if err != nil {
			return err
		}
		changed, err := fn(&r)
		if err != nil || !changed {
			return err
		}
		r.Revision++
		r.UpdatedAt = time.Now().UTC()
		record, err := json.Marshal(r)
		if err != nil {
			return err
		}
		if _, err := conn.ExecContext(ctx, "UPDATE runs SET record=? WHERE id=?", string(record), id); err != nil {
			return err
		}
		if kind == "run.finished" {
			data = map[string]any{"state": r.State, "reason": r.Reason}
		}
		return insertEvent(ctx, conn, r, kind, data)
	})
	return r, err
}

func (s *Store) Start(ctx context.Context, id string) (Run, error) {
	return s.mutate(ctx, id, "run.started", nil, func(r *Run) (bool, error) {
		if r.State != Queued {
			return false, ErrConflict
		}
		r.State = Running
		return true, nil
	})
}

func (s *Store) RequestCancel(ctx context.Context, id string) (Run, error) {
	return s.mutate(ctx, id, "run.cancel_requested", nil, func(r *Run) (bool, error) {
		if r.State.Terminal() || r.State == Cancelling {
			return false, nil
		}
		if r.State == Queued {
			r.State = Cancelled // No worker was ever started.
		} else {
			r.State = Cancelling
		}
		r.Reason = "operator requested cancellation"
		return true, nil
	})
}

func (s *Store) Record(ctx context.Context, id, kind string, data any, step bool) (Run, error) {
	return s.RecordOwned(ctx, id, "", kind, data, step)
}

func (s *Store) RecordOwned(ctx context.Context, id, owner, kind string, data any, step bool) (Run, error) {
	return s.mutate(ctx, id, kind, data, func(r *Run) (bool, error) {
		if r.State != Running || !r.ownedBy(owner) {
			return false, ErrConflict
		}
		if step {
			r.Steps++
		}
		return true, nil
	})
}

func (s *Store) Finish(ctx context.Context, id string, state State, reason string) (Run, error) {
	return s.FinishOwned(ctx, id, "", state, reason)
}

func (s *Store) FinishOwned(ctx context.Context, id, owner string, state State, reason string) (Run, error) {
	if !state.Terminal() {
		return Run{}, errors.New("finish requires a terminal state")
	}
	return s.mutate(ctx, id, "run.finished", map[string]any{"requested_state": state, "reason": reason}, func(r *Run) (bool, error) {
		if r.State.Terminal() {
			return false, nil
		}
		if r.WorkerID != owner {
			return false, ErrConflict
		}
		if r.State != Running && r.State != Cancelling {
			return false, ErrConflict
		}
		if r.State == Cancelling {
			r.State = Cancelled
		} else {
			r.State, r.Reason = state, reason
		}
		return true, nil
	})
}

func (s *Store) Events(ctx context.Context, id string) ([]Event, error) {
	if _, err := s.Get(ctx, id); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT event FROM events WHERE run_id=? ORDER BY sequence", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []Event{}
	for rows.Next() {
		var record string
		if err := rows.Scan(&record); err != nil {
			return nil, err
		}
		var event Event
		if err := json.Unmarshal([]byte(record), &event); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}
