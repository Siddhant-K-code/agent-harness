package delivery

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Siddhant-K-code/agent-harness/internal/runner"
	"github.com/Siddhant-K-code/agent-harness/internal/scaffold"
	"github.com/Siddhant-K-code/agent-harness/internal/statefile"
	"github.com/Siddhant-K-code/agent-harness/internal/store"
	"github.com/Siddhant-K-code/agent-harness/internal/task"
	"github.com/Siddhant-K-code/agent-harness/internal/workspace"
)

// Fault injection is confined to tests. Patch staging below uses real Git objects.
type remoteFixture struct {
	actor, base, branch       string
	pushes, posts             int
	losePush, losePR, revoked bool
	pr                        *PullRequest
	baseAfterCreate           string
}

func (r *remoteFixture) Identity(context.Context) (string, error) { return r.actor, nil }
func (r *remoteFixture) Repository(context.Context, string) (Repository, error) {
	v := Repository{Name: "owner/repo", DefaultBranch: "main"}
	v.Permissions.Push = !r.revoked
	return v, nil
}
func (r *remoteFixture) Branch(_ context.Context, _ string, branch string) (string, error) {
	if branch == "main" {
		return r.base, nil
	}
	return r.branch, nil
}
func (r *remoteFixture) Push(_ context.Context, _ workspace.Workspace, a Approval) error {
	r.pushes++
	if r.branch != "" {
		return errors.New("lease rejected")
	}
	r.branch = a.Commit
	if r.losePush {
		return errors.New("lost push response")
	}
	return nil
}
func (r *remoteFixture) FindPR(context.Context, Approval) (*PullRequest, error) { return r.pr, nil }
func (r *remoteFixture) CreatePR(_ context.Context, a Approval) (PullRequest, error) {
	r.posts++
	p := PullRequest{Number: 1, URL: "https://github.com/owner/repo/pull/1", Draft: true, Body: a.Body}
	p.Head.SHA = a.Commit
	p.Base.SHA = r.base
	if r.baseAfterCreate != "" {
		p.Base.SHA = r.baseAfterCreate
	}
	r.pr = &p
	if r.losePR {
		return PullRequest{}, errors.New("lost PR response")
	}
	return p, nil
}
func fixture(t *testing.T) (Service, *remoteFixture, string) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	dir := filepath.Join(t.TempDir(), "task")
	if _, err := scaffold.Create(ctx, scaffold.Options{Directory: dir, Ref: "HEAD", Model: "gpt-5.4", MaxUSD: .2}); err != nil {
		t.Fatal(err)
	}
	spec, err := task.Load(filepath.Join(dir, "harness.task.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = scaffold.Git(ctx, spec.Repository, "remote", "add", "origin", "https://github.com/owner/repo.git"); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(root, "harness.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	run, err := db.Create(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Start(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	runRoot := filepath.Join(root, "runs", run.ID)
	w, err := workspace.Prepare(ctx, runRoot, spec.Repository, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(w.Path, "tags.js"), []byte("module.exports = value => value;\n"), 0600); err != nil {
		t.Fatal(err)
	}
	patch, err := w.Patch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(runRoot, "changes.patch"), []byte(patch), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Finish(ctx, run.ID, store.Completed, "test fixture"); err != nil {
		t.Fatal(err)
	}
	report := runner.Report{RunID: run.ID, State: store.Completed, Verified: true, CleanupConfirmed: true, BaseCommit: w.Base, PatchSHA256: digest([]byte(patch)), VerifierSHA256: strings.Repeat("a", 64)}
	if err = statefile.Write(filepath.Join(runRoot, "report.json"), report); err != nil {
		t.Fatal(err)
	}
	remote := &remoteFixture{actor: "reviewer", base: w.Base}
	return Service{Root: root, DB: db, Remote: remote}, remote, run.ID
}
func TestPreviewApprovalAndIdempotentDelivery(t *testing.T) {
	s, remote, id := fixture(t)
	ctx := context.Background()
	p, err := s.Prepare(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if p.Patch == "" || p.Approval.BaseCommit != remote.base || p.Hash != statefile.Hash(p.Approval) || remote.pushes+remote.posts != 0 {
		t.Fatal("invalid preview or external mutation")
	}
	if _, err = s.Publish(ctx, id, strings.Repeat("0", 64)); err == nil {
		t.Fatal("wrong approval accepted")
	}
	r, err := s.Publish(ctx, id, p.Hash)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "complete" || !r.PR.Draft || remote.posts != 1 || remote.pushes != 1 {
		t.Fatal("delivery incomplete")
	}
	if _, err = s.Publish(ctx, id, p.Hash); err != nil {
		t.Fatal(err)
	}
	if remote.posts != 1 || remote.pushes != 1 {
		t.Fatal("repeated mutation")
	}
}
func TestRejectChangedApprovalInputs(t *testing.T) {
	for _, name := range []string{"base", "identity", "permissions", "patch", "report", "commit", "expiry", "existing-branch"} {
		t.Run(name, func(t *testing.T) {
			s, remote, id := fixture(t)
			ctx := context.Background()
			p, err := s.Prepare(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			switch name {
			case "base":
				remote.base = strings.Repeat("a", 40)
			case "identity":
				remote.actor = "another-user"
			case "permissions":
				remote.revoked = true
			case "patch":
				if err = os.WriteFile(filepath.Join(s.Root, "runs", id, "changes.patch"), []byte("changed"), 0600); err != nil {
					t.Fatal(err)
				}
			case "report":
				var r runner.Report
				path := filepath.Join(s.Root, "runs", id, "report.json")
				if err = statefile.Read(path, &r, 1<<20); err != nil {
					t.Fatal(err)
				}
				r.VerifierSHA256 = strings.Repeat("b", 64)
				if err = statefile.Write(path, r); err != nil {
					t.Fatal(err)
				}
			case "commit":
				w, _ := s.workspace(p.Record)
				if _, err = localGit(ctx, w, nil, nil, "reset", "--hard", w.Base); err != nil {
					t.Fatal(err)
				}
			case "expiry":
				p.Approval.ExpiresAt = time.Now().Add(-time.Minute)
				p.Hash = statefile.Hash(p.Approval)
				if err = s.save(p.Record); err != nil {
					t.Fatal(err)
				}
			case "existing-branch":
				remote.branch = p.Approval.Commit
			}
			if _, err = s.Publish(ctx, id, p.Hash); err == nil {
				t.Fatal("invalid approval accepted")
			}
			if remote.pushes+remote.posts != 0 {
				t.Fatal("external side effect after failed check")
			}
		})
	}
}
func TestLostAcknowledgementsReconcileWithoutDuplicate(t *testing.T) {
	for _, lost := range []string{"push", "pr"} {
		t.Run(lost, func(t *testing.T) {
			s, r, id := fixture(t)
			ctx := context.Background()
			p, err := s.Prepare(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			r.losePush = lost == "push"
			r.losePR = lost == "pr"
			if _, err = s.Publish(ctx, id, p.Hash); err == nil {
				t.Fatal("missing injected failure")
			}
			if _, err = s.Publish(ctx, id, p.Hash); err == nil {
				t.Fatal("uncertain mutation repeated")
			}
			status, err := s.Reconcile(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if lost == "push" {
				if status.Status != "branch_pushed" {
					t.Fatal(status.Status)
				}
				// An expired approval can be refreshed without attempting another branch push.
				status.Approval.ExpiresAt = time.Now().Add(-time.Minute)
				status.Hash = statefile.Hash(status.Approval)
				if err = s.save(status); err != nil {
					t.Fatal(err)
				}
				fresh, err := s.Prepare(ctx, id)
				if err != nil {
					t.Fatal(err)
				}
				if fresh.Patch == "" {
					t.Fatal("fresh approval has no patch")
				}
				if _, err = s.Publish(ctx, id, fresh.Hash); err != nil {
					t.Fatal(err)
				}
			}
			if r.posts != 1 || r.pushes != 1 {
				t.Fatal("duplicate side effect", r.posts, r.pushes)
			}
		})
	}
}
func TestUnknownPRStaysUnknownAndMovingBaseIsReported(t *testing.T) {
	s, r, id := fixture(t)
	ctx := context.Background()
	p, err := s.Prepare(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	r.losePR = true
	if _, err = s.Publish(ctx, id, p.Hash); err == nil {
		t.Fatal("missing failure")
	}
	pr := r.pr
	r.pr = nil
	if _, err = s.Reconcile(ctx, id); err == nil {
		t.Fatal("unknown PR claimed settled")
	}
	if r.posts != 1 {
		t.Fatal("reconciliation posted PR")
	}
	pr.Base.SHA = strings.Repeat("e", 40)
	r.pr = pr
	final, err := s.Reconcile(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if final.Attention == "" {
		t.Fatal("moved PR base hidden")
	}
}

func TestLatePushAfterAbsentReconciliationIsAdoptedWithoutRepush(t *testing.T) {
	s, remote, id := fixture(t)
	ctx := context.Background()
	p, err := s.Prepare(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	remote.losePush = true
	if _, err = s.Publish(ctx, id, p.Hash); err == nil {
		t.Fatal("missing transport failure")
	}
	remote.branch = "" // GitHub read has not observed the completed push yet.
	r, err := s.Reconcile(ctx, id)
	if err != nil || r.Status != "prepared" || !r.BranchAttempted {
		t.Fatal(r, err)
	}
	// A user may refresh an expired preview while the original push settles.
	fresh, err := s.Prepare(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	remote.branch = p.Approval.Commit
	if _, err = s.Publish(ctx, id, fresh.Hash); err != nil {
		t.Fatal(err)
	}
	if remote.pushes != 1 || remote.posts != 1 {
		t.Fatal("repeated late push or PR", remote.pushes, remote.posts)
	}
}
func TestOriginRejectsCredentialAndForeignURLs(t *testing.T) {
	for _, raw := range []string{"https://github.com/owner/repo.git", "git@github.com:owner/repo.git", "ssh://git@github.com/owner/repo"} {
		if got, err := Origin(raw); err != nil || got != "owner/repo" {
			t.Fatal(raw, got, err)
		}
	}
	for _, raw := range []string{"https://token@github.com/owner/repo", "https://github.com.evil/owner/repo", "https://github.com/owner/repo?token=x", "/tmp/repo", "--help"} {
		if _, err := Origin(raw); err == nil {
			t.Fatal(raw)
		}
	}
}

func TestHostEnvironmentDoesNotForwardProviderOrCloudKeys(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-only-provider-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test-only-cloud-key")
	t.Setenv("GH_TOKEN", "test-only-host-github-key")
	env := strings.Join(hostEnvironment(), "\n")
	if strings.Contains(env, "test-only-provider-key") || strings.Contains(env, "test-only-cloud-key") || !strings.Contains(env, "GH_TOKEN=test-only-host-github-key") {
		t.Fatal("incorrect host credential boundary")
	}
}
func TestEvidenceRequiresRecordedHashAndCleanTerminalState(t *testing.T) {
	s, _, id := fixture(t)
	path := filepath.Join(s.Root, "runs", id, "report.json")
	var report runner.Report
	if err := statefile.Read(path, &report, 1<<20); err != nil {
		t.Fatal(err)
	}
	for _, which := range []string{"hash", "cleanup", "state"} {
		r := report
		switch which {
		case "hash":
			r.PatchSHA256 = ""
		case "cleanup":
			r.CleanupConfirmed = false
		case "state":
			r.State = store.Failed
		}
		if err := statefile.Write(path, r); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Prepare(context.Background(), id); err == nil {
			t.Fatal("accepted", which)
		}
	}
}
