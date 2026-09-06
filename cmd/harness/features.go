package main

import (
	"errors"
	"flag"
	"strings"

	"github.com/Siddhant-K-code/agent-harness/internal/skills"
	"github.com/Siddhant-K-code/agent-harness/internal/task"
)

type featureFlags struct {
	enabled, learn, clear      *bool
	trigger, keep, compactions *int
	summary                    *int64
	skills                     []skills.Ref
}

func bindFeatureFlags(f *flag.FlagSet) *featureFlags {
	c := task.DefaultCompaction()
	v := &featureFlags{
		enabled:     f.Bool("compaction", false, "enable bounded context summarization (omitted uses task)"),
		learn:       f.Bool("learn", false, "capture local run observations for future skill proposals (omitted uses task)"),
		clear:       f.Bool("clear-skills", false, "remove all selected skills"),
		trigger:     f.Int("compact-at-percent", c.TriggerPercent, "compact at this percentage of available input capacity"),
		keep:        f.Int("keep-recent-turns", c.KeepRecentTurns, "complete recent tool exchanges preserved during compaction"),
		compactions: f.Int("max-compactions", c.MaxCompactions, "maximum compactions per run"),
		summary:     f.Int64("max-summary-tokens", c.MaxSummaryTokens, "reserved output tokens for each compaction"),
	}
	f.Func("skill", "select id or id@sha256; repeat to replace the selected skill list", func(s string) error {
		parts := strings.SplitN(s, "@", 2)
		ref := skills.Ref{ID: parts[0]}
		if len(parts) == 2 {
			if parts[1] == "" {
				return errors.New("explicit skill version cannot be empty")
			}
			ref.Version = parts[1]
		}
		if err := ref.Validate(); err != nil {
			return err
		}
		v.skills = append(v.skills, ref)
		return nil
	})
	return v
}

func (v *featureFlags) apply(f *flag.FlagSet, s *task.Spec) (int, error) {
	visited := map[string]bool{}
	f.Visit(func(v *flag.Flag) { visited[v.Name] = true })
	if visited["clear-skills"] && *v.clear && visited["skill"] {
		return 0, errors.New("use either --skill or --clear-skills")
	}
	if visited["compaction"] && !*v.enabled {
		for _, name := range []string{"compact-at-percent", "keep-recent-turns", "max-compactions", "max-summary-tokens"} {
			if visited[name] {
				return 0, errors.New("compaction tuning flags conflict with --compaction=false")
			}
		}
	}
	count := 0
	if visited["compaction"] {
		count++
		if *v.enabled {
			s.Compaction = task.DefaultCompaction()
			if s.Compaction.MaxSummaryTokens > s.Limits.MaxOutputTokens {
				s.Compaction.MaxSummaryTokens = s.Limits.MaxOutputTokens
			}
		} else {
			s.Compaction = nil
		}
	}
	for _, name := range []string{"compact-at-percent", "keep-recent-turns", "max-compactions", "max-summary-tokens"} {
		if !visited[name] {
			continue
		}
		count++
		if s.Compaction == nil {
			s.Compaction = task.DefaultCompaction()
		}
		switch name {
		case "compact-at-percent":
			s.Compaction.TriggerPercent = *v.trigger
		case "keep-recent-turns":
			s.Compaction.KeepRecentTurns = *v.keep
		case "max-compactions":
			s.Compaction.MaxCompactions = *v.compactions
		case "max-summary-tokens":
			s.Compaction.MaxSummaryTokens = *v.summary
		}
	}
	if visited["learn"] {
		s.Learn = *v.learn
		count++
	}
	if visited["skill"] {
		s.Skills = v.skills
		count++
	}
	if visited["clear-skills"] && *v.clear {
		s.Skills = nil
		count++
	}
	return count, nil
}
