package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/Siddhant-K-code/agent-harness/internal/model"
	"github.com/Siddhant-K-code/agent-harness/internal/task"
)

type modelFlags struct {
	model                        *string
	contextWindow, output, total *int64
	usd                          *float64
}

func bindModelFlags(f *flag.FlagSet, defaults bool) modelFlags {
	var id string
	var window, output, total int64
	var usd float64
	if defaults {
		id, window, output, total, usd = "gpt-5.4", task.DefaultContextWindowTokens, 2048, 50000, 0.5
	}
	return modelFlags{
		f.String("model", id, "OpenAI model ID (harness models lists configured models; omitted uses task)"),
		f.Int64("context-window", window, "tokens per request, including input plus reserved output (omitted uses task)"),
		f.Int64("max-output-tokens", output, "maximum output tokens per request, including reasoning (omitted uses task)"),
		f.Int64("max-total-tokens", total, "cumulative input/output token budget across the run (omitted uses task)"),
		f.Float64("max-usd", usd, "estimated model cost limit per run in USD (omitted uses task)"),
	}
}

func (m modelFlags) apply(f *flag.FlagSet, spec *task.Spec) int {
	count := 0
	f.Visit(func(v *flag.Flag) {
		switch v.Name {
		case "model":
			spec.Model = *m.model
		case "context-window":
			spec.Limits.ContextWindowTokens = *m.contextWindow
		case "max-output-tokens":
			spec.Limits.MaxOutputTokens = *m.output
		case "max-total-tokens":
			spec.Limits.MaxTotalTokens = *m.total
		case "max-usd":
			spec.Limits.MaxUSD = *m.usd
		default:
			return
		}
		count++
	})
	return count
}

func modelsCommand(args []string) error {
	f := flag.NewFlagSet("models", flag.ContinueOnError)
	asJSON := f.Bool("json", false, "emit the configured model catalog as JSON")
	if err := f.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if f.NArg() != 0 {
		return errors.New("models takes no positional arguments")
	}
	if *asJSON {
		return json.NewEncoder(os.Stdout).Encode(model.Catalog())
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "MODEL\tMAX CONTEXT\tMAX OUTPUT\tINPUT $/1M\tOUTPUT $/1M")
	for _, p := range model.Catalog() {
		fmt.Fprintf(w, "%s\t%d\t%d\t%.2f\t%.2f\n", p.ID, p.ContextWindowTokens, p.MaxOutputTokens, p.InputUSDPerMillion, p.OutputUSDPerMillion)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, "Standard uncached prices. GPT-5.4 uses 2x input and 1.5x output estimates for configurations that can exceed 272000 input tokens.\nCatalog capabilities checked 2026-09-06; account access is not checked. Use a smaller context window to bound each request.")
	return nil
}

func configCommand(args []string) error {
	if len(args) == 0 {
		args = []string{"show"}
	}
	if args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintln(os.Stdout, "Usage: harness config show|set [--task PATH] [--model ID] [--context-window TOKENS] [--max-output-tokens TOKENS] [--max-total-tokens TOKENS] [--max-usd USD]\nSettings are saved in the task JSON. show previews overrides; set persists them. No API calls or key access.")
		return nil
	}
	action := args[0]
	if action != "show" && action != "set" {
		return errors.New("use harness config show or harness config set")
	}
	f := flag.NewFlagSet("config "+action, flag.ContinueOnError)
	path := f.String("task", "harness.task.json", "task JSON file to inspect or update")
	flags := bindModelFlags(f, false)
	if err := f.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if f.NArg() != 0 {
		return errors.New("config takes no positional arguments; use --task PATH")
	}
	// Decode without resolving repository/verifier paths, so saving model
	// settings preserves the task's original relative paths and other fields.
	file, err := os.Open(*path)
	if err != nil {
		return err
	}
	spec, err := task.Decode(file)
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	changed := flags.apply(f, &spec)
	settings, err := model.ResolveSettings(spec)
	if err != nil {
		return err
	}
	if action == "set" {
		if changed == 0 {
			return errors.New("config set requires at least one model/budget flag; use config show to inspect settings")
		}
		spec.Limits.ContextWindowTokens = settings.ContextWindowTokens
		if err := saveTaskConfig(*path, spec); err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, "Saved task settings to", *path)
	}
	result := struct {
		Task      task.Spec      `json:"task"`
		Effective model.Settings `json:"effective"`
		Saved     bool           `json:"saved"`
	}{spec, settings, action == "set"}
	e := json.NewEncoder(os.Stdout)
	e.SetIndent("", "  ")
	return e.Encode(result)
}

func saveTaskConfig(path string, spec task.Spec) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("task config must be a regular file; refusing to replace a symlink")
	}
	b, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".harness-task-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(info.Mode().Perm()); err != nil {
		_ = f.Close()
		return err
	}
	_, writeErr := f.Write(append(b, '\n'))
	if err := errors.Join(writeErr, f.Sync(), f.Close()); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
