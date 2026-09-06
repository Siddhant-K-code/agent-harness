package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"syscall"
	"time"

	"github.com/Siddhant-K-code/agent-harness/internal/credentials"
	"github.com/Siddhant-K-code/agent-harness/internal/evaluation"
	"github.com/Siddhant-K-code/agent-harness/internal/learning"
	"github.com/Siddhant-K-code/agent-harness/internal/scaffold"
	"github.com/Siddhant-K-code/agent-harness/internal/skills"
	"github.com/Siddhant-K-code/agent-harness/internal/store"
	"github.com/Siddhant-K-code/agent-harness/internal/task"
)

func learnCommand(args []string) error {
	if len(args) > 0 && args[0] == "init" {
		if len(args) != 2 || args[1] == "--help" {
			fmt.Println("Usage: harness learn init NEW_DIRECTORY\nCreates real tasks, verifiers, a seed skill and an evaluation suite; no API calls.")
			return nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		dir, err := scaffold.CreateLearning(ctx, args[1])
		if err != nil {
			return err
		}
		fmt.Printf("Created learning lab at %s\n\n  cd %s\n  harness skills import --id repair --repo repo --file SKILL.md\n  harness run --task words.task.json --skill repair\n  harness learn cycle --repo repo --skill repair --suite suite.json --max-usd 1.30\n\nSetup was unpaid. The run and cycle use your OpenAI key. Review the tasks and verifiers first.\n", dir, shellQuote(dir))
		return nil
	}
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Println(`Usage:
  harness learn init NEW_DIRECTORY
  harness learn list
  harness learn capture RUN_ID
  harness learn propose --repo PATH --skill ID --max-usd 0.10 [--observation HASH ...]
  harness learn evaluate --suite suite.json --max-usd 1.00 CANDIDATE_VERSION
  harness learn promote EVALUATION_HASH
  harness learn cycle --repo PATH --skill ID --suite suite.json --max-usd 1.10

propose uses up to five recent unexpired observations by default. cycle runs one
bounded proposal/evaluation cycle and promotes only if its gate passes. Re-run
after collecting new observations. Paid commands require an explicit --max-usd.
All commands accept --state-dir PATH. No changes to model weights or verifiers.`)
		return nil
	}
	action := args[0]
	if action != "list" && action != "capture" && action != "propose" && action != "evaluate" && action != "promote" && action != "cycle" {
		return errors.New("unknown learn command")
	}
	f := flag.NewFlagSet("learn "+action, flag.ContinueOnError)
	rootFlag := f.String("state-dir", ".harness", "controller state directory")
	keyPath := f.String("api-key-file", "", "private OpenAI key file")
	skillID := f.String("skill", "", "active skill to improve")
	repo := f.String("repo", "", "repository scope")
	suitePath := f.String("suite", "", "evaluation suite JSON")
	proposalUSD := f.Float64("proposal-usd", 0.10, "portion reserved for proposal in a cycle")
	models := bindModelFlags(f, false)
	var selected []string
	f.Func("observation", "source observation hash; repeat to select multiple", func(s string) error { selected = append(selected, s); return nil })
	if err := f.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	positional := action == "capture" || action == "evaluate" || action == "promote"
	if (positional && f.NArg() != 1) || (!positional && f.NArg() != 0) {
		return errors.New("wrong number of positional arguments; use harness learn --help")
	}
	root, err := filepath.Abs(*rootFlag)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	encode := func(v any) error { e := json.NewEncoder(os.Stdout); e.SetIndent("", "  "); return e.Encode(v) }
	if action == "list" {
		observations, err := learning.List(root)
		if err != nil {
			return err
		}
		return encode(observations)
	}
	if action == "capture" {
		db, err := store.Open(filepath.Join(root, "harness.db"))
		if err != nil {
			return err
		}
		defer db.Close()
		hash, err := learning.Capture(ctx, root, db, f.Arg(0))
		if err != nil {
			return err
		}
		return encode(map[string]string{"observation": hash})
	}
	if action == "promote" {
		if err := evaluation.Promote(ctx, root, f.Arg(0)); err != nil {
			return err
		}
		return encode(map[string]any{"evaluation": f.Arg(0), "promoted": true})
	}
	if !(*models.usd > 0 && *models.usd <= 100) {
		return errors.New("paid learning commands require --max-usd greater than 0 and at most 100")
	}
	key, err := credentials.Resolve(*keyPath, root)
	if err != nil {
		return err
	}
	var suite evaluation.Suite
	if action == "evaluate" || action == "cycle" {
		suite, err = evaluation.LoadSuite(*suitePath)
		if err != nil {
			return err
		}
	}
	if action == "evaluate" {
		e, hash, err := evaluation.Run(ctx, root, key.Value, f.Arg(0), suite, *models.usd, os.Stderr)
		return errors.Join(err, encode(map[string]any{"evaluation": e, "evaluation_hash": hash}))
	}
	if *repo == "" || *skillID == "" {
		return errors.New("--repo and --skill are required")
	}
	scope, err := skills.Scope(*repo)
	if err != nil {
		return err
	}
	parent, err := (skills.Store{Root: root}).Active(scope, *skillID)
	if err != nil {
		return err
	}
	if len(selected) == 0 {
		observations, err := learning.List(root)
		if err != nil {
			return err
		}
		for hash, o := range observations {
			if o.Repository == scope && time.Now().Before(o.ExpiresAt) {
				selected = append(selected, hash)
			}
		}
		sort.Slice(selected, func(i, j int) bool {
			return observations[selected[i]].CreatedAt.After(observations[selected[j]].CreatedAt)
		})
		if len(selected) > 5 {
			selected = selected[:5]
		}
	}
	if len(selected) == 0 {
		return errors.New("no observations available; run a task with --learn or capture a terminal run")
	}
	o, err := learning.Load(root, selected[0])
	if err != nil {
		return err
	}
	db, err := store.Open(filepath.Join(root, "harness.db"))
	if err != nil {
		return err
	}
	r, err := db.Get(ctx, o.RunID)
	db.Close()
	if err != nil {
		return err
	}
	spec := r.Spec
	spec.Compaction = nil
	spec.Skills = nil
	spec.Learn = false
	spec.Limits.ContextWindowTokens = 32768
	spec.Limits.MaxOutputTokens = 2048
	spec.Limits.MaxTotalTokens = 50000
	models.apply(f, &spec)
	if action == "cycle" {
		if !(*proposalUSD > 0 && *proposalUSD < *models.usd) {
			return errors.New("--proposal-usd must be positive and smaller than the cycle budget")
		}
		spec.Limits.MaxUSD = *proposalUSD
		if err := evaluation.Reserve(suite, *models.usd-*proposalUSD); err != nil {
			return err
		}
		// Reject accidental training/holdout overlap before paying for a proposal.
		holdoutGoals := map[string]bool{}
		for _, c := range suite.Cases {
			if c.Role == "holdout" {
				t, e := task.Load(c.Task)
				if e != nil {
					return e
				}
				holdoutGoals[t.Goal] = true
			}
		}
		sources, e := store.Open(filepath.Join(root, "harness.db"))
		if e != nil {
			return e
		}
		defer sources.Close()
		for _, hash := range selected {
			o, e := learning.Load(root, hash)
			if e != nil {
				return e
			}
			r, e := sources.Get(ctx, o.RunID)
			if e != nil {
				return e
			}
			if holdoutGoals[r.Spec.Goal] {
				return errors.New("a selected observation came from a holdout task; choose --observation hashes from other tasks")
			}
		}
	}
	proposalCtx, stop := context.WithTimeout(ctx, 2*time.Minute)
	p, err := learning.Propose(proposalCtx, root, key.Value, parent, selected, spec)
	stop()
	if err != nil {
		return errors.Join(err, encode(p))
	}
	if action == "propose" {
		return encode(p)
	}
	// The evaluation reserves at most the remaining portion. Unused proposal
	// reservation is not silently turned into additional experiments.
	e, hash, err := evaluation.Run(ctx, root, key.Value, p.Candidate, suite, *models.usd-*proposalUSD, os.Stderr)
	promoted := false
	if err == nil && e.Eligible {
		err = evaluation.Promote(ctx, root, hash)
		promoted = err == nil
	}
	return errors.Join(err, encode(map[string]any{"proposal": p, "evaluation": e, "evaluation_hash": hash, "promoted": promoted, "estimated_usd_uncached": p.EstimatedUSD + e.EstimatedUSD}))
}
