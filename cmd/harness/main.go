package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Siddhant-K-code/agent-harness/internal/model"
	"github.com/Siddhant-K-code/agent-harness/internal/runner"
	"github.com/Siddhant-K-code/agent-harness/internal/sandbox"
	"github.com/Siddhant-K-code/agent-harness/internal/store"
	"github.com/Siddhant-K-code/agent-harness/internal/task"
	"github.com/Siddhant-K-code/agent-harness/internal/trace/agenttrace"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: harness <run|doctor|list|status|cancel|events|trace|reconcile> [flags]")
	}
	f := flag.NewFlagSet(args[0], flag.ContinueOnError)
	rootFlag := f.String("state-dir", ".harness", "local state and artifact directory")
	keyFile := f.String("api-key-file", "", "key file (default: <state-dir>/openai-key)")
	taskFile := f.String("task", "", "task JSON file")
	traceOutput := f.String("output", "", "AgentTrace root (default: <state-dir>/traces)")
	tracePython := f.String("python", "", "Python with pinned AgentTrace (default: <state-dir>/agenttrace-venv/bin/python)")
	traceContent := f.Bool("include-content", false, "export selected command/output content with AgentTrace redaction")
	if err := f.Parse(args[1:]); err != nil {
		return err
	}
	root, err := filepath.Abs(*rootFlag)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	encode := func(v any) error { e := json.NewEncoder(os.Stdout); e.SetIndent("", "  "); return e.Encode(v) }
	switch args[0] {
	case "run", "doctor", "list", "status", "cancel", "events", "trace", "reconcile":
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
	if args[0] == "run" || args[0] == "doctor" {
		if *taskFile == "" {
			return errors.New("--task is required")
		}
		if f.NArg() != 0 {
			return errors.New("unexpected positional argument")
		}
		spec, err := task.Load(*taskFile)
		if err != nil {
			return err
		}
		if *keyFile == "" {
			*keyFile = filepath.Join(root, "openai-key")
		}
		key, keyErr := model.LoadKey(*keyFile)
		if args[0] == "doctor" {
			checkCtx, stop := context.WithTimeout(ctx, 15*time.Second)
			defer stop()
			image, dockerErr := sandbox.Check(checkCtx, spec.Image)
			result := map[string]any{"key_configured": keyErr == nil, "image_id": image, "model": spec.Model, "max_usd": spec.Limits.MaxUSD}
			if keyErr != nil {
				result["key_error"] = keyErr.Error()
			}
			if dockerErr != nil {
				result["docker_error"] = dockerErr.Error()
			}
			if err = encode(result); err != nil {
				return err
			}
			return errors.Join(keyErr, dockerErr)
		}
		if keyErr != nil {
			return keyErr
		}
		db, err := store.Open(filepath.Join(root, "harness.db"))
		if err != nil {
			return err
		}
		defer db.Close()
		report, runErr := (runner.Runner{Store: db, Root: root, Key: key, Progress: os.Stderr}).Run(ctx, spec)
		return errors.Join(runErr, encode(report))
	}
	if args[0] == "list" {
		if f.NArg() != 0 {
			return errors.New("list takes no run ID")
		}
	} else if f.NArg() != 1 {
		return errors.New("exactly one run ID is required; put flags before it")
	}
	db, err := store.Open(filepath.Join(root, "harness.db"))
	if err != nil {
		return err
	}
	defer db.Close()
	switch args[0] {
	case "reconcile":
		reconcileCtx, stop := context.WithTimeout(ctx, 2*time.Minute)
		defer stop()
		report, e := runner.Reconcile(reconcileCtx, db, root, f.Arg(0))
		if e != nil {
			return e
		}
		return encode(report)
	case "trace":
		v, e := db.Get(ctx, f.Arg(0))
		if e != nil {
			return e
		}
		events, e := db.Events(ctx, v.ID)
		if e != nil {
			return e
		}
		projection, e := agenttrace.Build(v, events, *traceContent)
		if e != nil {
			return e
		}
		if e = projection.AttachOutcome(filepath.Join(root, "runs", v.ID, "report.json")); e != nil {
			return e
		}
		if *traceOutput == "" {
			*traceOutput = filepath.Join(root, "traces")
		}
		if *tracePython == "" {
			*tracePython = filepath.Join(root, "agenttrace-venv", "bin", "python")
		}
		exportCtx, stop := context.WithTimeout(ctx, 2*time.Minute)
		defer stop()
		result, e := agenttrace.Export(exportCtx, *tracePython, *traceOutput, projection)
		if e != nil {
			return e
		}
		return encode(result)
	case "list":
		v, e := db.List(ctx)
		if e != nil {
			return e
		}
		return encode(v)
	case "status":
		v, e := db.Get(ctx, f.Arg(0))
		if e != nil {
			return e
		}
		return encode(v)
	case "cancel":
		v, e := db.RequestCancel(ctx, f.Arg(0))
		if e != nil {
			return e
		}
		return encode(v)
	case "events":
		events, e := db.Events(ctx, f.Arg(0))
		if e != nil {
			return e
		}
		for _, event := range events {
			if e := json.NewEncoder(os.Stdout).Encode(event); e != nil {
				return e
			}
		}
		return nil
	}
	return nil
}
