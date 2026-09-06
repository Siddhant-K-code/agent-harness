package integrations

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Siddhant-K-code/agent-harness/internal/process"
)

// gh obtains credentials from its own environment/keychain. The broker neither
// asks gh to print a token nor copies credentials into the task workspace.
func gh(ctx context.Context, args ...string) (process.Result, error) {
	env := []string{"GH_HOST=github.com", "GH_PROMPT_DISABLED=1", "GH_PAGER=cat", "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null"}
	for _, k := range []string{"PATH", "HOME", "XDG_CONFIG_HOME", "GH_CONFIG_DIR", "GH_TOKEN", "GITHUB_TOKEN", "SSL_CERT_FILE", "SSL_CERT_DIR"} {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	res, err := process.Run(ctx, "", env, nil, 64<<10, "gh", args...)
	if err != nil || res.ExitCode != 0 {
		return process.Result{}, errors.New("GitHub request failed; check gh auth status --hostname github.com and repository access")
	}
	return res, nil
}
func GitHubStatus(ctx context.Context) error {
	_, err := gh(ctx, "api", "--hostname", "github.com", "--method", "GET", "user", "--jq", ".login")
	return err
}

func GitHubRead(ctx context.Context, repo, resource string, number int) (process.Result, error) {
	if !Repository(repo) {
		return process.Result{}, errors.New("expected owner/repository")
	}
	base := "repos/" + repo
	endpoint := ""
	if resource != "issues" && resource != "pulls" && (number < 1 || number > 100000000) {
		return process.Result{}, errors.New("a positive issue or PR number is required")
	}
	n := strconv.Itoa(number)
	switch resource {
	case "issues":
		endpoint = base + "/issues?state=open&per_page=30"
	case "issue":
		endpoint = base + "/issues/" + n
	case "pulls":
		endpoint = base + "/pulls?state=open&per_page=30"
	case "pull":
		endpoint = base + "/pulls/" + n
	case "diff":
		endpoint = base + "/pulls/" + n
	case "checks":
		// Resolve the head through the selected repository; never accept an API URL from the model.
		head, err := gh(ctx, "api", "--hostname", "github.com", "--method", "GET", base+"/pulls/"+n, "--jq", ".head.sha")
		if err != nil {
			return head, err
		}
		sha := strings.TrimSpace(head.Output)
		if len(sha) != 40 || strings.Trim(sha, "0123456789abcdef") != "" {
			return process.Result{}, errors.New("invalid PR head")
		}
		endpoint = base + "/commits/" + sha + "/check-runs?per_page=50"
	default:
		return process.Result{}, errors.New("resource must be issues, issue, pulls, pull, diff, or checks")
	}
	accept := "application/vnd.github+json"
	if resource == "diff" {
		accept = "application/vnd.github.diff"
	}
	return gh(ctx, "api", "--hostname", "github.com", "--method", "GET", "-H", "Accept: "+accept, endpoint)
}

// Clone uses an isolated per-command Git configuration and gh's credential helper.
// It never changes global credential configuration or places a token in a URL.
func Clone(ctx context.Context, repo, dest string) error {
	if !Repository(repo) || dest == "" {
		return errors.New("clone requires owner/repository and a new destination")
	}
	dest, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	if _, err = os.Lstat(dest); !errors.Is(err, os.ErrNotExist) {
		return errors.New("clone destination must not exist")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	env := []string{"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_LFS_SKIP_SMUDGE=1", "GH_HOST=github.com", "GH_PROMPT_DISABLED=1"}
	for _, k := range []string{"PATH", "HOME", "XDG_CONFIG_HOME", "GH_CONFIG_DIR", "GH_TOKEN", "GITHUB_TOKEN", "SSL_CERT_FILE", "SSL_CERT_DIR"} {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	res, e := process.Run(ctx, "", env, nil, 32<<10, "git", "-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "credential.helper=", "-c", "credential.https://github.com.helper=!gh auth git-credential", "-c", "http.followRedirects=false", "clone", "--", "https://github.com/"+repo+".git", dest)
	if e != nil || res.ExitCode != 0 {
		return fmt.Errorf("GitHub clone failed; check gh auth status and repository access (any partial checkout remains at %s)", dest)
	}
	return nil
}
