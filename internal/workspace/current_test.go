package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCandidatePublicationPreservesPreviousWorkspace(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	source := filepath.Join(root, "workspace")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(source, "old"), []byte("baseline"), 0600)
	w := Workspace{Root: root, Path: source}
	if got, err := Current(root); err != nil || got != source {
		t.Fatalf("initial pointer: %s %v", got, err)
	}
	s, err := w.SnapshotTo(ctx, filepath.Join(root, "snapshots"))
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "snapshots", s.SHA256+".tar")
	if err := w.AcceptSnapshot(ctx, archive, s); err != nil {
		t.Fatal(err)
	}
	if got, err := Current(root); err != nil || got != w.Path || got == source {
		t.Fatalf("published pointer: %s %v", got, err)
	}
	if b, _ := os.ReadFile(filepath.Join(source, "old")); string(b) != "baseline" {
		t.Fatal("destroyed previous workspace")
	}
	previous := w.Path
	bad := s
	bad.SHA256 = "../../outside"
	if err := w.AcceptSnapshot(ctx, archive, bad); err == nil {
		t.Fatal("accepted bad digest")
	}
	if got, _ := Current(root); got != previous {
		t.Fatal("changed pointer after failed publication")
	}
	if err := w.AcceptSnapshot(ctx, archive, s); err != nil {
		t.Fatal("duplicate publication:", err)
	}
}
