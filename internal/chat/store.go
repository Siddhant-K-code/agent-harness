// Package chat persists local project conversations and runs bounded Q&A.
package chat

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Siddhant-K-code/agent-harness/internal/model"
	"github.com/Siddhant-K-code/agent-harness/internal/ownership"
	"github.com/Siddhant-K-code/agent-harness/internal/statefile"
	"github.com/Siddhant-K-code/agent-harness/internal/task"
)

const MaxTurns = 40
const maxConversationBytes = 16 << 20

var ErrConflict = errors.New("conversation request conflicts with an existing or active turn")

type Conversation struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	PresetHash string    `json:"preset_hash"`
	Task       task.Spec `json:"task"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	Turns      []Turn    `json:"turns"`
}
type Turn struct {
	ID             string         `json:"id"`
	RequestHash    string         `json:"request_hash"`
	Kind           string         `json:"kind"`
	Question       string         `json:"question"`
	Answer         string         `json:"answer"`
	State          string         `json:"state"`
	Phase          string         `json:"phase"`
	Error          string         `json:"error,omitempty"`
	RunID          string         `json:"run_id,omitempty"`
	Settings       model.Settings `json:"settings"`
	MaxUSD         float64        `json:"max_usd"`
	EstimatedUSD   float64        `json:"estimated_usd_uncached"`
	InputTokens    int64          `json:"input_tokens"`
	OutputTokens   int64          `json:"output_tokens"`
	Pending        bool           `json:"pending"`
	BillingUnknown bool           `json:"billing_unknown"`
	Requests       int            `json:"requests"`
	OmittedTurns   int            `json:"omitted_turns"`
	Sources        []Source       `json:"sources"`
	PromptVersion  string         `json:"prompt_version,omitempty"`
	PromptSHA256   string         `json:"prompt_sha256,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
}
type Source struct {
	Tool string `json:"tool"`
	Path string `json:"path"`
	Text string `json:"text"`
}
type Summary struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	Repository string    `json:"repository"`
	Base       string    `json:"base"`
	UpdatedAt  time.Time `json:"updated_at"`
	Turns      int       `json:"turns"`
	State      string    `json:"state"`
}
type Store struct {
	root string
	mu   sync.Mutex
	lock *ownership.Lock
}

func ValidID(id string) bool {
	b, e := hex.DecodeString(id)
	return e == nil && len(b) == 16 && hex.EncodeToString(b) == id
}
func NewID() (string, error) {
	var b [16]byte
	_, e := rand.Read(b[:])
	return hex.EncodeToString(b[:]), e
}

// Only one dashboard may own chats in a state directory. On restart, unfinished
// turns are terminal and never replayed. A pending request's billing is unknown.
func Open(root string) (*Store, error) {
	s := &Store{root: filepath.Join(root, "chats")}
	l, e := ownership.Acquire(s.root)
	if e != nil {
		return nil, e
	}
	s.lock = l
	list, e := s.List()
	if e == nil {
		for _, summary := range list {
			c, err := s.Get(summary.ID)
			if err != nil {
				e = err
				break
			}
			changed := false
			for i := range c.Turns {
				t := &c.Turns[i]
				if t.State == "running" {
					t.State = "interrupted"
					t.Phase = "Stopped when the server exited"
					t.Error = "This turn was interrupted. It has not been retried."
					t.BillingUnknown = t.BillingUnknown || t.Pending || t.Kind == "task"
					changed = true
				}
			}
			if changed {
				c.UpdatedAt = time.Now().UTC()
				if e = s.write(c); e != nil {
					break
				}
			}
		}
	}
	if e != nil {
		l.Close()
		return nil, e
	}
	return s, nil
}
func (s *Store) Close() error          { return s.lock.Close() }
func (s *Store) path(id string) string { return filepath.Join(s.root, id+".json") }
func (s *Store) read(id string) (Conversation, error) {
	var c Conversation
	if !ValidID(id) {
		return c, os.ErrNotExist
	}
	e := statefile.Read(s.path(id), &c, maxConversationBytes)
	if e == nil && (c.ID != id || len(c.Turns) > MaxTurns) {
		e = errors.New("invalid conversation")
	}
	return c, e
}
func (s *Store) write(c Conversation) error {
	b, e := json.Marshal(c)
	if e != nil {
		return e
	}
	if len(b) > maxConversationBytes {
		return errors.New("conversation storage limit reached; start a new chat")
	}
	return statefile.Write(s.path(c.ID), c)
}
func (s *Store) Get(id string) (Conversation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.read(id)
}
func (s *Store) List() ([]Summary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, e := os.ReadDir(s.root)
	if e != nil {
		return nil, e
	}
	out := []Summary{}
	for _, f := range entries {
		id := strings.TrimSuffix(f.Name(), ".json")
		if !strings.HasSuffix(f.Name(), ".json") || !ValidID(id) {
			continue
		}
		c, e := s.read(id)
		if e != nil {
			return nil, e
		}
		state := "ready"
		if len(c.Turns) > 0 {
			state = c.Turns[len(c.Turns)-1].State
		}
		out = append(out, Summary{c.ID, c.Title, c.Task.Repository, c.Task.Ref, c.UpdatedAt, len(c.Turns), state})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}
func (s *Store) Create(spec task.Spec, presetHash string) (Conversation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, e := os.ReadDir(s.root)
	if e != nil {
		return Conversation{}, e
	}
	if len(entries) >= 201 {
		return Conversation{}, errors.New("200 conversation limit reached")
	}
	id, e := NewID()
	if e != nil {
		return Conversation{}, e
	}
	now := time.Now().UTC()
	c := Conversation{ID: id, Title: spec.Name, PresetHash: presetHash, Task: spec, CreatedAt: now, UpdatedAt: now, Turns: []Turn{}}
	return c, s.write(c)
}
func (s *Store) Existing(id, requestID, hash string) (*Turn, error) {
	c, e := s.Get(id)
	if e != nil {
		return nil, e
	}
	for _, t := range c.Turns {
		if t.ID == requestID {
			if t.RequestHash != hash {
				return nil, ErrConflict
			}
			return &t, nil
		}
	}
	return nil, nil
}
func (s *Store) Begin(id string, t Turn) (Conversation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, e := s.read(id)
	if e != nil {
		return c, e
	}
	if !ValidID(t.ID) || !statefile.ValidHash(t.RequestHash) || (t.Kind != "ask" && t.Kind != "task") || strings.TrimSpace(t.Question) == "" || len(t.Question) > 32000 {
		return c, errors.New("invalid conversation turn")
	}
	if len(c.Turns) >= MaxTurns {
		return c, errors.New("40 turn limit reached; start a new conversation")
	}
	for _, old := range c.Turns {
		if old.ID == t.ID || old.State == "running" {
			return c, ErrConflict
		}
	}
	t.State = "running"
	t.Phase = "Preparing"
	t.CreatedAt = time.Now().UTC()
	c.Turns = append(c.Turns, t)
	c.UpdatedAt = t.CreatedAt
	if len(c.Turns) == 1 {
		title := strings.Join(strings.Fields(t.Question), " ")
		r := []rune(title)
		if len(r) > 70 {
			title = string(r[:70]) + "…"
		}
		c.Title = title
	}
	return c, s.write(c)
}
func (s *Store) Update(id, turnID string, update func(*Turn)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, e := s.read(id)
	if e != nil {
		return e
	}
	for i := range c.Turns {
		if c.Turns[i].ID == turnID {
			update(&c.Turns[i])
			c.UpdatedAt = time.Now().UTC()
			return s.write(c)
		}
	}
	return os.ErrNotExist
}
