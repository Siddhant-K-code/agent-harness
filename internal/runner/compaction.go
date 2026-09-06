package runner

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Siddhant-K-code/agent-harness/internal/contextwindow"
	"github.com/Siddhant-K-code/agent-harness/internal/model"
	"github.com/Siddhant-K-code/agent-harness/internal/statefile"
	"github.com/Siddhant-K-code/agent-harness/internal/task"
)

func compactWindow(ctx context.Context, client model.Client, spec task.Spec, settings model.Settings, root string, w contextwindow.Window, before int64, report *Report, record func(string, any, bool) error) (contextwindow.Window, error) {
	c := spec.Compaction
	if report.CompactionAttempts >= c.MaxCompactions {
		return w, errors.New("compaction limit reached")
	}
	prompt, err := w.Prompt(c.KeepRecentTurns)
	if err != nil {
		return w, err
	}
	count, err := client.CountText(ctx, contextwindow.Instructions, prompt)
	if err != nil {
		return w, err
	}
	// Use the same input ceiling that selected the run's pricing tier. A
	// smaller summary output reservation must not admit a long-context input
	// that the coding configuration would price at the standard tier.
	if count > settings.MaxInputTokens {
		return w, errors.New("compaction source exceeds the configured input ceiling")
	}
	limits := spec.Limits
	limits.MaxOutputTokens = c.MaxSummaryTokens
	reservation, err := admit(limits, settings.Price(), *report, count)
	if err != nil {
		return w, fmt.Errorf("compaction admission: %w", err)
	}
	if err = record("model.requested", map[string]any{"purpose": "compaction", "input_tokens": count, "reserved_usd": reservation, "max_output_tokens": limits.MaxOutputTokens, "context_window_tokens": settings.ContextWindowTokens, "pricing_basis": settings.PricingBasis}, false); err != nil {
		return w, err
	}
	report.CompactionAttempts++
	w.LastAttemptTokens = before
	reply, summary, err := client.Text(ctx, contextwindow.Instructions, prompt, limits.MaxOutputTokens)
	if err != nil {
		report.BillingUnknown = true
		return w, err
	}
	report.InputTokens += reply.Usage.InputTokens
	report.OutputTokens += reply.Usage.OutputTokens
	report.EstimatedUSD += settings.Price().Cost(reply.Usage.InputTokens, reply.Usage.OutputTokens)
	if err = record("model.responded", reply.Raw, false); err != nil {
		return w, err
	}
	if !reply.HasUsage() {
		report.BillingUnknown = true
		return w, errors.New("compaction omitted usage")
	}
	if reply.Status != "completed" || strings.TrimSpace(summary) == "" {
		return w, errors.New("compaction did not return a complete summary")
	}
	candidate := w.Compact(summary, c.KeepRecentTurns)
	after, err := client.Count(ctx, candidate.Input())
	if err != nil {
		return w, err
	}
	accepted := after <= before*9/10 && after <= settings.MaxInputTokens
	sourceHash, summaryHash := statefile.Hash(w.Input()), statefile.Hash(summary)
	artifact := map[string]any{"version": 1, "source_sha256": sourceHash, "summary_sha256": summaryHash, "before_tokens": before, "after_tokens": after, "accepted": accepted, "source": w.Input(), "summary": summary, "input": candidate.Input()}
	if err = writeJSON(filepath.Join(root, "compactions", fmt.Sprintf("%03d.json", report.CompactionAttempts)), artifact); err != nil {
		return w, err
	}
	if err = record("context.compacted", map[string]any{"source_sha256": sourceHash, "summary_sha256": summaryHash, "before_tokens": before, "after_tokens": after, "accepted": accepted, "retained_turns": c.KeepRecentTurns}, false); err != nil {
		return w, err
	}
	if !accepted {
		if before <= settings.MaxInputTokens {
			return w, nil
		}
		return w, errors.New("compaction did not reduce context enough; original history preserved")
	}
	report.Compactions++
	return candidate, nil
}
