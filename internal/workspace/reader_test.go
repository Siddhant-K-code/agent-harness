package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReaderPinsCommitAndCannotFollowLinksOrReadUntrackedFiles(t *testing.T) {
	ctx := context.Background()
	repo := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		if _, e := git(ctx, repo, args...); e != nil {
			t.Fatal(e)
		}
	}
	run("init")
	os.WriteFile(filepath.Join(repo, "file.txt"), []byte("committed\n"), 0600)
	os.Symlink("/etc/passwd", filepath.Join(repo, "link"))
	os.WriteFile(filepath.Join(repo, "large.txt"), []byte(strings.Repeat("x", (256<<10)+1)), 0600)
	run("add", ".")
	run("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-m", "base")
	base, e := ResolveCommit(ctx, repo, "HEAD")
	if e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(repo, "file.txt"), []byte("new head\n"), 0600)
	run("add", "file.txt")
	run("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-m", "new")
	os.WriteFile(filepath.Join(repo, "secret"), []byte("untracked"), 0600)
	r, e := OpenReader(ctx, repo, base)
	if e != nil {
		t.Fatal(e)
	}
	text, e := r.Read(ctx, "file.txt")
	if e != nil || text != "committed\n" {
		t.Fatalf("pin lost: %q %v", text, e)
	}
	for _, p := range []string{"link", "secret", "large.txt", "../secret", "/etc/passwd", ".git/config", "a/../file.txt"} {
		if _, e := r.Read(ctx, p); e == nil {
			t.Fatalf("read unsafe/unsupported %q", p)
		}
	}
	for _, p := range []string{"", "../x", "a\nb", "a\\b", "/x", "a/.git/x"} {
		if ValidReadPath(p) {
			t.Fatalf("accepted %q", p)
		}
	}
	if _, e = ResolveCommit(ctx, repo, "--help"); e == nil {
		t.Fatal("option accepted as commit")
	}
}
