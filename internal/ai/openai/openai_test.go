package openai_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"git-ui/internal/ai"
	"git-ui/internal/ai/openai"
)

func chunks(w http.ResponseWriter, lines ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, l := range lines {
		fmt.Fprintf(w, "data: %s\n\n", l)
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
}

func collect(t *testing.T, ch <-chan ai.Chunk) (text string, calls []ai.ToolCall, done bool, err error) {
	t.Helper()
	for c := range ch {
		text += c.Delta
		calls = append(calls, c.ToolCalls...)
		if c.Done {
			done = true
		}
		if c.Err != nil {
			err = c.Err
		}
	}
	return text, calls, done, err
}

func TestChatStreamsText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test" {
			t.Errorf("Authorization = %q", got)
		}
		chunks(w,
			`{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"Hola"}}]}`,
			`{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":" mundo"}}]}`,
			`{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		)
	}))
	defer srv.Close()

	ch, err := openai.New("sk-test", openai.WithBaseURL(srv.URL)).Chat(context.Background(), ai.Request{
		Model:    "gpt-4.1",
		System:   "be brief",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "hola"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	text, calls, done, err := collect(t, ch)
	if err != nil || text != "Hola mundo" || len(calls) != 0 || !done {
		t.Errorf("text = %q, calls = %v, done = %v, err = %v", text, calls, done, err)
	}
}

// Arguments arrive split across chunks and are only valid once joined.
func TestChatStreamsAToolCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunks(w,
			`{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"list_conflicts","arguments":"{\"path\""}}]}}]}`,
			`{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":":\"a.go\"}"}}]}}]}`,
			`{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		)
	}))
	defer srv.Close()

	ch, err := openai.New("sk-test", openai.WithBaseURL(srv.URL)).Chat(context.Background(), ai.Request{
		Model:    "gpt-4.1",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "resolve"}},
		Tools:    []ai.ToolSpec{{Name: "list_conflicts", Description: "list", Parameters: map[string]any{"type": "object"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, calls, done, err := collect(t, ch)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].Name != "list_conflicts" || calls[0].ID != "call_1" || calls[0].Args["path"] != "a.go" {
		t.Fatalf("calls = %#v", calls)
	}
	if !done {
		t.Error("done = false")
	}
}

// The stream ends (network drop, server bug) without a finish_reason ever
// arriving. The tool call the model was in the middle of asking for must
// still reach the caller, not be silently dropped.
func TestChatEmitsToolCallWhenStreamEndsWithoutFinishReason(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunks(w,
			`{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"list_conflicts","arguments":"{\"path\":\"a.go\"}"}}]}}]}`,
		)
	}))
	defer srv.Close()

	ch, err := openai.New("sk-test", openai.WithBaseURL(srv.URL)).Chat(context.Background(), ai.Request{
		Model:    "gpt-4.1",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "resolve"}},
		Tools:    []ai.ToolSpec{{Name: "list_conflicts", Description: "list", Parameters: map[string]any{"type": "object"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, calls, done, err := collect(t, ch)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].Name != "list_conflicts" || calls[0].ID != "call_1" || calls[0].Args["path"] != "a.go" {
		t.Fatalf("calls = %#v, want the unfinished call emitted", calls)
	}
	if !done {
		t.Error("done = false")
	}
}

func TestChatReportsRateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"error":{"message":"slow down","type":"rate_limit_error"}}`)
	}))
	defer srv.Close()

	ch, err := openai.New("sk-test", openai.WithBaseURL(srv.URL)).Chat(context.Background(), ai.Request{
		Model:    "gpt-4.1",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "hola"}},
	})
	if err == nil {
		_, _, _, err = collect(t, ch)
	}
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "rate") {
		t.Fatalf("err = %v, want a rate limit error", err)
	}
}

// The raw list carries embeddings and image models; only chat models belong
// in the dropdown.
func TestListModelsKeepsOnlyChatModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"object":"list","data":[{"id":"gpt-4.1","object":"model"},{"id":"text-embedding-3-small","object":"model"},{"id":"dall-e-3","object":"model"},{"id":"whisper-1","object":"model"},{"id":"o4-mini","object":"model"}]}`)
	}))
	defer srv.Close()

	got, err := openai.New("sk-test", openai.WithBaseURL(srv.URL)).ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"gpt-4.1": true, "o4-mini": true}
	if len(got) != len(want) {
		t.Fatalf("models = %v, want only the chat ones", got)
	}
	for _, m := range got {
		if !want[m] {
			t.Errorf("models = %v, want only the chat ones", got)
		}
	}
}

// Two tool calls in one assistant turn must have their results paired to the
// right call ids POSITIONALLY, not by looking up the tool name in history
// (which would pair both results to whichever call the name-search finds
// first). This mirrors the Anthropic provider's messages()/pendingCalls
// approach.
func TestChatPairsToolResultsByIDPositionally(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatal(err)
		}
		chunks(w, `{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`)
	}))
	defer srv.Close()

	history := []ai.Message{
		{Role: ai.RoleUser, Content: "resolve both"},
		{
			Role: ai.RoleAssistant,
			ToolCalls: []ai.ToolCall{
				{ID: "call_1", Name: "list_conflicts", Args: map[string]any{}},
				{ID: "call_2", Name: "list_conflicts", Args: map[string]any{}},
			},
		},
		{Role: ai.RoleTool, ToolName: "list_conflicts", Content: "result for call_1"},
		{Role: ai.RoleTool, ToolName: "list_conflicts", Content: "result for call_2"},
	}

	ch, err := openai.New("sk-test", openai.WithBaseURL(srv.URL)).Chat(context.Background(), ai.Request{
		Model:    "gpt-4.1",
		Messages: history,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := collect(t, ch); err != nil {
		t.Fatal(err)
	}

	msgs, ok := body["messages"].([]any)
	if !ok {
		t.Fatalf("body[messages] = %#v", body["messages"])
	}
	var toolMsgs []map[string]any
	for _, m := range msgs {
		mm := m.(map[string]any)
		if mm["role"] == "tool" {
			toolMsgs = append(toolMsgs, mm)
		}
	}
	if len(toolMsgs) != 2 {
		t.Fatalf("tool messages = %#v, want 2", toolMsgs)
	}
	if toolMsgs[0]["tool_call_id"] != "call_1" || toolMsgs[0]["content"] != "result for call_1" {
		t.Errorf("first tool message = %#v", toolMsgs[0])
	}
	if toolMsgs[1]["tool_call_id"] != "call_2" || toolMsgs[1]["content"] != "result for call_2" {
		t.Errorf("second tool message = %#v", toolMsgs[1])
	}
}

// An assistant message with no content and no tool calls is what
// agent.Execute saves when a run is stopped before the first delta arrives.
// It must never reach the API: the SDK serializes it as a bare
// {"role":"assistant"}, and this guards that Anthropic's `len(blocks) > 0`
// treatment has an OpenAI equivalent.
func TestChatOmitsEmptyAssistantMessage(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatal(err)
		}
		chunks(w, `{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`)
	}))
	defer srv.Close()

	history := []ai.Message{
		{Role: ai.RoleUser, Content: "resolve"},
		{Role: ai.RoleAssistant, Stopped: true},
	}
	ch, err := openai.New("sk-test", openai.WithBaseURL(srv.URL)).Chat(context.Background(), ai.Request{
		Model:    "gpt-4.1",
		Messages: history,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := collect(t, ch); err != nil {
		t.Fatal(err)
	}

	msgs, ok := body["messages"].([]any)
	if !ok {
		t.Fatalf("body[messages] = %#v", body["messages"])
	}
	for _, m := range msgs {
		mm := m.(map[string]any)
		if mm["role"] == "assistant" {
			t.Fatalf("empty assistant message reached the request: %#v", mm)
		}
	}
}

// TestChatReportsNoCredit guards against "try again in a moment" being shown
// for insufficient_quota, which never resolves by itself no matter how many
// times the user retries.
func TestChatReportsNoCredit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"error":{"message":"You exceeded your current quota","type":"insufficient_quota","code":"insufficient_quota"}}`)
	}))
	defer srv.Close()

	ch, err := openai.New("sk-test", openai.WithBaseURL(srv.URL)).Chat(context.Background(), ai.Request{
		Model:    "gpt-4.1",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "hola"}},
	})
	if err == nil {
		_, _, _, err = collect(t, ch)
	}
	if err == nil || err.Error() != "openai reports no available credit." {
		t.Fatalf("err = %v, want the no-credit message", err)
	}
}

// Two tool calls streamed with their indices interleaved across chunks (the
// order a real completion arrives in when the model asks for both at once)
// must each keep their own id, name and full accumulated arguments.
func TestChatAccumulatesTwoInboundToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunks(w,
			`{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{\"path\""}}]}}]}`,
			`{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_2","type":"function","function":{"name":"read_file","arguments":"{\"path\""}}]}}]}`,
			`{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":":\"a.go\"}"}}]}}]}`,
			`{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"function":{"arguments":":\"b.go\"}"}}]}}]}`,
			`{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		)
	}))
	defer srv.Close()

	ch, err := openai.New("sk-test", openai.WithBaseURL(srv.URL)).Chat(context.Background(), ai.Request{
		Model:    "gpt-4.1",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "check both files"}},
		Tools:    []ai.ToolSpec{{Name: "read_file", Description: "read", Parameters: map[string]any{"type": "object"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, calls, done, err := collect(t, ch)
	if err != nil {
		t.Fatal(err)
	}
	if !done {
		t.Fatal("done = false")
	}
	if len(calls) != 2 {
		t.Fatalf("calls = %#v, want 2", calls)
	}
	if calls[0].ID != "call_1" || calls[0].Name != "read_file" || calls[0].Args["path"] != "a.go" {
		t.Errorf("first call = %#v", calls[0])
	}
	if calls[1].ID != "call_2" || calls[1].Name != "read_file" || calls[1].Args["path"] != "b.go" {
		t.Errorf("second call = %#v", calls[1])
	}
}

// The usage chunk arrives after finish_reason, with no choices.
func TestChatReportsUsageAfterTheFinishReason(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		chunks(w,
			`{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"list_refs","arguments":"{}"}}]}}]}`,
			`{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
			`{"id":"1","object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":900,"completion_tokens":40,"total_tokens":940,"prompt_tokens_details":{"cached_tokens":512}}}`,
		)
	}))
	defer srv.Close()
	ch, err := openai.New("sk-test", openai.WithBaseURL(srv.URL)).Chat(context.Background(), ai.Request{
		Model: "gpt-4.1", Messages: []ai.Message{{Role: ai.RoleUser, Content: "hi"}},
		Tools: []ai.ToolSpec{{Name: "list_refs", Parameters: map[string]any{"type": "object"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var calls []ai.ToolCall
	var usage *ai.Usage
	done := false
	for c := range ch {
		calls = append(calls, c.ToolCalls...)
		if c.Done {
			done, usage = true, c.Usage
		}
		if c.Err != nil {
			t.Fatal(c.Err)
		}
	}
	if !done || len(calls) != 1 || calls[0].ID != "call_1" {
		t.Fatalf("done %v calls %#v", done, calls)
	}
	if usage == nil || *usage != (ai.Usage{Input: 900, Output: 40, CacheRead: 512}) {
		t.Fatalf("usage = %#v", usage)
	}
	opts, _ := body["stream_options"].(map[string]any)
	if opts["include_usage"] != true {
		t.Fatalf("stream_options = %#v", body["stream_options"])
	}
}

// An OpenAI-compatible server may ignore include_usage.
func TestChatWithoutAUsageChunkReportsNoUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunks(w,
			`{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"hi"}}]}`,
			`{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		)
	}))
	defer srv.Close()
	ch, err := openai.New("sk-test", openai.WithBaseURL(srv.URL)).Chat(context.Background(), ai.Request{Model: "gpt-4.1", Messages: []ai.Message{{Role: ai.RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	done := false
	for c := range ch {
		if c.Done {
			done = true
			if c.Usage != nil {
				t.Fatalf("usage = %#v, want nil", c.Usage)
			}
		}
	}
	if !done {
		t.Fatal("no Done chunk")
	}
}

// The connection drops after finish_reason, before the usage chunk: the
// answer is complete, so it still ends Done (without usage), not in error.
func TestChatErrorAfterTheFinishReasonStillEndsDone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"id\":\"1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"id\":\"1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprint(w, "data: {not json\n\n")
	}))
	defer srv.Close()
	ch, err := openai.New("sk-test", openai.WithBaseURL(srv.URL)).Chat(context.Background(), ai.Request{Model: "gpt-4.1", Messages: []ai.Message{{Role: ai.RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	text, _, done, err := collect(t, ch)
	if err != nil || !done || text != "hi" {
		t.Fatalf("text %q done %v err %v", text, done, err)
	}
}
