package workspace

import (
	"context"
	"fmt"
	"github.com/Siddhant-K-code/agent-harness/internal/process"
	"os"
	"path/filepath"
	"strings"
)

const MaxPatchBytes = 8 << 20

type Workspace struct{ Root, Path, GitDir, Base string }

func git(ctx context.Context, dir string, args ...string) (string, error) {
	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=/nonexistent", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_LFS_SKIP_SMUDGE=1"}
	a := append([]string{"-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "protocol.file.allow=always"}, args...)
	r, err := process.Run(ctx, dir, env, nil, MaxPatchBytes, "git", a...)
	if err != nil {
		return "", err
	}
	if r.ExitCode != 0 {
		return "", fmt.Errorf("git exited %d: %s", r.ExitCode, r.Stderr)
	}
	if r.Truncated {
		return "", fmt.Errorf("git output exceeds %d bytes", MaxPatchBytes)
	}
	return r.Output, nil
}
func Prepare(ctx context.Context, root, source, ref string) (Workspace, error) {
	w := Workspace{Root: root, Path: filepath.Join(root, "workspace"), GitDir: filepath.Join(root, "git")}
	if err := os.MkdirAll(root, 0700); err != nil {
		return w, err
	}
	source, err := filepath.Abs(source)
	if err != nil {
		return w, err
	}
	if _, err := os.Stat(source); err != nil {
		return w, fmt.Errorf("local repository: %w", err)
	}
	base, err := git(ctx, source, "rev-parse", "--verify", ref+"^{commit}")
	if err != nil {
		return w, err
	}
	w.Base = strings.TrimSpace(base)
	if _, err = git(ctx, "", "clone", "--no-local", "--no-checkout", "--", source, w.Path); err != nil {
		return w, err
	}
	if _, err = git(ctx, w.Path, "checkout", "--detach", w.Base); err != nil {
		return w, err
	}
	if _, err := os.Stat(filepath.Join(w.Path, ".gitmodules")); err == nil {
		return w, fmt.Errorf("submodules are not supported yet")
	}
	// The agent cannot mutate the baseline, index, hooks, or Git configuration.
	if err = os.Rename(filepath.Join(w.Path, ".git"), w.GitDir); err != nil {
		return w, err
	}
	return w, nil
}
func (w Workspace) Patch(ctx context.Context) (string, error) {
	args := []string{"--git-dir=" + w.GitDir, "--work-tree=" + w.Path, "-c", "core.bare=false"}
	if _, err := git(ctx, w.Path, append(args, "add", "--all", "--force", "--", ".")...); err != nil {
		return "", err
	}
	return git(ctx, w.Path, append(args, "diff", "--cached", "--binary", "--no-ext-diff", "--no-textconv", w.Base, "--")...)
}
