package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/Siddhant-K-code/agent-harness/internal/dashboard"
	"github.com/Siddhant-K-code/agent-harness/internal/integrations"
	"github.com/Siddhant-K-code/agent-harness/internal/prompt"
	"github.com/Siddhant-K-code/agent-harness/internal/statefile"
	"github.com/Siddhant-K-code/agent-harness/internal/task"
	"github.com/Siddhant-K-code/agent-harness/internal/tooling"
)

type many []string

func (m *many) String() string     { return strings.Join(*m, ",") }
func (m *many) Set(s string) error { *m = append(*m, s); return nil }
func printJSON(v any) error {
	e := json.NewEncoder(os.Stdout)
	e.SetIndent("", "  ")
	return e.Encode(v)
}

func serveCommand(args []string) error {
	f := flag.NewFlagSet("serve", flag.ContinueOnError)
	root := f.String("state-dir", ".harness", "controller state directory")
	key := f.String("api-key-file", "", "private OpenAI key file")
	port := f.Int("port", 8765, "loopback port; 0 chooses a free port")
	var files many
	f.Var(&files, "task", "prepared task available in the UI (repeatable; otherwise view-only)")
	if err := f.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if f.NArg() != 0 {
		return errors.New("serve accepts flags only")
	}
	abs, err := filepath.Abs(*root)
	if err != nil {
		return err
	}
	o := dashboard.Options{Root: abs, KeyFile: *key, Port: *port, Tasks: []task.Spec{}}
	for _, file := range files {
		s, err := task.Load(file)
		if err != nil {
			return err
		}
		o.Tasks = append(o.Tasks, s)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return dashboard.Serve(ctx, o, os.Stdout)
}
func promptCommand(args []string) error {
	f := flag.NewFlagSet("prompt", flag.ContinueOnError)
	backend := f.String("backend", "docker", "docker or agentcore")
	asJSON := f.Bool("json", false, "show the prompt and tool manifest")
	if err := f.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if f.NArg() != 0 || (*backend != "docker" && *backend != "agentcore") {
		return errors.New("prompt requires --backend docker or agentcore")
	}
	p := prompt.Build(*backend, tooling.Native())
	if *asJSON {
		return printJSON(p)
	}
	fmt.Printf("%s (%s)\n\n%s\n", p.Version, p.SHA256, p.Instructions)
	return nil
}
func integrationsCommand(args []string) error {
	if len(args) == 0 || args[0] == "--help" {
		fmt.Println("Usage: harness integrations list|check\n       harness integrations add-mcp --name NAME --url URL --allow-tool TOOL [--token-env ENV]\n       harness integrations allow-github --repo OWNER/REPO\n       harness integrations select [--clear] [--mcp-tool SERVER/TOOL] [--github-repo OWNER/REPO]\nAll commands accept --state-dir; check/select accept --task. Repeat tool/repository flags. Server tools are explicit operator grants; enable only intended capabilities.")
		return nil
	}
	action := args[0]
	f := flag.NewFlagSet("integrations "+action, flag.ContinueOnError)
	root := f.String("state-dir", ".harness", "controller-owned integration policy directory")
	file := f.String("task", "harness.task.json", "task to select or check")
	name := f.String("name", "", "MCP server name")
	url := f.String("url", "", "Streamable HTTP endpoint")
	token := f.String("token-env", "", "environment variable holding this server's bearer token")
	repo := f.String("repo", "", "GitHub repository to allow")
	clear := f.Bool("clear", false, "clear existing task integration selection before adding flags")
	var allowed, selected, repos many
	f.Var(&allowed, "allow-tool", "MCP tool enabled by the operator (repeatable)")
	f.Var(&selected, "mcp-tool", "MCP server/tool selected for this task (repeatable)")
	f.Var(&repos, "github-repo", "GitHub repository selected for this task (repeatable)")
	if err := f.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if f.NArg() != 0 {
		return errors.New("unexpected positional argument")
	}
	c, err := integrations.Load(*root)
	if err != nil {
		return err
	}
	switch action {
	case "list":
		return printJSON(c)
	case "add-mcp":
		c.MCP[*name] = integrations.Server{URL: *url, TokenEnv: *token, Tools: allowed}
		if err = integrations.Save(*root, c); err != nil {
			return err
		}
		return printJSON(c)
	case "allow-github":
		if !slices.Contains(c.GitHubRepositories, *repo) {
			c.GitHubRepositories = append(c.GitHubRepositories, *repo)
		}
		if err = integrations.Save(*root, c); err != nil {
			return err
		}
		return printJSON(c)
	case "check", "select":
		s, err := task.Load(*file)
		if err != nil {
			return err
		}
		if action == "check" {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			b, err := integrations.Connect(ctx, *root, s.Integrations)
			if err != nil {
				return err
			}
			defer b.Close()
			return printJSON(b.Manifest)
		}
		if *clear {
			s.Integrations = integrations.Selection{}
		}
		for _, repo := range repos {
			if !slices.Contains(s.Integrations.GitHubRepositories, repo) {
				s.Integrations.GitHubRepositories = append(s.Integrations.GitHubRepositories, repo)
			}
		}
		for _, pair := range selected {
			server, tool, ok := strings.Cut(pair, "/")
			if !ok {
				return errors.New("--mcp-tool requires SERVER/TOOL")
			}
			found := false
			for i := range s.Integrations.MCP {
				m := &s.Integrations.MCP[i]
				if m.Server == server {
					found = true
					if !slices.Contains(m.Tools, tool) {
						m.Tools = append(m.Tools, tool)
					}
				}
			}
			if !found {
				s.Integrations.MCP = append(s.Integrations.MCP, integrations.MCPSelection{Server: server, Tools: []string{tool}})
			}
		}
		if err = s.Validate(); err != nil {
			return err
		}
		if err = c.Authorize(s.Integrations); err != nil {
			return err
		}
		originalFile, err := os.Open(*file)
		if err != nil {
			return err
		}
		original, err := task.Decode(originalFile)
		originalFile.Close()
		if err != nil {
			return err
		}
		original.Integrations = s.Integrations
		if err = statefile.Write(*file, original); err != nil {
			return err
		}
		return printJSON(s.Integrations)
	default:
		return errors.New("unknown integrations command")
	}
}
func githubCommand(args []string) error {
	if len(args) == 0 || args[0] == "--help" {
		fmt.Println("Usage: harness github status\n       harness github clone OWNER/REPO NEW_DIRECTORY\n       harness github read --repo OWNER/REPO --resource issues|issue|pulls|pull|diff|checks [--number N]\nAuthenticate locally first: gh auth login --hostname github.com --web. Tokens are never copied to the executor.")
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	switch args[0] {
	case "status":
		if len(args) != 1 {
			return errors.New("status accepts no arguments")
		}
		if err := integrations.GitHubStatus(ctx); err != nil {
			return err
		}
		fmt.Println("GitHub API access verified through gh; credentials remain on the host.")
		return nil
	case "clone":
		if len(args) != 3 {
			return errors.New("clone requires OWNER/REPO and a new directory")
		}
		if err := integrations.Clone(ctx, args[1], args[2]); err != nil {
			return err
		}
		fmt.Println("Cloned", args[1], "to", args[2])
		return nil
	case "read":
		f := flag.NewFlagSet("github read", flag.ContinueOnError)
		repo := f.String("repo", "", "owner/repository")
		resource := f.String("resource", "issues", "read-only resource")
		number := f.Int("number", 0, "issue or PR number")
		if err := f.Parse(args[1:]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil
			}
			return err
		}
		if f.NArg() != 0 {
			return errors.New("read accepts flags only")
		}
		res, err := integrations.GitHubRead(ctx, *repo, *resource, *number)
		if err != nil {
			return err
		}
		return printJSON(res)
	default:
		return errors.New("unknown github command")
	}
}
