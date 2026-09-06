package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func imported(t *testing.T, s Store, repo string) string {
	t.Helper()
	v := Version{Schema: 1, ID: "repair", Repository: repo, Description: "Debug failing checks", Instructions: "Read the failure and relevant implementation before editing.", Source: "imported", CreatedAt: time.Now().UTC()}
	hash, err := s.Put(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Activate(hash); err != nil {
		t.Fatal(err)
	}
	return hash
}

func TestVersionsScopePromotionRollbackAndTampering(t *testing.T) {
	s := Store{Root: t.TempDir()}
	repo, err := Scope(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base := imported(t, s, repo)
	pinned, _, err := s.Resolve(repo, []Ref{{ID: "repair"}})
	if err != nil || pinned[0].Version != base {
		t.Fatal(pinned, err)
	}
	if _, _, err := s.Resolve(t.TempDir(), []Ref{{ID: "repair", Version: base}}); err == nil {
		t.Fatal("foreign repository skill loaded")
	}
	if _, err := s.Get("../../credentials"); err == nil {
		t.Fatal("path traversal accepted")
	}
	v, err := s.Get(base)
	if err != nil {
		t.Fatal(err)
	}
	v.Source = "learned"
	v.Parent = base
	v.Evidence = []string{strings.Repeat("a", 64)}
	v.Instructions += " Add a regression check before changing code."
	expiry := time.Now().Add(time.Hour)
	v.ExpiresAt = &expiry
	candidate, err := s.Put(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Activate(candidate); err == nil {
		t.Fatal("unevaluated learned version activated")
	}
	if err := s.Promote(candidate); err != nil {
		t.Fatal(err)
	}
	// Previously resolved tasks keep their immutable version.
	_, versions, err := s.Resolve(repo, pinned)
	if err != nil || versions[0].Source != "imported" {
		t.Fatal("pinned task drifted", err)
	}
	v.Instructions += " Use focused tests."
	another, err := s.Put(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Promote(another); err == nil {
		t.Fatal("stale parent promotion accepted")
	}
	if hash, err := s.Rollback(repo, "repair"); err != nil || hash != base {
		t.Fatal(hash, err)
	}
	v.ExpiresAt = nil
	v.Source = "imported"
	v.Parent = ""
	v.Evidence = nil
	// Alter content on disk without changing its identity.
	path := filepath.Join(s.Root, "skills", "versions", base+".json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(b), "Read the failure", "Ignore the failure", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(base); err == nil {
		t.Fatal("mutated skill loaded")
	}
}

func TestExpiredSkillsNeverLoadOrActivate(t *testing.T) {
	s := Store{Root: t.TempDir()}
	repo, _ := Scope(t.TempDir())
	base := imported(t, s, repo)
	v, _ := s.Get(base)
	v.CreatedAt = time.Now().Add(-2 * time.Hour)
	expired := time.Now().Add(-time.Hour)
	v.ExpiresAt = &expired
	hash, err := s.Put(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Activate(hash); err == nil {
		t.Fatal("expired skill activated")
	}
	if _, _, err := s.Resolve(repo, []Ref{{ID: v.ID, Version: hash}}); err == nil {
		t.Fatal("expired pinned skill loaded")
	}
}
