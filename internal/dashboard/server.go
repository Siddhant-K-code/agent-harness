// Package dashboard embeds a local, authenticated control surface in the CLI.
package dashboard

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Siddhant-K-code/agent-harness/internal/chat"
	"github.com/Siddhant-K-code/agent-harness/internal/credentials"
	"github.com/Siddhant-K-code/agent-harness/internal/integrations"
	"github.com/Siddhant-K-code/agent-harness/internal/model"
	"github.com/Siddhant-K-code/agent-harness/internal/prompt"
	"github.com/Siddhant-K-code/agent-harness/internal/runner"
	"github.com/Siddhant-K-code/agent-harness/internal/skills"
	"github.com/Siddhant-K-code/agent-harness/internal/store"
	"github.com/Siddhant-K-code/agent-harness/internal/task"
	"github.com/Siddhant-K-code/agent-harness/internal/tooling"
)

//go:embed web/*
var assets embed.FS
var runID = regexp.MustCompile(`^[a-f0-9]{32}$`)

type Options struct {
	Root, KeyFile string
	Port          int
	Tasks         []task.Spec
}
type Server struct {
	options     Options
	db          *store.Store
	token, host string
	ctx         context.Context
	mu          sync.Mutex
	projectMu   sync.Mutex
	busy        bool
	lastError   string
	wg          sync.WaitGroup
	chats       *chat.Store
	chatCancel  context.CancelFunc
	activeChat  string
}

func Serve(ctx context.Context, o Options, out io.Writer) error {
	ctx, cancelRuns := context.WithCancel(ctx)
	defer cancelRuns()
	if o.Port < 0 || o.Port > 65535 {
		return errors.New("port must be 0..65535")
	}
	listener, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", o.Port))
	if err != nil {
		return err
	}
	defer listener.Close()
	db, err := store.Open(filepath.Join(o.Root, "harness.db"))
	if err != nil {
		return err
	}
	defer db.Close()
	chats, err := chat.Open(o.Root)
	if err != nil {
		return err
	}
	defer chats.Close()
	var token [32]byte
	if _, err = rand.Read(token[:]); err != nil {
		return err
	}
	s := &Server{options: o, db: db, chats: chats, token: hex.EncodeToString(token[:]), host: listener.Addr().String(), ctx: ctx}
	if err := s.loadProjects(); err != nil {
		return err
	}
	httpServer := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	fmt.Fprintf(out, "Local dashboard: http://%s/#token=%s\nKeep this local access link private. Ctrl-C stops the server and cancels runs it owns.\n", s.host, s.token)
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			shutdown, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			_ = httpServer.Shutdown(shutdown)
		case <-done:
		}
	}()
	err = httpServer.Serve(listener)
	close(done)
	cancelRuns()
	s.wg.Wait()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/overview", s.overview)
	mux.HandleFunc("POST /api/projects/inspect", s.inspectProject)
	mux.HandleFunc("POST /api/projects", s.createProject)
	mux.HandleFunc("POST /api/projects/check", s.checkProject)
	mux.HandleFunc("POST /api/credentials", s.configureKey)
	mux.HandleFunc("GET /api/runs/{id}", s.detail)
	mux.HandleFunc("GET /api/runs/{id}/delivery", s.delivery)
	mux.HandleFunc("POST /api/runs/{id}/delivery", s.delivery)
	mux.HandleFunc("POST /api/runs", s.start)
	mux.HandleFunc("POST /api/runs/{id}/cancel", s.cancel)
	mux.HandleFunc("POST /api/chats", s.createChat)
	mux.HandleFunc("GET /api/chats/{id}", s.getChat)
	mux.HandleFunc("POST /api/chats/{id}/ask", s.askChat)
	mux.HandleFunc("POST /api/chats/{id}/cancel", s.cancelChat)
	sub, _ := fs.Sub(assets, "web")
	static := http.FileServer(http.FS(sub))
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != "/app.js" && r.URL.Path != "/chat.js" && r.URL.Path != "/projects.js" && r.URL.Path != "/delivery.js" && r.URL.Path != "/style.css" {
			http.NotFound(w, r)
			return
		}
		static.ServeHTTP(w, r)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		if r.Host != s.host {
			http.Error(w, "invalid host", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+s.host {
			http.Error(w, "invalid origin", http.StatusForbidden)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+s.token)) != 1 {
				http.Error(w, "open the private access link printed by harness serve", http.StatusUnauthorized)
				return
			}
			if r.Method == "POST" && r.Header.Get("Content-Type") != "application/json" {
				http.Error(w, "application/json required", http.StatusUnsupportedMediaType)
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}
func send(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, err error) { http.Error(w, err.Error(), http.StatusBadRequest) }
func (s *Server) artifact(id, name string, max int64) ([]byte, error) {
	if !runID.MatchString(id) {
		return nil, store.ErrNotFound
	}
	root, err := os.OpenRoot(filepath.Join(s.options.Root, "runs", id))
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("invalid artifact")
	}
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if len(b) > int(max) {
		return nil, errors.New("artifact exceeds preview limit; inspect it with the CLI")
	}
	return b, err
}
func (s *Server) report(id string) any {
	b, e := s.artifact(id, "report.json", 1<<20)
	if e != nil {
		return nil
	}
	var out any
	if json.Unmarshal(b, &out) != nil {
		return nil
	}
	return out
}
func (s *Server) overview(w http.ResponseWriter, r *http.Request) {
	runs, err := s.db.List(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	items := []any{}
	for i, run := range runs {
		if i == 200 {
			break
		}
		items = append(items, map[string]any{"run": run, "report": s.report(run.ID)})
	}
	registry, err := (skills.Store{Root: s.options.Root}).List()
	if err != nil {
		fail(w, err)
		return
	}
	versions := []any{}
	ss := skills.Store{Root: s.options.Root}
	for _, entry := range registry.Entries {
		v, e := ss.Get(entry.Current)
		if e != nil {
			fail(w, e)
			return
		}
		versions = append(versions, map[string]any{"id": v.ID, "description": v.Description, "repository": v.Repository, "version": entry.Current, "source": v.Source, "expires_at": v.ExpiresAt, "previous": len(entry.Previous)})
	}
	c, configErr := integrations.Load(s.options.Root)
	integrationError := ""
	if configErr != nil {
		integrationError = configErr.Error()
	}
	key, keyErr := credentials.Resolve(s.options.KeyFile, s.options.Root)
	keySource := "not configured"
	if keyErr == nil {
		keySource = key.Source
	}
	s.mu.Lock()
	busy, lastError := s.busy, s.lastError
	s.mu.Unlock()
	chats := []chat.Summary{}
	if s.chats != nil {
		chats, err = s.chats.List()
		if err != nil {
			fail(w, err)
			return
		}
	}
	send(w, map[string]any{"runs": items, "tasks": s.tasks(), "chats": chats, "models": model.Catalog(), "skills": versions, "integrations": c, "integration_error": integrationError, "key_present": keyErr == nil, "key_source": keySource, "prompt": prompt.Build("docker", tooling.Native()), "busy": busy, "last_error": lastError})
}
func (s *Server) detail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !runID.MatchString(id) {
		http.NotFound(w, r)
		return
	}
	run, err := s.db.Get(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	events, err := s.db.Events(r.Context(), id)
	if err != nil {
		fail(w, err)
		return
	}
	// Do not send raw provider responses/opaque reasoning to the browser.
	for i := range events {
		if events[i].Type == "model.responded" {
			var v struct {
				Usage  any    `json:"usage"`
				Status string `json:"status"`
			}
			_ = json.Unmarshal(events[i].Data, &v)
			events[i].Data, _ = json.Marshal(v)
		}
	}
	patch, patchErr := s.artifact(id, "changes.patch", 2<<20)
	patchError := ""
	if patchErr != nil && !errors.Is(patchErr, os.ErrNotExist) {
		patchError = patchErr.Error()
	}
	var manifest any
	if b, e := s.artifact(id, "prompt.json", 128<<10); e == nil {
		_ = json.Unmarshal(b, &manifest)
	}
	send(w, map[string]any{"run": run, "report": s.report(id), "events": events, "patch": string(patch), "patch_error": patchError, "prompt": manifest})
}

type startRequest struct {
	Conversation string       `json:"conversation,omitempty"`
	RequestID    string       `json:"request_id,omitempty"`
	Task         int          `json:"task"`
	Goal         string       `json:"goal"`
	Model        string       `json:"model"`
	Context      int64        `json:"context_window_tokens"`
	Output       int64        `json:"max_output_tokens"`
	USD          float64      `json:"max_usd"`
	Skills       []skills.Ref `json:"skills"`
}

func (s *Server) start(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	var a startRequest
	if d.Decode(&a) != nil || d.Decode(new(any)) != io.EOF {
		fail(w, errors.New("invalid run request"))
		return
	}
	tasks := s.tasks()
	if a.Task < 0 || a.Task >= len(tasks) {
		fail(w, errors.New("select a configured task"))
		return
	}
	base := tasks[a.Task]
	if a.Conversation != "" {
		c, e := s.configuredChat(a.Conversation)
		if e != nil {
			fail(w, e)
			return
		}
		if c.PresetHash != presetHash(base) {
			fail(w, errors.New("conversation belongs to a different prepared task"))
			return
		}
		base.Ref = c.Task.Ref
	}
	spec := base
	// Copy mutable pointers before applying overrides to avoid changing the preset.
	if base.Compaction != nil {
		c := *base.Compaction
		spec.Compaction = &c
	}
	spec.Goal, spec.Model, spec.Skills = a.Goal, a.Model, a.Skills
	spec.Limits.ContextWindowTokens, spec.Limits.MaxOutputTokens, spec.Limits.MaxUSD = a.Context, a.Output, a.USD
	if !(a.USD > 0 && a.USD <= base.Limits.MaxUSD) {
		fail(w, fmt.Errorf("run budget must be within the configured $%.2f ceiling", base.Limits.MaxUSD))
		return
	}
	if spec.Compaction != nil && spec.Compaction.MaxSummaryTokens > a.Output {
		spec.Compaction.MaxSummaryTokens = a.Output
	}
	if err := spec.Validate(); err != nil {
		fail(w, err)
		return
	}
	if _, err := model.ResolveSettings(spec); err != nil {
		fail(w, err)
		return
	}
	if _, _, err := (skills.Store{Root: s.options.Root}).Resolve(spec.Repository, spec.Skills); err != nil {
		fail(w, err)
		return
	}
	key, err := credentials.Resolve(s.options.KeyFile, s.options.Root)
	if err != nil {
		fail(w, err)
		return
	}
	s.mu.Lock()
	if a.Conversation != "" {
		old, e := s.chats.Existing(a.Conversation, a.RequestID, requestHash(a))
		if e != nil {
			s.mu.Unlock()
			fail(w, e)
			return
		}
		if old != nil {
			s.mu.Unlock()
			send(w, map[string]any{"accepted": true, "turn": old})
			return
		}
	}
	if s.busy {
		s.mu.Unlock()
		http.Error(w, "a dashboard run is already active", http.StatusConflict)
		return
	}
	if s.ctx.Err() != nil {
		s.mu.Unlock()
		http.Error(w, "server is stopping", http.StatusServiceUnavailable)
		return
	}
	if a.Conversation != "" {
		settings, _ := model.ResolveSettings(spec)
		_, e := s.chats.Begin(a.Conversation, chat.Turn{ID: a.RequestID, RequestHash: requestHash(a), Kind: "task", Question: a.Goal, Settings: settings, MaxUSD: a.USD})
		if e != nil {
			s.mu.Unlock()
			fail(w, e)
			return
		}
	}
	s.busy = true
	s.lastError = ""
	runCtx, stopRun := context.WithCancel(s.ctx)
	s.chatCancel, s.activeChat = stopRun, a.Conversation
	s.wg.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.wg.Done()
		defer stopRun()
		run := runner.Runner{Store: s.db, Root: s.options.Root, Key: key.Value}
		if a.Conversation != "" {
			run.Created = func(id string) error {
				return s.chats.Update(a.Conversation, a.RequestID, func(t *chat.Turn) { t.RunID = id; t.Phase = "Coding run in progress" })
			}
		}
		report, err := run.Run(runCtx, spec)
		if a.Conversation != "" {
			e := s.chats.Update(a.Conversation, a.RequestID, func(t *chat.Turn) {
				t.State = string(report.State)
				if t.State == "" {
					t.State = "failed"
				}
				t.RunID = report.RunID
				t.Phase = "Coding run ended"
				t.EstimatedUSD = report.EstimatedUSD
				t.BillingUnknown = report.BillingUnknown
				t.InputTokens = report.InputTokens
				t.OutputTokens = report.OutputTokens
				if err != nil {
					t.Error = err.Error()
				}
				if report.Verified {
					t.Answer = "The coding run passed its independent verifier. Open the run to review the patch and evidence. The source project is unchanged."
				} else {
					t.Answer = "The coding run did not produce a verified completion. Open the run for its results and any saved patch."
				}
			})
			err = errors.Join(err, e)
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		s.busy = false
		s.chatCancel, s.activeChat = nil, ""
		if err != nil {
			s.lastError = err.Error()
		}
	}()
	w.WriteHeader(http.StatusAccepted)
	send(w, map[string]any{"accepted": true})
}
func (s *Server) cancel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !runID.MatchString(id) {
		http.NotFound(w, r)
		return
	}
	run, err := s.db.RequestCancel(r.Context(), id)
	if err != nil {
		fail(w, err)
		return
	}
	send(w, run)
}
