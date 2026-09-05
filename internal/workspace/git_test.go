package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommittedSnapshotPatchAndSourceIsolation(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	if _, err := git(ctx, source, "init"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "a.txt"), []byte("base\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := git(ctx, source, "add", "a.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := git(ctx, source, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-m", "base"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "a.txt"), []byte("uncommitted\n"), 0600); err != nil {
		t.Fatal(err)
	}
	w, err := Prepare(ctx, filepath.Join(t.TempDir(), "run"), source, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(w.Path, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "base\n" {
		t.Fatal("copied uncommitted state")
	}
	if _, err = os.Stat(filepath.Join(w.Path, ".git")); !os.IsNotExist(err) {
		t.Fatal("agent can access Git database")
	}
	if err = os.WriteFile(filepath.Join(w.Path, "a.txt"), []byte("fixed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(w.Path, "new.txt"), []byte("new\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink("/etc/passwd", filepath.Join(w.Path, "link")); err != nil {
		t.Fatal(err)
	}
	patch, err := w.Patch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"+fixed", "+new", "new file mode 120000", "+/etc/passwd"} {
		if !strings.Contains(patch, s) {
			t.Fatalf("missing %s in patch", s)
		}
	}
	b, err = os.ReadFile(filepath.Join(source, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "uncommitted\n" {
		t.Fatal("source changed")
	}
}
