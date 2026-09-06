// Package tooling defines the shared, inspectable tool contract.
package tooling

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

type Definition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

func Define(name, description string, fields ...string) Definition {
	p := map[string]any{}
	for _, f := range fields {
		p[f] = map[string]any{"type": "string"}
	}
	return Definition{name, description, map[string]any{"type": "object", "properties": p, "required": fields, "additionalProperties": false}}
}

func Native() []Definition {
	return []Definition{
		Define("list_files", "List repository files beneath a relative directory. Use path '.' for the repository root. Output is bounded.", "path"),
		Define("read_file", "Read a UTF-8 text file by repository-relative path. Output is bounded; use exec for ranged reads of large files.", "path"),
		Define("search_text", "Search for literal text under a repository-relative path using grep. Returns matching lines and line numbers; no regex interpretation.", "path", "text"),
		Define("write_file", "Create or replace one UTF-8 text file. Read existing content first. Parent directories must exist. Use exec for targeted edits of large files.", "path", "content"),
		Define("exec", "Run a shell script in the isolated repository workspace. Use for targeted edits, tests and diagnostics. Environment resets between commands; output is bounded.", "command"),
		Define("finish", "Submit a concise change summary and trigger independent verification. A passing verifier is required for completion.", "summary"),
	}
}

func Decode(s string, out any) error {
	if len(s) > 128<<10 {
		return errors.New("tool arguments exceed 128 KiB")
	}
	d := json.NewDecoder(strings.NewReader(s))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return errors.New("arguments do not match the tool schema")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("invalid trailing tool arguments")
	}
	return nil
}

func Quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }

// Command builds scripts for the executor, never the controller's host shell.
// Reject symlink components as well as traversal, including for writes.
func Command(name, raw string) (command string, readonly bool, err error) {
	var a struct {
		Path    string `json:"path"`
		Text    string `json:"text,omitempty"`
		Content string `json:"content,omitempty"`
	}
	var fields map[string]json.RawMessage
	if err = Decode(raw, &fields); err != nil {
		return
	}
	allowed := map[string]bool{"path": true}
	if name == "search_text" {
		allowed["text"] = true
	}
	if name == "write_file" {
		allowed["content"] = true
	}
	for f, value := range fields {
		if strings.TrimSpace(string(value)) == "null" {
			return "", false, errors.New("tool string arguments cannot be null")
		}
		if !allowed[f] {
			return "", false, errors.New("unknown argument")
		}
	}
	for f := range allowed {
		if _, ok := fields[f]; !ok {
			return "", false, errors.New("missing argument")
		}
	}
	if err = Decode(raw, &a); err != nil {
		return
	}
	if a.Path == "" || len(a.Path) > 4096 || strings.ContainsAny(a.Path, "\x00\n\r") || strings.HasPrefix(a.Path, "/") || path.Clean(a.Path) != a.Path {
		return "", false, errors.New("use a clean repository-relative path")
	}
	for _, part := range strings.Split(a.Path, "/") {
		if part == ".." || part == ".git" {
			return "", false, errors.New("path is outside the editable repository")
		}
	}
	if strings.ContainsRune(a.Text, 0) || strings.ContainsRune(a.Content, 0) || len(a.Content) > 32000 || len(a.Text) > 1000 {
		return "", false, errors.New("text argument is too large or contains NUL")
	}
	guard := "set -eu\n"
	partPath := "."
	for _, part := range strings.Split(a.Path, "/") {
		partPath += "/" + part
		guard += "[ ! -L " + Quote(partPath) + " ] || { printf '%s\\n' 'symlink paths are unsupported' >&2; exit 1; }\n"
	}
	p := Quote("./" + a.Path)
	switch name {
	case "read_file":
		command, readonly = "[ -f "+p+" ] && cat "+p, true
	case "list_files":
		command, readonly = "[ -d "+p+" ] && find "+p+" -name .git -prune -o -type f -print", true
	case "search_text":
		if a.Text == "" {
			return "", false, errors.New("search text is empty")
		}
		command, readonly = "grep -r -n -F -e "+Quote(a.Text)+" "+p+";", true
		// grep's no-match status is a valid search result, not an execution failure.
		command = "set +e\n" + command + " code=$?; [ \"$code\" -le 1 ] || exit \"$code\""
	case "write_file":
		if a.Path == "." {
			return "", false, errors.New("write_file requires a file")
		}
		command = "printf '%s' " + Quote(a.Content) + " > " + p + "\nprintf '%s\\n' 'file written'"
	default:
		return "", false, fmt.Errorf("unknown native tool %q", name)
	}
	return guard + command, readonly, nil
}
