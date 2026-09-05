package workspace

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestSnapshotRoundTripAndChecksum(t *testing.T) {
	base := t.TempDir()
	source := filepath.Join(base, "source")
	if err := os.MkdirAll(filepath.Join(source, "bin"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "bin", "run"), []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "new.txt"), []byte("untracked\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("bin/run", filepath.Join(source, "link")); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(base, "snapshots")
	s, err := (Workspace{Path: source}).SnapshotTo(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(dir, s.SHA256+".tar")
	destination := filepath.Join(base, "restored")
	if err := RestoreSnapshot(context.Background(), archive, destination, s); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(destination, "link"))
	if err != nil || string(b) != "#!/bin/sh\nexit 0\n" {
		t.Fatalf("link roundtrip: %q %v", b, err)
	}
	info, _ := os.Stat(filepath.Join(destination, "bin", "run"))
	if info.Mode().Perm()&0100 == 0 {
		t.Fatal("lost executable bit")
	}
	if b, _ := os.ReadFile(filepath.Join(destination, "new.txt")); string(b) != "untracked\n" {
		t.Fatal("lost untracked file")
	}
	if err := os.WriteFile(archive, []byte("corrupted"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := RestoreSnapshot(context.Background(), archive, filepath.Join(base, "bad"), s); err == nil {
		t.Fatal("accepted corrupted snapshot")
	}
}

func TestSnapshotRejectsExternalLinksAndOversize(t *testing.T) {
	source := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret")
	os.WriteFile(outside, []byte("private"), 0600)
	os.Symlink(outside, filepath.Join(source, "link"))
	if _, err := (Workspace{Path: source}).SnapshotTo(context.Background(), t.TempDir()); err == nil {
		t.Fatal("accepted external symlink")
	}
	os.Remove(filepath.Join(source, "link"))
	f, err := os.Create(filepath.Join(source, "large"))
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(MaxSnapshotBytes + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if _, err := (Workspace{Path: source}).SnapshotTo(context.Background(), t.TempDir()); err == nil {
		t.Fatal("accepted oversized content")
	}
}

func TestRestoreRejectsUnsafeArchive(t *testing.T) {
	for _, header := range []*tar.Header{
		{Name: "../outside", Typeflag: tar.TypeReg, Mode: 0600},
		{Name: "/absolute", Typeflag: tar.TypeReg, Mode: 0600},
		{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "../outside"},
		{Name: "device", Typeflag: tar.TypeFifo},
		{Name: ".git/config", Typeflag: tar.TypeReg, Mode: 0600},
	} {
		t.Run(header.Name, func(t *testing.T) {
			var b bytes.Buffer
			tw := tar.NewWriter(&b)
			if err := tw.WriteHeader(header); err != nil {
				t.Fatal(err)
			}
			tw.Close()
			h := sha256.Sum256(b.Bytes())
			s := Snapshot{SHA256: hex.EncodeToString(h[:]), Bytes: int64(b.Len()), Files: 1}
			base := t.TempDir()
			file := filepath.Join(base, "input.tar")
			os.WriteFile(file, b.Bytes(), 0600)
			destination := filepath.Join(base, "output")
			if err := RestoreSnapshot(context.Background(), file, destination, s); err == nil {
				t.Fatal("accepted unsafe archive")
			}
			if _, err := os.Stat(destination); !os.IsNotExist(err) {
				t.Fatal("left partially restored workspace")
			}
		})
	}
}
