package tooling

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Siddhant-K-code/agent-harness/internal/process"
)

func TestNativeCommandsPreserveLiteralDataAndRejectEscapes(t *testing.T) {
	dir := t.TempDir()
	content := "quotes ' \"; $(touch escaped) `touch escaped`\nsecond line\n"
	execute := func(name string, args any) process.Result {
		t.Helper()
		raw, _ := json.Marshal(args)
		cmd, _, err := Command(name, string(raw))
		if err != nil {
			t.Fatal(err)
		}
		res, err := process.Run(context.Background(), dir, nil, nil, 32000, "sh", "-c", cmd)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	name := "a ' file.txt"
	res := execute("write_file", map[string]string{"path": name, "content": content})
	if res.ExitCode != 0 {
		t.Fatal(res)
	}
	res = execute("read_file", map[string]string{"path": name})
	if res.Output != content {
		t.Fatalf("content changed: %q", res.Output)
	}
	if _, err := os.Stat(filepath.Join(dir, "escaped")); !os.IsNotExist(err) {
		t.Fatal("shell expansion occurred")
	}
	res = execute("search_text", map[string]string{"path": ".", "text": "unmatched literal"})
	if res.ExitCode != 0 {
		t.Fatal("no-match search failed")
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	res = execute("write_file", map[string]string{"path": "link/file", "content": "bad"})
	if res.ExitCode == 0 {
		t.Fatal("symlink write accepted")
	}
	for _, path := range []string{"../secret", "a/../../secret", "/tmp/secret", ".git/config", "a/.git/file", "a//b", "a\nfile", ""} {
		raw, _ := json.Marshal(map[string]string{"path": path})
		if _, _, err := Command("read_file", string(raw)); err == nil {
			t.Fatal("unsafe path accepted:", path)
		}
	}
	for _, raw := range []string{`{"path":"a","content":"b","command":"bad"}`, `{"path":"a"}`, `null`, `{"path":"a","content":"b"} {}`} {
		if _, _, err := Command("write_file", raw); err == nil {
			t.Fatal("invalid arguments accepted:", raw)
		}
	}
	res = execute("list_files", map[string]string{"path": "."})
	if !strings.Contains(res.Output, name) {
		t.Fatal("listing omitted file")
	}
}
