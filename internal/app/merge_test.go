package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
