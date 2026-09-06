package dashboard

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Siddhant-K-code/agent-harness/internal/credentials"
	"github.com/Siddhant-K-code/agent-harness/internal/diagnostics"
	"github.com/Siddhant-K-code/agent-harness/internal/scaffold"
	"github.com/Siddhant-K-code/agent-harness/internal/statefile"
	"github.com/Siddhant-K-code/agent-harness/internal/task"
	"github.com/Siddhant-K-code/agent-harness/internal/workspace"
)

type projectRegistry struct {
	Schema int      `json:"schema"`
	IDs    []string `json:"ids"`
}

func (s *Server) tasks() []task.Spec {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]task.Spec(nil), s.options.Tasks...)
}
func (s *Server) projectRegistry() (projectRegistry, error) {
	r := projectRegistry{Schema: 1, IDs: []string{}}
	err := statefile.Read(filepath.Join(s.options.Root, "projects.json"), &r, 64<<10)
	if errors.Is(err, os.ErrNotExist) {
		return r, nil
	}
	if err != nil {
		return r, err
	}
	if r.Schema != 1 || len(r.IDs) > 50 {
		return r, errors.New("invalid project registry")
	}
	seen := map[string]bool{}
	for _, id := range r.IDs {
		if !runID.MatchString(id) || seen[id] {
			return r, errors.New("invalid project registry entry")
		}
		seen[id] = true
	}
	return r, nil
}
func (s *Server) loadProjects() error {
	r, err := s.projectRegistry()
	if err != nil {
		return err
	}
	for _, id := range r.IDs {
		spec, err := task.Load(filepath.Join(s.options.Root, "projects", id, "harness.task.json"))
		if err != nil {
			return err
		}
		found := false
		for _, t := range s.options.Tasks {
			if presetHash(t) == presetHash(spec) {
				found = true
			}
		}
		if !found {
			s.options.Tasks = append(s.options.Tasks, spec)
		}
	}
	return nil
}

type projectRequest struct {
	Repository string  `json:"repository"`
	Ref        string  `json:"ref"`
	Goal       string  `json:"goal"`
	Image      string  `json:"image"`
	Verifier   string  `json:"verifier_script"`
	Model      string  `json:"model"`
	Context    int64   `json:"context_window_tokens"`
	Output     int64   `json:"max_output_tokens"`
	USD        float64 `json:"max_usd"`
}

func inspectRepository(ctx context.Context, path, ref string) (string, string, error) {
	if !filepath.IsAbs(path) {
		return "", "", errors.New("enter an absolute local repository path")
	}
	path, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", "", err
	}
	top, err := scaffold.Git(ctx, path, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", "", err
	}
	top, err = filepath.EvalSymlinks(top)
	if err != nil {
		return "", "", err
	}
	if ref == "" {
		ref = "HEAD"
	}
	sha, err := workspace.ResolveCommit(ctx, top, ref)
	if err != nil {
		return "", "", err
	}
	submodules, err := scaffold.Git(ctx, top, "ls-tree", "--name-only", sha, "--", ".gitmodules")
	if err != nil {
		return "", "", err
	}
	if submodules != "" {
		return "", "", errors.New("submodules are not supported")
	}
	return top, sha, nil
}
func (s *Server) inspectProject(w http.ResponseWriter, r *http.Request) {
	var a struct {
		Repository string `json:"repository"`
		Ref        string `json:"ref"`
	}
	if err := decodeChat(w, r, &a); err != nil {
		fail(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	path, sha, err := inspectRepository(ctx, a.Repository, a.Ref)
	if err != nil {
		fail(w, err)
		return
	}
	status, err := scaffold.Git(ctx, path, "status", "--porcelain", "--untracked-files=normal")
	if err != nil {
		fail(w, err)
		return
	}
	send(w, map[string]any{"repository": path, "commit": sha, "dirty": status != "", "name": filepath.Base(path)})
}
func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var a projectRequest
	if err := decodeChat(w, r, &a); err != nil {
		fail(w, err)
		return
	}
	if strings.TrimSpace(a.Verifier) == "" || len(a.Verifier) > 32000 {
		fail(w, errors.New("provide an independent verification script (up to 32000 bytes)"))
		return
	}
	s.projectMu.Lock()
	defer s.projectMu.Unlock()
	registry, err := s.projectRegistry()
	if err != nil {
		fail(w, err)
		return
	}
	if len(registry.IDs) >= 50 {
		fail(w, errors.New("at most 50 saved projects"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	path, sha, err := inspectRepository(ctx, a.Repository, a.Ref)
	if err != nil {
		fail(w, err)
		return
	}
	// Check the exact revision seen in the preview. The UI submits that full hash.
	var random [16]byte
	if _, err = rand.Read(random[:]); err != nil {
		fail(w, err)
		return
	}
	id := hex.EncodeToString(random[:])
	parent := filepath.Join(s.options.Root, "projects")
	if err = os.MkdirAll(parent, 0700); err != nil {
		fail(w, err)
		return
	}
	file, err := os.CreateTemp(parent, ".verifier-*")
	if err != nil {
		fail(w, err)
		return
	}
	defer os.Remove(file.Name())
	_, err = file.WriteString(a.Verifier)
	err = errors.Join(err, file.Close())
	if err != nil {
		fail(w, err)
		return
	}
	compaction := task.DefaultCompaction()
	if a.Output < compaction.MaxSummaryTokens {
		compaction.MaxSummaryTokens = a.Output
	}
	dir, err := scaffold.Create(ctx, scaffold.Options{Directory: filepath.Join(parent, id), Repository: path, Ref: sha, Goal: a.Goal, Image: a.Image, Verifier: file.Name(), Model: a.Model, MaxUSD: a.USD, ContextWindowTokens: &a.Context, MaxOutputTokens: &a.Output, Compaction: compaction, Learn: true})
	if err != nil {
		fail(w, err)
		return
	}
	spec, err := task.Load(filepath.Join(dir, "harness.task.json"))
	if err != nil {
		_ = os.RemoveAll(dir)
		fail(w, err)
		return
	}
	registry.IDs = append(registry.IDs, id)
	if err = statefile.Write(filepath.Join(s.options.Root, "projects.json"), registry); err != nil {
		_ = os.RemoveAll(dir)
		fail(w, err)
		return
	}
	s.mu.Lock()
	index := len(s.options.Tasks)
	s.options.Tasks = append(s.options.Tasks, spec)
	s.mu.Unlock()
	w.WriteHeader(http.StatusCreated)
	send(w, map[string]any{"id": id, "task_index": index, "task": spec})
}
func (s *Server) checkProject(w http.ResponseWriter, r *http.Request) {
	var a struct {
		Task *int `json:"task"`
	}
	if err := decodeChat(w, r, &a); err != nil {
		fail(w, err)
		return
	}
	tasks := s.tasks()
	if a.Task == nil || *a.Task < 0 || *a.Task >= len(tasks) {
		fail(w, errors.New("select a configured project"))
		return
	}
	key, keyErr := credentials.Resolve(s.options.KeyFile, s.options.Root)
	result, _ := diagnostics.Check(r.Context(), tasks[*a.Task], s.options.Root, key, keyErr)
	send(w, result)
}
func (s *Server) configureKey(w http.ResponseWriter, r *http.Request) {
	var a struct {
		Key string `json:"key"`
	}
	if err := decodeChat(w, r, &a); err != nil {
		fail(w, err)
		return
	}
	s.projectMu.Lock()
	defer s.projectMu.Unlock()
	existing, _ := credentials.Resolve(s.options.KeyFile, s.options.Root)
	if existing.Source != "user_config" {
		fail(w, errors.New("an environment or task key is selected; manage that credential on the host"))
		return
	}
	path, err := credentials.Path()
	if err != nil {
		fail(w, err)
		return
	}
	if err = credentials.Save(path, a.Key, false); err != nil {
		fail(w, err)
		return
	}
	send(w, map[string]bool{"configured": true, "api_validated": false})
}
