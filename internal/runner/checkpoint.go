package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/Siddhant-K-code/agent-harness/internal/store"
	"github.com/Siddhant-K-code/agent-harness/internal/workspace"
	"github.com/openai/openai-go/v3/responses"
)

type Checkpoint struct {
	Version    int                `json:"version"`
	RunID      string             `json:"run_id"`
	Sequence   int                `json:"sequence"`
	TaskSHA256 string             `json:"task_sha256"`
	Snapshot   workspace.Snapshot `json:"snapshot"`
	Report     Report             `json:"report"`
	Input      json.RawMessage    `json:"input"`
}

func saveCheckpoint(ctx context.Context, root string, run store.Run, w workspace.Workspace, report Report, input []responses.ResponseInputItemUnionParam) (Checkpoint, error) {
	var cp Checkpoint
	snapshot, err := w.SnapshotTo(ctx, filepath.Join(root, "snapshots"))
	if err != nil {
		return cp, err
	}
	spec, err := json.Marshal(run.Spec)
	if err != nil {
		return cp, err
	}
	hash := sha256.Sum256(spec)
	conversation, err := json.Marshal(input)
	if err != nil {
		return cp, err
	}
	cp = Checkpoint{Version: 1, RunID: run.ID, Sequence: run.Revision, TaskSHA256: hex.EncodeToString(hash[:]), Snapshot: snapshot, Report: report, Input: conversation}
	return cp, writeJSON(filepath.Join(root, "checkpoint.json"), cp)
}

func atomicJSON(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(directory, ".json-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err = f.Write(append(b, '\n')); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	d, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
