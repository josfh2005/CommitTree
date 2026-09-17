package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"git-ui/internal/ai"
	"git-ui/internal/ai/agent"
	"git-ui/internal/ai/apple"
	"git-ui/internal/ai/chatstore"
	"git-ui/internal/ai/prompts"
	"git-ui/internal/ai/settings"
	"git-ui/internal/gitlog"
)

type event struct {
	name string
	data any
}

type events struct {
	mu   sync.Mutex
	list []event
	ch   chan event
}

func newEvents() *events { return &events{ch: make(chan event, 1000)} }

func (e *events) emit(name string, data any) {
	e.mu.Lock()
	e.list = append(e.list, event{name, data})
	e.mu.Unlock()
	e.ch <- event{name, data}
}

// wait returns the first event with the given name, failing after 5 s.
func (e *events) wait(t *testing.T, name string) event {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case ev := <-e.ch:
			if ev.name == name {
				return ev
			}
		case <-timeout:
			t.Fatalf("timed out waiting for %s; got %v", name, e.names())
		}
	}
}

func (e *events) names() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []string
	for _, ev := range e.list {
		out = append(out, ev.name)
	}
	return out
}

func newAIApp(t *testing.T, ollamaURL string) (*App, string, *events) {
	t.Helper()
	a, id := newTestApp(t)
	dir := t.TempDir()
	ev := newEvents()
	a.EnableAI(AIDeps{
		SettingsPath: filepath.Join(dir, "ai.json"),
		Chats:        chatstore.New(filepath.Join(dir, "chats")),
		Prompts:      prompts.New(filepath.Join(dir, "prompts")),
		Apple:        apple.New(""),
		Emit:         ev.emit,
	})
	s, err := a.GetAISettings()
	if err != nil {
		t.Fatal(err)
	}
	s.OllamaURL = ollamaURL
	if err := a.SaveAISettings(s); err != nil {
		t.Fatal(err)
	}
	return a, id, ev
}

func writeLines(w http.ResponseWriter, lines ...string) {
	for _, l := range lines {
		fmt.Fprintln(w, l)
		w.(http.Flusher).Flush()
	}
}

// fakeOllama answers /api/tags, /api/pull and /api/chat. The chat handler asks
// for list_refs first and answers with text once a tool result is present.
func fakeOllama(t *testing.T, onChat func(req map[string]any)) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			fmt.Fprint(w, `{"models":[{"name":"qwen2.5:7b","size":4683087332}]}`)
		case "/api/pull":
			writeLines(w, `{"status":"pulling manifest"}`, `{"status":"downloading","total":10,"completed":5}`, `{"status":"success"}`)
		case "/api/chat":
			var req map[string]any
			_ = json.NewDecoder(r.Body).Decode(&req)
			if onChat != nil {
				onChat(req)
			}
			msgs := req["messages"].([]any)
			last := msgs[len(msgs)-1].(map[string]any)
			if _, hasTools := req["tools"]; hasTools && last["role"] != "tool" {
				writeLines(w, `{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","function":{"name":"list_refs","arguments":{}}}]},"done":false}`, `{"message":{"content":""},"done":true}`)
				return
			}
			writeLines(w, `{"message":{"role":"assistant","content":"Hay "},"done":false}`, `{"message":{"content":"ramas."},"done":false}`, `{"message":{"content":""},"done":true}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestAIDisabledByDefault(t *testing.T) {
	a, id := newTestApp(t)
	if err := a.SendChat(id, "hi", "run"); !errors.Is(err, ErrAIDisabled) {
		t.Fatalf("err = %v", err)
	}
}

func TestSendChatRunsToolsStreamsAndSaves(t *testing.T) {
	var system string
	srv := fakeOllama(t, func(req map[string]any) {
		first := req["messages"].([]any)[0].(map[string]any)
		if first["role"] == "system" {
			system = first["content"].(string)
		}
	})
	a, id, ev := newAIApp(t, srv.URL)

	if err := a.SendChat(id, "¿qué ramas hay?", "run-1"); err != nil {
		t.Fatal(err)
	}
	done := ev.wait(t, agent.EventDone)
	if done.data != (agent.DoneEvent{RepoID: id, RunID: "run-1"}) {
		t.Fatalf("done = %#v", done.data)
	}
	names := strings.Join(ev.names(), ",")
	if names != "chat:tool,chat:tool_result,chat:delta,chat:delta,chat:done" {
		t.Fatalf("events = %s", names)
	}
	if !strings.Contains(system, "read-only") || !strings.Contains(system, "main") {
		t.Fatalf("system prompt = %q", system)
	}

	history, err := a.GetChat(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 4 || history[0].Content != "¿qué ramas hay?" || history[2].Role != ai.RoleTool ||
		!strings.Contains(history[2].Content, "Current branch: main") || history[3].Content != "Hay ramas." {
		t.Fatalf("history = %#v", history)
	}

	if err := a.ClearChat(id); err != nil {
		t.Fatal(err)
	}
	if h, _ := a.GetChat(id); len(h) != 0 {
		t.Fatalf("after clear = %#v", h)
	}
}

func TestSendChatBusyAndStop(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeLines(w, `{"message":{"role":"assistant","content":"Pensando"},"done":false}`)
		<-r.Context().Done()
	}))
	defer srv.Close()
	a, id, ev := newAIApp(t, srv.URL)

	if err := a.SendChat(id, "hola", "run-1"); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, agent.EventDelta)
	if err := a.SendChat(id, "otra", "run-2"); !errors.Is(err, ErrChatBusy) {
		t.Fatalf("second send: %v", err)
	}
	if err := a.ClearChat(id); !errors.Is(err, ErrChatBusy) {
		t.Fatalf("clear while running: %v", err)
	}
	if err := a.StopChat(id); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, agent.EventDone)

	history, _ := a.GetChat(id)
	last := history[len(history)-1]
	if last.Content != "Pensando" || !last.Stopped {
		t.Fatalf("last = %#v", last)
	}
	if err := a.SendChat(id, "de nuevo", "run-3"); err != nil {
		t.Fatalf("send after stop: %v", err)
	}
	ev.wait(t, agent.EventDelta)
	if err := a.StopChat(id); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, agent.EventDone)
}

func TestSendChatReportsOllamaDown(t *testing.T) {
	a, id, ev := newAIApp(t, "http://127.0.0.1:1")
	if err := a.SendChat(id, "hola", "run-1"); err != nil {
		t.Fatal(err)
	}
	got := ev.wait(t, agent.EventError).data.(agent.ErrorEvent)
	if got.Code != "ollama_down" || got.RunID != "run-1" {
		t.Fatalf("error event = %#v", got)
	}
}

func TestExplainCommitWithOllama(t *testing.T) {
	var system, prompt string
	srv := fakeOllama(t, func(req map[string]any) {
		msgs := req["messages"].([]any)
		system = msgs[0].(map[string]any)["content"].(string)
		prompt = msgs[1].(map[string]any)["content"].(string)
	})
	a, id, ev := newAIApp(t, srv.URL)
	head, err := a.GetLog(id, gitlog.Filters{}, 0, 1)
	if err != nil {
		t.Fatal(err)
	}

	if err := a.ExplainCommit(id, head.Rows[0].Hash, "ollama", "exp-1"); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, "explain:done")
	if !strings.Contains(strings.Join(ev.names(), ","), "explain:delta") {
		t.Fatalf("events = %v", ev.names())
	}
	if !strings.Contains(system, "3 to 6") || !strings.Contains(prompt, "Subject: Merge feature") {
		t.Fatalf("system %q\nprompt %q", system, prompt)
	}
}

func TestExplainCommitWithMissingAppleHelper(t *testing.T) {
	srv := fakeOllama(t, nil)
	a, id, ev := newAIApp(t, srv.URL)
	head, _ := a.GetLog(id, gitlog.Filters{}, 0, 1)

	if err := a.ExplainCommit(id, head.Rows[0].Hash, "apple", "exp-2"); err != nil {
		t.Fatal(err)
	}
	got := ev.wait(t, "explain:error").data.(ExplainError)
	if got.RunID != "exp-2" || !strings.Contains(got.Message, "helper not found") {
		t.Fatalf("error = %#v", got)
	}
}

func TestAIStatusAndPull(t *testing.T) {
	srv := fakeOllama(t, nil)
	a, _, ev := newAIApp(t, srv.URL)

	st := a.AIStatus()
	if !st.Ollama.Running || !st.Ollama.ChatModelInstalled || len(st.Ollama.Models) != 1 || st.Apple.Reason != "helperNotFound" {
		t.Fatalf("status = %+v", st)
	}

	if err := a.PullModel("qwen2.5:7b"); err != nil {
		t.Fatal(err)
	}
	done := ev.wait(t, "model:done").data.(ModelDone)
	if done.Name != "qwen2.5:7b" || done.Error != "" {
		t.Fatalf("done = %#v", done)
	}
	if !strings.Contains(strings.Join(ev.names(), ","), "model:progress") {
		t.Fatalf("no progress events: %v", ev.names())
	}
}

func TestSettingsValidationAndPrompts(t *testing.T) {
	a, _, _ := newAIApp(t, "http://localhost:11434")
	if err := a.SaveAISettings(settings.Settings{OllamaURL: "nope"}); !errors.Is(err, settings.ErrInvalid) {
		t.Fatalf("err = %v", err)
	}
	list, err := a.ListPrompts()
	if err != nil || len(list) != 2 || list[0].Customized {
		t.Fatalf("prompts = %+v, err %v", list, err)
	}
	if err := a.ResetPrompt("explain-commit"); err != nil {
		t.Fatal(err)
	}
	if err := a.ResetPrompt("nope"); !errors.Is(err, prompts.ErrUnknownPrompt) {
		t.Fatalf("err = %v", err)
	}
}

func TestSendChatValidatesInput(t *testing.T) {
	a, id, _ := newAIApp(t, "http://localhost:11434")
	if err := a.SendChat(id, "   ", "run"); err == nil {
		t.Fatal("empty message accepted")
	}
	if err := a.SendChat(id, "hola", ""); err == nil {
		t.Fatal("empty run id accepted")
	}
	if err := a.SendChat("unknown", "hola", "run"); err == nil {
		t.Fatal("unknown repo accepted")
	}
}
