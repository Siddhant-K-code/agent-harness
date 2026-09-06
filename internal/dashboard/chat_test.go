package dashboard

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Siddhant-K-code/agent-harness/internal/chat"
	"github.com/Siddhant-K-code/agent-harness/internal/task"
)

func TestChatRoutesAuthorizePresetAndDeduplicateBeforeDispatch(t *testing.T) {
	s := setup(t)
	t.Setenv("OPENAI_API_KEY", "test-private-key")
	c, e := s.chats.Create(s.options.Tasks[0], presetHash(s.options.Tasks[0]))
	if e != nil {
		t.Fatal(e)
	}
	path := "/api/chats/" + c.ID
	w := request(s, "GET", path, "", "", "", s.host)
	if w.Code != 401 {
		t.Fatal("unauthenticated chat history")
	}
	w = request(s, "POST", "/api/chats", `{"task":0,"repository":"/etc"}`, s.token, "", s.host)
	if w.Code != 400 {
		t.Fatal("client supplied repository accepted")
	}
	w = request(s, "POST", "/api/chats", `{}`, s.token, "", s.host)
	if w.Code != 400 {
		t.Fatal("missing preset accepted")
	}
	w = request(s, "POST", path+"/ask", `{"request_id":"bad","message":"hello","model":"gpt-5.4","context_window_tokens":8192,"max_output_tokens":1024,"max_usd":1}`, s.token, "", s.host)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "ceiling") {
		t.Fatal("chat budget bypass", w.Body.String())
	}
	a := askRequest{RequestID: strings.Repeat("a", 32), Message: "Explain the code", Model: "gpt-5.4", Context: 8192, Output: 1024, USD: .1}
	_, e = s.chats.Begin(c.ID, chat.Turn{ID: a.RequestID, RequestHash: requestHash(a), Kind: "ask", Question: a.Message})
	if e != nil {
		t.Fatal(e)
	}
	s.busy = true
	payload, _ := json.Marshal(a)
	w = request(s, "POST", path+"/ask", string(payload), s.token, "", s.host)
	if w.Code != 200 {
		t.Fatal("same request did not deduplicate", w.Body.String())
	}
	a.Message = "different question"
	payload, _ = json.Marshal(a)
	w = request(s, "POST", path+"/ask", string(payload), s.token, "", s.host)
	if w.Code != 400 {
		t.Fatal("mismatched request accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.chatCancel = cancel
	s.activeChat = c.ID
	w = request(s, "POST", path+"/cancel", `{}`, s.token, "", s.host)
	if w.Code != 200 || ctx.Err() == nil {
		t.Fatal("cancel did not reach worker")
	}
	s.options.Tasks = []task.Spec{}
	w = request(s, "GET", path, "", s.token, "", s.host)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"available":false`) {
		t.Fatal("history lost when preset removed")
	}
	w = request(s, "POST", path+"/ask", string(payload), s.token, "", s.host)
	if w.Code != 400 {
		t.Fatal("removed preset authorized new work")
	}
}

func TestChatRunCannotSwitchPreparedProjectAndDeduplicates(t *testing.T) {
	s := setup(t)
	t.Setenv("OPENAI_API_KEY", "test-private-key")
	c, _ := s.chats.Create(s.options.Tasks[0], presetHash(s.options.Tasks[0]))
	a := startRequest{Conversation: c.ID, RequestID: strings.Repeat("b", 32), Task: 0, Goal: "fix the code", Model: "gpt-5.4", Context: 8192, Output: 1024, USD: .1}
	_, e := s.chats.Begin(c.ID, chat.Turn{ID: a.RequestID, RequestHash: requestHash(a), Kind: "task", Question: a.Goal})
	if e != nil {
		t.Fatal(e)
	}
	s.busy = true
	payload, _ := json.Marshal(a)
	w := request(s, "POST", "/api/runs", string(payload), s.token, "", s.host)
	if w.Code != 200 {
		t.Fatal("duplicate task dispatched or rejected", w.Body.String())
	}
	other := s.options.Tasks[0]
	other.Name = "different project"
	s.options.Tasks = append(s.options.Tasks, other)
	a.Task = 1
	payload, _ = json.Marshal(a)
	w = request(s, "POST", "/api/runs", string(payload), s.token, "", s.host)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "different prepared task") {
		t.Fatal("conversation switched project", w.Body.String())
	}
}
