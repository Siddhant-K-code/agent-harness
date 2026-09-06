package dashboard

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Siddhant-K-code/agent-harness/internal/credentials"
	"github.com/Siddhant-K-code/agent-harness/internal/scaffold"
)

func projectRepo(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "code.txt"), []byte("committed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "--template="}, {"add", "code.txt"}, {"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-m", "baseline"}} {
		if _, err := scaffold.Git(context.Background(), dir, args...); err != nil {
			t.Fatal(err)
		}
	}
	sha, err := scaffold.Git(context.Background(), dir, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return canonical, sha
}
func projectPayload(path, sha string) projectRequest {
	return projectRequest{Repository: path, Ref: sha, Goal: "Fix the documented behavior", Image: "node:22-alpine", Verifier: "set -eu\n[ \"$(cat code.txt)\" = committed ]\n", Model: "gpt-5.4", Context: 32768, Output: 2048, USD: .2}
}
func postProject(s *Server, v any) (int, string) {
	b, _ := json.Marshal(v)
	w := request(s, "POST", "/api/projects", string(b), s.token, "", s.host)
	return w.Code, w.Body.String()
}
func TestProjectPinsRevisionPersistsAndDoesNotExecuteVerifier(t *testing.T) {
	s := setup(t)
	repo, sha := projectRepo(t)
	marker := filepath.Join(t.TempDir(), "must-not-exist")
	payload := projectPayload(repo, sha)
	payload.Verifier = "touch '" + marker + "'\nexit 1\n"
	status, body := postProject(s, payload)
	if status != 201 {
		t.Fatal(status, body)
	}
	tasks := s.tasks()
	created := tasks[len(tasks)-1]
	if created.Ref != sha || created.Repository != repo || created.Limits.MaxUSD != .2 {
		t.Fatalf("wrong project: %+v", created)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("verifier ran during setup")
	}
	if strings.HasPrefix(created.Verifier, repo+string(filepath.Separator)) {
		t.Fatal("verifier stored inside agent repository")
	}
	verifier, err := os.ReadFile(created.Verifier)
	if err != nil || string(verifier) != payload.Verifier {
		t.Fatal("verifier changed")
	}
	for _, p := range []string{created.Verifier, filepath.Join(s.options.Root, "projects.json")} {
		info, err := os.Stat(p)
		if err != nil || info.Mode().Perm()&0077 != 0 {
			t.Fatal("project data not private")
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "code.txt"), []byte("dirty\n"), 0600); err != nil {
		t.Fatal(err)
	}
	fresh := setup(t)
	fresh.options.Root = s.options.Root
	fresh.options.Tasks = nil
	if err := fresh.loadProjects(); err != nil {
		t.Fatal(err)
	}
	if len(fresh.tasks()) != 1 || fresh.tasks()[0].Ref != sha {
		t.Fatal("saved task failed to reload")
	}
	b, _ := json.Marshal(map[string]string{"repository": repo, "ref": "HEAD"})
	w := request(s, "POST", "/api/projects/inspect", string(b), s.token, "", s.host)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"dirty":true`) {
		t.Fatal(w.Body.String())
	}
}
func TestProjectInvalidInputsDoNotBecomePresets(t *testing.T) {
	s := setup(t)
	repo, sha := projectRepo(t)
	for _, mutate := range []func(*projectRequest){func(p *projectRequest) { p.Repository = "relative" }, func(p *projectRequest) { p.Ref = "--help" }, func(p *projectRequest) { p.USD = 0 }, func(p *projectRequest) { p.Model = "unpriced-model" }, func(p *projectRequest) { p.Verifier = "" }, func(p *projectRequest) { p.Output = 128000 }} {
		p := projectPayload(repo, sha)
		mutate(&p)
		status, _ := postProject(s, p)
		if status != 400 {
			t.Fatal("invalid project accepted", status)
		}
	}
	if len(s.tasks()) != 1 {
		t.Fatal("invalid tasks registered")
	}
	if _, err := os.Stat(filepath.Join(s.options.Root, "projects.json")); !os.IsNotExist(err) {
		t.Fatal("invalid tasks persisted")
	}
	w := request(s, "POST", "/api/projects", `{"repository":"/tmp","key":"secret"}`, s.token, "", s.host)
	if w.Code != 400 {
		t.Fatal("unknown sensitive field accepted")
	}
}
func TestProjectRegistrationConcurrentWithReaders(t *testing.T) {
	s := setup(t)
	repo, sha := projectRepo(t)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				_ = s.tasks()
			}
		}()
	}
	for i := 0; i < 2; i++ {
		status, body := postProject(s, projectPayload(repo, sha))
		if status != 201 {
			t.Fatal(body)
		}
	}
	wg.Wait()
	if len(s.tasks()) != 3 {
		t.Fatal("lost project")
	}
}
func TestProjectRegistryRejectsPathEscape(t *testing.T) {
	s := setup(t)
	os.WriteFile(filepath.Join(s.options.Root, "projects.json"), []byte(`{"schema":1,"ids":["../../escape"]}`), 0600)
	if err := s.loadProjects(); err == nil {
		t.Fatal("path escape accepted")
	}
}
func TestWebKeySetupIsLocalPrivateAndNeverReturned(t *testing.T) {
	s := setup(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("OPENAI_API_KEY", "")
	body := `{"key":"local-key-canary"}`
	for _, endpoint := range []string{"/api/credentials", "/api/projects", "/api/projects/check", "/api/projects/inspect"} {
		w := request(s, "POST", endpoint, body, "", "", s.host)
		if w.Code != 401 {
			t.Fatal("unprotected endpoint", endpoint)
		}
	}
	w := request(s, "POST", "/api/credentials", body, s.token, "http://evil.invalid", s.host)
	if w.Code != 403 {
		t.Fatal("cross-origin credential write")
	}
	w = request(s, "POST", "/api/credentials", body, s.token, "", s.host)
	if w.Code != 200 || strings.Contains(w.Body.String(), "local-key-canary") {
		t.Fatal("key setup failed or leaked", w.Body.String())
	}
	key, err := credentials.Resolve("", s.options.Root)
	if err != nil || key.Value != "local-key-canary" {
		t.Fatal(err)
	}
	w = request(s, "GET", "/api/overview", "", s.token, "", s.host)
	if strings.Contains(w.Body.String(), "local-key-canary") {
		t.Fatal("key exposed in overview")
	}
	w = request(s, "POST", "/api/credentials", `{"key":"replacement"}`, s.token, "", s.host)
	if w.Code != 400 {
		t.Fatal("implicit replacement")
	}
	t.Setenv("OPENAI_API_KEY", "environment-canary")
	w = request(s, "POST", "/api/credentials", `{"key":"unused"}`, s.token, "", s.host)
	if w.Code != 400 {
		t.Fatal("saved a shadowed key")
	}
}
