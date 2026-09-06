// Package integrations brokers explicitly selected external capabilities on the host.
package integrations

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/Siddhant-K-code/agent-harness/internal/statefile"
)

type MCPSelection struct {
	Server string   `json:"server"`
	Tools  []string `json:"tools"`
}
type Selection struct {
	MCP                []MCPSelection `json:"mcp,omitempty"`
	GitHubRepositories []string       `json:"github_repositories,omitempty"`
}
type Server struct {
	URL      string   `json:"url"`
	TokenEnv string   `json:"token_env,omitempty"`
	Tools    []string `json:"tools"`
}
type Config struct {
	Schema             int               `json:"schema"`
	MCP                map[string]Server `json:"mcp"`
	GitHubRepositories []string          `json:"github_repositories"`
}

var namePattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,63}$`)
var repoPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]{0,38}/[a-zA-Z0-9_.-]{1,100}$`)
var envPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,127}$`)

func Repository(s string) bool {
	return repoPattern.MatchString(s) && !strings.HasSuffix(s, "/.") && !strings.HasSuffix(s, "/..")
}
func (s Selection) Empty() bool { return len(s.MCP) == 0 && len(s.GitHubRepositories) == 0 }
func names(ss []string, valid func(string) bool, max int) bool {
	seen := map[string]bool{}
	if len(ss) > max {
		return false
	}
	for _, s := range ss {
		if !valid(s) || seen[s] {
			return false
		}
		seen[s] = true
	}
	return true
}
func validTool(s string) bool {
	return len(s) > 0 && len(s) <= 128 && !strings.ContainsAny(s, "\x00\n\r")
}
func (s Selection) Validate() error {
	if len(s.MCP) > 8 || !names(s.GitHubRepositories, Repository, 8) {
		return errors.New("invalid integration selection")
	}
	seen := map[string]bool{}
	total := 0
	for _, m := range s.MCP {
		total += len(m.Tools)
		if !namePattern.MatchString(m.Server) || seen[m.Server] || len(m.Tools) == 0 || !names(m.Tools, validTool, 32) {
			return errors.New("invalid MCP server/tool selection")
		}
		seen[m.Server] = true
	}
	if total > 32 {
		return errors.New("at most 32 MCP tools may be selected")
	}
	return nil
}
func (c Config) Validate() error {
	if c.Schema != 1 || c.MCP == nil || len(c.MCP) > 16 || !names(c.GitHubRepositories, Repository, 32) {
		return errors.New("invalid integrations configuration")
	}
	for name, s := range c.MCP {
		if !namePattern.MatchString(name) || len(s.Tools) == 0 || !names(s.Tools, validTool, 64) {
			return errors.New("invalid allowed MCP tools")
		}
		u, e := url.Parse(s.URL)
		if e != nil || len(s.URL) > 2048 || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return errors.New("MCP URL must have a host and no credentials, query or fragment")
		}
		loopback := net.ParseIP(u.Hostname())
		if u.Scheme != "https" && !(u.Scheme == "http" && loopback != nil && loopback.IsLoopback()) {
			return errors.New("MCP requires HTTPS, or HTTP on a literal loopback address")
		}
		if s.TokenEnv != "" && !envPattern.MatchString(s.TokenEnv) {
			return errors.New("token_env must name an uppercase environment variable")
		}
	}
	return nil
}
func Load(root string) (Config, error) {
	c := Config{Schema: 1, MCP: map[string]Server{}, GitHubRepositories: []string{}}
	p := filepath.Join(root, "integrations.json")
	info, e := os.Lstat(p)
	if errors.Is(e, os.ErrNotExist) {
		return c, nil
	}
	if e != nil {
		return c, e
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return c, errors.New("integrations.json must be a regular owner-only file (chmod 600)")
	}
	if e = statefile.Read(p, &c, 64<<10); e != nil {
		return c, e
	}
	return c, c.Validate()
}
func Save(root string, c Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	return statefile.Write(filepath.Join(root, "integrations.json"), c)
}
func (c Config) Authorize(s Selection) error {
	if err := s.Validate(); err != nil {
		return err
	}
	for _, repo := range s.GitHubRepositories {
		if !slices.Contains(c.GitHubRepositories, repo) {
			return fmt.Errorf("GitHub repository %s is not enabled by the operator", repo)
		}
	}
	for _, m := range s.MCP {
		server, ok := c.MCP[m.Server]
		if !ok {
			return fmt.Errorf("MCP server %s is not configured", m.Server)
		}
		for _, t := range m.Tools {
			if !slices.Contains(server.Tools, t) {
				return fmt.Errorf("MCP tool %s/%s is not enabled by the operator", m.Server, t)
			}
		}
	}
	return nil
}
