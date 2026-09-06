package delivery

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Siddhant-K-code/agent-harness/internal/scaffold"
	"github.com/Siddhant-K-code/agent-harness/internal/statefile"
	"github.com/Siddhant-K-code/agent-harness/internal/workspace"
)

func TestCommitPreservesVerifiedPatchWhitespace(t *testing.T) {
	for _, suffix := range []string{"  \n", "\t\n", "\u00a0\n"} {
		t.Run(suffix, func(t *testing.T) {
			s, _, id := fixture(t)
			ctx := context.Background()
			run, report, _, err := s.evidence(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			root := filepath.Join(s.Root, "runs", id)
			w := workspace.Workspace{Root: root, Path: filepath.Join(root, "workspace"), GitDir: filepath.Join(root, "git"), Base: report.BaseCommit}
			if err := os.WriteFile(filepath.Join(w.Path, "tags.js"), []byte("module.exports = value => value;"+suffix), 0600); err != nil {
				t.Fatal(err)
			}
			patch, err := w.Patch(ctx)
			if err != nil {
				t.Fatal(err)
			}
			staging, err := workspace.Prepare(ctx, t.TempDir(), run.Spec.Repository, report.BaseCommit)
			if err != nil {
				t.Fatal(err)
			}
			commit, err := createCommit(ctx, staging, patch, "Preserve verified whitespace", run.CreatedAt)
			if err != nil || !validCommit.MatchString(commit) {
				t.Fatal("valid Git patch rejected", commit, err)
			}
			actual, err := localGit(ctx, staging, nil, nil, "diff", "--binary", "--no-ext-diff", "--no-textconv", report.BaseCommit, commit, "--")
			if err != nil || actual != patch {
				t.Fatal("committed patch bytes changed", err)
			}
		})
	}
}

func TestPublishWithCanonicalAndLegacyRepositoryCasing(t *testing.T) {
	for _, mode := range []string{"new", "legacy", "legacy-lost-pr"} {
		t.Run(mode, func(t *testing.T) {
			s, remote, id := fixture(t)
			ctx := context.Background()
			run, err := s.DB.Get(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := scaffold.Git(ctx, run.Spec.Repository, "remote", "set-url", "origin", "https://github.com/Owner/Repo.git"); err != nil {
				t.Fatal(err)
			}
			p, err := s.Prepare(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if p.Approval.Repository != "owner/repo" {
				t.Fatal("approval did not use GitHub's canonical name")
			}
			if mode != "new" {
				// Emulate a persisted approval created before name normalization.
				p.Approval.Repository = "Owner/Repo"
				p.Hash = statefile.Hash(p.Approval)
				if err := s.save(p.Record); err != nil {
					t.Fatal(err)
				}
			}
			remote.losePR = mode == "legacy-lost-pr"
			r, err := s.Publish(ctx, id, p.Hash)
			if remote.losePR {
				if err == nil {
					t.Fatal("missing injected transport failure")
				}
				r, err = s.Reconcile(ctx, id)
			}
			if err != nil || r.Status != "complete" || r.PR == nil {
				t.Fatal("canonical GitHub URL rejected", r.Status, err)
			}
			if remote.pushes != 1 || remote.posts != 1 {
				t.Fatal("duplicate external mutation", remote.pushes, remote.posts)
			}
		})
	}
}

func TestFinishRejectsMismatchedPRURLs(t *testing.T) {
	s := Service{Root: t.TempDir()}
	r := Record{Approval: Approval{RunID: strings.Repeat("a", 32), Repository: "owner/repo", Commit: strings.Repeat("b", 40)}}
	for _, url := range []string{
		"https://github.com/other/repo/pull/1",
		"https://github.com/owner/other/pull/1",
		"https://github.com/owner/repo/pull/2",
		"https://github.com/owner/repo/pull/1/extra",
		"https://github.com.evil/owner/repo/pull/1",
		"https://github.com@evil.invalid/owner/repo/pull/1",
		"http://github.com/owner/repo/pull/1",
	} {
		pr := PullRequest{Number: 1, URL: url}
		pr.Head.SHA = r.Approval.Commit
		if _, err := s.finish(r, pr); err == nil {
			t.Fatal("accepted mismatched PR URL", url)
		}
	}
}

func TestRefreshLegacyApprovalPreservesDelayedPushIntent(t *testing.T) {
	s, remote, id := fixture(t)
	ctx := context.Background()
	p, err := s.Prepare(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	p.Approval.Repository = "Owner/Repo"
	p.Hash = statefile.Hash(p.Approval)
	if err := s.save(p.Record); err != nil {
		t.Fatal(err)
	}
	remote.losePush = true
	if _, err := s.Publish(ctx, id, p.Hash); err == nil {
		t.Fatal("missing injected transport failure")
	}
	remote.branch = ""
	if _, err := s.Reconcile(ctx, id); err != nil {
		t.Fatal(err)
	}
	fresh, err := s.Prepare(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Approval.Repository != "owner/repo" || !fresh.BranchAttempted {
		t.Fatal("normalizing the repository lost the original push attempt")
	}
	remote.branch = p.Approval.Commit
	if _, err := s.Publish(ctx, id, fresh.Hash); err != nil {
		t.Fatal(err)
	}
	if remote.pushes != 1 || remote.posts != 1 {
		t.Fatal("repeated late push or PR", remote.pushes, remote.posts)
	}
}
