// Package skills manages explicit repository-scoped, immutable skill revisions.
package skills

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Siddhant-K-code/agent-harness/internal/ownership"
	"github.com/Siddhant-K-code/agent-harness/internal/statefile"
)

const MaxInstructions = 32000

var idPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

type Ref struct {
	ID      string `json:"id"`
	Version string `json:"version,omitempty"` // empty resolves the active version once
}

func (r Ref) Validate() error {
	if !idPattern.MatchString(r.ID) || (r.Version != "" && !statefile.ValidHash(r.Version)) {
		return errors.New("skill requires a lowercase id and optional SHA-256 version")
	}
	return nil
}

type Version struct {
	Schema       int        `json:"schema"`
	ID           string     `json:"id"`
	Repository   string     `json:"repository"`
	Description  string     `json:"description"`
	Instructions string     `json:"instructions"`
	Parent       string     `json:"parent,omitempty"`
	Source       string     `json:"source"` // imported or learned; learned revisions require evaluation
	Evidence     []string   `json:"evidence,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
}

func Scope(repo string) (string, error) {
	abs, err := filepath.Abs(repo)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

func (v Version) Validate() error {
	if err := (Ref{ID: v.ID, Version: v.Parent}).Validate(); err != nil {
		return err
	}
	if v.Schema != 1 || !filepath.IsAbs(v.Repository) || v.CreatedAt.IsZero() {
		return errors.New("invalid skill metadata")
	}
	if len(v.Description) > 1000 || strings.TrimSpace(v.Description) == "" || len(v.Instructions) > MaxInstructions || strings.TrimSpace(v.Instructions) == "" || strings.ContainsRune(v.Instructions, 0) {
		return errors.New("skill requires a description and 1..32000 bytes of instructions without NUL")
	}
	if v.Source != "imported" && v.Source != "learned" {
		return errors.New("unknown skill source")
	}
	if v.Source == "learned" && (v.Parent == "" || len(v.Evidence) == 0 || v.ExpiresAt == nil) {
		return errors.New("learned skills require parent, evidence, and expiry")
	}
	if len(v.Evidence) > 20 {
		return errors.New("at most 20 source observations per revision")
	}
	for _, e := range v.Evidence {
		if !statefile.ValidHash(e) {
			return errors.New("invalid skill evidence hash")
		}
	}
	if v.ExpiresAt != nil && !v.ExpiresAt.After(v.CreatedAt) {
		return errors.New("skill expiry must follow creation")
	}
	return nil
}

type Selection struct {
	Current  string   `json:"current"`
	Previous []string `json:"previous"`
}
type Registry struct {
	Schema  int                  `json:"schema"`
	Entries map[string]Selection `json:"entries"`
}
type Store struct{ Root string }

func (s Store) path(parts ...string) string {
	return filepath.Join(append([]string{s.Root, "skills"}, parts...)...)
}
func registryKey(repo, id string) string { return statefile.Hash([]string{repo, id}) }
func (s Store) List() (Registry, error) {
	v := Registry{Schema: 1, Entries: map[string]Selection{}}
	err := statefile.Read(s.path("registry.json"), &v, 4<<20)
	if errors.Is(err, os.ErrNotExist) {
		return v, nil
	}
	if err == nil && (v.Schema != 1 || v.Entries == nil) {
		err = errors.New("invalid skill registry")
	}
	return v, err
}
func (s Store) Put(v Version) (string, error) {
	if err := v.Validate(); err != nil {
		return "", err
	}
	if v.Parent != "" {
		p, err := s.Get(v.Parent)
		if err != nil {
			return "", err
		}
		if p.Repository != v.Repository || p.ID != v.ID {
			return "", errors.New("skill parent scope mismatch")
		}
	}
	id := statefile.Hash(v)
	return id, statefile.Write(s.path("versions", id+".json"), v)
}
func (s Store) Get(hash string) (Version, error) {
	var v Version
	if !statefile.ValidHash(hash) {
		return v, errors.New("invalid skill version")
	}
	if err := statefile.Read(s.path("versions", hash+".json"), &v, 64<<10); err != nil {
		return v, err
	}
	if err := v.Validate(); err != nil {
		return v, err
	}
	if statefile.Hash(v) != hash {
		return v, errors.New("skill content hash mismatch")
	}
	return v, nil
}
func (s Store) Active(repo, id string) (string, error) {
	r, err := s.List()
	if err != nil {
		return "", err
	}
	hash := r.Entries[registryKey(repo, id)].Current
	if hash == "" {
		return "", fmt.Errorf("skill %s has no active version for this repository", id)
	}
	return hash, nil
}

// Activate requires compare-and-swap against the evaluated parent. The caller
// of Promote must have validated its evaluation; ordinary activation only
// accepts imported revisions. These are local owner-controlled artifacts.
func (s Store) Activate(hash string) error { return s.change(hash, false) }
func (s Store) Promote(hash string) error  { return s.change(hash, true) }
func (s Store) change(hash string, evaluated bool) error {
	lock, err := ownership.Acquire(s.path("registry-lock"))
	if err != nil {
		return err
	}
	defer lock.Close()
	v, err := s.Get(hash)
	if err != nil {
		return err
	}
	if v.Source == "learned" && !evaluated {
		return errors.New("learned revisions require harness learn promote and a passing evaluation")
	}
	if v.ExpiresAt != nil && !time.Now().Before(*v.ExpiresAt) {
		return errors.New("skill revision expired")
	}
	r, err := s.List()
	if err != nil {
		return err
	}
	key := registryKey(v.Repository, v.ID)
	entry := r.Entries[key]
	if entry.Current == hash {
		return nil
	}
	if evaluated && entry.Current != v.Parent {
		return errors.New("active skill changed since evaluation; evaluate against the current parent")
	}
	if entry.Current != "" {
		entry.Previous = append(entry.Previous, entry.Current)
	}
	entry.Current = hash
	r.Entries[key] = entry
	return statefile.Write(s.path("registry.json"), r)
}
func (s Store) Rollback(repo, id string) (string, error) {
	lock, err := ownership.Acquire(s.path("registry-lock"))
	if err != nil {
		return "", err
	}
	defer lock.Close()
	r, err := s.List()
	if err != nil {
		return "", err
	}
	key := registryKey(repo, id)
	e := r.Entries[key]
	if len(e.Previous) == 0 {
		return "", errors.New("no previous active skill revision")
	}
	hash := e.Previous[len(e.Previous)-1]
	v, err := s.Get(hash)
	if err != nil {
		return "", err
	}
	if v.ID != id || v.Repository != repo || (v.ExpiresAt != nil && !time.Now().Before(*v.ExpiresAt)) {
		return "", errors.New("previous skill is expired or has the wrong scope")
	}
	e.Current, e.Previous = hash, e.Previous[:len(e.Previous)-1]
	r.Entries[key] = e
	return hash, statefile.Write(s.path("registry.json"), r)
}

func (s Store) Resolve(repo string, refs []Ref) ([]Ref, []Version, error) {
	if len(refs) == 0 {
		return nil, nil, nil
	}
	scope, err := Scope(repo)
	if err != nil {
		return nil, nil, err
	}
	pinned := make([]Ref, 0, len(refs))
	versions := make([]Version, 0, len(refs))
	total := 0
	for _, ref := range refs {
		if err := ref.Validate(); err != nil {
			return nil, nil, err
		}
		if ref.Version == "" {
			ref.Version, err = s.Active(scope, ref.ID)
			if err != nil {
				return nil, nil, err
			}
		}
		v, err := s.Get(ref.Version)
		if err != nil {
			return nil, nil, err
		}
		if v.Repository != scope || v.ID != ref.ID {
			return nil, nil, errors.New("skill is not applicable to this repository/id")
		}
		if v.ExpiresAt != nil && !time.Now().Before(*v.ExpiresAt) {
			return nil, nil, errors.New("selected skill expired; evaluate a refreshed revision")
		}
		total += len(v.Instructions)
		if total > 64000 {
			return nil, nil, errors.New("selected skills exceed 64000 instruction bytes")
		}
		pinned = append(pinned, ref)
		versions = append(versions, v)
	}
	return pinned, versions, nil
}
