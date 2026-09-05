package sandbox

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/Siddhant-K-code/agent-harness/internal/process"
	"os"
	"strings"
	"time"
)

const MaxOutputBytes = 64 << 10

var ErrCleanup = errors.New("container cleanup failed")

type Docker struct{ Workspace, Image string }

func Check(ctx context.Context, image string) (string, error) {
	r, err := process.Run(ctx, "", nil, nil, 8192, "docker", "info", "--format", "{{.ServerVersion}}")
	if err != nil || r.ExitCode != 0 {
		return "", fmt.Errorf("Docker daemon unavailable: %s (%v)", strings.TrimSpace(r.Stderr), err)
	}
	r, err = process.Run(ctx, "", nil, nil, 8192, "docker", "image", "inspect", "--format", "{{.Id}}", image)
	if err != nil || r.ExitCode != 0 {
		return "", fmt.Errorf("image %q unavailable locally; pull/build it first: %s (%v)", image, strings.TrimSpace(r.Stderr), err)
	}
	id := strings.TrimSpace(r.Output)
	if !strings.HasPrefix(id, "sha256:") {
		return "", fmt.Errorf("invalid image ID %q", id)
	}
	return id, nil
}
func (d Docker) args(name string, readonly bool) []string {
	mount := "type=bind,src=" + d.Workspace + ",dst=/workspace"
	if readonly {
		mount += ",readonly"
	}
	return []string{"run", "--rm", "--pull=never", "--name", name, "--init", "--network=none", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--pids-limit=128", "--memory=1g", "--cpus=2", "--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()), "--tmpfs", "/tmp:rw,nosuid,nodev,size=256m,mode=1777", "--mount", mount, "--workdir", "/workspace", "--env", "HOME=/tmp", "--env", "TMPDIR=/tmp", "--env", "PYTHONDONTWRITEBYTECODE=1", "--env", "GOCACHE=/tmp/go-build", "--env", "GOPATH=/tmp/go", "--entrypoint", "/bin/sh", "-i", d.Image, "-s"}
}
func (d Docker) Exec(ctx context.Context, command string, readonly bool) (result process.Result, runErr error) {
	if os.Getuid() == 0 {
		return result, errors.New("run the controller as a non-root user")
	}
	if strings.Contains(d.Workspace, ",") {
		return process.Result{}, fmt.Errorf("workspace path cannot contain a comma")
	}
	var random [12]byte
	if _, err := rand.Read(random[:]); err != nil {
		return process.Result{}, err
	}
	name := "agent-harness-" + hex.EncodeToString(random[:])
	// Killing the Docker client alone does not stop its container.
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		removed, err := process.Run(cleanup, "", nil, nil, 8192, "docker", "rm", "--force", name)
		if err != nil || (removed.ExitCode != 0 && !strings.Contains(removed.Stderr, "No such container")) {
			runErr = errors.Join(runErr, fmt.Errorf("%w: %s", ErrCleanup, name))
		}
	}()
	return process.Run(ctx, "", nil, strings.NewReader(command), MaxOutputBytes, "docker", d.args(name, readonly)...)
}
