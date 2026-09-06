package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/Siddhant-K-code/agent-harness/internal/chat"
	"github.com/Siddhant-K-code/agent-harness/internal/credentials"
	"github.com/Siddhant-K-code/agent-harness/internal/model"
	"github.com/Siddhant-K-code/agent-harness/internal/statefile"
	"github.com/Siddhant-K-code/agent-harness/internal/task"
	"github.com/Siddhant-K-code/agent-harness/internal/workspace"
)

func presetHash(spec task.Spec) string { return statefile.Hash(spec) }
func requestHash(v any) string         { return statefile.Hash(v) }
func decodeChat(w http.ResponseWriter, r *http.Request, out any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(out) != nil || d.Decode(new(any)) != io.EOF {
		return errors.New("invalid chat request")
	}
	return nil
}
func (s *Server) configuredChat(id string) (chat.Conversation, error) {
	if s.chats == nil {
		return chat.Conversation{}, errors.New("chat store unavailable")
	}
	c, e := s.chats.Get(id)
	if e != nil {
		return c, e
	}
	for _, t := range s.options.Tasks {
		if presetHash(t) == c.PresetHash {
			return c, nil
		}
	}
	return c, errors.New("this chat's prepared task changed or is no longer configured; start a new conversation")
}
func (s *Server) createChat(w http.ResponseWriter, r *http.Request) {
	var a struct {
		Task *int `json:"task"`
	}
	if e := decodeChat(w, r, &a); e != nil {
		fail(w, e)
		return
	}
	if a.Task == nil || *a.Task < 0 || *a.Task >= len(s.options.Tasks) {
		fail(w, errors.New("select a configured project task"))
		return
	}
	spec := s.options.Tasks[*a.Task]
	hash := presetHash(spec)
	ctx, stop := context.WithTimeout(r.Context(), 10*time.Second)
	defer stop()
	base, e := workspace.ResolveCommit(ctx, spec.Repository, spec.Ref)
	if e != nil {
		fail(w, e)
		return
	}
	spec.Ref = base
	c, e := s.chats.Create(spec, hash)
	if e != nil {
		fail(w, e)
		return
	}
	send(w, c)
}
func (s *Server) getChat(w http.ResponseWriter, r *http.Request) {
	c, e := s.chats.Get(r.PathValue("id"))
	if e != nil {
		http.NotFound(w, r)
		return
	}
	// A removed preset keeps history readable but cannot authorize new work.
	_, configErr := s.configuredChat(c.ID)
	available := configErr == nil
	taskIndex := -1
	for i, t := range s.options.Tasks {
		if presetHash(t) == c.PresetHash {
			taskIndex = i
			break
		}
	}
	send(w, map[string]any{"conversation": c, "available": available, "task_index": taskIndex})
}

type askRequest struct {
	RequestID string  `json:"request_id"`
	Message   string  `json:"message"`
	Model     string  `json:"model"`
	Context   int64   `json:"context_window_tokens"`
	Output    int64   `json:"max_output_tokens"`
	USD       float64 `json:"max_usd"`
}

func (s *Server) askChat(w http.ResponseWriter, r *http.Request) {
	var a askRequest
	if e := decodeChat(w, r, &a); e != nil {
		fail(w, e)
		return
	}
	c, e := s.configuredChat(r.PathValue("id"))
	if e != nil {
		fail(w, e)
		return
	}
	spec := c.Task
	spec.Compaction = nil
	spec.Goal = a.Message
	spec.Model = a.Model
	spec.Limits.ContextWindowTokens = a.Context
	spec.Limits.MaxOutputTokens = a.Output
	spec.Limits.MaxUSD = a.USD
	if !(a.USD > 0 && a.USD <= c.Task.Limits.MaxUSD) {
		fail(w, errors.New("question budget exceeds the prepared task ceiling"))
		return
	}
	settings, e := model.ResolveSettings(spec)
	if e != nil {
		fail(w, e)
		return
	}
	key, e := credentials.Resolve(s.options.KeyFile, s.options.Root)
	if e != nil {
		fail(w, e)
		return
	}
	s.mu.Lock()
	old, e := s.chats.Existing(c.ID, a.RequestID, requestHash(a))
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
	if s.busy {
		s.mu.Unlock()
		http.Error(w, "a dashboard question or run is already active", http.StatusConflict)
		return
	}
	if s.ctx.Err() != nil {
		s.mu.Unlock()
		http.Error(w, "server is stopping", http.StatusServiceUnavailable)
		return
	}
	c, e = s.chats.Begin(c.ID, chat.Turn{ID: a.RequestID, RequestHash: requestHash(a), Kind: "ask", Question: a.Message, Settings: settings, MaxUSD: a.USD})
	if e != nil {
		s.mu.Unlock()
		fail(w, e)
		return
	}
	ctx, stop := context.WithCancel(s.ctx)
	s.busy = true
	s.lastError = ""
	s.chatCancel = stop
	s.activeChat = c.ID
	s.wg.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.wg.Done()
		defer stop()
		e := chat.Ask(ctx, s.chats, c, spec, key.Value)
		s.mu.Lock()
		defer s.mu.Unlock()
		s.busy = false
		s.chatCancel = nil
		s.activeChat = ""
		if e != nil {
			s.lastError = e.Error()
		}
	}()
	w.WriteHeader(http.StatusAccepted)
	send(w, map[string]any{"accepted": true, "turn_id": a.RequestID})
}
func (s *Server) cancelChat(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeChat == r.PathValue("id") && s.chatCancel != nil {
		s.chatCancel()
	}
	send(w, map[string]bool{"accepted": true})
}
