package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Siddhant-K-code/agent-harness/internal/credentials"
	"github.com/Siddhant-K-code/agent-harness/internal/model"
	"github.com/Siddhant-K-code/agent-harness/internal/sandbox"
	"github.com/Siddhant-K-code/agent-harness/internal/sandbox/agentcore"
	"github.com/Siddhant-K-code/agent-harness/internal/scaffold"
	"github.com/Siddhant-K-code/agent-harness/internal/task"
)

type diagnostic struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

func doctor(ctx context.Context, spec task.Spec, root string, key credentials.Key, keyErr error, asJSON bool) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var checks []diagnostic
	var failures []error
	add := func(name, detail string, err error) {
		if err != nil {
			detail = err.Error()
			failures = append(failures, err)
		}
		checks = append(checks, diagnostic{name, err == nil, detail})
	}
	add("OpenAI key", key.Source+" (presence only; API access not validated)", keyErr)
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
	if asJSON {
		result := map[string]any{"ready": len(failures) == 0, "key_configured": keyErr == nil, "credential": key, "api_validated": false, "backend": backend, "image_id": image, "model": spec.Model, "max_usd": spec.Limits.MaxUSD, "checks": checks, "model_settings": settings}
		if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
			return err
		}
	} else {
		for _, check := range checks {
			label := "OK"
			if !check.OK {
				label = "FAIL"
			}
			fmt.Fprintf(os.Stdout, "%s  %s: %s\n", label, check.Name, check.Detail)
		}
		fmt.Fprintln(os.Stdout, "No model request made. Key validity, account credit and model access are checked only when running a task.")
		if len(failures) == 0 {
			fmt.Fprintln(os.Stdout, "Ready. Review your task, then run: harness run")
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("setup needs attention (%d failed checks)", len(failures))
	}
	return nil
}
