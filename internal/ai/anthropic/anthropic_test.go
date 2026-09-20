package anthropic_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"git-ui/internal/ai"
	"git-ui/internal/ai/anthropic"
)

// sse writes Server-Sent Events the way the Messages API streams them.
func sse(w http.ResponseWriter, events ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, e := range events {
		name := e[:strings.Index(e, " ")]
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, e[strings.Index(e, " ")+1:])
	}
}

// textStream is a reply of two text deltas and a clean stop.
func textStream(w http.ResponseWriter) {
	sse(w,
		`message_start {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5","content":[],"stop_reason":null,"usage":{"input_tokens":1,"output_tokens":1}}}`,
		`content_block_start {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`content_block_delta {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hola"}}`,
		`content_block_delta {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" mundo"}}`,
		`content_block_stop {"type":"content_block_stop","index":0}`,
		`message_delta {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
		`message_stop {"type":"message_stop"}`,
	)
}

// toolStream is a reply whose only content block is a tool call, streamed as
// partial JSON the way the API sends it.
func toolStream(w http.ResponseWriter) {
	sse(w,
		`message_start {"type":"message_start","message":{"id":"msg_2","type":"message","role":"assistant","model":"claude-opus-5","content":[],"stop_reason":null,"usage":{"input_tokens":1,"output_tokens":1}}}`,
		`content_block_start {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"list_conflicts","input":{}}}`,
		`content_block_delta {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"path\""}}`,
		`content_block_delta {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":":\"a.go\"}"}}`,
		`content_block_stop {"type":"content_block_stop","index":0}`,
		`message_delta {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":2}}`,
		`message_stop {"type":"message_stop"}`,
	)
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
		if r.Header.Get("x-api-key") != "sk-test" {
			t.Errorf("x-api-key = %q", r.Header.Get("x-api-key"))
		}
		textStream(w)
	}))
	defer srv.Close()

	c := anthropic.New("sk-test", anthropic.WithBaseURL(srv.URL))
	ch, err := c.Chat(context.Background(), ai.Request{
		Model:    "claude-opus-5",
		System:   "be brief",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "hola"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	text, calls, done, err := collect(t, ch)
	if err != nil {
		t.Fatal(err)
	}
	if text != "Hola mundo" || len(calls) != 0 || !done {
		t.Errorf("text = %q, calls = %v, done = %v", text, calls, done)
	}
}

func TestChatStreamsAToolCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { toolStream(w) }))
	defer srv.Close()

	c := anthropic.New("sk-test", anthropic.WithBaseURL(srv.URL))
	ch, err := c.Chat(context.Background(), ai.Request{
		Model:    "claude-opus-5",
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
	if len(calls) != 1 || calls[0].Name != "list_conflicts" || calls[0].ID != "toolu_1" {
		t.Fatalf("calls = %#v", calls)
	}
	if calls[0].Args["path"] != "a.go" {
		t.Errorf("args = %#v, want the accumulated partial JSON", calls[0].Args)
	}
	if !done {
		t.Error("done = false")
	}
}

func TestChatReportsRateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`)
	}))
	defer srv.Close()

	c := anthropic.New("sk-test", anthropic.WithBaseURL(srv.URL))
	ch, err := c.Chat(context.Background(), ai.Request{Model: "claude-opus-5", Messages: []ai.Message{{Role: ai.RoleUser, Content: "hola"}}})
	if err == nil {
		_, _, _, err = collect(t, ch)
	}
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "rate") {
		t.Fatalf("err = %v, want a rate limit error", err)
	}
}

func TestListModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"id":"claude-opus-5","display_name":"Claude Opus 5","type":"model"},{"id":"claude-sonnet-5","display_name":"Claude Sonnet 5","type":"model"}],"has_more":false}`)
	}))
	defer srv.Close()

	got, err := anthropic.New("sk-test", anthropic.WithBaseURL(srv.URL)).ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "claude-opus-5" {
		t.Errorf("models = %v", got)
	}
}
