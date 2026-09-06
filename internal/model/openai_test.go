package model

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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

func TestConfiguredSnapshotAndOutputReachBothAPIEndpoints(t *testing.T) {
	spec := settingSpec(t)
	spec.Model = "gpt-5.4-mini-2026-03-17"
	spec.Limits.ContextWindowTokens = 128000
	spec.Limits.MaxOutputTokens = 4096
	settings, err := ResolveSettings(spec)
	if err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		if body["model"] != settings.Model {
			t.Error("configured snapshot was replaced")
		}
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "input_tokens") {
			fmt.Fprint(w, `{"input_tokens":100,"object":"response.input_tokens"}`)
		} else {
			if body["truncation"] != "disabled" {
				t.Error("provider truncation must remain disabled")
			}
			if body["max_output_tokens"] != float64(4096) {
				t.Error("output setting was not sent")
			}
			fmt.Fprint(w, `{"id":"resp_protocol","status":"completed","output":[],"usage":{"input_tokens":100,"output_tokens":10,"total_tokens":110}}`)
		}
	}))
	defer server.Close()
	client := New("protocol-test-key", settings.Model)
	client.API = openai.NewClient(option.WithBaseURL(server.URL+"/"), option.WithAPIKey("protocol-test-key"), option.WithMaxRetries(0))
	input := []responses.ResponseInputItemUnionParam{responses.ResponseInputItemParamOfMessage("test model settings", "user")}
	if _, err := client.Count(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Next(context.Background(), input, settings.MaxOutputTokens); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 {
		t.Fatal("unexpected provider requests", requests.Load())
	}
}
