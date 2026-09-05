package sandbox

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Siddhant-K-code/agent-harness/internal/process"
	"os"
	"strings"
	"time"
)

const MaxOutputBytes = 64 << 10

var ErrCleanup = errors.New("executor cleanup is unconfirmed")

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
	return d.execute(ctx, name, command, readonly)
}

func (d Docker) Capabilities() Capabilities {
	return Capabilities{Backend: "docker", Isolation: "shared_kernel", NetworkDisabled: true, ReadOnlyWorkspace: true, DurableExecutionID: true, DiskQuota: false}
}

func (d Docker) Execute(ctx context.Context, request Request) (process.Result, error) {
	if request.TimeoutMS > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(request.TimeoutMS)*time.Millisecond)
		defer cancel()
	}
	if !referencePattern.MatchString(request.ID) {
		return process.Result{}, errors.New("invalid execution reference")
	}
	return d.execute(ctx, request.ID, request.Command, request.ReadOnly)
}

func (d Docker) execute(ctx context.Context, name, command string, readonly bool) (result process.Result, runErr error) {
	if os.Getuid() == 0 {
		return result, errors.New("run the controller as a non-root user")
	}
	if strings.Contains(d.Workspace, ",") {
		return result, errors.New("workspace path cannot contain a comma")
	}
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

func (d Docker) Inspect(ctx context.Context, id string) (ExecutionState, error) {
	state := ExecutionState{ID: id}
	if !referencePattern.MatchString(id) {
		return state, errors.New("invalid execution reference")
	}
	r, err := process.Run(ctx, "", nil, nil, 8192, "docker", "container", "inspect", "--format", "{{json .State}}", id)
	if err != nil {
		return state, err
	}
	if r.ExitCode != 0 {
		if strings.Contains(r.Stderr, "No such container") || strings.Contains(r.Stderr, "No such object") {
			return state, nil
		}
		return state, fmt.Errorf("inspect container: %s", r.Stderr)
	}
	var result struct{ Running bool }
	if err := json.Unmarshal([]byte(r.Output), &result); err != nil {
		return state, err
	}
	state.Exists = true
	state.Running = result.Running
	return state, nil
}

func (d Docker) Stop(ctx context.Context, id string) error {
	if !referencePattern.MatchString(id) {
		return errors.New("invalid execution reference")
	}
	r, err := process.Run(ctx, "", nil, nil, 8192, "docker", "rm", "--force", id)
	if err != nil {
		return err
	}
	if r.ExitCode != 0 && !strings.Contains(r.Stderr, "No such container") {
		return fmt.Errorf("remove container: %s", r.Stderr)
	}
	state, err := d.Inspect(ctx, id)
	if err != nil {
		return err
	}
	if state.Exists {
		return errors.New("container remains after removal")
	}
	return nil
}
