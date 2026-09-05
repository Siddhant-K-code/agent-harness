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
		{"reject long-context pricing tier", Report{}, 272001},
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
