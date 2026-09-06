package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Siddhant-K-code/agent-harness/internal/process"
	"github.com/Siddhant-K-code/agent-harness/internal/workspace"
)

type GitHub struct{}

// Only host Git/gh subprocesses receive GitHub credentials. Local patch preparation
// uses a separate environment with no credentials, hooks, filters or global config.
func hostEnvironment() []string {
	env := []string{"GH_HOST=github.com", "GH_PROMPT_DISABLED=1", "GH_PAGER=cat", "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_LFS_SKIP_SMUDGE=1"}
	for _, k := range []string{"PATH", "HOME", "XDG_CONFIG_HOME", "GH_CONFIG_DIR", "GH_TOKEN", "GITHUB_TOKEN", "SSL_CERT_FILE", "SSL_CERT_DIR"} {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	return env
}
func githubJSON(ctx context.Context, method, endpoint string, input any, output any) error {
	args := []string{"api", "--hostname", "github.com", "--method", method, "-H", "Accept: application/vnd.github+json", endpoint}
	var stdin io.Reader
	if input != nil {
		b, err := json.Marshal(input)
		if err != nil {
			return err
		}
		stdin = strings.NewReader(string(b))
		args = append(args, "--input", "-")
	}
	r, err := process.Run(ctx, "", hostEnvironment(), stdin, 512<<10, "gh", args...)
	if err != nil || r.ExitCode != 0 || r.Truncated {
		return errors.New("GitHub request failed or its acknowledgement was lost; check gh auth status and repository access")
	}
	if err := json.Unmarshal([]byte(r.Output), output); err != nil {
		return errors.New("invalid GitHub response")
	}
	return nil
}
func (GitHub) Identity(ctx context.Context) (string, error) {
	var user struct {
		Login string `json:"login"`
	}
	err := githubJSON(ctx, "GET", "user", nil, &user)
	return user.Login, err
}
func (GitHub) Repository(ctx context.Context, repo string) (Repository, error) {
	var r Repository
	err := githubJSON(ctx, "GET", "repos/"+repo, nil, &r)
	return r, err
}
func (GitHub) Branch(ctx context.Context, repo, branch string) (string, error) {
	var refs []struct {
		Ref    string `json:"ref"`
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	err := githubJSON(ctx, "GET", "repos/"+repo+"/git/matching-refs/heads/"+url.PathEscape(branch), nil, &refs)
	if err != nil {
		return "", err
	}
	for _, ref := range refs {
		if ref.Ref == "refs/heads/"+branch {
			if !validCommit.MatchString(ref.Object.SHA) {
				return "", errors.New("invalid remote commit")
			}
			return ref.Object.SHA, nil
		}
	}
	return "", nil
}
func (GitHub) Push(ctx context.Context, w workspace.Workspace, a Approval) error {
	args := []string{"--git-dir=" + w.GitDir, "-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "credential.helper=", "-c", "credential.https://github.com.helper=!gh auth git-credential", "-c", "http.followRedirects=false", "push", "--porcelain", "--force-with-lease=refs/heads/" + a.Branch + ":", "--", "https://github.com/" + a.Repository + ".git", a.Commit + ":refs/heads/" + a.Branch}
	r, err := process.Run(ctx, "", hostEnvironment(), nil, 64<<10, "git", args...)
	if err != nil || r.ExitCode != 0 || r.Truncated {
		return errors.New("branch push failed or its acknowledgement was lost; reconcile before retrying")
	}
	return nil
}
func marker(a Approval) string {
	return "<!-- harness-run:" + a.RunID + " patch:" + a.PatchSHA256 + " -->"
}
func (GitHub) FindPR(ctx context.Context, a Approval) (*PullRequest, error) {
	q := url.Values{"state": {"all"}, "head": {strings.Split(a.Repository, "/")[0] + ":" + a.Branch}, "base": {a.BaseBranch}, "per_page": {"100"}}
	var prs []PullRequest
	if err := githubJSON(ctx, "GET", "repos/"+a.Repository+"/pulls?"+q.Encode(), nil, &prs); err != nil {
		return nil, err
	}
	if len(prs) >= 100 {
		return nil, errors.New("too many matching PRs; inspect GitHub manually")
	}
	var found *PullRequest
	for i := range prs {
		if prs[i].Head.SHA != a.Commit || !strings.Contains(prs[i].Body, marker(a)) || found != nil {
			return nil, errors.New("an existing PR conflicts with the approved delivery")
		}
		found = &prs[i]
	}
	return found, nil
}
func (GitHub) CreatePR(ctx context.Context, a Approval) (PullRequest, error) {
	var pr PullRequest
	err := githubJSON(ctx, "POST", "repos/"+a.Repository+"/pulls", map[string]any{"title": a.Title, "body": a.Body, "head": a.Branch, "base": a.BaseBranch, "draft": true, "maintainer_can_modify": false}, &pr)
	return pr, err
}

func localGit(ctx context.Context, w workspace.Workspace, stdin io.Reader, extraEnv []string, args ...string) (string, error) {
	env := append([]string{"PATH=" + os.Getenv("PATH"), "HOME=/nonexistent", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0"}, extraEnv...)
	options := []string{"--git-dir=" + w.GitDir, "--work-tree=" + w.Path, "-c", "core.bare=false", "-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "commit.gpgsign=false"}
	r, err := process.Run(ctx, w.Path, env, stdin, workspace.MaxPatchBytes, "git", append(options, args...)...)
	if err != nil || r.ExitCode != 0 || r.Truncated {
		return "", fmt.Errorf("prepare approved Git commit failed: %v (exit %d)", err, r.ExitCode)
	}
	return r.Output, nil
}
func createCommit(ctx context.Context, w workspace.Workspace, patch, title string, created time.Time) (string, error) {
	if _, err := localGit(ctx, w, strings.NewReader(patch), nil, "apply", "--index", "--binary", "-"); err != nil {
		return "", err
	}
	actual, err := localGit(ctx, w, nil, nil, "diff", "--cached", "--binary", "--no-ext-diff", "--no-textconv", w.Base, "--")
	if err != nil {
		return "", err
	}
	// Diff whitespace is patch data, including trailing spaces and context lines.
	if actual != patch {
		return "", errors.New("staged diff does not match the verified patch")
	}
	env := []string{"GIT_AUTHOR_NAME=Agent Harness", "GIT_AUTHOR_EMAIL=harness@users.noreply.github.com", "GIT_COMMITTER_NAME=Agent Harness", "GIT_COMMITTER_EMAIL=harness@users.noreply.github.com", "GIT_AUTHOR_DATE=" + created.UTC().Format(time.RFC3339), "GIT_COMMITTER_DATE=" + created.UTC().Format(time.RFC3339)}
	if _, err := localGit(ctx, w, nil, env, "commit", "--no-gpg-sign", "-m", title); err != nil {
		return "", err
	}
	commit, err := localGit(ctx, w, nil, nil, "rev-parse", "HEAD")
	return strings.TrimSpace(commit), err
}
