package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Siddhant-K-code/agent-harness/internal/skills"
)

func skillsCommand(args []string) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Println("Usage: harness skills import --id ID --repo PATH --file SKILL.md [--description TEXT] [--activate=false]\n       harness skills list\n       harness skills show|activate VERSION\n       harness skills rollback --repo PATH --id ID\nAll commands accept --state-dir PATH. Select an imported skill with harness config set --skill ID.")
		return nil
	}
	action := args[0]
	if action != "import" && action != "list" && action != "show" && action != "activate" && action != "rollback" {
		return errors.New("unknown skills command")
	}
	f := flag.NewFlagSet("skills "+action, flag.ContinueOnError)
	rootFlag := f.String("state-dir", ".harness", "controller state directory")
	id := f.String("id", "", "skill identifier")
	repo := f.String("repo", "", "repository scope")
	file := f.String("file", "", "Markdown skill file to import")
	description := f.String("description", "Imported coding guidance", "short applicability description")
	activate := f.Bool("activate", true, "activate the imported revision for explicitly selected tasks")
	if err := f.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	root, err := filepath.Abs(*rootFlag)
	if err != nil {
		return err
	}
	s := skills.Store{Root: root}
	encode := func(v any) error { e := json.NewEncoder(os.Stdout); e.SetIndent("", "  "); return e.Encode(v) }
	if action == "show" || action == "activate" {
		if f.NArg() != 1 {
			return errors.New("exactly one version hash required; put flags first")
		}
		if action == "activate" {
			if err := s.Activate(f.Arg(0)); err != nil {
				return err
			}
			return encode(map[string]any{"active": f.Arg(0)})
		}
		v, err := s.Get(f.Arg(0))
		if err != nil {
			return err
		}
		return encode(v)
	}
	if f.NArg() != 0 {
		return errors.New("unexpected positional argument")
	}
	if action == "list" {
		r, err := s.List()
		if err != nil {
			return err
		}
		out := []map[string]any{}
		for _, entry := range r.Entries {
			v, err := s.Get(entry.Current)
			if err != nil {
				return err
			}
			out = append(out, map[string]any{"id": v.ID, "repository": v.Repository, "version": entry.Current, "description": v.Description, "source": v.Source, "previous": entry.Previous, "expires_at": v.ExpiresAt})
		}
		return encode(out)
	}
	if *repo == "" || *id == "" {
		return errors.New("--repo and --id are required")
	}
	scope, err := skills.Scope(*repo)
	if err != nil {
		return err
	}
	if action == "rollback" {
		hash, err := s.Rollback(scope, *id)
		if err != nil {
			return err
		}
		return encode(map[string]any{"active": hash, "rolled_back": true})
	}
	info, err := os.Lstat(*file)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > skills.MaxInstructions {
		return errors.New("skill must be a regular Markdown file of 1..32000 bytes")
	}
	b, err := os.ReadFile(*file)
	if err != nil {
		return err
	}
	v := skills.Version{Schema: 1, ID: *id, Repository: scope, Description: *description, Instructions: string(b), Source: "imported", CreatedAt: time.Now().UTC()}
	if parent, e := s.Active(scope, *id); e == nil {
		v.Parent = parent
	}
	hash, err := s.Put(v)
	if err != nil {
		return err
	}
	if *activate {
		if err := s.Activate(hash); err != nil {
			return err
		}
	}
	return encode(map[string]any{"id": *id, "version": hash, "active": *activate, "repository": scope})
}
