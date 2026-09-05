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
)

const MaxFileBytes = 1 << 20

type Limits struct {
	MaxSteps        int     `json:"max_steps"`
	TimeoutMS       int     `json:"timeout_ms"`
	ToolTimeoutMS   int     `json:"tool_timeout_ms"`
	MaxOutputTokens int64   `json:"max_output_tokens"`
	MaxTotalTokens  int64   `json:"max_total_tokens"`
	MaxRepairs      int     `json:"max_repairs"`
	MaxUSD          float64 `json:"max_usd"`
}
type Spec struct {
	Backend       string     `json:"backend,omitempty"`
	AWS           *AWSConfig `json:"aws,omitempty"`
	SchemaVersion int        `json:"schema_version"`
	Name          string     `json:"name"`
	Goal          string     `json:"goal"`
	Repository    string     `json:"repository"`
	Ref           string     `json:"ref"`
	Model         string     `json:"model"`
	Image         string     `json:"image"`
	Verifier      string     `json:"verifier"`
	Limits        Limits     `json:"limits"`
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
	if l.MaxOutputTokens < 256 || l.MaxOutputTokens > 16384 {
		return errors.New("max_output_tokens must be 256..16384")
	}
	if l.MaxTotalTokens < l.MaxOutputTokens || l.MaxTotalTokens > 1000000 {
		return errors.New("max_total_tokens must be max_output_tokens..1000000")
	}
	if l.MaxRepairs < 0 || l.MaxRepairs > 5 {
		return errors.New("max_repairs must be 0..5")
	}
	if !(l.MaxUSD > 0 && l.MaxUSD <= 100) {
		return errors.New("max_usd must be greater than 0 and at most 100")
	}
	return nil
}
