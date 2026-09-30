package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"git-ui/internal/ai"
	"git-ui/internal/ai/agent"
	"git-ui/internal/ai/chatstore"
	"git-ui/internal/ai/prompts"
	"git-ui/internal/ai/settings"
	"git-ui/internal/merge"
	"git-ui/internal/repos"
	"git-ui/internal/testrepo"
)

// newMergeApp returns an App holding one repository where main and feature
// both changed greeting.txt, so merging feature conflicts.
func newMergeApp(t *testing.T) (*App, *testrepo.Repo, string) {
	t.Helper()
	store, err := repos.Open(filepath.Join(t.TempDir(), "repos.json"))
	if err != nil {
		t.Fatal(err)
	}
	r := testrepo.New(t)
	r.WriteFile("greeting.txt", "hello\n")
	r.Git("add", "greeting.txt")
	r.Git("commit", "-q", "-m", "add greeting")
	r.Git("switch", "-q", "-c", "feature")
	r.WriteFile("greeting.txt", "hola\n")
	r.Git("commit", "-q", "-am", "spanish")
	r.Git("switch", "-q", "main")
	r.WriteFile("greeting.txt", "hi\n")
	r.Git("commit", "-q", "-am", "informal")
	repo, err := store.Add(context.Background(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	return New(store), r, repo.ID
}

func TestMergeBranchReportsConflicts(t *testing.T) {
	a, _, id := newMergeApp(t)
	got, err := a.MergeBranch(id, "feature")
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != merge.Conflicted || len(got.Conflicts) != 1 {
		t.Fatalf("result = %+v", got)
	}
}

func TestGetMergeStateDuringAMerge(t *testing.T) {
	a, _, id := newMergeApp(t)
	if _, err := a.MergeBranch(id, "feature"); err != nil {
		t.Fatal(err)
	}
	st, err := a.GetMergeState(id)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Merging || st.From != "feature" || st.Into != "main" {
		t.Fatalf("state = %+v", st)
	}
}

func TestGetConflictFileShowsMarkersThenTheDiff(t *testing.T) {
	a, r, id := newMergeApp(t)
	if _, err := a.MergeBranch(id, "feature"); err != nil {
		t.Fatal(err)
	}

	f, err := a.GetConflictFile(id, "greeting.txt")
	if err != nil {
		t.Fatal(err)
	}
	if f.Resolved || !strings.Contains(f.Text, "<<<<<<<") {
		t.Fatalf("while conflicted: resolved = %v, text = %q", f.Resolved, f.Text)
	}

	r.WriteFile("greeting.txt", "hi / hola\n")
	r.Git("add", "greeting.txt")
	f, err = a.GetConflictFile(id, "greeting.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !f.Resolved || !strings.Contains(f.Text, "+hi / hola") {
		t.Fatalf("after staging: resolved = %v, text = %q", f.Resolved, f.Text)
	}
}

func TestGetConflictFileRefusesPathsOutsideTheMerge(t *testing.T) {
	a, r, id := newMergeApp(t)
	if _, err := a.MergeBranch(id, "feature"); err != nil {
		t.Fatal(err)
	}
	// A file in the repository that this merge never touched.
	r.WriteFile("secret.txt", "not part of the merge\n")

	for _, path := range []string{"secret.txt", ":(glob)*", ":/", "../escape.txt"} {
		got, err := a.GetConflictFile(id, path)
		if err == nil {
			t.Errorf("%s: no error, got %+v", path, got)
		}
	}
}

func TestAbortMergeRestoresTheRepo(t *testing.T) {
	a, r, id := newMergeApp(t)
	before := r.Git("rev-parse", "HEAD")
	if _, err := a.MergeBranch(id, "feature"); err != nil {
		t.Fatal(err)
	}
	if err := a.AbortMerge(id); err != nil {
		t.Fatal(err)
	}
	if got := r.Git("rev-parse", "HEAD"); got != before {
		t.Errorf("HEAD moved: %s != %s", got, before)
	}
}

func TestCommitMergeCreatesTheMergeCommit(t *testing.T) {
	a, r, id := newMergeApp(t)
	if _, err := a.MergeBranch(id, "feature"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.Dir, "greeting.txt"), []byte("hi / hola\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.Git("add", "greeting.txt")
	if err := a.CommitMerge(id); err != nil {
		t.Fatal(err)
	}
	if n := len(strings.Fields(r.Git("rev-list", "--parents", "-n", "1", "HEAD"))); n != 3 {
		t.Errorf("want a merge commit with two parents, got %d fields", n)
	}
	st, err := a.GetMergeState(id)
	if err != nil || st.Merging {
		t.Errorf("state = %+v, err = %v", st, err)
	}
}

// newAIMergeApp is newMergeApp with AI wired in, modelled on newAIApp in
// ai_test.go: a conflicting repository plus an event recorder and a
// settings/chats/prompts directory pointed at ollamaURL.
func newAIMergeApp(t *testing.T, ollamaURL string) (*App, *testrepo.Repo, string, *events) {
	t.Helper()
	a, r, id := newMergeApp(t)
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
	return a, r, id, ev
}

func TestResolveConflictsStreamsIntoTheConversation(t *testing.T) {
	srv := fakeOllama(t, nil)
	a, _, id, ev := newAIMergeApp(t, srv.URL)
	if _, err := a.MergeBranch(id, "feature"); err != nil {
		t.Fatal(err)
	}

	if err := a.ResolveConflicts(id, "run1"); err != nil {
		t.Fatal(err)
	}
	start := ev.wait(t, agent.EventStart).data.(agent.StartEvent)
	if start.Provider != "ollama" || start.Model != "qwen2.5:7b" {
		t.Fatalf("start = %#v", start)
	}
	done := ev.wait(t, agent.EventDone).data.(agent.DoneEvent)

	messages, err := a.GetChat(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) < 2 || messages[0].Role != ai.RoleUser {
		t.Fatalf("messages = %+v", messages)
	}
	last := messages[len(messages)-1]
	if last.Role != ai.RoleAssistant || last.Provider != "ollama" || last.Model != "qwen2.5:7b" || last.At == "" || last.At != done.At {
		t.Fatalf("answer = %+v, done = %+v", last, done)
	}
	if !strings.Contains(messages[0].Content, "feature") || !strings.Contains(messages[0].Content, "main") {
		t.Errorf("the question should name both branches: %q", messages[0].Content)
	}
}

func TestResolveConflictsRefusesWhenNotMerging(t *testing.T) {
	srv := fakeOllama(t, nil)
	a, _, id, _ := newAIMergeApp(t, srv.URL)
	err := a.ResolveConflicts(id, "run1")
	if err == nil {
		t.Fatal("want an error when the repository is not merging")
	}
}

func TestResolveConflictsIsBusyWhileChatting(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeLines(w, `{"message":{"role":"assistant","content":"Pensando"},"done":false}`)
		<-r.Context().Done()
	}))
	defer srv.Close()
	a, _, id, ev := newAIMergeApp(t, srv.URL)
	if _, err := a.MergeBranch(id, "feature"); err != nil {
		t.Fatal(err)
	}

	if err := a.SendChat(id, "hola", "run1"); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, agent.EventDelta)
	if err := a.ResolveConflicts(id, "run2"); !errors.Is(err, ErrChatBusy) {
		t.Fatalf("err = %v, want ErrChatBusy", err)
	}
	if err := a.StopChat(id); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, agent.EventDone)
}

// A merge tool call routed through ResolveConflicts' RunTool reaches
// mergetools, edits the file and announces merge:changed for the repository.
func TestResolveConflictsRunsMergeToolsAndAnnouncesTheChange(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			http.NotFound(w, r)
			return
		}
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		msgs := req["messages"].([]any)
		last := msgs[len(msgs)-1].(map[string]any)
		if last["role"] != "tool" {
			writeLines(w, `{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","function":{"name":"resolve_hunk","arguments":{"path":"greeting.txt","hunk":0,"resolved":"hi / hola\n"}}}]},"done":false}`, `{"message":{"content":""},"done":true}`)
			return
		}
		writeLines(w, `{"message":{"role":"assistant","content":"Resuelto."},"done":false}`, `{"message":{"content":""},"done":true}`)
	}))
	t.Cleanup(srv.Close)
	a, r, id, ev := newAIMergeApp(t, srv.URL)
	if _, err := a.MergeBranch(id, "feature"); err != nil {
		t.Fatal(err)
	}

	if err := a.ResolveConflicts(id, "run1"); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, agent.EventDone)

	if data, _ := os.ReadFile(filepath.Join(r.Dir, "greeting.txt")); string(data) != "hi / hola\n" {
		t.Fatalf("greeting.txt = %q, want the resolution written by the tool", data)
	}
	// One merge:changed for the edit, one for the end of the run.
	ev.mu.Lock()
	changed := 0
	for _, e := range ev.list {
		if e.name == EventMergeChanged {
			if e.data != (MergeChangedEvent{RepoID: id}) {
				t.Errorf("merge:changed carries %#v, want repo %s", e.data, id)
			}
			changed++
		}
	}
	ev.mu.Unlock()
	if changed < 2 {
		t.Errorf("merge:changed emitted %d time(s), want one for the edit and one at the end: %v", changed, ev.names())
	}
}

// Aborting the merge must stop the agent working on it, or the run carries on
// against a merge that no longer exists — or against the next one.
func TestAbortMergeStopsARunningAgent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeLines(w, `{"message":{"role":"assistant","content":"Pensando"},"done":false}`)
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	a, _, id, ev := newAIMergeApp(t, srv.URL)
	// Registered after srv.Close, so it runs first: if AbortMerge fails to
	// stop the run, stop it here so the server can shut down.
	t.Cleanup(func() { a.StopChat(id) })
	if _, err := a.MergeBranch(id, "feature"); err != nil {
		t.Fatal(err)
	}

	if err := a.ResolveConflicts(id, "run1"); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, agent.EventDelta)
	if err := a.AbortMerge(id); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, agent.EventDone)

	// The slot is free again.
	if err := a.SendChat(id, "hola", "run2"); err != nil {
		t.Fatalf("send after abort: %v", err)
	}
	ev.wait(t, agent.EventDelta)
	if err := a.StopChat(id); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, agent.EventDone)
}

// Committing the merge stops a running agent the same way.
func TestCommitMergeStopsARunningAgent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeLines(w, `{"message":{"role":"assistant","content":"Pensando"},"done":false}`)
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	a, r, id, ev := newAIMergeApp(t, srv.URL)
	t.Cleanup(func() { a.StopChat(id) })
	if _, err := a.MergeBranch(id, "feature"); err != nil {
		t.Fatal(err)
	}
	if err := a.ResolveConflicts(id, "run1"); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, agent.EventDelta)
	r.WriteFile("greeting.txt", "hi / hola\n")
	r.Git("add", "greeting.txt")
	if err := a.CommitMerge(id); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, agent.EventDone)
}

// A run must never touch a merge it was not started for. Here the merge is
// aborted and a different one started behind the app's back, from a
// terminal, so cancellation never happens; the MERGE_HEAD check still
// refuses the stale run's edit.
func TestResolveConflictsRefusesToEditADifferentMerge(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		msgs := req["messages"].([]any)
		if msgs[len(msgs)-1].(map[string]any)["role"] == "tool" {
			writeLines(w, `{"message":{"role":"assistant","content":"Vale."},"done":false}`, `{"message":{"content":""},"done":true}`)
			return
		}
		<-release
		writeLines(w, `{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","function":{"name":"resolve_hunk","arguments":{"path":"greeting.txt","hunk":0,"resolved":"stale\n"}}}]},"done":false}`, `{"message":{"content":""},"done":true}`)
	}))
	t.Cleanup(srv.Close)
	a, r, id, ev := newAIMergeApp(t, srv.URL)
	r.Git("switch", "-q", "-c", "other", "feature")
	r.WriteFile("greeting.txt", "bonjour\n")
	r.Git("commit", "-q", "-am", "french")
	r.Git("switch", "-q", "main")
	if _, err := a.MergeBranch(id, "feature"); err != nil {
		t.Fatal(err)
	}

	if err := a.ResolveConflicts(id, "run1"); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, agent.EventStart)
	r.Git("merge", "--abort")
	if _, err := merge.Start(context.Background(), r.Dir, "other"); err != nil {
		t.Fatal(err)
	}
	close(release)
	ev.wait(t, agent.EventDone)

	data, _ := os.ReadFile(filepath.Join(r.Dir, "greeting.txt"))
	if strings.Contains(string(data), "stale") || !strings.Contains(string(data), "<<<<<<<") {
		t.Fatalf("greeting.txt = %q: the stale run edited the new merge", data)
	}
	history, err := a.GetChat(id)
	if err != nil {
		t.Fatal(err)
	}
	refused := false
	for _, m := range history {
		if m.Role == ai.RoleTool && strings.Contains(m.Content, "no longer in progress") {
			refused = true
		}
	}
	if !refused {
		t.Errorf("history = %#v, want the tool call refused as a different merge", history)
	}
}

// newAIRebaseApp is newAIMergeApp stopped in a rebase of feature onto main
// with two conflicting steps instead of in a merge.
func newAIRebaseApp(t *testing.T, ollamaURL string) (*App, *testrepo.Repo, string, *events) {
	t.Helper()
	a, r, id, ev := newAIMergeApp(t, ollamaURL)
	_ = a.AbortMerge(id)
	r.Git("switch", "-q", "feature")
	r.WriteFile("greeting.txt", "hola!!\n")
	r.Git("commit", "-q", "-am", "shout")
	r.GitFails("rebase", "main")
	return a, r, id, ev
}

func TestResolveConflictsWorksOnARebase(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeLines(w, `{"message":{"role":"assistant","content":"Hecho."},"done":false}`, `{"message":{"content":""},"done":true}`)
	}))
	t.Cleanup(srv.Close)
	a, _, id, ev := newAIRebaseApp(t, srv.URL)
	if err := a.ResolveConflicts(id, "run1"); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, agent.EventStart)
	ev.wait(t, agent.EventDone)
	history, err := a.GetChat(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) == 0 || !strings.Contains(history[0].Content, "rebasing feature onto main") {
		t.Errorf("history = %#v, want the request to name the rebase", history)
	}
}

func TestResolveConflictsRefusesTheNextRebaseStep(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		msgs := req["messages"].([]any)
		if msgs[len(msgs)-1].(map[string]any)["role"] == "tool" {
			writeLines(w, `{"message":{"role":"assistant","content":"Vale."},"done":false}`, `{"message":{"content":""},"done":true}`)
			return
		}
		<-release
		writeLines(w, `{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","function":{"name":"resolve_hunk","arguments":{"path":"greeting.txt","hunk":0,"resolved":"stale\n"}}}]},"done":false}`, `{"message":{"content":""},"done":true}`)
	}))
	t.Cleanup(srv.Close)
	a, r, id, ev := newAIRebaseApp(t, srv.URL)
	if err := a.ResolveConflicts(id, "run1"); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, agent.EventStart)
	// Step 1 is finished behind the run's back (a terminal), leaving step 2.
	r.WriteFile("greeting.txt", "step one\n")
	r.Git("add", "greeting.txt")
	if err := merge.Continue(context.Background(), r.Dir); err != nil {
		t.Fatal(err)
	}
	close(release)
	ev.wait(t, agent.EventDone)

	data, _ := os.ReadFile(filepath.Join(r.Dir, "greeting.txt"))
	if strings.Contains(string(data), "stale") || !strings.Contains(string(data), "<<<<<<<") {
		t.Fatalf("greeting.txt = %q: the run edited the next step", data)
	}
}

func TestResolveConflictsRefusesARevert(t *testing.T) {
	a, r, id, _ := newAIMergeApp(t, "http://127.0.0.1:0")
	_ = a.AbortMerge(id) // back on main, clean
	r.WriteFile("greeting.txt", "a\n")
	r.Git("commit", "-q", "-am", "a")
	r.WriteFile("greeting.txt", "b\n")
	r.Git("commit", "-q", "-am", "b")
	middle := r.Git("rev-parse", "HEAD")
	r.WriteFile("greeting.txt", "c\n")
	r.Git("commit", "-q", "-am", "c")
	r.GitFails("revert", "--no-edit", middle) // b→a conflicts with c

	err := a.ResolveConflicts(id, "run1")
	if err == nil || !strings.Contains(err.Error(), "merges, rebases and cherry-picks") {
		t.Fatalf("err = %v, want a refusal naming the three kinds", err)
	}
}

func TestResolveConflictsRefusesAStash(t *testing.T) {
	a, r, id, _ := newAIMergeApp(t, "http://127.0.0.1:0")
	_ = a.AbortMerge(id) // back on main, clean
	r.WriteFile("a.txt", "one\n")
	r.Git("add", "a.txt")
	r.Git("commit", "-q", "-m", "base")
	r.WriteFile("a.txt", "stashed\n")
	r.Git("stash", "push", "-q", "-m", "wip")
	r.WriteFile("a.txt", "conflicting\n")
	r.Git("commit", "-q", "-am", "conflicting")
	r.GitFails("stash", "pop")

	err := a.ResolveConflicts(id, "run1")
	if err == nil || !strings.Contains(err.Error(), "merges, rebases and cherry-picks") {
		t.Fatalf("err = %v, want a refusal naming the three kinds", err)
	}
}

func TestResolveSummary(t *testing.T) {
	cases := []struct {
		name string
		st   merge.State
		want []string
	}{
		{"not merging", merge.State{}, nil},
		{"nothing left", merge.State{Merging: true, Staged: []string{"a.go"}}, []string{"nothing left to resolve", "Staged"}},
		{"conflicts and manual", merge.State{Merging: true, Conflicts: []string{"greet.go"}, Manual: []string{"deps.lock"}}, []string{"Still conflicted: greet.go", "Needs you: deps.lock"}},
		{"manual only", merge.State{Merging: true, Manual: []string{"deps.lock"}}, []string{"Needs you: deps.lock"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveSummary(tc.st)
			if tc.want == nil && got != "" {
				t.Errorf("got %q, want no summary", got)
			}
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Errorf("got %q, want it to contain %q", got, w)
				}
			}
		})
	}
	if got := resolveSummary(merge.State{Merging: true, Manual: []string{"deps.lock"}}); strings.Contains(got, "Still conflicted") {
		t.Errorf("got %q, lists an empty conflicts part", got)
	}
}

// The model's own closing words can claim success; the run ends with git's
// account of what is left, before chat:done.
func TestResolveConflictsEndsWithGitsSummary(t *testing.T) {
	srv := fakeOllama(t, nil)
	a, _, id, ev := newAIMergeApp(t, srv.URL)
	if _, err := a.MergeBranch(id, "feature"); err != nil {
		t.Fatal(err)
	}
	if err := a.ResolveConflicts(id, "run1"); err != nil {
		t.Fatal(err)
	}
	// The fake model stops without resolving anything: the run is first
	// told to carry on, then ends with git's own account.
	var texts []string
	for len(texts) == 0 || !strings.Contains(texts[len(texts)-1], "Still conflicted") {
		notice := ev.wait(t, agent.EventNotice)
		n, ok := notice.data.(agent.NoticeEvent)
		if !ok || n.RunID != "run1" || n.RepoID != id {
			t.Fatalf("notice = %#v", notice.data)
		}
		texts = append(texts, n.Text)
	}
	if !strings.Contains(texts[0], "carry on") || !strings.Contains(texts[len(texts)-1], "Still conflicted: greeting.txt") {
		t.Fatalf("notices = %q", texts)
	}
	ev.wait(t, agent.EventDone)
}

func countEvents(ev *events, name string) int {
	n := 0
	for _, got := range ev.names() {
		if got == name {
			n++
		}
	}
	return n
}

// The merge-writing wrappers refuse a path git did not list, changing nothing
// and announcing nothing; a real change is announced as merge:changed.
func TestMergeFileActionsRejectOrAnnounce(t *testing.T) {
	a, r, id, ev := newAIMergeApp(t, "http://127.0.0.1:0")
	if _, err := a.MergeBranch(id, "feature"); err != nil {
		t.Fatal(err)
	}
	r.WriteFile("greeting.txt", "hi there\n")
	r.Git("add", "greeting.txt")

	for _, err := range []error{
		a.StageMergeFile(id, ":(glob)*"),
		a.UnstageMergeFile(id, "*"),
		a.TakeMergeSide(id, "greeting.txt", "theirs"), // a text file, not Manual
	} {
		if !errors.Is(err, merge.ErrNotInMerge) {
			t.Errorf("err = %v, want ErrNotInMerge", err)
		}
	}
	if n := countEvents(ev, EventMergeChanged); n != 0 {
		t.Fatalf("%d merge:changed events after refusals, want 0", n)
	}

	if err := a.UnstageMergeFile(id, "greeting.txt"); err != nil {
		t.Fatal(err)
	}
	if err := a.StageMergeFile(id, "greeting.txt"); err != nil {
		t.Fatal(err)
	}
	if n := countEvents(ev, EventMergeChanged); n != 2 {
		t.Errorf("%d merge:changed events, want one per change", n)
	}
	st, err := a.GetMergeState(id)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(st.Staged, []string{"greeting.txt"}) {
		t.Errorf("staged = %v, want greeting.txt back", st.Staged)
	}
}

// ResolveConflicts is scoped to a real merge by its own existing MERGE_HEAD
// check (mergeHead returns "" outside a merge); this pins that it stays
// refused once nothing is merging, now that Status also reports a rebase or
// a stash conflict as Merging true — AbortMerge (kind-aware since Task 2)
// is used here specifically to leave the repository in a clean state
// regardless of whatever newAIMergeApp's own conflicted history started it
// in, without this test needing to know that shape itself.
func TestResolveConflictsRefusesOutsideARealMerge(t *testing.T) {
	a, _, id, _ := newAIMergeApp(t, "http://127.0.0.1:0")
	_ = a.AbortMerge(id)

	if err := a.ResolveConflicts(id, "run1"); err == nil {
		t.Error("want a refusal outside a merge")
	}
}

// A stopped resolve run is always told to carry on once; again only when
// it left a conflicted file unread since, and never beyond the cap.
func TestResolveNudge(t *testing.T) {
	r := testrepo.New(t)
	for _, f := range []string{"a.txt", "b.txt"} {
		r.WriteFile(f, "base\n")
	}
	r.Git("add", ".")
	r.Git("commit", "-q", "-m", "base")
	r.Git("switch", "-q", "-c", "feature")
	r.WriteFile("a.txt", "theirs\n")
	r.WriteFile("b.txt", "theirs\n")
	r.Git("commit", "-q", "-am", "theirs")
	r.Git("switch", "-q", "main")
	r.WriteFile("a.txt", "ours\n")
	r.WriteFile("b.txt", "ours\n")
	r.Git("commit", "-q", "-am", "ours")
	ctx := context.Background()
	if _, err := merge.Start(ctx, r.Dir, "feature"); err != nil {
		t.Fatal(err)
	}
	read := func(n *resolveNudge, path string) {
		n.saw(ai.ToolCall{Name: "read_conflict", Args: map[string]any{"path": path}})
	}

	// Read everything, declined everything: nudged once, then left alone.
	n := &resolveNudge{dir: r.Dir, startedFor: merge.Fingerprint(ctx, r.Dir)}
	read(n, "a.txt")
	read(n, "b.txt")
	if first := n.next(ctx); !strings.Contains(first, "a.txt") || !strings.Contains(first, "b.txt") {
		t.Fatalf("first nudge = %q", first)
	}
	read(n, "a.txt")
	read(n, "b.txt")
	if again := n.next(ctx); again != "" {
		t.Fatalf("nudged a model that read every file left: %q", again)
	}

	// Stopped without reading b.txt: nudged again, but not past the cap.
	n = &resolveNudge{dir: r.Dir, startedFor: merge.Fingerprint(ctx, r.Dir)}
	n.next(ctx)
	read(n, "a.txt")
	if second := n.next(ctx); !strings.Contains(second, "b.txt") {
		t.Fatalf("second nudge = %q", second)
	}
	if third := n.next(ctx); third != "" {
		t.Fatalf("nudged past the cap: %q", third)
	}
}

func TestResolveMergeRegionStagesTheLastRegion(t *testing.T) {
	a, r, id, _ := newAIMergeApp(t, "http://127.0.0.1:0")
	if _, err := a.MergeBranch(id, "feature"); err != nil {
		t.Fatal(err)
	}
	f, err := a.GetConflictFile(id, "greeting.txt")
	if err != nil || len(f.Regions) != 1 || !f.Restartable {
		t.Fatalf("file = %+v, %v", f, err)
	}
	lines := strings.SplitAfter(f.Text, "\n")
	if !strings.HasPrefix(lines[f.Regions[0].Start], "<<<<<<<") {
		t.Fatalf("region starts at %q", lines[f.Regions[0].Start])
	}
	res, err := a.ResolveMergeRegion(id, "greeting.txt", f.Regions[0].ID, "both", "")
	if err != nil || res.Left != 0 || !res.Staged {
		t.Fatalf("res = %+v, %v", res, err)
	}
	st, _ := merge.Status(context.Background(), r.Dir)
	if !slices.Contains(st.Staged, "greeting.txt") {
		t.Fatalf("not staged: %+v", st)
	}
	if err := a.RestartConflictFile(id, "greeting.txt"); err != nil {
		t.Fatal(err)
	}
	if st, _ := merge.Status(context.Background(), r.Dir); !slices.Contains(st.Conflicts, "greeting.txt") {
		t.Fatalf("not conflicted after restart: %+v", st)
	}
}

func TestResolveMergeRegionWithEditedText(t *testing.T) {
	a, r, id, _ := newAIMergeApp(t, "http://127.0.0.1:0")
	if _, err := a.MergeBranch(id, "feature"); err != nil {
		t.Fatal(err)
	}
	f, _ := a.GetConflictFile(id, "greeting.txt")
	if _, err := a.ResolveMergeRegion(id, "greeting.txt", f.Regions[0].ID, "text", "hello there\n"); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(r.Dir, "greeting.txt")); string(data) != "hello there\n" {
		t.Fatalf("file = %q", data)
	}
}

func TestResolveMergeRegionRefusedWhileAIRuns(t *testing.T) {
	a, _, id, _ := newAIMergeApp(t, "http://127.0.0.1:0")
	if _, err := a.MergeBranch(id, "feature"); err != nil {
		t.Fatal(err)
	}
	f, _ := a.GetConflictFile(id, "greeting.txt")
	a.ai.mu.Lock()
	a.ai.runs[id] = func() {}
	a.ai.mu.Unlock()
	if _, err := a.ResolveMergeRegion(id, "greeting.txt", f.Regions[0].ID, "ours", ""); !errors.Is(err, ErrChatBusy) {
		t.Fatalf("err = %v", err)
	}
	if err := a.RestartConflictFile(id, "greeting.txt"); !errors.Is(err, ErrChatBusy) {
		t.Fatalf("restart err = %v", err)
	}
}
