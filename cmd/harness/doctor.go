package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/Siddhant-K-code/agent-harness/internal/credentials"
	"github.com/Siddhant-K-code/agent-harness/internal/diagnostics"
	"github.com/Siddhant-K-code/agent-harness/internal/task"
)

func doctor(ctx context.Context, spec task.Spec, root string, key credentials.Key, keyErr error, asJSON bool) error {
	result, err := diagnostics.Check(ctx, spec, root, key, keyErr)
	if asJSON {
		if e := json.NewEncoder(os.Stdout).Encode(result); e != nil {
			return e
		}
	} else {
		for _, check := range result.Checks {
			label := "OK"
			if !check.OK {
				label = "FAIL"
			}
			fmt.Fprintf(os.Stdout, "%s  %s: %s\n", label, check.Name, check.Detail)
		}
		fmt.Fprintln(os.Stdout, "No model request made. Key validity, account credit and model access are checked only when running a task.")
		if result.Ready {
			fmt.Fprintln(os.Stdout, "Ready. Review your task, then run: harness run")
		}
	}
	return err
}
