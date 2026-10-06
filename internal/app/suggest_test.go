package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"git-ui/internal/ai/agent"
	"git-ui/internal/ai/reposettings"
	"git-ui/internal/ai/settings"
)

// suggestServer is fakeOllama plus an answer for the suggested replies
// prompt; it counts those requests and records their prompt. With hold, a
// chat answer streams one line and then waits to be stopped.
func suggestServer(t *testing.T, hold bool) (*httptest.Server, *atomic.Int32, *atomic.Value) {
	var count atomic.Int32
	var prompt atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			fmt.Fprint(w, `{"models":[{"name":"qwen2.5:7b","size":1}]}`)
		case "/api/chat":
			var req map[string]any
			_ = json.NewDecoder(r.Body).Decode(&req)
			msgs := req["messages"].([]any)
			first := msgs[0].(map[string]any)
			if first["role"] == "system" && strings.Contains(first["content"].(string), "suggested replies") {
				count.Add(1)
				prompt.Store(msgs[1].(map[string]any)["content"].(string))
				writeLines(w, `{"message":{"role":"assistant","content":"[\"ok dale\", \"sí, crea la rama\"]"},"done":false}`, `{"message":{"content":""},"done":true}`)
				return
			}
			writeLines(w, `{"message":{"role":"assistant","content":"Hay ramas."},"done":false}`)
			if hold {
				<-r.Context().Done()
				return
			}
			writeLines(w, `{"message":{"content":""},"done":true}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &count, &prompt
}

func newSuggestApp(t *testing.T, url, mode, taskProvider string, delay time.Duration) (*App, string, *events) {
	t.Helper()
	a, id, ev := newAIApp(t, url)
	a.ai.deps.SuggestDelay = delay
	s, err := a.GetAISettings()
	if err != nil {
		t.Fatal(err)
	}
	s.SuggestReplies, s.TaskProvider = mode, taskProvider
	if err := a.SaveAISettings(s); err != nil {
		t.Fatal(err)
	}
	return a, id, ev
}

func TestSuggestionsFollowAnAnswer(t *testing.T) {
	srv, count, prompt := suggestServer(t, false)
	a, id, ev := newSuggestApp(t, srv.URL, settings.SuggestAutoLocal, settings.ProviderOllama, 20*time.Millisecond)

	if err := a.SendChat(id, "hola", "run-1"); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, agent.EventDone)
	got := ev.wait(t, agent.EventSuggestions).data.(agent.SuggestionsEvent)
	want := agent.SuggestionsEvent{RepoID: id, RunID: "run-1", Replies: []string{"ok dale", "sí, crea la rama"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("suggestions = %#v", got)
	}
	if count.Load() != 1 || prompt.Load().(string) != "User: hola\n\nAssistant: Hay ramas." {
		t.Fatalf("requests = %d, prompt = %q", count.Load(), prompt.Load())
	}
}

func TestSuggestionsRespectTheMode(t *testing.T) {
	cases := []struct {
		mode, task string
		want       bool
	}{
		{settings.SuggestOff, settings.ProviderOllama, false},
		{settings.SuggestAutoLocal, settings.ProviderAnthropic, false},
		{settings.SuggestAuto, settings.ProviderOllama, true},
	}
	for _, c := range cases {
		t.Run(c.mode+"/"+c.task, func(t *testing.T) {
			srv, count, _ := suggestServer(t, false)
			a, id, ev := newSuggestApp(t, srv.URL, c.mode, c.task, 20*time.Millisecond)
			if err := a.SendChat(id, "hola", "run-1"); err != nil {
				t.Fatal(err)
			}
			ev.wait(t, agent.EventDone)
			if c.want {
				ev.wait(t, agent.EventSuggestions)
				return
			}
			time.Sleep(200 * time.Millisecond)
			if n := countEvents(ev, agent.EventSuggestions); n != 0 || count.Load() != 0 {
				t.Fatalf("events = %d, requests = %d; want none", n, count.Load())
			}
		})
	}
}

func TestSuggestionsSkipAStoppedAnswer(t *testing.T) {
	srv, count, _ := suggestServer(t, true)
	a, id, ev := newSuggestApp(t, srv.URL, settings.SuggestAuto, settings.ProviderOllama, 20*time.Millisecond)

	if err := a.SendChat(id, "hola", "run-1"); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, agent.EventDelta)
	if err := a.StopChat(id); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, agent.EventDone)
	time.Sleep(200 * time.Millisecond)
	if n := countEvents(ev, agent.EventSuggestions); n != 0 || count.Load() != 0 {
		t.Fatalf("events = %d, requests = %d; want none after Stop", n, count.Load())
	}
}

func TestANewMessageCancelsPendingSuggestions(t *testing.T) {
	srv, count, _ := suggestServer(t, false)
	a, id, ev := newSuggestApp(t, srv.URL, settings.SuggestAuto, settings.ProviderOllama, 300*time.Millisecond)

	if err := a.SendChat(id, "hola", "run-1"); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, agent.EventDone)
	if err := a.SendChat(id, "otra", "run-2"); err != nil {
		t.Fatal(err)
	}
	got := ev.wait(t, agent.EventSuggestions).data.(agent.SuggestionsEvent)
	if got.RunID != "run-2" {
		t.Fatalf("suggestions for %q, want only the latest answer's", got.RunID)
	}
	time.Sleep(200 * time.Millisecond)
	if n := countEvents(ev, agent.EventSuggestions); n != 1 || count.Load() != 1 {
		t.Fatalf("events = %d, requests = %d; want the first one cancelled before its call", n, count.Load())
	}
}

func TestSuggestionsSkippedWhenAIIsTurnedOffDuringTheDelay(t *testing.T) {
	srv, count, _ := suggestServer(t, false)
	a, id, ev := newSuggestApp(t, srv.URL, settings.SuggestAuto, settings.ProviderOllama, 300*time.Millisecond)

	if err := a.SendChat(id, "hola", "run-1"); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, agent.EventDone)
	// Written to the store directly, as a save landing after the answer's run
	// ended would be: no run is left for the save to cancel.
	setRepoAI(t, a, id, reposettings.Override{AIOff: true})
	time.Sleep(600 * time.Millisecond)
	if n := countEvents(ev, agent.EventSuggestions); n != 0 || count.Load() != 0 {
		t.Fatalf("events = %d, requests = %d; want none once AI is off", n, count.Load())
	}
}

func TestClearChatCancelsPendingSuggestions(t *testing.T) {
	srv, count, _ := suggestServer(t, false)
	a, id, ev := newSuggestApp(t, srv.URL, settings.SuggestAuto, settings.ProviderOllama, 300*time.Millisecond)

	if err := a.SendChat(id, "hola", "run-1"); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, agent.EventDone)
	if err := a.ClearChat(id); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	if n := countEvents(ev, agent.EventSuggestions); n != 0 || count.Load() != 0 {
		t.Fatalf("events = %d, requests = %d; want none after clearing", n, count.Load())
	}
}
