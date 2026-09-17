package ollama_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"git-ui/internal/ai"
	"git-ui/internal/ai/ollama"
)

func stream(w http.ResponseWriter, lines ...string) {
	for _, l := range lines {
		fmt.Fprintln(w, l)
		w.(http.Flusher).Flush()
	}
}

func collect(t *testing.T, ch <-chan ai.Chunk) []ai.Chunk {
	t.Helper()
	var out []ai.Chunk
	for c := range ch {
		out = append(out, c)
	}
	return out
}

func TestChatSendsRequestAndStreamsChunks(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" || r.Method != http.MethodPost {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		stream(w,
			`{"message":{"role":"assistant","content":"Hel"},"done":false}`,
			`{"message":{"role":"assistant","content":"lo"},"done":false}`,
			`{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","function":{"index":0,"name":"list_refs","arguments":{"limit":5}}}]},"done":false}`,
			`{"message":{"role":"assistant","content":""},"done":true,"done_reason":"stop"}`,
		)
	}))
	defer srv.Close()

	req := ai.Request{
		Model:  "qwen2.5:7b",
		System: "be brief",
		Messages: []ai.Message{
			{Role: ai.RoleUser, Content: "refs?"},
			{Role: ai.RoleAssistant, ToolCalls: []ai.ToolCall{{ID: "c0", Name: "list_refs", Args: map[string]any{}}}},
			{Role: ai.RoleTool, ToolName: "list_refs", Content: "main"},
		},
		Tools: []ai.ToolSpec{{Name: "list_refs", Description: "List refs", Parameters: map[string]any{"type": "object", "properties": map[string]any{}}}},
	}
	ch, err := ollama.New(srv.URL).Chat(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	chunks := collect(t, ch)

	if got["model"] != "qwen2.5:7b" || got["stream"] != true {
		t.Fatalf("request = %v", got)
	}
	msgs := got["messages"].([]any)
	if len(msgs) != 4 || msgs[0].(map[string]any)["role"] != "system" || msgs[0].(map[string]any)["content"] != "be brief" {
		t.Fatalf("messages = %v", msgs)
	}
	toolMsg := msgs[3].(map[string]any)
	if toolMsg["role"] != "tool" || toolMsg["tool_name"] != "list_refs" {
		t.Fatalf("tool message = %v", toolMsg)
	}
	call := msgs[2].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)
	if call["name"] != "list_refs" {
		t.Fatalf("assistant tool call = %v", call)
	}
	tool := got["tools"].([]any)[0].(map[string]any)
	if tool["type"] != "function" || tool["function"].(map[string]any)["name"] != "list_refs" {
		t.Fatalf("tools = %v", tool)
	}

	want := []ai.Chunk{
		{Delta: "Hel"},
		{Delta: "lo"},
		{ToolCalls: []ai.ToolCall{{ID: "call_1", Name: "list_refs", Args: map[string]any{"limit": float64(5)}}}},
		{Done: true},
	}
	if !reflect.DeepEqual(chunks, want) {
		t.Fatalf("chunks:\n got  %#v\n want %#v", chunks, want)
	}
}

func TestErrorsAreClassified(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   error
	}{
		{http.StatusBadRequest, `{"error":"registry.ollama.ai/library/gemma:2b does not support tools"}`, ollama.ErrNoToolSupport},
		{http.StatusNotFound, `{"error":"model 'nope:1b' not found"}`, ollama.ErrModelNotFound},
	}
	for _, c := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.status)
			fmt.Fprint(w, c.body)
		}))
		_, err := ollama.New(srv.URL).Chat(context.Background(), ai.Request{Model: "m"})
		srv.Close()
		if !errors.Is(err, c.want) {
			t.Errorf("%s: want %v, got %v", c.body, c.want, err)
		}
	}
}

func TestUnreachable(t *testing.T) {
	_, err := ollama.New("http://127.0.0.1:1").ListModels(context.Background())
	if !errors.Is(err, ollama.ErrUnreachable) {
		t.Fatalf("want ErrUnreachable, got %v", err)
	}
}

func TestListModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Errorf("path = %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"models":[{"name":"qwen2.5:7b","size":4683087332},{"name":"llama3.1:8b","size":4920753328}]}`)
	}))
	defer srv.Close()

	got, err := ollama.New(srv.URL + "/").ListModels(context.Background())
	want := []ollama.Model{{Name: "qwen2.5:7b", Size: 4683087332}, {Name: "llama3.1:8b", Size: 4920753328}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, err %v", got, err)
	}
}

func TestPullReportsProgress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["model"] != "qwen2.5:7b" {
			t.Errorf("body = %v", body)
		}
		stream(w,
			`{"status":"pulling manifest"}`,
			`{"status":"pulling 845dbda0ea48","total":100,"completed":50}`,
			`{"status":"success"}`,
		)
	}))
	defer srv.Close()

	var events []string
	err := ollama.New(srv.URL).Pull(context.Background(), "qwen2.5:7b", func(status string, completed, total int64) {
		events = append(events, fmt.Sprintf("%s %d/%d", status, completed, total))
	})
	want := []string{"pulling manifest 0/0", "pulling 845dbda0ea48 50/100", "success 0/0"}
	if err != nil || !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %v, err %v", events, err)
	}
}

func TestPullErrorLine(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stream(w, `{"status":"pulling manifest"}`, `{"error":"pull model manifest: file does not exist"}`)
	}))
	defer srv.Close()

	err := ollama.New(srv.URL).Pull(context.Background(), "nope", func(string, int64, int64) {})
	if err == nil || !strings.Contains(err.Error(), "file does not exist") {
		t.Fatalf("err = %v", err)
	}
}

func TestPullCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stream(w, `{"status":"pulling manifest"}`)
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	err := ollama.New(srv.URL).Pull(ctx, "m", func(string, int64, int64) { cancel() })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

func TestResponderUsesInstructionsAsSystem(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		stream(w, `{"message":{"role":"assistant","content":"- ok"},"done":false}`, `{"message":{"content":""},"done":true}`)
	}))
	defer srv.Close()

	ch, err := ollama.New(srv.URL).Responder("qwen2.5:3b").Respond(context.Background(), "explain", "commit text")
	if err != nil {
		t.Fatal(err)
	}
	chunks := collect(t, ch)
	msgs := got["messages"].([]any)
	if got["model"] != "qwen2.5:3b" || msgs[0].(map[string]any)["content"] != "explain" || msgs[1].(map[string]any)["content"] != "commit text" {
		t.Fatalf("request = %v", got)
	}
	if _, hasTools := got["tools"]; hasTools {
		t.Fatal("responder must not send tools")
	}
	if len(chunks) != 2 || chunks[0].Delta != "- ok" || !chunks[1].Done {
		t.Fatalf("chunks = %#v", chunks)
	}
}
