package model

import (
	"context"
	"encoding/json"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// HTTP stubs exist only in tests; production always uses the actual OpenAI API.
func TestResponsesProtocolPreservesReasoningAndCallIDs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["model"] != "gpt-5.4" || body["parallel_tool_calls"] != false {
			t.Errorf("wrong API parameters: %v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "input_tokens") {
			w.Write([]byte(`{"input_tokens":500,"object":"response.input_tokens"}`))
			return
		}
		if body["store"] != false {
			t.Error("server storage enabled")
		}
		w.Write([]byte(`{"id":"resp_test","status":"completed","output":[{"id":"rs_test","type":"reasoning","summary":[],"encrypted_content":"opaque-test-value"},{"id":"fc_test","type":"function_call","call_id":"call_test","name":"exec","arguments":"{\"command\":\"pwd\"}","status":"completed"}],"usage":{"input_tokens":500,"output_tokens":20,"total_tokens":520}}`))
	}))
	defer server.Close()
	c := Client{API: openai.NewClient(option.WithBaseURL(server.URL+"/"), option.WithAPIKey("test-key"), option.WithMaxRetries(0)), Model: "gpt-5.4"}
	input := []responses.ResponseInputItemUnionParam{responses.ResponseInputItemParamOfMessage("fix", "user")}
	count, err := c.Count(context.Background(), input)
	if err != nil || count != 500 {
		t.Fatalf("count: %d %v", count, err)
	}
	reply, err := c.Next(context.Background(), input, 256)
	if err != nil {
		t.Fatal(err)
	}
	if len(reply.Calls) != 1 || reply.Calls[0].ID != "call_test" {
		t.Fatalf("lost call: %+v", reply)
	}
	b, err := json.Marshal(reply.Items)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "opaque-test-value") {
		t.Fatal("lost reasoning continuation")
	}
	b, err = json.Marshal(ToolResult("call_test", map[string]int{"exit_code": 0}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"call_id":"call_test"`) {
		t.Fatalf("lost call ID: %s", b)
	}
}
func TestKeyFilePermissionsAndUnknownPricing(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	p := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(p, []byte("test-key\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if key, err := LoadKey(p); err != nil || key != "test-key" {
		t.Fatal("key loading failed")
	}
	if err := os.Chmod(p, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadKey(p); err == nil {
		t.Fatal("world-readable key accepted")
	}
	if _, err := Pricing("unknown"); err == nil {
		t.Fatal("unknown model pricing accepted")
	}
}
