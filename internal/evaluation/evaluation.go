// Package evaluation compares skill revisions using the real runner and
// independent verifiers. Its promotion gate consumes controller evidence.
package evaluation

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/Siddhant-K-code/agent-harness/internal/learning"
	"github.com/Siddhant-K-code/agent-harness/internal/model"
	"github.com/Siddhant-K-code/agent-harness/internal/ownership"
	"github.com/Siddhant-K-code/agent-harness/internal/runner"
	"github.com/Siddhant-K-code/agent-harness/internal/scaffold"
	"github.com/Siddhant-K-code/agent-harness/internal/skills"
	"github.com/Siddhant-K-code/agent-harness/internal/statefile"
	"github.com/Siddhant-K-code/agent-harness/internal/store"
	"github.com/Siddhant-K-code/agent-harness/internal/task"
)

type Case struct {
	ID   string `json:"id"`
	Task string `json:"task"`
	Role string `json:"role"` // regression or holdout
}
type Suite struct {
	Schema      int    `json:"schema"`
	Name        string `json:"name"`
	Repetitions int    `json:"repetitions"`
	Cases       []Case `json:"cases"`
}
type FrozenCase struct {
	ID             string    `json:"id"`
	Role           string    `json:"role"`
	Spec           task.Spec `json:"task"`
	VerifierSHA256 string    `json:"verifier_sha256"`
}
type Result struct {
	CaseID        string        `json:"case_id"`
	Trial         int           `json:"trial"`
	Variant       string        `json:"variant"`
	Report        runner.Report `json:"report"`
	ReportSHA256  string        `json:"report_sha256"`
	JournalSHA256 string        `json:"journal_sha256"`
}
type Evaluation struct {
	Schema       int          `json:"schema"`
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	Candidate    string       `json:"candidate"`
	Parent       string       `json:"parent"`
	SkillID      string       `json:"skill_id"`
	CreatedAt    time.Time    `json:"created_at"`
	Repetitions  int          `json:"repetitions"`
	Cases        []FrozenCase `json:"cases"`
	MaxUSD       float64      `json:"max_usd"`
	ReservedUSD  float64      `json:"reserved_usd"`
	EstimatedUSD float64      `json:"estimated_usd_uncached"`
	Status       string       `json:"status"`
	Pending      string       `json:"pending,omitempty"`
	Results      []Result     `json:"results"`
	Eligible     bool         `json:"eligible"`
	Reason       string       `json:"reason"`
}

var caseID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)
var runID = regexp.MustCompile(`^[a-f0-9]{32}$`)

func LoadSuite(path string) (Suite, error) {
	var s Suite
	if err := statefile.Read(path, &s, 1<<20); err != nil {
		return s, err
	}
	if s.Schema != 1 || s.Name == "" || len(s.Name) > 200 || s.Repetitions < 2 || s.Repetitions > 5 || len(s.Cases) < 2 || len(s.Cases) > 20 {
		return s, errors.New("suite requires schema 1, name, 2..20 cases and 2..5 repetitions")
	}
	seen := map[string]bool{}
	holdout := false
	for i, c := range s.Cases {
		if !caseID.MatchString(c.ID) || seen[c.ID] || (c.Role != "regression" && c.Role != "holdout") || c.Task == "" {
			return s, errors.New("case requires unique id, task path, and regression/holdout role")
		}
		seen[c.ID] = true
		holdout = holdout || c.Role == "holdout"
		if !filepath.IsAbs(c.Task) {
			s.Cases[i].Task = filepath.Join(filepath.Dir(path), c.Task)
		}
	}
	if !holdout {
		return s, errors.New("suite needs at least one holdout case")
	}
	return s, nil
}

// Reserve checks all task/model contracts and the worst-case model budget
// without calling a model or starting an executor.
func Reserve(suite Suite, maxUSD float64) error {
	if suite.Repetitions < 2 || suite.Repetitions > 5 || len(suite.Cases) < 2 || len(suite.Cases) > 20 {
		return errors.New("invalid evaluation suite size")
	}
	var total float64
	for _, c := range suite.Cases {
		s, err := task.Load(c.Task)
		if err != nil {
			return err
		}
		if _, err := model.ResolveSettings(s); err != nil {
			return err
		}
		total += 2 * float64(suite.Repetitions) * s.Limits.MaxUSD
	}
	if !(maxUSD > 0 && maxUSD <= 100) || total > maxUSD+1e-9 {
		return fmt.Errorf("suite requires a $%.4f model reservation; available budget is $%.4f", total, maxUSD)
	}
	return nil
}

// Freeze resolves Git refs, copies verifiers, and pins all other skills before
// paid execution. Both arms differ ONLY in the target skill revision.
func freeze(ctx context.Context, root, dir string, suite Suite, candidate skills.Version) ([]FrozenCase, float64, error) {
	var cases []FrozenCase
	var reserve float64
	seen := map[string]bool{}
	sourceTasks := map[string]bool{}
	for _, hash := range candidate.Evidence {
		o, err := learning.Load(root, hash)
		if err != nil {
			return nil, 0, err
		}
		if !time.Now().Before(o.ExpiresAt) {
			return nil, 0, errors.New("candidate observation expired")
		}
		// Resolve source goals from the durable run, not task path/name.
		db, err := store.Open(filepath.Join(root, "harness.db"))
		if err != nil {
			return nil, 0, err
		}
		r, e := db.Get(ctx, o.RunID)
		db.Close()
		if e != nil {
			return nil, 0, e
		}
		if statefile.Hash(r.Spec) != o.TaskSHA256 {
			return nil, 0, errors.New("source observation disagrees with its run")
		}
		sourceTasks[statefile.Hash(r.Spec.Goal)] = true
	}
	for _, c := range suite.Cases {
		s, err := task.Load(c.Task)
		if err != nil {
			return nil, 0, err
		}
		scope, err := skills.Scope(s.Repository)
		if err != nil {
			return nil, 0, err
		}
		if scope != candidate.Repository {
			return nil, 0, errors.New("evaluation tasks must match the candidate repository")
		}
		s.Repository = scope
		if c.Role == "holdout" && sourceTasks[statefile.Hash(s.Goal)] {
			return nil, 0, errors.New("holdout task was used to propose this revision")
		}
		sha, err := scaffold.Git(ctx, s.Repository, "rev-parse", "--verify", "--end-of-options", s.Ref+"^{commit}")
		if err != nil {
			return nil, 0, err
		}
		s.Ref = sha
		info, err := os.Lstat(s.Verifier)
		if err != nil {
			return nil, 0, err
		}
		if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > 1<<20 {
			return nil, 0, errors.New("invalid evaluation verifier")
		}
		verifier, err := os.ReadFile(s.Verifier)
		if err != nil {
			return nil, 0, err
		}
		identity := statefile.Hash([]string{s.Repository, s.Ref, s.Goal, string(verifier)})
		if seen[identity] {
			return nil, 0, errors.New("duplicate evaluation task content")
		}
		seen[identity] = true
		s.Verifier = filepath.Join(dir, c.ID+".verify.sh")
		if err := os.WriteFile(s.Verifier, verifier, 0600); err != nil {
			return nil, 0, err
		}
		s.Learn = false
		found := false
		for i, ref := range s.Skills {
			if ref.ID == candidate.ID {
				s.Skills[i].Version = candidate.Parent
				found = true
			}
		}
		if !found {
			s.Skills = append(s.Skills, skills.Ref{ID: candidate.ID, Version: candidate.Parent})
		}
		s.Skills, _, err = (skills.Store{Root: root}).Resolve(s.Repository, s.Skills)
		if err != nil {
			return nil, 0, err
		}
		settings, err := model.ResolveSettings(s)
		if err != nil {
			return nil, 0, err
		}
		s.Limits.ContextWindowTokens = settings.ContextWindowTokens
		// Pin model snapshots for repeatable comparisons; aliases remain fine
		// for normal runs, but evals cannot silently change model revisions.
		if s.Model == "gpt-5.4" {
			s.Model = "gpt-5.4-2026-03-05"
		}
		if s.Model == "gpt-5.4-mini" {
			s.Model = "gpt-5.4-mini-2026-03-17"
		}
		cases = append(cases, FrozenCase{ID: c.ID, Role: c.Role, Spec: s, VerifierSHA256: hashBytes(verifier)})
		reserve += 2 * float64(suite.Repetitions) * s.Limits.MaxUSD
	}
	return cases, reserve, nil
}

func specFor(c FrozenCase, id, version string) task.Spec {
	s := c.Spec
	s.Skills = append([]skills.Ref{}, s.Skills...)
	for i := range s.Skills {
		if s.Skills[i].ID == id {
			s.Skills[i].Version = version
		}
	}
	return s
}

func Run(ctx context.Context, root, key, candidateHash string, suite Suite, maxUSD float64, progress io.Writer) (e Evaluation, hash string, err error) {
	if err := Reserve(suite, maxUSD); err != nil {
		return e, "", err
	}
	if !(maxUSD > 0 && maxUSD <= 100) || math.IsNaN(maxUSD) {
		return e, "", errors.New("evaluation requires --max-usd greater than 0 and at most 100")
	}
	ss := skills.Store{Root: root}
	candidate, err := ss.Get(candidateHash)
	if err != nil {
		return e, "", err
	}
	if candidate.Source != "learned" {
		return e, "", errors.New("evaluation candidate must be a learned revision")
	}
	if _, _, err = ss.Resolve(candidate.Repository, []skills.Ref{{ID: candidate.ID, Version: candidateHash}}); err != nil {
		return e, "", err
	}
	active, err := ss.Active(candidate.Repository, candidate.ID)
	if err != nil {
		return e, "", err
	}
	if active != candidate.Parent {
		return e, "", errors.New("candidate parent is no longer active")
	}
	baseDir := filepath.Join(root, "learning", "evaluations")
	if err = os.MkdirAll(baseDir, 0700); err != nil {
		return e, "", err
	}
	dir, err := os.MkdirTemp(baseDir, "eval-")
	if err != nil {
		return e, "", err
	}
	lock, err := ownership.Acquire(dir)
	if err != nil {
		return e, "", err
	}
	defer lock.Close()
	cases, reserve, err := freeze(ctx, root, dir, suite, candidate)
	if err != nil {
		return e, "", err
	}
	if reserve > maxUSD+1e-9 {
		return e, "", fmt.Errorf("suite reserves $%.4f across %d runs, exceeding $%.4f; reduce per-task budgets or case count", reserve, 2*len(cases)*suite.Repetitions, maxUSD)
	}
	e = Evaluation{Schema: 1, ID: filepath.Base(dir), Name: suite.Name, Candidate: candidateHash, Parent: candidate.Parent, SkillID: candidate.ID, CreatedAt: time.Now().UTC(), Repetitions: suite.Repetitions, Cases: cases, MaxUSD: maxUSD, ReservedUSD: reserve, Status: "running", Results: []Result{}}
	path := filepath.Join(dir, "evaluation.json")
	if err = statefile.Write(path, e); err != nil {
		return e, "", err
	}
	defer func() {
		if err != nil {
			e.Status = "interrupted"
			e.Eligible = false
			e.Reason = err.Error()
		}
		err = errors.Join(err, statefile.Write(path, e))
		if err == nil {
			hash = statefile.Hash(e)
			err = statefile.Write(filepath.Join(baseDir, "results", hash+".json"), e)
		}
	}()
	db, err := store.Open(filepath.Join(root, "harness.db"))
	if err != nil {
		return e, "", err
	}
	defer db.Close()
	r := runner.Runner{Store: db, Root: root, Key: key, Progress: progress}
	for ci := range e.Cases {
		for trial := 0; trial < suite.Repetitions; trial++ {
			variants := []string{"baseline", "candidate"}
			if trial%2 == 1 {
				variants = []string{"candidate", "baseline"}
			}
			for _, variant := range variants {
				if err = ctx.Err(); err != nil {
					return e, "", err
				}
				e.Pending = fmt.Sprintf("%s/%d/%s", e.Cases[ci].ID, trial, variant)
				if err = statefile.Write(path, e); err != nil {
					return e, "", err
				}
				v := e.Parent
				if variant == "candidate" {
					v = e.Candidate
				}
				spec := specFor(e.Cases[ci], e.SkillID, v)
				if progress != nil {
					fmt.Fprintln(progress, "evaluation", e.Pending)
				}
				report, runErr := r.Run(ctx, spec)
				if report.RunID == "" {
					return e, "", fmt.Errorf("evaluation setup: %w", runErr)
				}
				events, readErr := db.Events(ctx, report.RunID)
				if readErr != nil {
					return e, "", readErr
				}
				e.Results = append(e.Results, Result{e.Cases[ci].ID, trial, variant, report, statefile.Hash(report), statefile.Hash(events)})
				e.EstimatedUSD += report.EstimatedUSD
				// Pin the first resolved image identity for every subsequent arm
				// of this case. An image/tag change is also rejected by Gate.
				if report.ImageID != "" {
					e.Cases[ci].Spec.Image = report.ImageID
				}
				if err = statefile.Write(path, e); err != nil {
					return e, "", err
				}
				if report.BillingUnknown || !report.CleanupConfirmed || report.State == store.Cancelled || !report.State.Terminal() {
					return e, "", errors.New("evaluation run has unknown billing, cleanup, cancellation, or terminal state; no promotion")
				}
				if e.EstimatedUSD > maxUSD {
					return e, "", errors.New("evaluation cost exceeded budget")
				}
			}
		}
	}
	e.Pending = ""
	e.Status = "completed"
	e.Eligible, e.Reason = Gate(e)
	return e, "", nil
}

// Gate requires complete paired evidence, no task-level success regressions,
// all holdout candidate trials passing, <=10% extra total model cost, and a
// strict improvement in successes, repair attempts, or >=5% estimated cost.
// This is a conservative small-sample gate, not a statistical quality claim.
func Gate(e Evaluation) (bool, string) {
	if e.Schema != 1 || e.Status != "completed" || e.Pending != "" || e.Repetitions < 2 || len(e.Cases) < 2 || len(e.Results) != len(e.Cases)*e.Repetitions*2 {
		return false, "incomplete paired evaluation"
	}
	seen := map[string]bool{}
	casesSeen := map[string]bool{}
	hasHoldout := false
	if !(e.MaxUSD > 0 && e.MaxUSD <= 100) || e.ReservedUSD > e.MaxUSD+1e-9 {
		return false, "invalid evaluation budget"
	}
	totalBase, totalCandidate, repairsBase, repairsCandidate := 0, 0, 0, 0
	var costBase, costCandidate float64
	for _, c := range e.Cases {
		if !caseID.MatchString(c.ID) || casesSeen[c.ID] || (c.Role != "regression" && c.Role != "holdout") {
			return false, "invalid case manifest"
		}
		casesSeen[c.ID] = true
		hasHoldout = hasHoldout || c.Role == "holdout"
		base, cand := 0, 0
		arms := 0
		for _, r := range e.Results {
			if r.CaseID != c.ID {
				continue
			}
			key := fmt.Sprintf("%s/%d/%s", r.CaseID, r.Trial, r.Variant)
			if seen[key] || r.Trial < 0 || r.Trial >= e.Repetitions || (r.Variant != "baseline" && r.Variant != "candidate") {
				return false, "duplicate or invalid comparison arm"
			}
			seen[key] = true
			arms++
			p := r.Report
			v := e.Parent
			if r.Variant == "candidate" {
				v = e.Candidate
			}
			expected := specFor(c, e.SkillID, v)
			if p.BillingUnknown || !p.CleanupConfirmed || !p.State.Terminal() || p.Reconciled || p.Model != c.Spec.Model || p.BaseCommit != c.Spec.Ref || p.ImageID != c.Spec.Image || p.VerifierSHA256 != c.VerifierSHA256 || statefile.Hash(p.Skills) != statefile.Hash(expected.Skills) {
				return false, "run identity, isolation, skill version, or billing mismatch"
			}
			if p.EstimatedUSD < 0 || math.IsNaN(p.EstimatedUSD) || math.IsInf(p.EstimatedUSD, 0) || p.InputTokens <= 0 || p.OutputTokens < 0 {
				return false, "invalid usage"
			}
			settings, err := model.ResolveSettings(expected)
			if err != nil || p.ModelSettings == nil || statefile.Hash(*p.ModelSettings) != statefile.Hash(settings) || math.Abs(settings.Price().Cost(p.InputTokens, p.OutputTokens)-p.EstimatedUSD) > 1e-9 || p.EstimatedUSD > expected.Limits.MaxUSD+1e-9 {
				return false, "cost or model settings disagree with evidence"
			}
			success := p.Verified && p.State == store.Completed
			repairs := p.VerificationAttempts
			if repairs > 0 {
				repairs--
			}
			if r.Variant == "baseline" {
				if success {
					base++
				}
				costBase += p.EstimatedUSD
				repairsBase += repairs
			} else {
				if success {
					cand++
				}
				costCandidate += p.EstimatedUSD
				repairsCandidate += repairs
			}
		}
		if arms != e.Repetitions*2 {
			return false, "missing paired trial"
		}
		if cand < base {
			return false, "candidate regressed on " + c.ID
		}
		if c.Role == "holdout" && cand != e.Repetitions {
			return false, "candidate did not pass every holdout trial"
		}
		totalBase += base
		totalCandidate += cand
	}
	if !hasHoldout {
		return false, "holdout evidence required"
	}
	if math.Abs(costBase+costCandidate-e.EstimatedUSD) > 1e-9 || e.EstimatedUSD > e.MaxUSD+1e-9 {
		return false, "evaluation total cost mismatch"
	}
	if len(seen) != len(e.Results) {
		return false, "missing or foreign comparison cases"
	}
	if costCandidate > costBase*1.10+1e-9 {
		return false, "candidate model cost increased by more than 10%"
	}
	if totalCandidate > totalBase || (totalCandidate == totalBase && (repairsCandidate < repairsBase || costCandidate < costBase*0.95)) {
		return true, "no case regressions; holdout passed; measured improvement within cost limit"
	}
	return false, "no measured improvement; active skill unchanged"
}

func Load(root, hash string) (Evaluation, error) {
	var e Evaluation
	if !statefile.ValidHash(hash) {
		return e, errors.New("invalid evaluation hash")
	}
	if err := statefile.Read(filepath.Join(root, "learning", "evaluations", "results", hash+".json"), &e, 4<<20); err != nil {
		return e, err
	}
	if statefile.Hash(e) != hash {
		return e, errors.New("evaluation hash mismatch")
	}
	return e, nil
}

func Promote(ctx context.Context, root, hash string) error {
	e, err := Load(root, hash)
	if err != nil {
		return err
	}
	if time.Since(e.CreatedAt) > 7*24*time.Hour || e.CreatedAt.After(time.Now()) {
		return errors.New("evaluation is stale; rerun it")
	}
	if ok, reason := Gate(e); !ok {
		return errors.New(reason)
	}
	ss := skills.Store{Root: root}
	candidate, err := ss.Get(e.Candidate)
	if err != nil {
		return err
	}
	if candidate.Parent != e.Parent || candidate.ID != e.SkillID || candidate.Source != "learned" {
		return errors.New("candidate provenance mismatch")
	}
	db, err := store.Open(filepath.Join(root, "harness.db"))
	if err != nil {
		return err
	}
	defer db.Close()
	for _, result := range e.Results {
		if !runID.MatchString(result.Report.RunID) {
			return errors.New("invalid evaluated run id")
		}
		var report runner.Report
		if err := statefile.Read(filepath.Join(root, "runs", result.Report.RunID, "report.json"), &report, 1<<20); err != nil {
			return err
		}
		r, err := db.Get(ctx, report.RunID)
		if err != nil {
			return err
		}
		events, err := db.Events(ctx, report.RunID)
		if err != nil {
			return err
		}
		if statefile.Hash(report) != result.ReportSHA256 || statefile.Hash(result.Report) != result.ReportSHA256 || statefile.Hash(events) != result.JournalSHA256 || r.State != report.State {
			return errors.New("evaluated evidence changed")
		}
		// Compare the actual recorded task to the frozen case, allowing only
		// the original mutable image tag from the first arm.
		for _, c := range e.Cases {
			if c.ID == result.CaseID {
				v := e.Parent
				if result.Variant == "candidate" {
					v = e.Candidate
				}
				expected := specFor(c, e.SkillID, v)
				actual := r.Spec
				actual.Image = expected.Image
				if statefile.Hash(actual) != statefile.Hash(expected) {
					return errors.New("recorded evaluation task changed")
				}
			}
		}
	}
	if err := ss.Promote(e.Candidate); err != nil {
		return err
	}
	return statefile.Write(filepath.Join(root, "learning", "promotions", hash+".json"), map[string]any{"evaluation": hash, "candidate": e.Candidate, "parent": e.Parent, "promoted_at": time.Now().UTC()})
}
