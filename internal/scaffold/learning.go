package scaffold

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	demo "github.com/Siddhant-K-code/agent-harness/examples/learning"
	"github.com/Siddhant-K-code/agent-harness/internal/task"
)

// CreateLearning installs an unpaid, isolated lab. The three functions and
// separate verifiers are real; improvement is never guaranteed or simulated.
func CreateLearning(ctx context.Context, directory string) (dir string, err error) {
	dir, err = filepath.Abs(directory)
	if err != nil {
		return "", err
	}
	if err = os.Mkdir(dir, 0700); err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(dir)
		}
	}()
	repo := filepath.Join(dir, "repo")
	if err = os.Mkdir(repo, 0700); err != nil {
		return "", err
	}
	for _, name := range []string{"utilities.js", "words.verify.sh", "sum.verify.sh", "clamp.verify.sh", "SKILL.md"} {
		b, e := demo.Files.ReadFile(name)
		if e != nil {
			return "", e
		}
		path := filepath.Join(dir, name)
		if name == "utilities.js" {
			path = filepath.Join(repo, name)
		}
		if err = os.WriteFile(path, b, 0600); err != nil {
			return "", err
		}
	}
	for _, args := range [][]string{{"init", "--template="}, {"add", "--", "utilities.js"}, {"-c", "user.name=Harness", "-c", "user.email=harness@example.invalid", "commit", "-m", "Add learning lab bug fixtures"}} {
		if _, err = Git(ctx, repo, args...); err != nil {
			return "", err
		}
	}
	base, err := Git(ctx, repo, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	goals := map[string]string{
		"words": "Fix only uniqueWords in utilities.js. Trim each string, remove empty strings, deduplicate case-insensitively while retaining the first trimmed spelling and order. Do not mutate input. Preserve other exports.",
		"sum":   "Fix only finiteSum in utilities.js. Sum only finite numbers (including fractions and negatives). Ignore non-numbers, NaN and infinities without coercion. Return zero for an empty array and do not mutate input. Preserve other exports.",
		"clamp": "Fix only clamp in utilities.js. For numeric non-NaN value, min and max, return value constrained to the inclusive [min,max] range. Reject non-numbers and NaN with TypeError. Reject min > max with RangeError. Preserve other exports.",
	}
	for name, goal := range goals {
		s := task.Spec{SchemaVersion: 1, Name: name, Goal: goal, Repository: "repo", Ref: base, Model: "gpt-5.4", Image: "node:22-alpine", Verifier: name + ".verify.sh", Compaction: task.DefaultCompaction(), Learn: name == "words", Limits: task.Limits{ContextWindowTokens: 200000, MaxSteps: 12, TimeoutMS: 300000, ToolTimeoutMS: 30000, MaxOutputTokens: 2048, MaxTotalTokens: 40000, MaxRepairs: 2, MaxUSD: 0.15}}
		b, e := json.MarshalIndent(s, "", "  ")
		if e != nil {
			return "", e
		}
		if err = os.WriteFile(filepath.Join(dir, name+".task.json"), append(b, '\n'), 0600); err != nil {
			return "", err
		}
	}
	if err = os.WriteFile(filepath.Join(dir, "suite.json"), []byte(`{
  "schema": 1,
  "name": "JavaScript utility skill comparison",
  "repetitions": 2,
  "cases": [
    {"id":"sum", "task":"sum.task.json", "role":"regression"},
    {"id":"clamp", "task":"clamp.task.json", "role":"holdout"}
  ]
}
`), 0600); err != nil {
		return "", err
	}
	if err = os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(".harness/\n"), 0600); err != nil {
		return "", err
	}
	if err = os.WriteFile(filepath.Join(dir, "README.md"), []byte(fmt.Sprintf("# Learning lab\n\nThree real bug-fix tasks pinned to %s, with separate controller-owned verifiers.\nThe words task supplies observations. The sum and clamp tasks compare revisions; clamp is held out from proposal inputs.\nThe suite runs eight paid GPT-5.4 tasks, reserving at most $1.20 in model estimates. A proposal reserves $0.10 separately.\nThis is a capability demo, not evidence of general improvement. A rejected candidate is a valid outcome.\n", base)), 0600); err != nil {
		return "", err
	}
	return dir, nil
}
