package app

import (
	"context"
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
	"git-ui/internal/ai/chatstore"
	"git-ui/internal/ai/keys"
	"git-ui/internal/ai/prompts"
	"git-ui/internal/ai/settings"
	"git-ui/internal/gitlog"
)

// fakeKeys is an in-memory keys.Store standing in for the OS secret store.
// It repeats internal/ai/keys/keys_test.go's fake because it can't be
// imported across packages.
type fakeKeys struct {
	items map[string]string
	err   error
}

func (f *fakeKeys) Get(service, user string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	v, ok := f.items[service+"/"+user]
	if !ok {
		return "", keys.ErrNotFound
	}
	return v, nil
}

func (f *fakeKeys) Set(service, user, password string) error {
	if f.err != nil {
		return f.err
	}
	f.items[service+"/"+user] = password
	return nil
}

func (f *fakeKeys) Delete(service, user string) error {
	if f.err != nil {
		return f.err
	}
	delete(f.items, service+"/"+user)
	return nil
}

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
	WithAI(a, AIDeps{
		SettingsPath: filepath.Join(dir, "ai.json"),
		Chats:        chatstore.New(filepath.Join(dir, "chats")),
		Prompts:      prompts.New(filepath.Join(dir, "prompts")),
		Emit:         ev.emit,
	})
	s, err := a.GetAISettings()
	if err != nil {
		t.Fatal(err)
	}
	s.OllamaURL = ollamaURL
	// Suggested replies would schedule background calls after every answer.
	s.SuggestReplies = settings.SuggestOff
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
			writeLines(w, `{"message":{"role":"assistant","content":"Hay "},"done":false}`, `{"message":{"content":"ramas."},"done":false}`, `{"message":{"content":""},"done":true,"prompt_eval_count":300,"eval_count":12}`)
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
	done := ev.wait(t, agent.EventDone).data.(agent.DoneEvent)
	if done.RepoID != id || done.RunID != "run-1" {
		t.Fatalf("done = %#v", done)
	}
	if _, err := time.Parse(time.RFC3339, done.At); err != nil {
		t.Fatalf("done.At = %q: %v", done.At, err)
	}
	names := strings.Join(ev.names(), ",")
	if names != "chat:start,chat:tool,chat:tool_result,chat:delta,chat:delta,chat:usage,chat:done" {
		t.Fatalf("events = %s", names)
	}
	if !strings.Contains(system, "approves or rejects") || !strings.Contains(system, "main") {
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
	// Only the call that reported counts carries a usage.
	if history[1].Usage != nil || history[3].Usage == nil || *history[3].Usage != (ai.Usage{Input: 300, Output: 12}) {
		t.Fatalf("usages = %#v / %#v", history[1].Usage, history[3].Usage)
	}
	// Every assistant message of the answer records what produced it; the
	// question and the tool result don't.
	for i, m := range history {
		want := m.Role == ai.RoleAssistant
		if got := m.Provider == "ollama" && m.Model == "qwen2.5:7b" && m.At == done.At; got != want {
			t.Fatalf("history[%d] provider/model = %q/%q", i, m.Provider, m.Model)
		}
		if !want && (m.Provider != "" || m.Model != "" || m.At != "") {
			t.Fatalf("history[%d] provider/model = %q/%q, want none", i, m.Provider, m.Model)
		}
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

func TestExplainInChatWritesTheAnswerToTheConversation(t *testing.T) {
	var system, prompt string
	srv := fakeOllama(t, func(req map[string]any) {
		msgs := req["messages"].([]any)
		system = msgs[0].(map[string]any)["content"].(string)
		prompt = msgs[1].(map[string]any)["content"].(string)
	})
	a, id, ev := newAIApp(t, srv.URL)
	head, err := a.GetLog(id, gitlog.Filters{}, gitlog.OrderTopo, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	hash := head.Rows[0].Hash

	if err := a.ExplainInChat(id, hash, "ollama", "exp-1"); err != nil {
		t.Fatal(err)
	}
	start := ev.wait(t, agent.EventStart).data.(agent.StartEvent)
	if start.RepoID != id || start.RunID != "exp-1" || !strings.Contains(start.Text, hash[:7]) ||
		start.Provider != "ollama" || start.Model != "qwen2.5:7b" {
		t.Fatalf("start = %#v", start)
	}
	usage := ev.wait(t, agent.EventUsage).data.(agent.UsageEvent)
	if usage.RunID != "exp-1" || usage.Usage != (ai.Usage{Input: 300, Output: 12}) {
		t.Fatalf("usage event = %#v", usage)
	}
	done := ev.wait(t, agent.EventDone).data.(agent.DoneEvent)
	if _, err := time.Parse(time.RFC3339, done.At); err != nil {
		t.Fatalf("done.At = %q: %v", done.At, err)
	}

	names := strings.Join(ev.names(), ",")
	if !strings.Contains(names, agent.EventDelta) || strings.Contains(names, "explain:") {
		t.Fatalf("events = %s", names)
	}
	if !strings.Contains(system, "3 to 6") || !strings.Contains(prompt, "Subject: Merge feature") {
		t.Fatalf("system %q\nprompt %q", system, prompt)
	}

	history, err := a.GetChat(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[0].Role != ai.RoleUser || !strings.Contains(history[0].Content, hash[:7]) {
		t.Fatalf("history = %#v", history)
	}
	if history[1].Role != ai.RoleAssistant || history[1].Content != "Hay ramas." ||
		history[1].Provider != "ollama" || history[1].Model != "qwen2.5:7b" || history[1].At != done.At || history[0].Provider != "" ||
		history[1].Usage == nil || *history[1].Usage != (ai.Usage{Input: 300, Output: 12}) {
		t.Fatalf("answer = %#v", history[1])
	}
}

func TestExplainInChatWithUnknownProvider(t *testing.T) {
	srv := fakeOllama(t, nil)
	a, id, ev := newAIApp(t, srv.URL)
	head, _ := a.GetLog(id, gitlog.Filters{}, gitlog.OrderTopo, 0, 1)

	if err := a.ExplainInChat(id, head.Rows[0].Hash, "acme", "exp-2"); err == nil {
		t.Fatal("want an error for an unknown provider")
	}
	if err := a.ExplainInChat(id, head.Rows[0].Hash, "ollama", "exp-3"); err != nil {
		t.Fatalf("repo still busy after a failed explain: %v", err)
	}
	ev.wait(t, agent.EventDone)
}

func TestExplainInChatIsBusyWhileChatting(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeLines(w, `{"message":{"role":"assistant","content":"Pensando"},"done":false}`)
		<-r.Context().Done()
	}))
	defer srv.Close()
	a, id, ev := newAIApp(t, srv.URL)
	head, _ := a.GetLog(id, gitlog.Filters{}, gitlog.OrderTopo, 0, 1)

	if err := a.SendChat(id, "hola", "run-1"); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, agent.EventDelta)

	if err := a.ExplainInChat(id, head.Rows[0].Hash, "ollama", "exp-1"); !errors.Is(err, ErrChatBusy) {
		t.Fatalf("explain while chatting: %v", err)
	}
	if err := a.StopChat(id); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, agent.EventDone)
}

func TestExplainLinesInChatWritesTheAnswerToTheConversation(t *testing.T) {
	var system, prompt string
	srv := fakeOllama(t, func(req map[string]any) {
		msgs := req["messages"].([]any)
		system = msgs[0].(map[string]any)["content"].(string)
		prompt = msgs[1].(map[string]any)["content"].(string)
	})
	a, id, ev := newAIApp(t, srv.URL)
	head, err := a.GetLog(id, gitlog.Filters{}, gitlog.OrderTopo, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	hash := head.Rows[0].Hash

	if err := a.ExplainLinesInChat(id, hash, "file-1.txt", 1, 1, "ollama", "lines-1"); err != nil {
		t.Fatal(err)
	}
	start := ev.wait(t, agent.EventStart).data.(agent.StartEvent)
	if start.Text != "Explain line 1 of file-1.txt (at "+hash[:7]+")" {
		t.Fatalf("start = %#v", start)
	}
	ev.wait(t, agent.EventDone)
	if !strings.Contains(system, "range of lines") || !strings.Contains(prompt, "Subject: base") {
		t.Fatalf("system %q\nprompt %q", system, prompt)
	}
	history, err := a.GetChat(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[0].Content != start.Text || history[1].Role != ai.RoleAssistant {
		t.Fatalf("history = %#v", history)
	}
}

func TestExplainLinesQuestion(t *testing.T) {
	cases := map[string]string{
		explainLinesQuestion("abcdef1234", "a.go", 40, 58): "Explain lines 40\u201358 of a.go (at abcdef1)",
		explainLinesQuestion("", "a.go", 3, 3):             "Explain line 3 of a.go (working tree)",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

func TestExplainLinesInChatValidatesAndIsBusyWhileChatting(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeLines(w, `{"message":{"role":"assistant","content":"Pensando"},"done":false}`)
		<-r.Context().Done()
	}))
	defer srv.Close()
	a, id, ev := newAIApp(t, srv.URL)

	if err := a.ExplainLinesInChat(id, "HEAD", "", 1, 1, "ollama", "x"); err == nil {
		t.Fatal("want an error without a path")
	}
	if err := a.ExplainLinesInChat(id, "HEAD", "file-1.txt", 5, 2, "ollama", "x"); err == nil {
		t.Fatal("want an error for an inverted range")
	}
	if err := a.SendChat(id, "hola", "run-1"); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, agent.EventDelta)
	if err := a.ExplainLinesInChat(id, "HEAD", "file-1.txt", 1, 1, "ollama", "x"); !errors.Is(err, ErrChatBusy) {
		t.Fatalf("explain while chatting: %v", err)
	}
	if err := a.StopChat(id); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, agent.EventDone)
}

// A git error while building the context (here: a path that does not exist)
// must come back from ExplainLinesInChat itself, before anything is written
// to the chat, and must not leave the repository's chat slot busy.
func TestExplainLinesInChatWithBadPathReturnsErrorAndDoesNotBusyTheChat(t *testing.T) {
	srv := fakeOllama(t, nil)
	a, id, ev := newAIApp(t, srv.URL)

	if err := a.ExplainLinesInChat(id, "HEAD", "no-such-file.txt", 1, 1, "ollama", "x"); err == nil {
		t.Fatal("want an error for a path that does not exist")
	}
	history, err := a.GetChat(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 0 {
		t.Fatalf("history = %#v, want it left empty by the failed explain", history)
	}
	if err := a.SendChat(id, "hola", "run-1"); err != nil {
		t.Fatalf("chat still busy after a failed explain: %v", err)
	}
	ev.wait(t, agent.EventDone)
}

func TestStopWhilePreparingAnExplanationIsNotAnError(t *testing.T) {
	srv := fakeOllama(t, nil)
	a, id, ev := newAIApp(t, srv.URL)

	building := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- a.explainTask(id, "ollama", "x", prompts.ExplainLines, func(ctx context.Context, dir string) (string, string, error) {
			close(building)
			<-ctx.Done()
			return "", "", ctx.Err()
		})
	}()
	<-building
	if err := a.StopChat(id); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("stopping while the context is built: %v, want nil", err)
	}
	history, err := a.GetChat(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 0 {
		t.Fatalf("history = %#v, want nothing stored", history)
	}
	if err := a.SendChat(id, "hola", "run-1"); err != nil {
		t.Fatalf("chat still busy after a stopped explain: %v", err)
	}
	ev.wait(t, agent.EventDone)
}

func TestSendChatEmitsStart(t *testing.T) {
	srv := fakeOllama(t, nil)
	a, id, ev := newAIApp(t, srv.URL)

	if err := a.SendChat(id, "hola", "run-1"); err != nil {
		t.Fatal(err)
	}
	got := ev.wait(t, agent.EventStart).data.(agent.StartEvent)
	if got != (agent.StartEvent{RepoID: id, RunID: "run-1", Text: "hola", Provider: "ollama", Model: "qwen2.5:7b"}) {
		t.Fatalf("start = %#v", got)
	}
	ev.wait(t, agent.EventDone)
}

func TestPullModelReportsCanceled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeLines(w, `{"status":"pulling manifest"}`)
		<-r.Context().Done()
	}))
	defer srv.Close()
	a, _, ev := newAIApp(t, srv.URL)

	if err := a.PullModel("qwen2.5:7b"); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, "model:progress")
	if err := a.CancelPull(); err != nil {
		t.Fatal(err)
	}
	done := ev.wait(t, "model:done").data.(ModelDone)
	if !done.Canceled || done.Error == "" {
		t.Fatalf("done = %#v, want Canceled=true with a non-empty error", done)
	}
}

func TestAIStatusAndPull(t *testing.T) {
	srv := fakeOllama(t, nil)
	a, _, ev := newAIApp(t, srv.URL)

	st := a.AIStatus()
	if !st.Ollama.Running || !st.Ollama.ChatModelInstalled || len(st.Ollama.Models) != 1 {
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
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != len(prompts.Names()) {
		t.Fatalf("prompts = %+v, want one per %v", list, prompts.Names())
	}
	for _, p := range list {
		if p.Customized {
			t.Fatalf("%s is customized on a fresh prompts directory", p.Name)
		}
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

// A hosted provider with no stored key must fail before any request, with a
// message telling the user where to fix it.
func TestChatWithoutAKeyAsksForOne(t *testing.T) {
	a, id, _ := newAIApp(t, "http://127.0.0.1:0")
	cfg, err := a.GetAISettings()
	if err != nil {
		t.Fatal(err)
	}
	cfg.ChatProvider, cfg.ChatModel = settings.ProviderAnthropic, "claude-opus-5"
	if err := a.SaveAISettings(cfg); err != nil {
		t.Fatal(err)
	}
	restore := keys.UseStore(&fakeKeys{items: map[string]string{}})
	defer restore()

	err = a.SendChat(id, "hola", "run1")
	if err == nil || !strings.Contains(err.Error(), "API key") {
		t.Fatalf("err = %v, want it to ask for an API key", err)
	}
}

func TestSetAndDeleteProviderKey(t *testing.T) {
	a, _, _ := newAIApp(t, "http://127.0.0.1:0")
	restore := keys.UseStore(&fakeKeys{items: map[string]string{}})
	defer restore()

	if err := a.SetProviderKey("openai", "sk-test-abcd1234"); err != nil {
		t.Fatal(err)
	}
	st := a.AIStatus()
	var found bool
	for _, p := range st.Providers {
		if p.Provider == "openai" {
			found = true
			if !p.HasKey || p.KeyHint != "sk-…1234" {
				t.Errorf("status = %+v, want a masked hint", p)
			}
			if strings.Contains(p.KeyHint, "test") {
				t.Error("the status leaks the key")
			}
		}
	}
	if !found {
		t.Fatal("openai missing from the status")
	}
	if err := a.DeleteProviderKey("openai"); err != nil {
		t.Fatal(err)
	}
	for _, p := range st2Providers(a) {
		if p.Provider == "openai" && p.HasKey {
			t.Error("the key is still reported after Delete")
		}
	}
}

func st2Providers(a *App) []ProviderStatus { return a.AIStatus().Providers }

func TestListModelsRefusesAnUnknownProvider(t *testing.T) {
	a, _, _ := newAIApp(t, "http://127.0.0.1:0")
	if _, err := a.ListModels("acme"); err == nil {
		t.Error("want an error for an unknown provider")
	}
}
