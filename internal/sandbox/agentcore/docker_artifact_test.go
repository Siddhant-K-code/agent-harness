package agentcore

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Siddhant-K-code/agent-harness/internal/artifact"
	"github.com/Siddhant-K-code/agent-harness/internal/workspace"
)

// This is the actual image and artifact service, not a simulated executor.
func TestDockerArtifactService(t *testing.T) {
	if os.Getenv("HARNESS_AGENTCORE_DOCKER_TEST") != "1" {
		t.Skip("set HARNESS_AGENTCORE_DOCKER_TEST=1 after building agent-harness-guard:test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	docker := func(args ...string) string {
		t.Helper()
		out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("docker %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	id := docker("run", "--rm", "-d", "-p", "127.0.0.1::8080", "agent-harness-guard:test")
	t.Cleanup(func() { exec.Command("docker", "rm", "-f", id).Run() })
	endpoint := "http://" + docker("port", id, "8080/tcp") + "/invocations"
	client := &http.Client{Timeout: 30 * time.Second}
	post := func(m artifact.Message) artifact.Message {
		t.Helper()
		b, _ := json.Marshal(m)
		res, err := client.Post(endpoint, "application/json", bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		out, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != 200 {
			t.Fatalf("artifact service: %d %s", res.StatusCode, out)
		}
		var response artifact.Message
		if err := json.Unmarshal(out, &response); err != nil {
			t.Fatal(err)
		}
		return response
	}
	source := t.TempDir()
	os.WriteFile(filepath.Join(source, "tags.js"), []byte("module.exports = { answer: 42 }\n"), 0644)
	w := workspace.Workspace{Path: source}
	archives := t.TempDir()
	snapshot, err := w.SnapshotTo(ctx, archives)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(archives, snapshot.SHA256+".tar"))
	if err != nil {
		t.Fatal(err)
	}
	post(artifact.Message{Operation: "import", Snapshot: snapshot, Archive: b})
	docker("exec", "-i", id, "/usr/local/bin/harness-guard", "work", "--", "set -eu; node -e \"if(require('/workspace/tags.js').answer !== 42) process.exit(1)\"; printf changed > new.txt; python3 /opt/harness/isolation-checks.py network")
	docker("exec", "-i", id, "/usr/local/bin/harness-guard", "verify", "--", "python3 /opt/harness/isolation-checks.py readonly")
	out := post(artifact.Message{Operation: "export"})
	root := t.TempDir()
	if err := saveArchive(root, out); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "received")
	if err := workspace.RestoreSnapshot(ctx, filepath.Join(root, "snapshots", out.Snapshot.SHA256+".tar"), destination, out.Snapshot); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(destination, "new.txt")); string(b) != "changed" {
		t.Fatal("lost remote write")
	}
}
