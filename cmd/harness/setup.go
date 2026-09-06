package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	traceinstall "github.com/Siddhant-K-code/agent-harness/integrations/agenttrace"
	"github.com/Siddhant-K-code/agent-harness/internal/credentials"
	"github.com/Siddhant-K-code/agent-harness/internal/scaffold"
	"golang.org/x/term"
)

var version = "dev"
var commit = "unknown"

const usage = `harness — bounded coding tasks with independent verification

First run (Git and a running Docker daemon required):
  harness init                 Create the bundled demo in ./harness-demo
  cd harness-demo
  harness auth login           Save your OpenAI key locally, with hidden input
  docker pull node:22-alpine    Prepare the demo environment
  harness doctor               Check setup; no model request or cloud creation
  harness run                  Run the task; real API charges, default max $0.50

Commands:
  init [flags] [directory]     Create a demo or a task for your own repository
  models [--json]              List configured models and their capacity limits
  config show|set [flags]      Preview or save model, context and budget settings
  auth login|status|logout     Manage the local OpenAI key; no secret uploads
  doctor [--json]              Check key source, Git, verifier, model and executor
  run                         Execute harness.task.json (override with --task)
  list                        List runs in the current state directory
  status RUN_ID               Inspect a run
  events RUN_ID               Export controller events as JSONL
  cancel RUN_ID               Request cancellation
  reconcile RUN_ID            Clean up after an interrupted controller
  trace setup                 Install the optional pinned native AgentTrace reader
  trace RUN_ID                Export a native AgentTrace session
  version                     Show version and source commit

Use harness COMMAND --help for flags. Put flags before the directory/run ID.
State defaults to .harness in the current directory. Results go to stdout;
progress goes to stderr. See docs/getting-started.md for setup and key precedence.
`

func initCommand(args []string) error {
	f := flag.NewFlagSet("init", flag.ContinueOnError)
	o := scaffold.Options{}
	f.StringVar(&o.Repository, "repo", "", "local Git repository (omit for the bundled demo)")
	f.StringVar(&o.Ref, "ref", "HEAD", "commit/ref to pin for --repo")
	f.StringVar(&o.Goal, "goal", "", "coding task to complete; required with --repo")
	f.StringVar(&o.Image, "image", "", "prepared Docker image; required with --repo")
	f.StringVar(&o.Verifier, "verifier", "", "independent shell check script; required with --repo")
	models := bindModelFlags(f, true)
	if err := f.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if f.NArg() > 1 {
		return errors.New("init takes at most one new directory; put flags first")
	}
	o.Model, o.MaxUSD = *models.model, *models.usd
	o.ContextWindowTokens, o.MaxOutputTokens, o.MaxTotalTokens = models.contextWindow, models.output, models.total
	o.Directory = f.Arg(0)
	if o.Directory == "" {
		o.Directory = "harness-demo"
		if o.Repository != "" {
			o.Directory = "harness-task"
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dir, err := scaffold.Create(ctx, o)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "Created task in %s\n\nNext:\n  cd %s\n  harness auth login  # or use OPENAI_API_KEY from your secret manager\n", dir, shellQuote(dir))
	image := o.Image
	if image == "" {
		image = "node:22-alpine"
	}
	fmt.Fprintf(os.Stdout, "  docker pull %s  # or build your prepared image\n  harness doctor\n  harness run\n\nReview harness.task.json first. Model budget: $%.2f per run; infrastructure costs are separate.\nSetup has not called a model, pulled an image, or created cloud resources.\n", shellQuote(image), o.MaxUSD)
	return nil
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }

func authCommand(args []string) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintln(os.Stdout, "Usage: harness auth login [--stdin] [--replace] | status [--state-dir PATH] [--api-key-file PATH] | logout\nLogin stores a plaintext, owner-only key on this machine. No GitHub or hosted-service upload.\nUse OPENAI_API_KEY from a secret manager for ephemeral credentials.")
		return nil
	}
	if args[0] != "login" && args[0] != "status" && args[0] != "logout" {
		return errors.New("use harness auth login, status, or logout")
	}
	f := flag.NewFlagSet("auth "+args[0], flag.ContinueOnError)
	stdin, replace := new(bool), new(bool)
	stateDir, explicit := new(string), new(string)
	*stateDir = ".harness"
	if args[0] == "login" {
		stdin = f.Bool("stdin", false, "read a key from piped stdin (for secret managers)")
		replace = f.Bool("replace", false, "replace the previously saved local key")
	}
	if args[0] == "status" {
		stateDir = f.String("state-dir", ".harness", "state directory for legacy per-task key lookup")
		explicit = f.String("api-key-file", "", "explicit private key file")
	}
	if err := f.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if f.NArg() != 0 {
		return errors.New("auth takes no key or other positional argument; use the hidden prompt or --stdin")
	}
	if args[0] == "status" {
		key, err := credentials.Resolve(*explicit, *stateDir)
		result := map[string]any{"provider": "openai", "configured": err == nil, "credential": key, "api_validated": false}
		if err != nil {
			result["error"] = err.Error()
		}
		return errors.Join(err, json.NewEncoder(os.Stdout).Encode(result))
	}
	path, err := credentials.Path()
	if err != nil {
		return err
	}
	if args[0] == "logout" {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		fmt.Fprintln(os.Stdout, "Removed the saved user-config key. Environment variables and per-task key files are unchanged; use harness auth status to check the effective source.")
		return nil
	}
	fmt.Fprintf(os.Stderr, "Save your OpenAI key locally at %s (plaintext, owner-only permissions).\nNo key is uploaded by this command.\n", path)
	var value []byte
	if *stdin {
		value, err = io.ReadAll(io.LimitReader(os.Stdin, credentials.MaxKeyBytes+1))
	} else {
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return errors.New("use an interactive terminal for hidden input, or --stdin with a secret manager")
		}
		fmt.Fprint(os.Stderr, "OpenAI API key (hidden): ")
		value, err = term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
	}
	if err != nil {
		return errors.New("could not read API key")
	}
	if len(value) > credentials.MaxKeyBytes {
		return errors.New("API key input exceeds 4096 bytes")
	}
	if err := credentials.Save(path, string(value), *replace); err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, "Key saved locally. No API request was made; run harness doctor in your task directory.")
	if strings.TrimSpace(os.Getenv("OPENAI_API_KEY")) != "" {
		fmt.Fprintln(os.Stdout, "OPENAI_API_KEY is set and takes precedence over the saved key.")
	}
	return nil
}

func traceSetup(args []string) error {
	f := flag.NewFlagSet("trace setup", flag.ContinueOnError)
	stateDir := f.String("state-dir", ".harness", "state directory for the optional native reader")
	python := f.String("python", "python3", "Python 3.12+ used to create the virtual environment")
	if err := f.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if f.NArg() != 0 {
		return errors.New("trace setup takes no positional arguments")
	}
	root, err := filepath.Abs(*stateDir)
	if err != nil {
		return err
	}
	venv := filepath.Join(root, "agenttrace-venv")
	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}
	if _, err := os.Lstat(venv); !errors.Is(err, os.ErrNotExist) {
		return errors.New("AgentTrace environment already exists; use harness trace RUN_ID, or remove that environment explicitly before reinstalling")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	fmt.Fprintln(os.Stderr, "Installing the pinned native AgentTrace dependency from GitHub into", venv)
	cmd := exec.CommandContext(ctx, *python, "-m", "venv", venv)
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	cmd = exec.CommandContext(ctx, filepath.Join(venv, "bin", "python"), "-m", "pip", "install", "--no-cache-dir", "-r", "/dev/stdin")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = strings.NewReader(traceinstall.Requirements), os.Stderr, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("AgentTrace installation failed; remove %s before retrying: %w", venv, err)
	}
	fmt.Fprintln(os.Stdout, "Native AgentTrace ready. Export with: harness trace RUN_ID")
	return nil
}
