// Package credentials keeps BYOK configuration on the controller machine.
package credentials

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const MaxKeyBytes = 4096

// Key deliberately hides the value from JSON status/diagnostic output.
type Key struct {
	Value  string `json:"-"`
	Source string `json:"source"`
	Path   string `json:"path,omitempty"`
}

func Path() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "agent-harness", "openai-key"), nil
}

func Validate(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > MaxKeyBytes {
		return "", errors.New("API key must contain 1..4096 bytes")
	}
	for _, c := range value {
		if c <= ' ' || c >= 127 {
			return "", errors.New("API key must be a single ASCII token without whitespace")
		}
	}
	return value, nil
}

func ReadFile(path string) (string, error) {
	// Check the opened object, not a pathname inspected before opening it.
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", fmt.Errorf("cannot open API key file: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return "", errors.New("API key file must be a regular file readable only by its owner (chmod 600)")
	}
	b, err := io.ReadAll(io.LimitReader(f, MaxKeyBytes+1))
	if err != nil || len(b) > MaxKeyBytes {
		return "", errors.New("cannot load API key file (maximum 4096 bytes)")
	}
	return Validate(string(b))
}

// Resolve preserves the original environment and per-run file precedence.
// An explicitly selected or present but invalid file never silently falls back.
func Resolve(explicit, stateDir string) (Key, error) {
	if value := strings.TrimSpace(os.Getenv("OPENAI_API_KEY")); value != "" {
		value, err := Validate(value)
		return Key{Value: value, Source: "environment"}, err
	}
	if explicit != "" {
		value, err := ReadFile(explicit)
		return Key{Value: value, Source: "explicit_file", Path: explicit}, err
	}
	legacy := filepath.Join(stateDir, "openai-key")
	if _, err := os.Lstat(legacy); !errors.Is(err, os.ErrNotExist) {
		value, err := ReadFile(legacy)
		return Key{Value: value, Source: "state_file", Path: legacy}, err
	}
	path, err := Path()
	if err != nil {
		return Key{}, err
	}
	value, err := ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		err = errors.New("no OpenAI key configured; run harness auth login or set OPENAI_API_KEY")
	}
	return Key{Value: value, Source: "user_config", Path: path}, err
}

// Save writes a private, plaintext file atomically. CLI login and authenticated local web setup call it.
func Save(path, value string, replace bool) error {
	value, err := Validate(value)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return errors.New("credential directory must be a private directory (chmod 700), not a symlink")
	}
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return errors.New("refusing to replace a non-regular credential file")
		}
		if !replace {
			return errors.New("a saved key already exists; use harness auth login --replace to replace it")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	f, err := os.CreateTemp(dir, ".openai-key-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, writeErr := io.WriteString(f, value)
	err = errors.Join(writeErr, f.Sync(), f.Close())
	if err != nil {
		return err
	}
	if replace {
		return os.Rename(f.Name(), path)
	}
	// Link installs without overwriting a key created by a concurrent login.
	return os.Link(f.Name(), path)
}
