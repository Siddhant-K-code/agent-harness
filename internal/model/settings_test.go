package model

import (
	"testing"

	"github.com/Siddhant-K-code/agent-harness/internal/task"
)

func settingSpec(t *testing.T) task.Spec {
	t.Helper()
	s, err := task.Load("../../examples/normalize-tags/task.json")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestModelCapacityAndConservativePricing(t *testing.T) {
	spec := settingSpec(t)
	for _, p := range Catalog() {
		spec.Model = p.ID
		spec.Limits.ContextWindowTokens = p.ContextWindowTokens
		settings, err := ResolveSettings(spec)
		if err != nil {
			t.Fatal(err)
		}
		if settings.Model != p.ID || settings.MaxInputTokens+settings.MaxOutputTokens != p.ContextWindowTokens {
			t.Fatal("lost selected model or output reservation")
		}
		if p.LongContextThreshold > 0 {
			if settings.Price() != (Price{5, 22.5}) {
				t.Fatal("long-context run underpriced")
			}
		} else if settings.Price() != (Price{0.75, 4.5}) {
			t.Fatal("mini incorrectly uses premium tier")
		}
		spec.Limits.ContextWindowTokens++
		if _, err := ResolveSettings(spec); err == nil {
			t.Fatal("accepted capacity above model maximum")
		}
	}
	spec.Model = "gpt-5.4"
	spec.Limits.ContextWindowTokens = 272000 + spec.Limits.MaxOutputTokens
	s, err := ResolveSettings(spec)
	if err != nil || s.PricingBasis != "standard_uncached" {
		t.Fatal("threshold should be standard", err)
	}
	spec.Limits.ContextWindowTokens++
	s, err = ResolveSettings(spec)
	if err != nil || s.PricingBasis != "long_context_uncached_upper_bound" {
		t.Fatal("threshold crossing was not priced from start", err)
	}
	spec.Limits.ContextWindowTokens = 0
	s, err = ResolveSettings(spec)
	if err != nil || s.ContextWindowTokens != 200000 {
		t.Fatal("legacy task default lost", err)
	}
}

func TestInvalidModelSettings(t *testing.T) {
	for _, change := range []func(*task.Spec){
		func(s *task.Spec) { s.Model = "unconfigured-model" },
		func(s *task.Spec) { s.Model = "gpt-5.4-mini"; s.Limits.ContextWindowTokens = 500000 },
		func(s *task.Spec) { s.Limits.ContextWindowTokens = 1024; s.Limits.MaxOutputTokens = 1024 },
		func(s *task.Spec) { s.Limits.MaxOutputTokens = 128001 },
		func(s *task.Spec) { s.Limits.MaxTotalTokens = 10000001 },
		func(s *task.Spec) { s.Limits.MaxTotalTokens = s.Limits.MaxOutputTokens },
		func(s *task.Spec) { s.Limits.MaxUSD = 0.001 },
	} {
		s := settingSpec(t)
		change(&s)
		if _, err := ResolveSettings(s); err == nil {
			t.Fatal("accepted invalid settings")
		}
	}
}
