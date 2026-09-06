package model

import (
	"fmt"

	"github.com/Siddhant-K-code/agent-harness/internal/task"
)

// Checked 2026-09-06 against the official OpenAI model pages linked in docs.
// A catalog entry means protocol/limits are configured, not that an account
// has access or that every model has passed a live coding benchmark.
type Profile struct {
	ID                   string  `json:"id"`
	ContextWindowTokens  int64   `json:"context_window_tokens"`
	MaxOutputTokens      int64   `json:"max_output_tokens"`
	InputUSDPerMillion   float64 `json:"input_usd_per_million"`
	OutputUSDPerMillion  float64 `json:"output_usd_per_million"`
	LongContextThreshold int64   `json:"long_context_input_threshold,omitempty"`
}

func Catalog() []Profile {
	return []Profile{
		{"gpt-5.4", 1050000, 128000, 2.5, 15, 272000},
		{"gpt-5.4-2026-03-05", 1050000, 128000, 2.5, 15, 272000},
		{"gpt-5.4-mini", 400000, 128000, 0.75, 4.5, 0},
		{"gpt-5.4-mini-2026-03-17", 400000, 128000, 0.75, 4.5, 0},
	}
}

func Lookup(id string) (Profile, error) {
	for _, p := range Catalog() {
		if p.ID == id {
			return p, nil
		}
	}
	return Profile{}, fmt.Errorf("model %q has no configured capabilities/price schedule; use harness models", id)
}

type Settings struct {
	Model               string  `json:"model"`
	ContextWindowTokens int64   `json:"context_window_tokens"`
	MaxInputTokens      int64   `json:"max_input_tokens"`
	MaxOutputTokens     int64   `json:"max_output_tokens"`
	MaxTotalTokens      int64   `json:"max_total_tokens"`
	PricingBasis        string  `json:"pricing_basis"`
	InputUSDPerMillion  float64 `json:"input_usd_per_million"`
	OutputUSDPerMillion float64 `json:"output_usd_per_million"`
}

func (s Settings) Price() Price { return Price{s.InputUSDPerMillion, s.OutputUSDPerMillion} }

func ResolveSettings(spec task.Spec) (Settings, error) {
	if err := spec.Validate(); err != nil {
		return Settings{}, err
	}
	p, err := Lookup(spec.Model)
	if err != nil {
		return Settings{}, err
	}
	l := spec.Limits
	if l.ContextWindow() > p.ContextWindowTokens {
		return Settings{}, fmt.Errorf("context window %d exceeds %s's maximum of %d tokens", l.ContextWindow(), p.ID, p.ContextWindowTokens)
	}
	if l.MaxOutputTokens > p.MaxOutputTokens {
		return Settings{}, fmt.Errorf("output limit exceeds %s's maximum of %d tokens", p.ID, p.MaxOutputTokens)
	}
	s := Settings{spec.Model, l.ContextWindow(), l.ContextWindow() - l.MaxOutputTokens, l.MaxOutputTokens, l.MaxTotalTokens, "standard_uncached", p.InputUSDPerMillion, p.OutputUSDPerMillion}
	// GPT-5.4 applies the premium for the full session once long input is used.
	// Select the conservative schedule BEFORE the first request when this
	// configuration can enter that tier, so earlier requests cannot be underpriced.
	if p.LongContextThreshold > 0 && s.MaxInputTokens > p.LongContextThreshold {
		s.PricingBasis = "long_context_uncached_upper_bound"
		s.InputUSDPerMillion *= 2
		s.OutputUSDPerMillion *= 1.5
	}
	if l.MaxTotalTokens <= l.MaxOutputTokens {
		return Settings{}, fmt.Errorf("total token budget must leave input space beyond %d reserved output tokens", l.MaxOutputTokens)
	}
	if s.Price().Cost(1, l.MaxOutputTokens) > l.MaxUSD {
		return Settings{}, fmt.Errorf("$%.2f budget cannot cover the output reservation at %s rates; reduce --max-output-tokens or increase --max-usd", l.MaxUSD, s.PricingBasis)
	}
	return s, nil
}
