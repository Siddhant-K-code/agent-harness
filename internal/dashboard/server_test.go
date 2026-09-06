package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Siddhant-K-code/agent-harness/internal/chat"
	"github.com/Siddhant-K-code/agent-harness/internal/store"
	"github.com/Siddhant-K-code/agent-harness/internal/task"
)

func setup(t *testing.T) *Server {
	t.Helper()
	root := t.TempDir()
	db, err := store.Open(filepath.Join(root, "harness.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	spec, err := task.Load("../../examples/normalize-tags/task.json")
	if err != nil {
		t.Fatal(err)
	}
	spec.Limits.MaxUSD = .25
	chats, err := chat.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { chats.Close() })
	return &Server{options: Options{Root: root, Tasks: []task.Spec{spec}}, db: db, chats: chats, token: "local-test-token", host: "127.0.0.1:8765", ctx: context.Background()}
}
func request(s *Server, method, path, body, token, origin, host string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://"+host+path, strings.NewReader(body))
	r.Host = host
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	r.Header.Set("Content-Type", "application/json")
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}
func TestAuthenticationOriginBudgetAndArtifactBoundaries(t *testing.T) {
	s := setup(t)
	for _, check := range []struct {
		token, origin, host string
		status              int
	}{{"", "", s.host, 401}, {s.token, "http://evil.invalid", s.host, 403}, {s.token, "", "evil.invalid", 403}, {s.token, "http://" + s.host, s.host, 200}} {
		w := request(s, "GET", "/api/overview", "", check.token, check.origin, check.host)
		if w.Code != check.status {
			t.Fatalf("auth status %d want %d", w.Code, check.status)
		}
	}
	run, err := s.db.Create(context.Background(), s.options.Tasks[0])
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.db.Start(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.db.Record(context.Background(), run.ID, "model.responded", map[string]any{"output": []string{"opaque-private-reasoning"}, "usage": map[string]int{"input_tokens": 10, "output_tokens": 2}, "status": "completed"}, false)
	if err != nil {
		t.Fatal(err)
	}
	w := request(s, "GET", "/api/runs/"+run.ID, "", s.token, "", s.host)
	if w.Code != 200 || strings.Contains(w.Body.String(), "opaque-private-reasoning") {
		t.Fatal("private provider response exposed")
	}
	dir := filepath.Join(s.options.Root, "runs", run.ID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(t.TempDir(), "secret")
	os.WriteFile(secret, []byte("outside-artifact"), 0600)
	os.Symlink(secret, filepath.Join(dir, "changes.patch"))
	if _, err := s.artifact(run.ID, "changes.patch", 1024); err == nil {
		t.Fatal("artifact symlink escape accepted")
	}
	w = request(s, "POST", "/api/runs", `{"task":0,"goal":"fix","model":"gpt-5.4","context_window_tokens":20000,"max_output_tokens":1024,"max_usd":1,"skills":[]}`, s.token, "", s.host)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "ceiling") {
		t.Fatal("budget ceiling bypassed")
	}
	w = request(s, "POST", "/api/runs", `{"task":99}`, s.token, "", s.host)
	if w.Code != 400 {
		t.Fatal("unknown task accepted")
	}
	w = request(s, "POST", "/api/runs/"+run.ID+"/cancel", `{}`, s.token, "", s.host)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	cancelled, _ := s.db.Get(context.Background(), run.ID)
	if cancelled.State != store.Cancelling {
		t.Fatal("cancel did not reach durable store")
	}
	w = request(s, "GET", "/.harness/openai-key", "", s.token, "", s.host)
	if w.Code != http.StatusNotFound {
		t.Fatal("unexpected static file access")
	}
}
func TestOverviewNeverReturnsKey(t *testing.T) {
	s := setup(t)
	t.Setenv("OPENAI_API_KEY", "private-key-canary")
	w := request(s, "GET", "/api/overview", "", s.token, "", s.host)
	var v map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if v["key_present"] != true || strings.Contains(w.Body.String(), "private-key-canary") {
		t.Fatal("key leaked or not detected")
	}
}

func TestExplicitKeyFileIsUsedWithoutExposingIt(t *testing.T) {
	s := setup(t)
	t.Setenv("OPENAI_API_KEY", "")
	s.options.KeyFile = filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(s.options.KeyFile, []byte("explicit-key-canary"), 0600); err != nil {
		t.Fatal(err)
	}
	w := request(s, "GET", "/api/overview", "", s.token, "", s.host)
	if !strings.Contains(w.Body.String(), `"key_present":true`) || !strings.Contains(w.Body.String(), `"key_source":"explicit_file"`) || strings.Contains(w.Body.String(), "explicit-key-canary") {
		t.Fatal("explicit key handling failed")
	}
}
