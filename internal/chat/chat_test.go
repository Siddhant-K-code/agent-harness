package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Siddhant-K-code/agent-harness/internal/model"
	"github.com/Siddhant-K-code/agent-harness/internal/statefile"
	"github.com/Siddhant-K-code/agent-harness/internal/task"
	"github.com/Siddhant-K-code/agent-harness/internal/workspace"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

func fixture(t *testing.T) (*Store, Conversation, task.Spec) {
	t.Helper()
	spec, e := task.Load("../../examples/normalize-tags/task.json")
	if e != nil {
		t.Fatal(e)
	}
	repo := t.TempDir()
	spec.Repository = repo
	spec.Compaction = nil
	spec.Limits.ContextWindowTokens = 8192
	spec.Limits.MaxOutputTokens = 1024
	spec.Limits.MaxUSD = .1
	os.WriteFile(filepath.Join(repo, "main.txt"), []byte("first\nhello project\nlast\n"), 0600)
	for _, args := range [][]string{{"init"}, {"add", "."}, {"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-m", "base"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=/nonexistent", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1"}
		if out, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("git: %s %v", out, e)
		}
	}
	spec.Ref, e = workspace.ResolveCommit(context.Background(), repo, "HEAD")
	if e != nil {
		t.Fatal(e)
	}
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	c, e := s.Create(spec, statefile.Hash(spec))
	if e != nil {
		t.Fatal(e)
	}
	return s, c, spec
}
func begin(t *testing.T, s *Store, c Conversation, spec task.Spec) Conversation {
	t.Helper()
	settings, e := model.ResolveSettings(spec)
	if e != nil {
		t.Fatal(e)
	}
	id, _ := NewID()
	c, e = s.Begin(c.ID, Turn{ID: id, RequestHash: statefile.Hash("request"), Kind: "ask", Question: "What does main.txt contain?", Settings: settings, MaxUSD: spec.Limits.MaxUSD})
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func TestStoreDurabilityExclusivityAndRequestIdentity(t *testing.T) {
	s, c, spec := fixture(t)
	c = begin(t, s, c, spec)
	turn := c.Turns[0]
	if _, e := Open(filepath.Dir(s.root)); e == nil {
		t.Fatal("second owner acquired chat store")
	}
	if old, e := s.Existing(c.ID, turn.ID, turn.RequestHash); e != nil || old == nil {
		t.Fatal("request identity lost")
	}
	if _, e := s.Existing(c.ID, turn.ID, statefile.Hash("different")); !errors.Is(e, ErrConflict) {
		t.Fatal("changed request reused")
	}
	if _, e := s.Begin(c.ID, turn); !errors.Is(e, ErrConflict) {
		t.Fatal("duplicate turn accepted")
	}
	s.Update(c.ID, turn.ID, func(v *Turn) { v.Pending = true; v.Requests = 1 })
	info, _ := os.Stat(s.path(c.ID))
	if info.Mode().Perm() != 0600 {
		t.Fatal("conversation is not private")
	}
	s.Close()
	reopened, e := Open(filepath.Dir(s.root))
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	got, e := reopened.Get(c.ID)
	if e != nil {
		t.Fatal(e)
	}
	if got.Turns[0].State != "interrupted" || !got.Turns[0].BillingUnknown || got.Turns[0].Requests != 1 {
		t.Fatal("interrupted request silently reset")
	}
	if _, e := reopened.Get("../../secret"); e == nil {
		t.Fatal("path traversal accepted")
	}
}

// These test-only HTTP fixtures validate the real SDK/protocol. Production
// always constructs model.New with the official OpenAI endpoint and BYOK.
func TestAskReadsRealGitAndPersistsUsageBeforeAndAfterProviderCall(t *testing.T) {
	s, c, spec := fixture(t)
	c = begin(t, s, c, spec)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		if body["instructions"] != Instructions || len(body["tools"].([]any)) != 3 {
			t.Error("read-only prompt/catalog missing")
		}
		if strings.HasSuffix(r.URL.Path, "input_tokens") {
			fmt.Fprint(w, `{"input_tokens":500}`)
			return
		}
		calls++
		saved, _ := s.Get(c.ID)
		if !saved.Turns[0].Pending || !saved.Turns[0].BillingUnknown {
			t.Error("no write-ahead billing record")
		}
		if body["store"] != false || body["previous_response_id"] != nil {
			t.Error("unexpected provider-side history")
		}
		if calls == 1 {
			fmt.Fprint(w, `{"id":"resp_1","status":"completed","output":[{"id":"rs_1","type":"reasoning","summary":[],"encrypted_content":"opaque-canary"},{"id":"fc_1","type":"function_call","call_id":"call_1","name":"read_file","arguments":"{\"path\":\"main.txt\",\"start_line\":\"1\",\"end_line\":\"3\"}","status":"completed"}],"usage":{"input_tokens":500,"output_tokens":20}}`)
		} else {
			encoded, _ := json.Marshal(body)
			if !strings.Contains(string(encoded), "main.txt:2: hello project") || !strings.Contains(string(encoded), "opaque-canary") {
				t.Error("real source or opaque tool continuation lost")
			}
			fmt.Fprint(w, `{"id":"resp_2","status":"completed","output":[{"id":"msg_2","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"The greeting is at main.txt:2.","annotations":[]}]}],"usage":{"input_tokens":700,"output_tokens":30}}`)
		}
	}))
	defer server.Close()
	client := model.Client{API: openai.NewClient(option.WithBaseURL(server.URL+"/"), option.WithAPIKey("test-key"), option.WithMaxRetries(0)), Model: spec.Model, Prompt: Instructions, ToolDefinitions: Definitions()}
	if e := ask(context.Background(), s, c, spec, "test-key", client); e != nil {
		t.Fatal(e)
	}
	got, _ := s.Get(c.ID)
	turn := got.Turns[0]
	if turn.State != "completed" || turn.InputTokens != 1200 || turn.OutputTokens != 50 || turn.BillingUnknown || turn.Answer != "The greeting is at main.txt:2." || len(turn.Sources) != 1 {
		t.Fatalf("bad receipt: %+v", turn)
	}
	bytes, _ := os.ReadFile(s.path(c.ID))
	if strings.Contains(string(bytes), "opaque-canary") {
		t.Fatal("opaque provider content persisted to chat")
	}
}

func TestAskFailureAndAdmissionDoNotRetry(t *testing.T) {
	for _, scenario := range []string{"budget", "context", "transport", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			s, c, spec := fixture(t)
			if scenario == "budget" {
				spec.Limits.MaxUSD = .02
			}
			c = begin(t, s, c, spec)
			calls := 0
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if strings.HasSuffix(r.URL.Path, "input_tokens") {
					count := 500
					if scenario == "budget" {
						count = 7000
					}
					if scenario == "context" {
						count = 9000
					}
					fmt.Fprintf(w, `{"input_tokens":%d}`, count)
					return
				}
				calls++
				if scenario == "cancel" {
					cancel()
					return
				}
				w.WriteHeader(503)
				fmt.Fprint(w, `{"error":{"message":"test","type":"server_error"}}`)
			}))
			defer server.Close()
			client := model.Client{API: openai.NewClient(option.WithBaseURL(server.URL+"/"), option.WithAPIKey("test-key"), option.WithMaxRetries(0)), Model: spec.Model, Prompt: Instructions, ToolDefinitions: Definitions()}
			if e := ask(ctx, s, c, spec, "test-key", client); e == nil {
				t.Fatal("failure accepted")
			}
			got, _ := s.Get(c.ID)
			turn := got.Turns[0]
			if scenario == "budget" || scenario == "context" {
				if calls != 0 || turn.BillingUnknown {
					t.Fatal("unadmitted generation attempted")
				}
			} else if calls != 1 || !turn.BillingUnknown {
				t.Fatal("lost uncertainty or retried generation")
			}
		})
	}
}

func TestReadToolsEnforceCapabilitiesAndRanges(t *testing.T) {
	_, c, _ := fixture(t)
	r, e := workspace.OpenReader(context.Background(), c.Task.Repository, c.Task.Ref)
	if e != nil {
		t.Fatal(e)
	}
	for _, call := range []model.Call{{Name: "exec", Arguments: `{"path":"."}`}, {Name: "write_file", Arguments: `{"path":"main.txt"}`}, {Name: "read_file", Arguments: `{"path":"main.txt","start_line":"0","end_line":"3"}`}, {Name: "read_file", Arguments: `{"path":"main.txt","start_line":"1","end_line":"500"}`}, {Name: "list_files", Arguments: `{"path":null}`}} {
		if _, e := ReadTool(context.Background(), r, call); e == nil {
			t.Fatal("unsafe tool accepted", call.Name)
		}
	}
	s, e := ReadTool(context.Background(), r, model.Call{Name: "search_text", Arguments: `{"path":".","text":"hello"}`})
	if e != nil || !strings.Contains(s.Text, "main.txt:2: hello project") {
		t.Fatal("search evidence wrong", e)
	}
}
