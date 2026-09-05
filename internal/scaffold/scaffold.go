package scaffold

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	normalizetags "github.com/Siddhant-K-code/agent-harness/examples/normalize-tags"
	"github.com/Siddhant-K-code/agent-harness/internal/model"
	"github.com/Siddhant-K-code/agent-harness/internal/process"
	"github.com/Siddhant-K-code/agent-harness/internal/task"
)

type Options struct {
	Directory, Repository, Ref, Goal, Image, Verifier, Model string
	MaxUSD                                                   float64
}

// Git ignores host hooks, filters and credential helpers during local setup.
func Git(ctx context.Context, dir string, args ...string) (string, error) {
	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=/nonexistent", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0"}
	args = append([]string{"-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "commit.gpgsign=false"}, args...)
	r, err := process.Run(ctx, dir, env, nil, 8192, "git", args...)
	if err != nil || r.ExitCode != 0 {
		return "", fmt.Errorf("local Git check failed: %s (%v)", strings.TrimSpace(r.Stderr), err)
	}
	return strings.TrimSpace(r.Output), nil
}

func Create(ctx context.Context, o Options) (string, error) {
	data, _ := normalizetags.Files.ReadFile("task.json")
	s, err := task.Decode(strings.NewReader(string(data)))
	if err != nil {
		return "", err
	}
	s.Backend, s.Model, s.Limits.MaxUSD = "docker", o.Model, o.MaxUSD
	s.Repository, s.Ref, s.Verifier = "repo", "HEAD", "harness.verify.sh"
	verifier, _ := normalizetags.Files.ReadFile("verify.sh")
	if o.Repository != "" {
		if o.Goal == "" || o.Image == "" || o.Verifier == "" {
			return "", errors.New("--repo requires --goal, --image, and --verifier (your independent check script)")
		}
		s.Repository, err = filepath.Abs(o.Repository)
		if err != nil {
			return "", err
		}
		s.Ref, err = Git(ctx, s.Repository, "rev-parse", "--verify", "--end-of-options", o.Ref+"^{commit}")
		if err != nil {
			return "", err
		}
		info, err := os.Stat(o.Verifier)
		if err != nil {
			return "", err
		}
		if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > 1<<20 {
			return "", errors.New("verifier must be a regular file of 1 byte..1 MiB")
		}
		verifier, err = os.ReadFile(o.Verifier)
		if err != nil {
			return "", err
		}
		s.Name, s.Goal, s.Image = filepath.Base(s.Repository), o.Goal, o.Image
	} else if o.Goal != "" || o.Image != "" || o.Verifier != "" || o.Ref != "HEAD" {
		return "", errors.New("--goal, --image, --verifier, and --ref require --repo; omit them for the bundled demo")
	}
	if err := s.Validate(); err != nil {
		return "", err
	}
	if _, err := model.Pricing(s.Model); err != nil {
		return "", err
	}
	dir, err := filepath.Abs(o.Directory)
	if err != nil {
		return "", err
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		return "", fmt.Errorf("choose a new task directory; existing files are never overwritten: %w", err)
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.RemoveAll(dir)
		}
	}()
	write := func(name string, b []byte) error { return os.WriteFile(filepath.Join(dir, name), b, 0600) }
	if o.Repository == "" {
		repo := filepath.Join(dir, "repo")
		if err := os.Mkdir(repo, 0700); err != nil {
			return "", err
		}
		source, _ := normalizetags.Files.ReadFile("tags.js")
		if err := os.WriteFile(filepath.Join(repo, "tags.js"), source, 0644); err != nil {
			return "", err
		}
		for _, args := range [][]string{{"init", "--template="}, {"add", "--", "tags.js"}, {"-c", "user.name=Harness", "-c", "user.email=harness@example.invalid", "commit", "-m", "Add tag normalization bug fixture"}} {
			if _, err := Git(ctx, repo, args...); err != nil {
				return "", err
			}
		}
	}
	data, err = json.MarshalIndent(s, "", "  ")
	if err != nil {
		return "", err
	}
	for name, contents := range map[string][]byte{
		"harness.task.json": append(data, '\n'), "harness.verify.sh": verifier,
		".gitignore": []byte(".harness/\n.env\n.env.*\n"),
	} {
		if err := write(name, contents); err != nil {
			return "", err
		}
	}
	complete = true
	return dir, nil
}
