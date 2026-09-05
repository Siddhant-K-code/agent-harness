package process

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"time"
)

type Result struct {
	Output    string `json:"output"`
	Stderr    string `json:"stderr"`
	ExitCode  int    `json:"exit_code"`
	Truncated bool   `json:"truncated"`
}
type limitedBuffer struct {
	buffer    bytes.Buffer
	max       int
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	left := b.max - b.buffer.Len()
	if len(p) > left {
		p = p[:left]
		b.truncated = true
	}
	_, err := b.buffer.Write(p)
	return n, err
}

// Run bounds captured output but drains all bytes so noisy processes cannot block.
func Run(ctx context.Context, dir string, env []string, stdin io.Reader, max int, name string, args ...string) (Result, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = 2 * time.Second
	cmd.Dir, cmd.Stdin = dir, stdin
	if env != nil {
		cmd.Env = env
	}
	b, stderr := &limitedBuffer{max: max}, &limitedBuffer{max: max}
	cmd.Stdout, cmd.Stderr = b, stderr
	err := cmd.Run()
	r := Result{Output: b.buffer.String(), Stderr: stderr.buffer.String(), Truncated: b.truncated || stderr.truncated}
	if ctx.Err() != nil {
		return r, ctx.Err()
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		r.ExitCode = exit.ExitCode()
		return r, nil
	}
	return r, err
}
