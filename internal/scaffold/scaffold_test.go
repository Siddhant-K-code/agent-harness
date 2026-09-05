package scaffold

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Siddhant-K-code/agent-harness/internal/sandbox"
	"github.com/Siddhant-K-code/agent-harness/internal/task"
	"github.com/Siddhant-K-code/agent-harness/internal/workspace"
)

func TestDemoAndOwnRepository(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "demo with spaces")
	o := Options{Directory: dir, Ref: "HEAD", Model: "gpt-5.4", MaxUSD: 0.5}
	if _, err := Create(ctx, o); err != nil {
		t.Fatal(err)
	}
	s, err := task.Load(filepath.Join(dir, "harness.task.json"))
	if err != nil {
		t.Fatal(err)
	}
	base, err := Git(ctx, s.Repository, "rev-parse", "HEAD")
	if err != nil || len(base) != 40 {
		t.Fatal("demo has no real Git commit", err)
	}
	if s.Limits.MaxUSD != 0.5 || s.Backend != "docker" {
		t.Fatal("unexpected defaults")
	}
	if _, err := Create(ctx, o); err == nil {
		t.Fatal("overwrote existing directory")
	}
	if _, err := os.Stat(filepath.Join(dir, "repo", "tags.js")); err != nil {
		t.Fatal(err)
	}
	// Creating a task for an existing repository pins a commit and copies the
	// verifier; it does not silently include an uncommitted edit in that source.
	if err := os.WriteFile(filepath.Join(s.Repository, "uncommitted.txt"), []byte("local"), 0600); err != nil {
		t.Fatal(err)
	}
	own := filepath.Join(t.TempDir(), "task")
	o = Options{Directory: own, Repository: s.Repository, Ref: "HEAD", Goal: "Fix tag handling", Image: s.Image, Verifier: s.Verifier, Model: s.Model, MaxUSD: 0.25}
	if _, err := Create(ctx, o); err != nil {
		t.Fatal(err)
	}
	created, err := task.Load(filepath.Join(own, "harness.task.json"))
	if err != nil || created.Ref != base || created.Repository != s.Repository {
		t.Fatal("own repo task did not pin existing commit", err)
	}
	if created.Verifier == s.Verifier {
		t.Fatal("verifier was not copied")
	}
	if data, err := os.ReadFile(filepath.Join(s.Repository, "uncommitted.txt")); err != nil || string(data) != "local" {
		t.Fatal("modified source repository")
	}
}

func TestInvalidInitDoesNotLeavePartialTask(t *testing.T) {
	for _, o := range []Options{
		{Model: "unpriced", Ref: "HEAD", MaxUSD: 0.5},
		{Model: "gpt-5.4", Ref: "HEAD", MaxUSD: 0},
		{Model: "gpt-5.4", Ref: "HEAD", MaxUSD: 0.5, Repository: "missing"},
	} {
		o.Directory = filepath.Join(t.TempDir(), "task")
		if _, err := Create(context.Background(), o); err == nil {
			t.Fatal("accepted invalid setup")
		}
		if _, err := os.Stat(o.Directory); !os.IsNotExist(err) {
			t.Fatal("left partial setup")
		}
	}
}

func TestBundledVerifierRejectsBugAndAcceptsRecordedRealPatch(t *testing.T) {
	if os.Getenv("HARNESS_DOCKER_TEST") != "1" {
		t.Skip("set HARNESS_DOCKER_TEST=1 for real bundled-demo verification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	base, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// Colima shares the checkout, but need not share macOS's temp directory.
	shared, err := os.MkdirTemp(base, ".docker-test-package-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(shared)
	dir := filepath.Join(shared, "demo")
	if _, err := Create(ctx, Options{Directory: dir, Ref: "HEAD", Model: "gpt-5.4", MaxUSD: 0.5}); err != nil {
		t.Fatal(err)
	}
	spec, err := task.Load(filepath.Join(dir, "harness.task.json"))
	if err != nil {
		t.Fatal(err)
	}
	w, err := workspace.Prepare(ctx, filepath.Join(shared, "run"), spec.Repository, spec.Ref)
	if err != nil {
		t.Fatal(err)
	}
	image, err := sandbox.Check(ctx, spec.Image)
	if err != nil {
		t.Fatal(err)
	}
	executor := sandbox.Docker{Workspace: w.Path, Image: image}
	verifier, err := os.ReadFile(spec.Verifier)
	if err != nil {
		t.Fatal(err)
	}
	result, err := executor.Exec(ctx, string(verifier), true)
	if err != nil || result.ExitCode == 0 || !strings.Contains(result.Stderr, "AssertionError") {
		t.Fatalf("bug must fail independent checks: %+v %v", result, err)
	}
	// This patch is evidence from a previous real GPT-5.4 run. No new model call.
	patch, err := filepath.Abs("../../docs/evidence/2026-09-05/changes.patch")
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"apply", "--", patch}, {"add", "--", "."}, {"-c", "user.name=Harness", "-c", "user.email=harness@example.invalid", "commit", "-m", "Apply recorded real model patch"}} {
		if _, err := Git(ctx, spec.Repository, args...); err != nil {
			t.Fatalf("prepare recorded candidate: %v", err)
		}
	}
	// Mount a fresh candidate: Colima's host file sharing may retain stale
	// entries when Git atomically replaces files in a previously mounted tree.
	w, err = workspace.Prepare(ctx, filepath.Join(shared, "patched-run"), spec.Repository, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	executor.Workspace = w.Path
	result, err = executor.Exec(ctx, string(verifier), true)
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("corrected fixture must pass: %+v %v", result, err)
	}
}
