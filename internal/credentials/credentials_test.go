package credentials

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

func isolatedConfig(t *testing.T) string {
	t.Helper()
	t.Setenv("OPENAI_API_KEY", "")
	base := t.TempDir()
	t.Setenv("HOME", base)
	t.Setenv("XDG_CONFIG_HOME", base)
	path, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCredentialPrecedenceAndRedaction(t *testing.T) {
	path := isolatedConfig(t)
	if err := Save(path, "saved-test-key", false); err != nil {
		t.Fatal(err)
	}
	state := t.TempDir()
	if err := os.Chmod(state, 0700); err != nil {
		t.Fatal(err)
	}
	key, err := Resolve("", state)
	if err != nil || key.Value != "saved-test-key" {
		t.Fatalf("saved source: %+v %v", key.Source, err)
	}
	legacy := filepath.Join(state, "openai-key")
	if err := Save(legacy, "legacy-test-key", false); err != nil {
		t.Fatal(err)
	}
	key, err = Resolve("", state)
	if err != nil || key.Source != "state_file" {
		t.Fatalf("legacy: %s %v", key.Source, err)
	}
	explicit := filepath.Join(t.TempDir(), "private", "key")
	if err := Save(explicit, "explicit-test-key", false); err != nil {
		t.Fatal(err)
	}
	key, err = Resolve(explicit, state)
	if err != nil || key.Source != "explicit_file" {
		t.Fatalf("explicit: %s %v", key.Source, err)
	}
	t.Setenv("OPENAI_API_KEY", "environment-test-key")
	key, err = Resolve("missing", state)
	if err != nil || key.Source != "environment" {
		t.Fatalf("env: %s %v", key.Source, err)
	}
	b, err := json.Marshal(key)
	if err != nil || strings.Contains(string(b), "environment-test-key") {
		t.Fatal("key leaked in status JSON")
	}
	t.Setenv("OPENAI_API_KEY", "")
	if _, err := Resolve("missing", state); err == nil {
		t.Fatal("explicit missing file must not fall back")
	}
	if err := os.Chmod(legacy, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve("", state); err == nil {
		t.Fatal("insecure legacy file must not fall back")
	}
}

func TestPrivateAtomicStorage(t *testing.T) {
	path := isolatedConfig(t)
	if err := Save(path, "first-test-key", false); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, "second-test-key", false); err == nil {
		t.Fatal("overwrote without explicit replace")
	}
	if err := Save(path, "second-test-key", true); err != nil {
		t.Fatal(err)
	}
	value, err := ReadFile(path)
	if err != nil || value != "second-test-key" {
		t.Fatal("replacement not saved")
	}
	for p, mode := range map[string]os.FileMode{path: 0600, filepath.Dir(path): 0700} {
		info, err := os.Stat(p)
		if err != nil || info.Mode().Perm() != mode {
			t.Fatalf("unexpected permission for %s", p)
		}
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFile(link); err == nil {
		t.Fatal("read symlink")
	}
	if err := Save(link, "other-test-key", true); err == nil {
		t.Fatal("replaced symlink")
	}
	if runtime.GOOS != "windows" {
		fifo := filepath.Join(t.TempDir(), "fifo")
		if err := syscall.Mkfifo(fifo, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadFile(fifo); err == nil {
			t.Fatal("accepted FIFO")
		}
	}
	for _, bad := range []string{"", "a b", "a\nb", "a\x00b", strings.Repeat("a", MaxKeyBytes+1)} {
		if err := Save(path, bad, true); err == nil {
			t.Fatal("accepted invalid credential")
		}
	}
}
