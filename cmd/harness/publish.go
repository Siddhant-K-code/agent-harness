package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Siddhant-K-code/agent-harness/internal/delivery"
	"github.com/Siddhant-K-code/agent-harness/internal/store"
)

func publishCommand(args []string) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "help" {
		fmt.Println("Usage: harness publish preview|status|reconcile [--state-dir PATH] RUN_ID\n       harness publish approve [--state-dir PATH] --approval-hash HASH RUN_ID\nPreview includes the patch, target, commit and an approval hash. Approve creates a branch and draft PR through host gh credentials. Put flags before the run ID.")
		return nil
	}
	f := flag.NewFlagSet("publish "+args[0], flag.ContinueOnError)
	rootFlag := f.String("state-dir", ".harness", "controller state directory")
	hash := f.String("approval-hash", "", "exact hash from the reviewed preview")
	if err := f.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if f.NArg() != 1 {
		return errors.New("one run ID is required; put flags before it")
	}
	root, err := filepath.Abs(*rootFlag)
	if err != nil {
		return err
	}
	db, err := store.Open(filepath.Join(root, "harness.db"))
	if err != nil {
		return err
	}
	defer db.Close()
	s := delivery.Service{Root: root, DB: db, Remote: delivery.GitHub{}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var value any
	switch args[0] {
	case "preview":
		value, err = s.Prepare(ctx, f.Arg(0))
	case "status":
		value, err = s.Status(f.Arg(0))
	case "reconcile":
		value, err = s.Reconcile(ctx, f.Arg(0))
	case "approve":
		value, err = s.Publish(ctx, f.Arg(0), *hash)
	default:
		return errors.New("publish expects preview, approve, status, or reconcile")
	}
	e := json.NewEncoder(os.Stdout)
	e.SetIndent("", "  ")
	return errors.Join(err, e.Encode(value))
}
