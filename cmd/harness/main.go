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

	"github.com/Siddhant-K-code/agent-harness/internal/credentials"
	"github.com/Siddhant-K-code/agent-harness/internal/runner"
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
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(os.Stdout, usage)
		return nil
	}
	switch args[0] {
	case "version", "--version":
		fmt.Fprintf(os.Stdout, "harness %s (commit %s)\n", version, commit)
		return nil
	case "init":
		return initCommand(args[1:])
	case "auth":
		return authCommand(args[1:])
	case "trace":
		if len(args) > 1 && args[1] == "setup" {
			return traceSetup(args[2:])
		}
	case "run", "doctor", "list", "status", "cancel", "events", "reconcile":
	default:
		return fmt.Errorf("unknown command %q; use harness --help", args[0])
	}
	f := flag.NewFlagSet(args[0], flag.ContinueOnError)
	rootFlag := f.String("state-dir", ".harness", "local state and artifact directory")
	keyFile, taskFile := new(string), new(string)
	traceOutput, tracePython, traceContent := new(string), new(string), new(bool)
	jsonOutput := new(bool)
	if args[0] == "run" || args[0] == "doctor" {
		keyFile = f.String("api-key-file", "", "private key file (OPENAI_API_KEY takes precedence)")
		taskFile = f.String("task", "harness.task.json", "task JSON file")
	}
	if args[0] == "doctor" {
		jsonOutput = f.Bool("json", false, "emit machine-readable diagnostics")
	}
	if args[0] == "trace" {
		traceOutput = f.String("output", "", "AgentTrace root (default: <state-dir>/traces)")
		tracePython = f.String("python", "", "Python with pinned AgentTrace (default: <state-dir>/agenttrace-venv/bin/python)")
		traceContent = f.Bool("include-content", false, "export selected command/output content with AgentTrace redaction")
	}
	if err := f.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	root, err := filepath.Abs(*rootFlag)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	encode := func(v any) error { e := json.NewEncoder(os.Stdout); e.SetIndent("", "  "); return e.Encode(v) }
	if args[0] == "run" || args[0] == "doctor" {
		if *taskFile == "" {
			return errors.New("--task is required")
		}
		if f.NArg() != 0 {
			return errors.New("unexpected positional argument")
		}
		spec, err := task.Load(*taskFile)
		if err != nil {
			return fmt.Errorf("load task: %w; use harness init for a demo or --task PATH", err)
		}
		key, keyErr := credentials.Resolve(*keyFile, root)
		if args[0] == "doctor" {
			return doctor(ctx, spec, root, key, keyErr, *jsonOutput)
		}
		if keyErr != nil {
			return keyErr
		}
		db, err := store.Open(filepath.Join(root, "harness.db"))
		if err != nil {
			return err
		}
		defer db.Close()
		fmt.Fprintf(os.Stderr, "Running %s with %s; estimated model budget $%.2f. Artifacts: %s\n", spec.Name, spec.Model, spec.Limits.MaxUSD, root)
		report, runErr := (runner.Runner{Store: db, Root: root, Key: key.Value, Progress: os.Stderr}).Run(ctx, spec)
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
		reconcileCtx, stop := context.WithTimeout(ctx, 15*time.Minute)
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
