package main

import (
	"flag"
	"testing"

	"github.com/Siddhant-K-code/agent-harness/internal/task"
)

func TestFeatureFlagsPreserveDefaultsAndPinExplicitSelection(t *testing.T) {
	s, err := task.Load("../../examples/normalize-tags/task.json")
	if err != nil {
		t.Fatal(err)
	}
	f := flag.NewFlagSet("test", flag.ContinueOnError)
	features := bindFeatureFlags(f)
	if err := f.Parse([]string{"--compaction", "--keep-recent-turns", "2", "--learn", "--skill", "repair"}); err != nil {
		t.Fatal(err)
	}
	if _, err := features.apply(f, &s); err != nil {
		t.Fatal(err)
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	if s.Compaction == nil || s.Compaction.KeepRecentTurns != 2 || !s.Learn || len(s.Skills) != 1 {
		t.Fatal("feature flags not applied")
	}
	f = flag.NewFlagSet("test", flag.ContinueOnError)
	features = bindFeatureFlags(f)
	if err := f.Parse([]string{"--compaction=false", "--learn=false", "--clear-skills"}); err != nil {
		t.Fatal(err)
	}
	if _, err := features.apply(f, &s); err != nil {
		t.Fatal(err)
	}
	if s.Compaction != nil || s.Learn || len(s.Skills) != 0 {
		t.Fatal("features cannot be disabled")
	}
}
