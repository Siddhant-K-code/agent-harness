// Package artifact transfers bounded, checksummed workspace archives through
// authenticated AgentCore invocations. It exposes no command execution endpoint.
package artifact

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/Siddhant-K-code/agent-harness/internal/workspace"
)

const MaxBytes = 8 << 20
const MaxWireBytes = 12 << 20

type Message struct {
	Operation string             `json:"operation"`
	Snapshot  workspace.Snapshot `json:"snapshot"`
	Archive   []byte             `json:"archive,omitempty"`
}

type Service struct {
	Root     string
	mu       sync.Mutex
	imported bool
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method == "GET" && r.URL.Path == "/ping" {
		io.WriteString(w, `{"status":"Healthy"}`)
		return
	}
	if r.Method != "POST" || r.URL.Path != "/invocations" {
		http.Error(w, "not found", 404)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxWireBytes)
	defer r.Body.Close()
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	var request Message
	if err := d.Decode(&request); err != nil {
		http.Error(w, "invalid artifact request", 400)
		return
	}
	if err := d.Decode(new(any)); err != io.EOF {
		http.Error(w, "trailing artifact request", 400)
		return
	}
	if request.Operation == "health" {
		io.WriteString(w, `{"status":"ready","artifact_protocol":1}`)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	result, err := s.exchange(ctx, request)
	if err != nil {
		http.Error(w, "artifact refused: "+err.Error(), 400)
		return
	}
	json.NewEncoder(w).Encode(result)
}

func (s *Service) exchange(ctx context.Context, request Message) (Message, error) {
	var result Message
	stage, err := os.MkdirTemp("", "harness-artifact-")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(stage)
	switch request.Operation {
	case "import":
		if s.imported {
			return result, errors.New("workspace already imported")
		}
		if len(request.Archive) > MaxBytes || int64(len(request.Archive)) != request.Snapshot.Bytes {
			return result, errors.New("archive exceeds bound or length differs")
		}
		filename := filepath.Join(stage, "input.tar")
		if err := os.WriteFile(filename, request.Archive, 0600); err != nil {
			return result, err
		}
		incoming := filepath.Join(s.Root, "incoming")
		defer os.RemoveAll(incoming)
		if err := workspace.RestoreSnapshot(ctx, filename, incoming, request.Snapshot); err != nil {
			return result, err
		}
		if info, err := os.Lstat(filepath.Join(s.Root, "repo")); err == nil {
			if !info.IsDir() {
				return result, errors.New("initial workspace is not a directory")
			}
			// Remove only the empty initial image directory. Existing contents
			// must never be replaced by an import after a command has run.
			if err := os.Remove(filepath.Join(s.Root, "repo")); err != nil {
				return result, err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return result, err
		}
		if err := os.Rename(incoming, filepath.Join(s.Root, "repo")); err != nil {
			return result, err
		}
		s.imported = true
		return Message{Operation: "imported", Snapshot: request.Snapshot}, nil
	case "export":
		if !s.imported {
			return result, errors.New("workspace not imported")
		}
		base, err := os.OpenRoot(s.Root)
		if err != nil {
			return result, err
		}
		defer base.Close()
		candidate, err := base.OpenRoot("repo")
		if err != nil {
			return result, err
		}
		defer candidate.Close()
		snapshot, err := workspace.SnapshotRootTo(ctx, candidate, stage)
		if err != nil {
			return result, err
		}
		if snapshot.Bytes > MaxBytes {
			return result, errors.New("remote archive exceeds 8 MiB")
		}
		filename := filepath.Join(stage, snapshot.SHA256+".tar")
		if err := workspace.VerifySnapshot(filename, snapshot); err != nil {
			return result, err
		}
		f, err := os.OpenFile(filename, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
		if err != nil {
			return result, err
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() {
			return result, errors.New("invalid snapshot file")
		}
		b, err := io.ReadAll(io.LimitReader(f, MaxBytes+1))
		if len(b) > MaxBytes {
			return result, errors.New("snapshot grew beyond transfer bound")
		}
		return Message{Operation: "exported", Snapshot: snapshot, Archive: b}, err
	default:
		return result, errors.New("unsupported artifact operation")
	}
}
