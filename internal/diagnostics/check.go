// Package diagnostics performs unpaid readiness checks shared by CLI and web setup.
package diagnostics

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Siddhant-K-code/agent-harness/internal/credentials"
	"github.com/Siddhant-K-code/agent-harness/internal/integrations"
	"github.com/Siddhant-K-code/agent-harness/internal/model"
	"github.com/Siddhant-K-code/agent-harness/internal/sandbox"
	"github.com/Siddhant-K-code/agent-harness/internal/sandbox/agentcore"
	"github.com/Siddhant-K-code/agent-harness/internal/scaffold"
	"github.com/Siddhant-K-code/agent-harness/internal/skills"
	"github.com/Siddhant-K-code/agent-harness/internal/task"
)

type Diagnostic struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}
type Result struct {
	Ready         bool            `json:"ready"`
	KeyConfigured bool            `json:"key_configured"`
	Credential    credentials.Key `json:"credential"`
	APIValidated  bool            `json:"api_validated"`
	Backend       string          `json:"backend"`
	ImageID       string          `json:"image_id"`
	Model         string          `json:"model"`
	MaxUSD        float64         `json:"max_usd"`
	Checks        []Diagnostic    `json:"checks"`
	ModelSettings model.Settings  `json:"model_settings"`
}

func Check(ctx context.Context, spec task.Spec, root string, key credentials.Key, keyErr error) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var checks []Diagnostic
	var failures []error
	add := func(name, detail string, err error) {
		if err != nil {
			detail = err.Error()
			failures = append(failures, err)
		}
		checks = append(checks, Diagnostic{name, err == nil, detail})
	}
	add("OpenAI key", key.Source+" (presence only; API access not validated)", keyErr)
	pinned, _, skillErr := (skills.Store{Root: root}).Resolve(spec.Repository, spec.Skills)
	add("Skills", fmt.Sprintf("%d explicitly selected, repository-scoped versions", len(pinned)), skillErr)
	compaction := "disabled"
	if c := spec.Compaction; c != nil {
		compaction = fmt.Sprintf("at %d%% input capacity; %d recent turns; %d summary tokens; up to %d compactions", c.TriggerPercent, c.KeepRecentTurns, c.MaxSummaryTokens, c.MaxCompactions)
	}
	add("Compaction", compaction, nil)
	configuration, configErr := integrations.Load(root)
	if configErr == nil {
		configErr = configuration.Authorize(spec.Integrations)
	}
	add("Integrations", "operator policy checked; use harness integrations check to connect", configErr)
	add("Learning", fmt.Sprintf("local observation capture: %t; skill promotion requires evaluation", spec.Learn), nil)
	settings, err := model.ResolveSettings(spec)
	add("Model", fmt.Sprintf("%s; estimated model budget $%.2f per run", spec.Model, spec.Limits.MaxUSD), err)
	if err == nil {
		add("Context", fmt.Sprintf("%d total = up to %d input + %d reserved output tokens; %d tokens across the run", settings.ContextWindowTokens, settings.MaxInputTokens, settings.MaxOutputTokens, settings.MaxTotalTokens), nil)
		add("Pricing", fmt.Sprintf("%s: $%.2f input / $%.2f output per million tokens", settings.PricingBasis, settings.InputUSDPerMillion, settings.OutputUSDPerMillion), nil)
	}
	base, err := scaffold.Git(ctx, spec.Repository, "rev-parse", "--verify", "--end-of-options", spec.Ref+"^{commit}")
	add("Git commit", base+"; only committed files will be copied", err)
	if err == nil {
		if files, e := scaffold.Git(ctx, spec.Repository, "ls-tree", "--name-only", spec.Ref, "--", ".gitmodules"); e != nil || files != "" {
			if e == nil {
				e = errors.New("submodules are not supported; select a repository without .gitmodules")
			}
			add("Repository support", "", e)
		}
	}
	info, err := os.Stat(spec.Verifier)
	if err == nil && (!info.Mode().IsRegular() || info.Size() < 1 || info.Size() > 1<<20) {
		err = errors.New("verifier must be a regular file of 1 byte..1 MiB")
	}
	if err == nil {
		f, openErr := os.Open(spec.Verifier)
		err = openErr
		if f != nil {
			_ = f.Close()
		}
	}
	add("Verifier", spec.Verifier+"; check script present (not executed)", err)
	backend, image := spec.Backend, spec.Image
	if backend == "" {
		backend = "docker"
	}
	if backend == "agentcore" {
		_, err = agentcore.Check(ctx, spec.AWS.Region, spec.Image, spec.AWS.ExecutionRole)
	} else {
		image, err = sandbox.Check(ctx, spec.Image)
		if err == nil && os.Getuid() == 0 {
			err = errors.New("run the Docker controller as a non-root user")
		}
		if err == nil && strings.Contains(root, ",") {
			err = errors.New("Docker state directory cannot contain a comma; use --state-dir PATH")
		}
	}
	add("Executor", backend+"; "+image, err)
	// Test artifact storage using a temporary file, without opening a run database.
	err = os.MkdirAll(root, 0700)
	if err == nil {
		f, e := os.CreateTemp(root, ".doctor-*")
		err = e
		if f != nil {
			err = f.Close()
			_ = os.Remove(f.Name())
		}
	}
	add("Local state", filepath.Clean(root), err)

	result := Result{Ready: len(failures) == 0, KeyConfigured: keyErr == nil, Credential: key, Backend: backend, ImageID: image, Model: spec.Model, MaxUSD: spec.Limits.MaxUSD, Checks: checks, ModelSettings: settings}
	if len(failures) > 0 {
		return result, fmt.Errorf("setup needs attention (%d failed checks)", len(failures))
	}
	return result, nil
}
