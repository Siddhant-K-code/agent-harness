package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Current recovers the last atomically published remote workspace. Candidates
// are immutable local directories; publishing never renames the old workspace.
func Current(root string) (string, error) {
	b, err := os.ReadFile(filepath.Join(root, "workspace-current.json"))
	if errors.Is(err, os.ErrNotExist) {
		return filepath.Join(root, "workspace"), nil
	}
	if err != nil {
		return "", err
	}
	var s Snapshot
	if len(b) > 4096 || json.Unmarshal(b, &s) != nil || !digestPattern.MatchString(s.SHA256) {
		return "", errors.New("invalid workspace pointer")
	}
	dir := filepath.Join(root, "candidates", s.SHA256)
	info, err := os.Lstat(dir)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("candidate is not a directory")
	}
	return dir, nil
}

// AcceptSnapshot is called only after remote runtime deletion is confirmed.
// A crash before pointer publication retains the previous accepted workspace.
func (w *Workspace) AcceptSnapshot(ctx context.Context, filename string, s Snapshot) error {
	if err := VerifySnapshot(filename, s); err != nil {
		return err
	}
	base := filepath.Join(w.Root, "candidates")
	if err := os.MkdirAll(base, 0700); err != nil {
		return err
	}
	dir := filepath.Join(base, s.SHA256)
	if _, err := os.Lstat(dir); errors.Is(err, os.ErrNotExist) {
		stage, err := os.MkdirTemp(base, ".incoming-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(stage)
		if err := RestoreSnapshot(ctx, filename, filepath.Join(stage, "tree"), s); err != nil {
			return err
		}
		if err := os.Rename(filepath.Join(stage, "tree"), dir); err != nil {
			return err
		}
		if err := syncDir(base); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(w.Root, ".current-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err := f.Write(b); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), filepath.Join(w.Root, "workspace-current.json")); err != nil {
		return err
	}
	if err := syncDir(w.Root); err != nil {
		return err
	}
	w.Path = dir
	return nil
}
