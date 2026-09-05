package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/Siddhant-K-code/agent-harness/internal/sandbox/agentcore"
	"os"
	"time"
)

func main() {
	root := flag.String("state-dir", ".harness/aws-e2e", "controller state directory to reconcile")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	count, err := agentcore.CleanupTree(ctx, *root)
	json.NewEncoder(os.Stdout).Encode(map[string]any{"recorded_executions_cleaned": count, "cleanup_confirmed": err == nil})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
