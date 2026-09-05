package sandbox

import (
	"context"
	"fmt"
	"regexp"

	"github.com/Siddhant-K-code/agent-harness/internal/process"
)

// Executor is the lifecycle boundary used by the controller. References must
// be journaled before Execute, so an interrupted dispatch can be reconciled.
type Executor interface {
	Capabilities() Capabilities
	Execute(context.Context, Request) (process.Result, error)
	Inspect(context.Context, string) (ExecutionState, error)
	Stop(context.Context, string) error
}

type Capabilities struct {
	Backend            string `json:"backend"`
	Isolation          string `json:"isolation"`
	NetworkDisabled    bool   `json:"network_disabled"`
	ReadOnlyWorkspace  bool   `json:"read_only_workspace"`
	DurableExecutionID bool   `json:"durable_execution_id"`
	DiskQuota          bool   `json:"disk_quota"`
}

type Request struct {
	ID        string
	Command   string
	ReadOnly  bool
	TimeoutMS int
}

type ExecutionState struct {
	ID      string `json:"id"`
	Exists  bool   `json:"exists"`
	Running bool   `json:"running"`
}

var referencePattern = regexp.MustCompile(`^agent-harness-[a-f0-9]{32}-[0-9]+$`)

func Reference(runID string, sequence int) (string, error) {
	id := fmt.Sprintf("agent-harness-%s-%d", runID, sequence)
	if !referencePattern.MatchString(id) || sequence < 1 {
		return "", fmt.Errorf("invalid execution reference")
	}
	return id, nil
}
