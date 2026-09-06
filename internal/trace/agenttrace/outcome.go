package agenttrace

import (
	"encoding/json"
	"errors"
	"io"
	"os"
)

// AttachOutcome reads the controller report, exporting only typed measurements.
// The patch, reason, workspace paths, and raw source files stay private.
func (p *Projection) AttachOutcome(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) && p.Meta["attribution"].(map[string]any)["state"] == "cancelled" && len(p.Events) == 0 {
		return nil // Cancelled before any worker started; no report was produced.
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > 64<<10 {
		return errors.New("invalid controller report")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var r struct {
		ExternalOutcomeUnknown *bool    `json:"external_outcome_unknown"`
		RunID                  string   `json:"run_id"`
		State                  string   `json:"state"`
		Verified               *bool    `json:"verified"`
		BillingUnknown         *bool    `json:"billing_unknown"`
		InputTokens            *int64   `json:"input_tokens"`
		OutputTokens           *int64   `json:"output_tokens"`
		EstimatedUSD           *float64 `json:"estimated_usd_uncached"`
		VerificationAttempts   *int     `json:"verification_attempts"`
	}
	d := json.NewDecoder(io.LimitReader(f, 64<<10+1))
	if err := d.Decode(&r); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("invalid trailing report data")
	}
	if r.RunID != p.SessionID || r.State != p.Meta["attribution"].(map[string]any)["state"] {
		return errors.New("controller report disagrees with run")
	}
	if (r.InputTokens != nil && *r.InputTokens < 0) || (r.OutputTokens != nil && *r.OutputTokens < 0) || (r.EstimatedUSD != nil && *r.EstimatedUSD < 0) {
		return errors.New("negative report usage or cost")
	}
	p.Outcome = map[string]any{"state": r.State, "verified": r.Verified, "billing_unknown": r.BillingUnknown,
		"input_tokens": r.InputTokens, "output_tokens": r.OutputTokens, "estimated_usd_uncached": r.EstimatedUSD,
		"verification_attempts": r.VerificationAttempts, "measurement_source": "controller_report",
		"cost_kind": "estimate_not_invoice", "external_outcome_unknown": r.ExternalOutcomeUnknown}
	return nil
}
