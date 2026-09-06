package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Siddhant-K-code/agent-harness/internal/contextwindow"
	"github.com/Siddhant-K-code/agent-harness/internal/model"
	"github.com/Siddhant-K-code/agent-harness/internal/statefile"
	"github.com/Siddhant-K-code/agent-harness/internal/task"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
)

func historyFixture() contextwindow.Window {
	w := contextwindow.Window{Pinned: []responses.ResponseInputItemUnionParam{responses.ResponseInputItemParamOfMessage("Keep the public API. Selected skill version abc.", "user")}, Feedback: "Verifier: empty input still fails."}
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("call_%d", i)
		w.Turns = append(w.Turns, []responses.ResponseInputItemUnionParam{
			param.Override[responses.ResponseInputItemUnionParam](json.RawMessage(`{"type":"reasoning","id":"r","encrypted_content":"opaque-reasoning"}`)),
			param.Override[responses.ResponseInputItemUnionParam](json.RawMessage(fmt.Sprintf(`{"type":"function_call","call_id":%q,"name":"exec","arguments":"{}"}`, id))),
			model.ToolResult(id, map[string]any{"output": "observed failure", "exit_code": 1}),
		})
	}
	return w
}

func TestCompactionBudgetProtocolAndFailureAtomicity(t *testing.T) {
	for _, mode := range []string{"success", "budget", "incomplete", "no_reduction", "missing_usage", "input_limit", "small_gain"} {
		t.Run(mode, func(t *testing.T) {
			paid := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if strings.HasSuffix(r.URL.Path, "input_tokens") {
					count := 500
					if mode == "input_limit" {
						count = 7000
					}
					if _, ok := body["input"].([]any); ok {
						count = 1000
						if mode == "small_gain" {
							count = 3900
						}
						if mode == "no_reduction" {
							count = 8000
						}
					}
					fmt.Fprintf(w, `{"input_tokens":%d}`, count)
					return
				}
				paid++
				if body["max_output_tokens"] != float64(1024) || body["store"] != false || body["truncation"] != "disabled" || body["tools"] != nil {
					t.Error("unbounded/tool-enabled summary request", body)
				}
				if strings.Contains(fmt.Sprint(body["input"]), "opaque-reasoning") {
					t.Error("opaque reasoning treated as summary text")
				}
				status := "completed"
				if mode == "incomplete" {
					status = "incomplete"
				}
				usage := `{"input_tokens":500,"output_tokens":100,"total_tokens":600}`
				if mode == "missing_usage" {
					usage = `{}`
				}
				fmt.Fprintf(w, `{"id":"summary-test","status":%q,"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Read parser.js; empty input failure remains."}]}],"usage":%s}`, status, usage)
			}))
			defer server.Close()
			client := model.New("protocol-test", "gpt-5.4")
			client.API = openai.NewClient(option.WithBaseURL(server.URL+"/"), option.WithAPIKey("protocol-test"), option.WithMaxRetries(0))
			spec, err := task.Load("../../examples/normalize-tags/task.json")
			if err != nil {
				t.Fatal(err)
			}
			spec.Compaction = task.DefaultCompaction()
			spec.Limits.ContextWindowTokens = 8192
			settings, err := model.ResolveSettings(spec)
			if err != nil {
				t.Fatal(err)
			}
			original := historyFixture()
			originalHash := statefile.Hash(original.Input())
			report := Report{}
			if mode == "budget" {
				report.EstimatedUSD = spec.Limits.MaxUSD
			}
			var kinds []string
			record := func(kind string, data any, step bool) error {
				kinds = append(kinds, kind)
				if step {
					t.Error("compaction consumed a coding step")
				}
				return nil
			}
			before := int64(4000)
			if mode == "no_reduction" {
				before = 7000
			}
			got, err := compactWindow(context.Background(), client, spec, settings, t.TempDir(), original, before, &report, record)
			if statefile.Hash(original.Input()) != originalHash {
				t.Fatal("original history mutated")
			}
			if mode == "small_gain" {
				if err != nil || statefile.Hash(got.Input()) != originalHash || report.Compactions != 0 || report.CompactionAttempts != 1 {
					t.Fatal("low-yield summary dropped history or stopped a fitting run", err)
				}
				return
			}
			if mode == "success" {
				if err != nil {
					t.Fatal(err)
				}
				if len(got.Turns) != 1 || statefile.Hash(got.Turns[0]) != statefile.Hash(original.Turns[2]) || statefile.Hash(got.Pinned) != statefile.Hash(original.Pinned) || got.Feedback != original.Feedback {
					t.Fatal("compaction lost pinned context or split recent protocol")
				}
				if report.Compactions != 1 || report.InputTokens != 500 || report.OutputTokens != 100 || report.EstimatedUSD <= 0 || len(kinds) != 3 {
					t.Fatal("missing budget or journal accounting", report, kinds)
				}
			} else {
				if err == nil || statefile.Hash(got.Input()) != originalHash {
					t.Fatal("failed compaction accepted or dropped history")
				}
				if (mode == "budget" || mode == "input_limit") && paid != 0 {
					t.Fatal("over-budget compaction dispatched")
				}
				if mode == "incomplete" && report.EstimatedUSD <= 0 {
					t.Fatal("incomplete paid summary not charged")
				}
				if mode == "missing_usage" && !report.BillingUnknown {
					t.Fatal("missing usage accepted")
				}
			}
		})
	}
}
