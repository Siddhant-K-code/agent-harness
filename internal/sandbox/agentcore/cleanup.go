package agentcore

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Siddhant-K-code/agent-harness/internal/ownership"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// CleanupTree retries only recorded infrastructure cleanup. It never runs a
// model or command and refuses a directory still owned by a live controller.
func CleanupTree(ctx context.Context, root string) (int, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && filepath.Base(filepath.Dir(path)) == "aws-executions" && strings.HasSuffix(entry.Name(), ".json") {
			files = append(files, path)
		}
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	count := 0
	var combined error
	for _, filename := range files {
		b, err := os.ReadFile(filename)
		if err != nil {
			combined = errors.Join(combined, err)
			continue
		}
		var s Execution
		if len(b) > 2<<20 || json.Unmarshal(b, &s) != nil {
			combined = errors.Join(combined, errors.New("invalid execution record"))
			continue
		}
		root := filepath.Dir(filepath.Dir(filename))
		lock, err := ownership.Acquire(root)
		if err != nil {
			combined = errors.Join(combined, err)
			continue
		}
		cfg, err := Check(ctx, s.Region, s.Image, s.Role)
		if err == nil {
			e := &Executor{Config: cfg, Image: s.Image, Role: s.Role, Root: root}
			err = e.Stop(ctx, s.ID)
		}
		lock.Close()
		if err != nil {
			combined = errors.Join(combined, err)
		} else {
			count++
		}
	}
	return count, combined
}
