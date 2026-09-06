package integrations

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPRealProtocolScopeSchemaRevocationAndRedaction(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HARNESS_TEST_MCP_TOKEN", "secret-canary-123")
	server := mcp.NewServer(&mcp.Implementation{Name: "file-reader-test", Version: "1"}, nil)
	file := filepath.Join(t.TempDir(), "document.txt")
	if err := os.WriteFile(file, []byte("document secret-canary-123"), 0600); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int64
	type args struct {
		Topic string `json:"topic"`
	}
	mcp.AddTool(server, &mcp.Tool{Name: "read_document", Description: "Read a prepared local document"}, func(ctx context.Context, _ *mcp.CallToolRequest, a args) (*mcp.CallToolResult, any, error) {
		calls.Add(1)
		b, err := os.ReadFile(file)
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}, nil, err
	})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{JSONResponse: true})
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret-canary-123" {
			t.Error("missing scoped token")
			http.Error(w, "auth", 401)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	defer httpServer.Close()
	c := Config{Schema: 1, MCP: map[string]Server{"docs": {URL: httpServer.URL, TokenEnv: "HARNESS_TEST_MCP_TOKEN", Tools: []string{"read_document"}}}}
	if err := Save(root, c); err != nil {
		t.Fatal(err)
	}
	selection := Selection{MCP: []MCPSelection{{Server: "docs", Tools: []string{"read_document"}}}}
	connectCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	b, err := Connect(connectCtx, root, selection)
	stop()
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	good := `{"server":"docs","tool":"read_document","arguments_json":"{\"topic\":\"coding\"}"}`
	result, err := b.Call(context.Background(), "mcp_call", good)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(result)
	if strings.Contains(string(raw), "secret-canary-123") || !strings.Contains(string(raw), "document [REDACTED]") {
		t.Fatalf("redaction: %s", raw)
	}
	for _, bad := range []string{strings.Replace(good, "read_document", "delete_everything", 1), strings.Replace(good, "docs", "other", 1), `{"server":"docs","tool":"read_document","arguments_json":"{}"}`} {
		if _, err := b.Call(context.Background(), "mcp_call", bad); err == nil {
			t.Fatal("invalid dispatch allowed")
		}
	}
	if calls.Load() != 1 {
		t.Fatal("unauthorized/invalid calls reached server")
	}
	c.MCP["docs"] = Server{URL: httpServer.URL, TokenEnv: "HARNESS_TEST_MCP_TOKEN", Tools: []string{"another"}}
	if err := Save(root, c); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Call(context.Background(), "mcp_call", good); err == nil {
		t.Fatal("revoked tool dispatched")
	}
	if calls.Load() != 1 {
		t.Fatal("revocation bypass")
	}
}

func TestMCPDisconnectHasNoRetry(t *testing.T) {
	root := t.TempDir()
	server := mcp.NewServer(&mcp.Implementation{Name: "slow-test", Version: "1"}, nil)
	var calls atomic.Int64
	mcp.AddTool(server, &mcp.Tool{Name: "slow"}, func(ctx context.Context, _ *mcp.CallToolRequest, a struct{}) (*mcp.CallToolResult, any, error) {
		calls.Add(1)
		<-ctx.Done()
		return nil, nil, ctx.Err()
	})
	h := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{JSONResponse: true}))
	defer h.Close()
	if err := Save(root, Config{Schema: 1, MCP: map[string]Server{"test": {URL: h.URL, Tools: []string{"slow"}}}}); err != nil {
		t.Fatal(err)
	}
	b, err := Connect(context.Background(), root, Selection{MCP: []MCPSelection{{Server: "test", Tools: []string{"slow"}}}})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	ctx, stop := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer stop()
	_, err = b.Call(ctx, "mcp_call", `{"server":"test","tool":"slow","arguments_json":"{}"}`)
	if !errors.Is(err, ErrUncertain) || calls.Load() != 1 {
		t.Fatalf("retry or dishonest outcome: %v calls=%d", err, calls.Load())
	}
}

func TestConfigurationAndRedirectBoundary(t *testing.T) {
	for _, url := range []string{"http://remote.example/mcp", "https://token@example.com/mcp", "https://example.com/mcp?token=x", "https://example.com/mcp#token", "file:///tmp/mcp"} {
		if err := (Config{Schema: 1, MCP: map[string]Server{"docs": {URL: url, Tools: []string{"read"}}}}).Validate(); err == nil {
			t.Fatal("invalid URL allowed:", url)
		}
	}
	var leaked atomic.Int64
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Add(1) }))
	defer destination.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	root := t.TempDir()
	if err := Save(root, Config{Schema: 1, MCP: map[string]Server{"docs": {URL: source.URL, Tools: []string{"read"}}}}); err != nil {
		t.Fatal(err)
	}
	_, err := Connect(context.Background(), root, Selection{MCP: []MCPSelection{{Server: "docs", Tools: []string{"read"}}}})
	if err == nil || leaked.Load() != 0 {
		t.Fatal("redirect accepted")
	}
	if err := os.Chmod(filepath.Join(root, "integrations.json"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root); err == nil {
		t.Fatal("shared writable policy accepted")
	}
}
