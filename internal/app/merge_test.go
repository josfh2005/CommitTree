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
	"git-ui/internal/ai/apple"
	"git-ui/internal/ai/chatstore"
	"git-ui/internal/ai/prompts"
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
	ev.wait(t, agent.EventDone)

	messages, err := a.GetChat(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) < 2 || messages[0].Role != ai.RoleUser {
		t.Fatalf("messages = %+v", messages)
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
	notice := ev.wait(t, agent.EventNotice)
	n, ok := notice.data.(agent.NoticeEvent)
	if !ok || n.RunID != "run1" || n.RepoID != id || !strings.Contains(n.Text, "Still conflicted: greeting.txt") {
		t.Fatalf("notice = %#v", notice.data)
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
