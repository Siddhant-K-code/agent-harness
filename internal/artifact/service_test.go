package artifact

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/Siddhant-K-code/agent-harness/internal/workspace"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestRealArchiveTransportAndRefusals(t *testing.T) {
	ctx := context.Background()
	src := t.TempDir()
	archives := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "source.js"), []byte("module.exports = 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	snapshot, err := (workspace.Workspace{Path: src}).SnapshotTo(ctx, archives)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(archives, snapshot.SHA256+".tar"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	server := httptest.NewServer(&Service{Root: root})
	defer server.Close()
	post := func(body []byte, status int) Message {
		t.Helper()
		res, err := http.Post(server.URL+"/invocations", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		payload, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != status {
			t.Fatalf("status %d: %s", res.StatusCode, payload)
		}
		var out Message
		if status == 200 && json.Unmarshal(payload, &out) != nil {
			t.Fatal("invalid response")
		}
		return out
	}
	encode := func(m Message) []byte {
		b, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	post([]byte(`{"operation":"export"}`), 400)
	bad := append([]byte(nil), b...)
	bad[0] ^= 1
	post(encode(Message{Operation: "import", Snapshot: snapshot, Archive: bad}), 400)
	if _, err := os.Lstat(filepath.Join(root, "repo")); !os.IsNotExist(err) {
		t.Fatal("bad archive left a workspace")
	}
	post([]byte(`{"operation":"import","command":"echo forbidden"}`), 400)
	ack := post(encode(Message{Operation: "import", Snapshot: snapshot, Archive: b}), 200)
	if ack.Snapshot != snapshot {
		t.Fatal("lost import identity")
	}
	post(encode(Message{Operation: "import", Snapshot: snapshot, Archive: b}), 400)
	if err := os.WriteFile(filepath.Join(root, "repo", "added"), []byte("real change"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "repo", "source.js")); err != nil {
		t.Fatal(err)
	}
	exported := post([]byte(`{"operation":"export"}`), 200)
	filename := filepath.Join(t.TempDir(), "received.tar")
	os.WriteFile(filename, exported.Archive, 0600)
	dst := filepath.Join(t.TempDir(), "candidate")
	if err := workspace.RestoreSnapshot(ctx, filename, dst, exported.Snapshot); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "added")); string(b) != "real change" {
		t.Fatal("lost changed file")
	}
	if _, err := os.Stat(filepath.Join(dst, "source.js")); !os.IsNotExist(err) {
		t.Fatal("lost deletion")
	}
	if err := os.Symlink("/etc/passwd", filepath.Join(root, "repo", "escape")); err != nil {
		t.Fatal(err)
	}
	post([]byte(`{"operation":"export"}`), 400)
}
