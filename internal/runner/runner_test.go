package runner

import (
	"github.com/Siddhant-K-code/agent-harness/internal/model"
	"github.com/Siddhant-K-code/agent-harness/internal/task"
	"testing"
)

func TestAdmissionReservesOutputAndAccumulatedUsage(t *testing.T) {
	price := model.Price{Input: 2.5, Output: 15}
	limits := task.Limits{MaxUSD: 2, MaxOutputTokens: 2048, MaxTotalTokens: 50000}
	if _, err := admit(limits, price, Report{}, 1000); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		report Report
		input  int64
	}{
		{"maximum output exceeds remaining dollars", Report{EstimatedUSD: 1.99}, 1000},
		{"previous requests consume token budget", Report{InputTokens: 48000}, 1000},
		{"reject input beyond default context", Report{}, 272001},
		{"missing count", Report{}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := admit(limits, price, c.report, c.input); err == nil {
				t.Fatal("request admitted")
			}
		})
	}
}

func TestContextAdmissionReservesOutputSeparatelyFromRunBudget(t *testing.T) {
	l := task.Limits{ContextWindowTokens: 8192, MaxOutputTokens: 2048, MaxTotalTokens: 50000, MaxUSD: 2}
	p := model.Price{Input: 2.5, Output: 15}
	if _, err := admit(l, p, Report{}, 6144); err != nil {
		t.Fatal("exact context boundary rejected", err)
	}
	if _, err := admit(l, p, Report{}, 6145); err == nil {
		t.Fatal("input plus output exceeded context")
	}
	if _, err := admit(l, p, Report{InputTokens: 45000}, 6144); err == nil {
		t.Fatal("per-run token budget bypassed by larger context")
	}
	spec, err := task.Load("../../examples/normalize-tags/task.json")
	if err != nil {
		t.Fatal(err)
	}
	spec.Limits.ContextWindowTokens = 1050000
	spec.Limits.MaxTotalTokens = 10000000
	spec.Limits.MaxUSD = 3
	settings, err := model.ResolveSettings(spec)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admit(spec.Limits, settings.Price(), Report{}, 900000); err == nil {
		t.Fatal("large request was admitted at cheaper standard prices")
	}
	spec.Limits.MaxUSD = 5
	if _, err := admit(spec.Limits, settings.Price(), Report{}, 900000); err != nil {
		t.Fatal("valid user-configured long context rejected", err)
	}
}
func TestArgumentsRejectUnknownAndTrailingFields(t *testing.T) {
	for _, input := range []string{`{"command":"pwd","host":true}`, `{"command":"pwd"} {}`, `{"command":12}`} {
		var args struct {
			Command string `json:"command"`
		}
		if err := decodeArguments(input, &args); err == nil {
			t.Fatalf("accepted %s", input)
		}
	}
}
