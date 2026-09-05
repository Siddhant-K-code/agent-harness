package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRealDockerIsolationAndTimeout(t *testing.T) {
	if os.Getenv("HARNESS_DOCKER_TEST") != "1" {
		t.Skip("set HARNESS_DOCKER_TEST=1 to run real Docker integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	image, err := Check(ctx, "node:22-alpine")
	if err != nil {
		t.Fatal(err)
	}
	// Use the checkout: Colima need not share the host's system temp directory.
	base, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(base, ".docker-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	d := Docker{Workspace: dir, Image: image}
	t.Setenv("OPENAI_API_KEY", "test-host-key-must-not-enter-container")
	r, err := d.Exec(ctx, `set -eu
test -z "${OPENAI_API_KEY:-}"
test ! -e /var/run/docker.sock
test ! -e /workspace/.git
test "$(id -u)" != 0
! touch /root-should-be-readonly
echo persisted > result.txt
node -e "const fs=require('fs'); if(fs.readdirSync('/sys/class/net').filter(x=>x!=='lo').length) process.exit(1)"
`, false)
	if err != nil || r.ExitCode != 0 {
		t.Fatalf("isolation checks: %+v %v", r, err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "result.txt"))
	if err != nil || string(b) != "persisted\n" {
		t.Fatalf("real edit missing: %q %v", b, err)
	}
	r, err = d.Exec(ctx, "cat result.txt; echo bad > result.txt", true)
	if err != nil || r.ExitCode == 0 {
		t.Fatalf("readonly verification allowed writes: %+v %v", r, err)
	}
	short, stop := context.WithTimeout(ctx, 2*time.Second)
	_, err = d.Exec(short, "sleep 10; echo leaked > leaked.txt", false)
	stop()
	if err == nil {
		t.Fatal("timeout was ignored")
	}
	r, err = d.Exec(ctx, "test ! -e leaked.txt; printf 'clean'", true)
	if err != nil || r.ExitCode != 0 || !strings.Contains(r.Output, "clean") {
		t.Fatalf("timeout cleanup: %+v %v", r, err)
	}
	// The very same independent verifier used in the live example must reject
	// the original buggy implementation, not simply pass any runnable program.
	source, err := os.ReadFile("../../examples/normalize-tags/tags.js")
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := os.ReadFile("../../examples/normalize-tags/verify.sh")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "tags.js"), source, 0600); err != nil {
		t.Fatal(err)
	}
	r, err = d.Exec(ctx, string(verifier), true)
	if err != nil || r.ExitCode == 0 || !strings.Contains(r.Stderr, "AssertionError") {
		t.Fatalf("verifier accepted the known bug: %+v %v", r, err)
	}
}
