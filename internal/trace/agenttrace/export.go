package agenttrace

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Siddhant-K-code/agent-harness/internal/process"
)

//go:embed bridge.py
var bridge string

type ExportResult struct {
	SessionID    string `json:"session_id"`
	Path         string `json:"path"`
	Reused       bool   `json:"reused"`
	NativeEvents int    `json:"native_events"`
}

func Export(ctx context.Context, python, destination string, projection Projection) (ExportResult, error) {
	var output ExportResult
	root, err := filepath.Abs(destination)
	if err != nil {
		return output, err
	}
	b, err := json.Marshal(projection)
	if err != nil {
		return output, err
	}
	if len(b) > 32<<20 {
		return output, errors.New("trace projection exceeds 32 MiB")
	}
	// Do not forward model/service credentials or AgentTrace tenant/workspace
	// defaults. Isolated Python also excludes cwd and PYTHONPATH imports.
	env := []string{"PATH=" + os.Getenv("PATH"), "PYTHONIOENCODING=utf-8"}
	r, err := process.Run(ctx, "", env, bytes.NewReader(b), 8192, python, "-I", "-c", bridge, root)
	if err != nil {
		return output, fmt.Errorf("AgentTrace bridge: %w", err)
	}
	if r.ExitCode != 0 {
		return output, fmt.Errorf("AgentTrace bridge failed: %s", r.Stderr)
	}
	if r.Truncated {
		return output, errors.New("AgentTrace bridge response exceeded limit")
	}
	if err := json.Unmarshal([]byte(r.Output), &output); err != nil {
		return output, fmt.Errorf("decode AgentTrace bridge result: %w", err)
	}
	if output.SessionID != projection.SessionID || output.Path != filepath.Join(root, projection.SessionID) {
		return output, errors.New("AgentTrace bridge returned an unexpected identity")
	}
	return output, nil
}
