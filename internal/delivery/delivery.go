// Package delivery publishes an operator-approved, verified patch through host GitHub credentials.
package delivery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Siddhant-K-code/agent-harness/internal/integrations"
	"github.com/Siddhant-K-code/agent-harness/internal/ownership"
	"github.com/Siddhant-K-code/agent-harness/internal/runner"
	"github.com/Siddhant-K-code/agent-harness/internal/scaffold"
	"github.com/Siddhant-K-code/agent-harness/internal/statefile"
	"github.com/Siddhant-K-code/agent-harness/internal/store"
	"github.com/Siddhant-K-code/agent-harness/internal/workspace"
)

var validID = regexp.MustCompile(`^[a-f0-9]{32}$`)
var validCommit = regexp.MustCompile(`^[a-f0-9]{40}$`)

type Repository struct {
	Name          string `json:"full_name"`
	DefaultBranch string `json:"default_branch"`
	Archived      bool   `json:"archived"`
	Permissions   struct {
		Push bool `json:"push"`
	} `json:"permissions"`
}
type PullRequest struct {
	Number int    `json:"number"`
	URL    string `json:"html_url"`
	Body   string `json:"body"`
	Draft  bool   `json:"draft"`
	Head   struct {
		SHA string `json:"sha"`
	} `json:"head"`
	Base struct {
		SHA string `json:"sha"`
	} `json:"base"`
}

// Remote has a real GitHub implementation. Tests inject transport faults without credentials.
type Remote interface {
	Identity(context.Context) (string, error)
	Repository(context.Context, string) (Repository, error)
	Branch(context.Context, string, string) (string, error)
	Push(context.Context, workspace.Workspace, Approval) error
	FindPR(context.Context, Approval) (*PullRequest, error)
	CreatePR(context.Context, Approval) (PullRequest, error)
}
type Approval struct {
	RunID        string    `json:"run_id"`
	Actor        string    `json:"actor"`
	Repository   string    `json:"repository"`
	BaseBranch   string    `json:"base_branch"`
	BaseCommit   string    `json:"base_commit"`
	Branch       string    `json:"branch"`
	Commit       string    `json:"commit"`
	PatchSHA256  string    `json:"patch_sha256"`
	ReportSHA256 string    `json:"report_sha256"`
	Title        string    `json:"title"`
	Body         string    `json:"body"`
	ExpiresAt    time.Time `json:"expires_at"`
}
type Record struct {
	Schema          int          `json:"schema"`
	Approval        Approval     `json:"approval"`
	Hash            string       `json:"approval_hash"`
	Status          string       `json:"status"`
	Staging         string       `json:"staging"`
	BranchAttempted bool         `json:"branch_attempted,omitempty"`
	PR              *PullRequest `json:"pull_request,omitempty"`
	Attention       string       `json:"attention,omitempty"`
}
type Preview struct {
	Record
	Patch string `json:"patch"`
}
type Service struct {
	Root   string
	DB     *store.Store
	Remote Remote
}

func (s Service) directory(id string) (string, error) {
	if !validID.MatchString(id) {
		return "", errors.New("invalid run ID")
	}
	return filepath.Join(s.Root, "delivery", id), nil
}
func (s Service) Status(id string) (Record, error) {
	dir, err := s.directory(id)
	var record Record
	if err != nil {
		return record, err
	}
	err = statefile.Read(filepath.Join(dir, "delivery.json"), &record, 128<<10)
	if err == nil && (record.Schema != 1 || record.Approval.RunID != id || record.Hash != statefile.Hash(record.Approval)) {
		err = errors.New("invalid delivery record")
	}
	return record, err
}
func (s Service) save(r Record) error {
	dir, err := s.directory(r.Approval.RunID)
	if err != nil {
		return err
	}
	return statefile.Write(filepath.Join(dir, "delivery.json"), r)
}
func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func (s Service) evidence(ctx context.Context, id string) (store.Run, runner.Report, string, error) {
	var report runner.Report
	if _, err := s.directory(id); err != nil {
		return store.Run{}, report, "", err
	}
	run, err := s.DB.Get(ctx, id)
	if err != nil {
		return run, report, "", err
	}
	err = statefile.Read(filepath.Join(s.Root, "runs", id, "report.json"), &report, 1<<20)
	if err != nil {
		return run, report, "", err
	}
	if run.State != store.Completed || report.State != store.Completed || report.RunID != id || !report.Verified || !report.CleanupConfirmed || !validCommit.MatchString(report.BaseCommit) || !statefile.ValidHash(report.PatchSHA256) {
		return run, report, "", errors.New("delivery requires a completed, verified run with confirmed cleanup and a recorded patch hash; rerun older tasks")
	}
	root, err := os.OpenRoot(filepath.Join(s.Root, "runs", id))
	if err != nil {
		return run, report, "", err
	}
	defer root.Close()
	f, err := root.Open("changes.patch")
	if err != nil {
		return run, report, "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return run, report, "", errors.New("invalid patch artifact")
	}
	b, err := io.ReadAll(io.LimitReader(f, workspace.MaxPatchBytes+1))
	if err != nil {
		return run, report, "", err
	}
	if len(b) == 0 || len(b) > workspace.MaxPatchBytes || digest(b) != report.PatchSHA256 {
		return run, report, "", errors.New("patch is empty, oversized, or changed since verification")
	}
	return run, report, string(b), nil
}

// Origin only accepts token-free GitHub URLs. No model-selected destination is accepted.
func Origin(raw string) (string, error) {
	var repo string
	for _, prefix := range []string{"https://github.com/", "git@github.com:", "ssh://git@github.com/"} {
		if strings.HasPrefix(raw, prefix) {
			repo = strings.TrimSuffix(strings.TrimPrefix(raw, prefix), ".git")
			break
		}
	}
	if !integrations.Repository(repo) {
		return "", errors.New("source origin must be a token-free github.com owner/repository URL")
	}
	return repo, nil
}
func (s Service) target(ctx context.Context, repo string) (Repository, string, string, error) {
	actor, err := s.Remote.Identity(ctx)
	if err != nil {
		return Repository{}, "", "", err
	}
	r, err := s.Remote.Repository(ctx, repo)
	if err != nil {
		return r, actor, "", err
	}
	if !strings.EqualFold(r.Name, repo) || r.Archived || !r.Permissions.Push || r.DefaultBranch == "" || actor == "" {
		return r, actor, "", errors.New("GitHub repository must be writable and active")
	}
	base, err := s.Remote.Branch(ctx, repo, r.DefaultBranch)
	return r, actor, base, err
}

func (s Service) Prepare(ctx context.Context, id string) (Preview, error) {
	dir, err := s.directory(id)
	if err != nil {
		return Preview{}, err
	}
	lock, err := ownership.Acquire(dir)
	if err != nil {
		return Preview{}, err
	}
	defer lock.Close()
	old, oldErr := s.Status(id)
	if oldErr == nil && old.Status == "branch_pushed" {
		if err := s.recheck(ctx, old); err != nil {
			return Preview{}, err
		}
		_, _, patch, err := s.evidence(ctx, id)
		if err != nil {
			return Preview{}, err
		}
		old.Approval.ExpiresAt = time.Now().UTC().Add(30 * time.Minute)
		old.Hash = statefile.Hash(old.Approval)
		if err := s.save(old); err != nil {
			return Preview{}, err
		}
		return Preview{Record: old, Patch: patch}, nil
	}
	if oldErr == nil && old.Status != "prepared" {
		return Preview{Record: old}, errors.New("delivery already started; inspect status or reconcile the existing operation")
	}
	if oldErr != nil && !errors.Is(oldErr, os.ErrNotExist) {
		return Preview{}, oldErr
	}
	run, report, patch, err := s.evidence(ctx, id)
	if err != nil {
		return Preview{}, err
	}
	origin, err := scaffold.Git(ctx, run.Spec.Repository, "remote", "get-url", "origin")
	if err != nil {
		return Preview{}, err
	}
	repo, err := Origin(origin)
	if err != nil {
		return Preview{}, err
	}
	remote, actor, base, err := s.target(ctx, repo)
	if err != nil {
		return Preview{}, err
	}
	if base != report.BaseCommit {
		return Preview{}, errors.New("default branch has moved from the verified base; run and verify a fresh task before publishing")
	}
	staging, err := os.MkdirTemp(dir, "staging-")
	if err != nil {
		return Preview{}, err
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(staging)
		}
	}()
	w, err := workspace.Prepare(ctx, staging, run.Spec.Repository, report.BaseCommit)
	if err != nil {
		return Preview{}, err
	}
	title := strings.Join(strings.Fields(run.Spec.Goal), " ")
	if len([]rune(title)) > 120 {
		title = string([]rune(title)[:117]) + "..."
	}
	if title == "" {
		title = "Apply verified harness patch"
	}
	commit, err := createCommit(ctx, w, patch, title, run.CreatedAt)
	if err != nil {
		return Preview{}, err
	}
	a := Approval{RunID: id, Actor: actor, Repository: repo, BaseBranch: remote.DefaultBranch, BaseCommit: base, Branch: "harness/" + id, Commit: commit, PatchSHA256: report.PatchSHA256, ReportSHA256: statefile.Hash(report), Title: title, ExpiresAt: time.Now().UTC().Add(30 * time.Minute)}
	a.Body = fmt.Sprintf("%s\n\nIndependent verifier passed against base `%s`. Review the patch and repository CI before merging.\n\n- Run: `%s`\n- Patch SHA-256: `%s`\n- Verifier SHA-256: `%s`\n- Estimated model cost: $%.6f (uncached estimate)\n\n<!-- harness-run:%s patch:%s -->", title, a.BaseCommit, id, a.PatchSHA256, report.VerifierSHA256, report.EstimatedUSD, id, a.PatchSHA256)
	record := Record{Schema: 1, Approval: a, Hash: statefile.Hash(a), Status: "prepared", Staging: filepath.Base(staging)}
	// Preserve a prior create attempt when refreshing the same target/commit.
	// A delayed push may become visible after reconciliation reported absence.
	record.BranchAttempted = oldErr == nil && old.BranchAttempted && old.Approval.Repository == a.Repository && old.Approval.Branch == a.Branch && old.Approval.Commit == a.Commit
	if err := s.save(record); err != nil {
		return Preview{}, err
	}
	keep = true
	return Preview{Record: record, Patch: patch}, nil
}
func (s Service) workspace(r Record) (workspace.Workspace, error) {
	dir, err := s.directory(r.Approval.RunID)
	if err != nil {
		return workspace.Workspace{}, err
	}
	if !strings.HasPrefix(r.Staging, "staging-") || filepath.Base(r.Staging) != r.Staging {
		return workspace.Workspace{}, errors.New("invalid staging directory")
	}
	root := filepath.Join(dir, r.Staging)
	return workspace.Workspace{Root: root, Path: filepath.Join(root, "workspace"), GitDir: filepath.Join(root, "git"), Base: r.Approval.BaseCommit}, nil
}
func (s Service) recheck(ctx context.Context, r Record) error {
	_, report, _, err := s.evidence(ctx, r.Approval.RunID)
	if err != nil {
		return err
	}
	if statefile.Hash(report) != r.Approval.ReportSHA256 {
		return errors.New("run evidence changed; approval is invalid")
	}
	repo, actor, base, err := s.target(ctx, r.Approval.Repository)
	if err != nil {
		return err
	}
	if actor != r.Approval.Actor || base != r.Approval.BaseCommit || repo.DefaultBranch != r.Approval.BaseBranch {
		return errors.New("GitHub identity or base changed; approval is invalid")
	}
	w, err := s.workspace(r)
	if err != nil {
		return err
	}
	commit, err := localGit(ctx, w, nil, nil, "rev-parse", "HEAD")
	if err != nil || strings.TrimSpace(commit) != r.Approval.Commit {
		return errors.New("staged commit changed; approval is invalid")
	}
	return nil
}

// Publish requires the hash displayed by Prepare. Durable states precede every
// external mutation. A lost acknowledgement is reconciled without repeating POST.
func (s Service) Publish(ctx context.Context, id, approvalHash string) (Record, error) {
	dir, err := s.directory(id)
	if err != nil {
		return Record{}, err
	}
	lock, err := ownership.Acquire(dir)
	if err != nil {
		return Record{}, err
	}
	defer lock.Close()
	r, err := s.Status(id)
	if err != nil {
		return r, err
	}
	if !statefile.ValidHash(approvalHash) || r.Hash != approvalHash {
		return r, errors.New("approval does not match the current preview")
	}
	if r.Status == "complete" {
		return r, nil
	}
	if r.Status != "prepared" && r.Status != "branch_pushed" {
		return r, errors.New("previous external outcome is uncertain; reconcile before continuing")
	}
	if time.Now().After(r.Approval.ExpiresAt) {
		return r, errors.New("approval expired; prepare a fresh preview before publishing")
	}
	if err = s.recheck(ctx, r); err != nil {
		return r, err
	}
	w, err := s.workspace(r)
	if err != nil {
		return r, err
	}
	if r.Status == "prepared" {
		head, err := s.Remote.Branch(ctx, r.Approval.Repository, r.Approval.Branch)
		if err != nil {
			return r, err
		}
		if head != "" && (!r.BranchAttempted || head != r.Approval.Commit) {
			return r, errors.New("target branch already exists; refusing to overwrite it")
		}
		if head == "" {
			r.Status, r.BranchAttempted = "publishing_branch", true
			if err = s.save(r); err != nil {
				return r, err
			}
			if err = s.Remote.Push(ctx, w, r.Approval); err != nil {
				return r, err
			}
		}
		r.Status = "branch_pushed"
		if err = s.save(r); err != nil {
			return r, err
		}
	}
	if err = s.recheck(ctx, r); err != nil {
		return r, err
	}
	head, err := s.Remote.Branch(ctx, r.Approval.Repository, r.Approval.Branch)
	if err != nil {
		return r, err
	}
	if head != r.Approval.Commit {
		return r, errors.New("published branch changed; refusing to create a PR")
	}
	prior, err := s.Remote.FindPR(ctx, r.Approval)
	if err != nil {
		return r, err
	}
	if prior != nil {
		return s.finish(r, *prior)
	}
	r.Status = "publishing_pr"
	if err = s.save(r); err != nil {
		return r, err
	}
	pr, err := s.Remote.CreatePR(ctx, r.Approval)
	if err != nil {
		return r, err
	}
	return s.finish(r, pr)
}
func (s Service) finish(r Record, pr PullRequest) (Record, error) {
	if pr.Head.SHA != r.Approval.Commit || pr.Number < 1 || !strings.HasPrefix(pr.URL, "https://github.com/"+r.Approval.Repository+"/pull/") {
		return r, errors.New("GitHub PR does not match the approved commit; inspect and reconcile")
	}
	r.PR, r.Status = &pr, "complete"
	if pr.Base.SHA != r.Approval.BaseCommit {
		r.Attention = "PR created, but its base moved since verification; verify again before merging"
	}
	return r, s.save(r)
}
func (s Service) Reconcile(ctx context.Context, id string) (Record, error) {
	dir, err := s.directory(id)
	if err != nil {
		return Record{}, err
	}
	lock, err := ownership.Acquire(dir)
	if err != nil {
		return Record{}, err
	}
	defer lock.Close()
	r, err := s.Status(id)
	if err != nil {
		return r, err
	}
	if r.Status == "complete" {
		return r, nil
	}
	if r.Status != "publishing_branch" && r.Status != "publishing_pr" {
		return r, nil
	}
	if r.Status == "publishing_pr" {
		pr, err := s.Remote.FindPR(ctx, r.Approval)
		if err != nil {
			return r, err
		}
		if pr == nil {
			return r, errors.New("PR outcome remains unknown; no duplicate request was sent. Inspect GitHub and retry read-only reconciliation")
		}
		return s.finish(r, *pr)
	}
	head, err := s.Remote.Branch(ctx, r.Approval.Repository, r.Approval.Branch)
	if err != nil {
		return r, err
	}
	if head == r.Approval.Commit {
		r.Status = "branch_pushed"
	} else if head == "" {
		// A future retry still uses a create-only lease, so a late push cannot be overwritten.
		r.Status = "prepared"
	} else {
		return r, errors.New("remote branch differs from the approved commit; manual review required")
	}
	return r, s.save(r)
}
