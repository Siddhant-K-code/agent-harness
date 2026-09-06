// Package task defines the versioned contract for real coding runs.
package task

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Siddhant-K-code/agent-harness/internal/integrations"
	"github.com/Siddhant-K-code/agent-harness/internal/skills"
)

const MaxFileBytes = 1 << 20
const DefaultContextWindowTokens int64 = 200000

type Limits struct {
	ContextWindowTokens int64   `json:"context_window_tokens,omitempty"`
	MaxSteps            int     `json:"max_steps"`
	TimeoutMS           int     `json:"timeout_ms"`
	ToolTimeoutMS       int     `json:"tool_timeout_ms"`
	MaxOutputTokens     int64   `json:"max_output_tokens"`
	MaxTotalTokens      int64   `json:"max_total_tokens"`
	MaxRepairs          int     `json:"max_repairs"`
	MaxUSD              float64 `json:"max_usd"`
}

// ContextWindow includes the complete request input and reserved output.
// Existing task files without this optional field use the documented default.
func (l Limits) ContextWindow() int64 {
	if l.ContextWindowTokens == 0 {
		return DefaultContextWindowTokens
	}
	return l.ContextWindowTokens
}

type Spec struct {
	Integrations  integrations.Selection `json:"integrations,omitempty,omitzero"`
	Compaction    *Compaction            `json:"compaction,omitempty"`
	Skills        []skills.Ref           `json:"skills,omitempty"`
	Learn         bool                   `json:"learn,omitempty"`
	Backend       string                 `json:"backend,omitempty"`
	AWS           *AWSConfig             `json:"aws,omitempty"`
	SchemaVersion int                    `json:"schema_version"`
	Name          string                 `json:"name"`
	Goal          string                 `json:"goal"`
	Repository    string                 `json:"repository"`
	Ref           string                 `json:"ref"`
	Model         string                 `json:"model"`
	Image         string                 `json:"image"`
	Verifier      string                 `json:"verifier"`
	Limits        Limits                 `json:"limits"`
}

// Compaction is an explicit bounded summarization policy. Omission disables it
// for existing tasks; newly scaffolded tasks opt in.
type Compaction struct {
	TriggerPercent   int   `json:"trigger_percent"`
	KeepRecentTurns  int   `json:"keep_recent_turns"`
	MaxSummaryTokens int64 `json:"max_summary_tokens"`
	MaxCompactions   int   `json:"max_compactions"`
}

func DefaultCompaction() *Compaction {
	return &Compaction{TriggerPercent: 75, KeepRecentTurns: 1, MaxSummaryTokens: 1024, MaxCompactions: 4}
}

type AWSConfig struct {
	Region        string `json:"region"`
	ExecutionRole string `json:"execution_role"`
}

// Paths are relative to the task file.
func Load(path string) (Spec, error) {
	f, err := os.Open(path)
	if err != nil {
		return Spec{}, err
	}
	defer f.Close()
	s, err := Decode(f)
	if err != nil {
		return s, err
	}
	base, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return s, err
	}
	if !filepath.IsAbs(s.Repository) {
		s.Repository = filepath.Join(base, s.Repository)
	}
	if !filepath.IsAbs(s.Verifier) {
		s.Verifier = filepath.Join(base, s.Verifier)
	}
	return s, nil
}
func Decode(r io.Reader) (Spec, error) {
	b, err := io.ReadAll(io.LimitReader(r, MaxFileBytes+1))
	if err != nil {
		return Spec{}, err
	}
	if len(b) > MaxFileBytes {
		return Spec{}, errors.New("task exceeds 1 MiB")
	}
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	var s Spec
	if err := d.Decode(&s); err != nil {
		return s, fmt.Errorf("decode task: %w", err)
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return s, errors.New("task must contain exactly one JSON object")
	}
	return s, s.Validate()
}
func (s Spec) Validate() error {
	if err := s.Integrations.Validate(); err != nil {
		return err
	}
	if len(s.Skills) > 8 {
		return errors.New("at most eight selected skills")
	}
	seen := map[string]bool{}
	for _, ref := range s.Skills {
		if err := ref.Validate(); err != nil {
			return err
		}
		if seen[ref.ID] {
			return errors.New("duplicate selected skill")
		}
		seen[ref.ID] = true
	}
	if c := s.Compaction; c != nil {
		if c.TriggerPercent < 10 || c.TriggerPercent > 90 || c.KeepRecentTurns < 1 || c.KeepRecentTurns > 10 || c.MaxSummaryTokens < 256 || c.MaxSummaryTokens > s.Limits.MaxOutputTokens || c.MaxCompactions < 1 || c.MaxCompactions > 20 {
			return errors.New("compaction requires trigger_percent 10..90, keep_recent_turns 1..10, max_summary_tokens 256..max_output_tokens, max_compactions 1..20")
		}
	}
	if s.Backend != "" && s.Backend != "docker" && s.Backend != "agentcore" {
		return errors.New("backend must be docker or agentcore")
	}
	if s.Backend == "agentcore" {
		if s.AWS == nil || s.AWS.Region != "us-east-1" || s.AWS.ExecutionRole == "" {
			return errors.New("agentcore requires aws.region us-east-1 and aws.execution_role")
		}
	} else if s.AWS != nil {
		return errors.New("aws configuration requires agentcore backend")
	}
	if s.SchemaVersion != 1 {
		return errors.New("schema_version must be 1")
	}
	for _, f := range []struct{ name, value string }{{"name", s.Name}, {"goal", s.Goal}, {"repository", s.Repository}, {"ref", s.Ref}, {"model", s.Model}, {"image", s.Image}, {"verifier", s.Verifier}} {
		if strings.TrimSpace(f.value) == "" {
			return fmt.Errorf("%s is required", f.name)
		}
		if strings.ContainsRune(f.value, 0) {
			return fmt.Errorf("%s contains NUL", f.name)
		}
	}
	if strings.HasPrefix(s.Ref, "-") || strings.HasPrefix(s.Image, "-") {
		return errors.New("ref and image cannot start with '-'")
	}
	if len(s.Name) > 200 || len(s.Goal) > 32000 {
		return errors.New("name or goal is too long")
	}
	l := s.Limits
	if l.MaxSteps < 1 || l.MaxSteps > 100 {
		return errors.New("max_steps must be 1..100")
	}
	if l.TimeoutMS < 1000 || l.TimeoutMS > 3600000 {
		return errors.New("timeout_ms must be 1000..3600000")
	}
	if l.ToolTimeoutMS < 100 || l.ToolTimeoutMS > l.TimeoutMS {
		return errors.New("tool_timeout_ms must be 100..timeout_ms")
	}
	if l.ContextWindowTokens < 0 || l.ContextWindow() < 1024 || l.ContextWindow() > 1050000 {
		return errors.New("context_window_tokens must be 1024..1050000 (omitted or 0 uses 200000); model limits also apply")
	}
	if l.MaxOutputTokens < 256 || l.MaxOutputTokens > 128000 || l.MaxOutputTokens >= l.ContextWindow() {
		return errors.New("max_output_tokens must be 256..128000 and smaller than the context window")
	}
	if l.MaxTotalTokens < l.MaxOutputTokens || l.MaxTotalTokens > 10000000 {
		return errors.New("max_total_tokens must be max_output_tokens..10000000")
	}
	if l.MaxRepairs < 0 || l.MaxRepairs > 5 {
		return errors.New("max_repairs must be 0..5")
	}
	if !(l.MaxUSD > 0 && l.MaxUSD <= 100) {
		return errors.New("max_usd must be greater than 0 and at most 100")
	}
	return nil
}
