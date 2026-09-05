package task

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestStrictRealTask(t *testing.T) {
	b, err := os.ReadFile("../../examples/normalize-tags/task.json")
	if err != nil {
		t.Fatal(err)
	}
	s, err := Decode(strings.NewReader(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	if s.Model != "gpt-5.4" || s.Limits.MaxUSD != 2 {
		t.Fatal("wrong task")
	}
	for _, input := range []string{string(b) + " {}", strings.Replace(string(b), `"schema_version": 1`, `"schema_version": 1, "backend": "fake"`, 1), strings.Replace(string(b), `"max_usd": 2`, `"max_usd": 0`, 1)} {
		if _, err := Decode(strings.NewReader(input)); err == nil {
			t.Fatal("invalid task accepted")
		}
	}
	s.Limits.ToolTimeoutMS = s.Limits.TimeoutMS + 1
	b, _ = json.Marshal(s)
	if _, err := Decode(strings.NewReader(string(b))); err == nil {
		t.Fatal("invalid timeout accepted")
	}
}
