package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Siddhant-K-code/agent-harness/internal/process"
)

var objectID = regexp.MustCompile(`^(?:[a-f0-9]{40}|[a-f0-9]{64})$`)

// Reader reads committed Git blobs without checkout, hooks, filters, shell
// commands, host credentials, or following repository symlinks.
type Reader struct {
	Repository, Base string
	Files            []File
}
type File struct {
	Path, Object string
	Size         int64
}

func readGit(ctx context.Context, repo string, limit int, args ...string) (string, error) {
	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=/nonexistent", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_NO_REPLACE_OBJECTS=1", "GIT_OPTIONAL_LOCKS=0"}
	a := append([]string{"-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false"}, args...)
	r, err := process.Run(ctx, repo, env, nil, limit, "git", a...)
	if err != nil {
		return "", err
	}
	if r.ExitCode != 0 || r.Truncated {
		return "", errors.New("cannot read committed repository data (missing object or size limit)")
	}
	return r.Output, nil
}

func ResolveCommit(ctx context.Context, repo, ref string) (string, error) {
	s, err := readGit(ctx, repo, 1024, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	s = strings.TrimSpace(s)
	if err != nil {
		return "", err
	}
	if !objectID.MatchString(s) {
		return "", errors.New("invalid repository commit")
	}
	return s, nil
}

func OpenReader(ctx context.Context, repo, base string) (*Reader, error) {
	if !objectID.MatchString(base) {
		return nil, errors.New("reader requires a pinned commit")
	}
	s, err := readGit(ctx, repo, 8<<20, "ls-tree", "-r", "-z", "-l", base)
	if err != nil {
		return nil, err
	}
	r := &Reader{Repository: repo, Base: base}
	for _, row := range strings.Split(s, "\x00") {
		meta, p, ok := strings.Cut(row, "\t")
		if !ok {
			continue
		}
		f := strings.Fields(meta)
		if len(f) != 4 || (f[0] != "100644" && f[0] != "100755") || f[1] != "blob" || !objectID.MatchString(f[2]) || !ValidReadPath(p) {
			continue
		}
		n, err := strconv.ParseInt(f[3], 10, 64)
		if err != nil || n < 0 {
			return nil, errors.New("invalid blob size")
		}
		r.Files = append(r.Files, File{p, f[2], n})
	}
	return r, nil
}

func ValidReadPath(p string) bool {
	if p == "" || len(p) > 4096 || !utf8.ValidString(p) || path.IsAbs(p) || path.Clean(p) != p || p == ".." || strings.HasPrefix(p, "../") || strings.Contains(p, "\\") {
		return false
	}
	for _, c := range p {
		if unicode.IsControl(c) {
			return false
		}
	}
	for _, c := range strings.Split(p, "/") {
		if c == ".git" {
			return false
		}
	}
	return true
}

func (r *Reader) Read(ctx context.Context, p string) (string, error) {
	if !ValidReadPath(p) {
		return "", errors.New("invalid repository-relative path")
	}
	for _, f := range r.Files {
		if f.Path != p {
			continue
		}
		if f.Size > 256<<10 {
			return "", errors.New("file exceeds 256 KiB text preview limit")
		}
		s, err := readGit(ctx, r.Repository, 256<<10, "cat-file", "blob", f.Object)
		if err != nil {
			return "", err
		}
		if int64(len(s)) != f.Size || !utf8.ValidString(s) || strings.ContainsRune(s, 0) {
			return "", errors.New("file is not supported UTF-8 text")
		}
		return s, nil
	}
	return "", fmt.Errorf("no regular tracked file at %q in this commit", p)
}
