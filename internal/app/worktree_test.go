package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"git-ui/internal/ai/reposettings"
	"git-ui/internal/repos"
	"git-ui/internal/testrepo"
	"git-ui/internal/worktree"
)

// newSubmoduleApp returns an App over a repository with lib added as a
// submodule at "lib", committed and in sync.
func newSubmoduleApp(t *testing.T) (*App, *testrepo.Repo, string) {
	t.Helper()
	lib := testrepo.New(t)
	lib.WriteFile("a.txt", "a")
	lib.Git("add", "a.txt")
	lib.Git("commit", "-q", "-m", "lib one")
	parent := testrepo.New(t)
	parent.WriteFile("p.txt", "p")
	parent.Commit("parent one")
	parent.Git("-c", "protocol.file.allow=always", "submodule", "add", "-q", lib.Dir, "lib")
	parent.Git("commit", "-q", "-m", "add submodule")

	store, err := repos.Open(filepath.Join(t.TempDir(), "repos.json"))
	if err != nil {
		t.Fatal(err)
	}
	repo, err := store.Add(context.Background(), parent.Dir)
	if err != nil {
		t.Fatal(err)
	}
	return New(store), parent, repo.ID
}

// The wrappers refuse a path git did not list, change nothing and announce
// nothing; a real change is announced once as worktree:changed.
func TestWorktreeActionsRejectOrAnnounce(t *testing.T) {
	a, r, id, ev := newAIMergeApp(t, "http://127.0.0.1:0")
	r.WriteFile("a.txt", "changed\n")

	for _, err := range []error{
		a.StageFile(id, ":(glob)*"),
		a.UnstageFile(id, "*"),
		a.DiscardFile(id, "../escape.txt"),
	} {
		if !errors.Is(err, worktree.ErrNotInWorktree) {
			t.Errorf("err = %v, want ErrNotInWorktree", err)
		}
	}
	if n := countEvents(ev, EventWorktreeChanged); n != 0 {
		t.Fatalf("%d worktree:changed events after refusals, want 0", n)
	}

	if err := a.StageFile(id, "a.txt"); err != nil {
		t.Fatal(err)
	}
	if n := countEvents(ev, EventWorktreeChanged); n != 1 {
		t.Errorf("%d worktree:changed events, want 1 per change", n)
	}
	st, err := a.GetWorktreeState(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Staged) != 1 || st.Staged[0].Path != "a.txt" {
		t.Errorf("staged = %+v", st.Staged)
	}
}

func TestCommitChangesAnnouncesAndClearsTheIndex(t *testing.T) {
	a, r, id, ev := newAIMergeApp(t, "http://127.0.0.1:0")
	r.WriteFile("a.txt", "changed\n")
	if err := a.StageFile(id, "a.txt"); err != nil {
		t.Fatal(err)
	}
	if err := a.CommitChanges(id, "change a", false); err != nil {
		t.Fatal(err)
	}
	if got := r.Git("log", "-1", "--pretty=%s"); got != "change a" {
		t.Errorf("subject = %q", got)
	}
	if n := countEvents(ev, EventWorktreeChanged); n < 2 {
		t.Errorf("%d worktree:changed events, want one for the stage and one for the commit", n)
	}
}

func TestGetWorktreeDiffRefusesAnUnlistedPath(t *testing.T) {
	a, r, id, _ := newAIMergeApp(t, "http://127.0.0.1:0")
	r.WriteFile("a.txt", "changed\n")
	if _, err := a.GetWorktreeDiff(id, "../secrets.txt", false); err == nil {
		t.Error("want a refusal for a path outside the listed changes")
	}
	d, err := a.GetWorktreeDiff(id, "a.txt", false)
	if err != nil {
		t.Fatal(err)
	}
	out := d.Text
	if !strings.Contains(out, "changed") {
		t.Errorf("diff = %q, want the change in it", out)
	}
}

// A path can go stale between the Status read inside GetWorktreeDiff and the
// git diff call that follows it: the renderer sent a real, listed path, but
// the file is gone from disk by the time the untracked --no-index diff runs.
// git's exit 1 there means either "the files differ" (the ordinary case) or
// "could not access the path" (this one) - they must not collapse into the
// same empty-string success.
func TestGetWorktreeDiffOfAVanishedUntrackedFileErrors(t *testing.T) {
	a, r, id, _ := newAIMergeApp(t, "http://127.0.0.1:0")
	r.WriteFile("a.txt", "changed\n")

	d, err := a.GetWorktreeDiff(id, "a.txt", false)
	if err != nil {
		t.Fatal(err)
	}
	out := d.Text
	if !strings.Contains(out, "changed") {
		t.Errorf("diff = %q, want the file's contents in it", out)
	}

	if err := os.Remove(filepath.Join(r.Dir, "a.txt")); err != nil {
		t.Fatal(err)
	}
	if d, err := a.GetWorktreeDiff(id, "a.txt", false); err == nil {
		t.Errorf("want an error for a path that vanished before the diff, got out = %q", d.Text)
	}
}

// A diff that exceeds worktreeDiffCap comes back truncated with a trailing
// note, so a huge untracked or changed file can never freeze the diff pane;
// a small diff is returned untouched. Covers the untracked branch (a whole
// new file, shown via --no-index) and the tracked branch (a large in-place
// change), which take different code paths inside GetWorktreeDiff.
func TestGetWorktreeDiffTruncatesALargeDiff(t *testing.T) {
	a, r, id, _ := newAIMergeApp(t, "http://127.0.0.1:0")

	big := strings.Repeat("x", worktreeDiffCap+1024) + "\n"
	r.WriteFile("huge.txt", big)
	d, err := a.GetWorktreeDiff(id, "huge.txt", false)
	if err != nil {
		t.Fatal(err)
	}
	out := d.Text
	if len(out) > worktreeDiffCap+64 {
		t.Errorf("untracked diff length = %d, want it capped near %d", len(out), worktreeDiffCap)
	}
	if !strings.Contains(out, "truncated") {
		t.Errorf("untracked diff = %q, want a truncation note", out[max(0, len(out)-64):])
	}

	r.WriteFile("small.txt", "one line\n")
	sd, err := a.GetWorktreeDiff(id, "small.txt", false)
	if err != nil {
		t.Fatal(err)
	}
	small := sd.Text
	if strings.Contains(small, "truncated") {
		t.Errorf("small diff = %q, want it untouched", small)
	}
}

// A submodule's diff comes back as git's --submodule=log summary, not the
// raw "Subproject commit" line: the pane must show what moved, not a hash.
func TestGetWorktreeDiffOfASubmodulePointerMove(t *testing.T) {
	a, r, id := newSubmoduleApp(t)
	subDir := filepath.Join(r.Dir, "lib")
	if err := os.WriteFile(filepath.Join(subDir, "a.txt"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.Git("-C", subDir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-am", "moved")

	d, err := a.GetWorktreeDiff(id, "lib", false)
	if err != nil {
		t.Fatal(err)
	}
	out := d.Text
	if !strings.Contains(out, "Submodule lib ") {
		t.Errorf("diff = %q, want the submodule=log summary", out)
	}
}

// The generated message streams as commit:delta events and ends with
// commit:done; nothing is committed by generating one.
func TestGenerateCommitMessageStreams(t *testing.T) {
	srv := fakeOllama(t, nil) // its canned reply is enough; we assert on events
	a, r, id, ev := newAIMergeApp(t, srv.URL)
	before := r.Git("rev-list", "--count", "HEAD")
	r.WriteFile("a.txt", "changed\n")
	if err := a.StageFile(id, "a.txt"); err != nil {
		t.Fatal(err)
	}
	if err := a.GenerateCommitMessage(id, "run1"); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, EventCommitDelta)
	ev.wait(t, EventCommitDone)
	if got := r.Git("rev-list", "--count", "HEAD"); got != before {
		t.Errorf("commit count = %s, want unchanged from %s; generating a message committed something", got, before)
	}
}

func TestGenerateCommitMessageRefusesWithNothingStaged(t *testing.T) {
	a, _, id, _ := newAIMergeApp(t, "http://127.0.0.1:0")
	if err := a.GenerateCommitMessage(id, "run1"); err == nil {
		t.Error("want an error with nothing staged")
	}
}

// GenerateCommitMessage must use the TASK provider/model — the cheap one —
// never the chat one, even when the two are configured differently.
func TestGenerateCommitMessageUsesTheTaskModelNotTheChatModel(t *testing.T) {
	var gotModel string
	srv := fakeOllama(t, func(req map[string]any) {
		if m, ok := req["model"].(string); ok {
			gotModel = m
		}
	})
	a, r, id, ev := newAIMergeApp(t, srv.URL)

	cfg, err := a.GetAISettings()
	if err != nil {
		t.Fatal(err)
	}
	cfg.ChatModel = "chat-model"
	cfg.TaskModel = "task-model"
	if err := a.SaveAISettings(cfg); err != nil {
		t.Fatal(err)
	}

	r.WriteFile("a.txt", "changed\n")
	if err := a.StageFile(id, "a.txt"); err != nil {
		t.Fatal(err)
	}
	if err := a.GenerateCommitMessage(id, "run1"); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, EventCommitDone)

	if gotModel != "task-model" {
		t.Errorf("model sent to Ollama = %q, want the task model %q", gotModel, "task-model")
	}
}

// A plain modification is patchable and carries the full diff's hash; an
// untracked file is not patchable; a truncated diff says so.
func TestGetWorktreeDiffFlags(t *testing.T) {
	a, r, id := newMergeApp(t)
	r.WriteFile("greeting.txt", "hey\n")
	d, err := a.GetWorktreeDiff(id, "greeting.txt", false)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Patchable || d.Truncated || d.Hash != worktree.DiffHash(d.Text) {
		t.Errorf("modified file: %+v", d)
	}

	r.WriteFile("new.txt", "new\n")
	if d, err := a.GetWorktreeDiff(id, "new.txt", false); err != nil || d.Patchable {
		t.Errorf("untracked file: %+v, %v", d, err)
	}

	r.WriteFile("greeting.txt", strings.Repeat("y", worktreeDiffCap+1024)+"\n")
	d, err = a.GetWorktreeDiff(id, "greeting.txt", false)
	if err != nil {
		t.Fatal(err)
	}
	full, _ := worktree.FileDiff(context.Background(), r.Dir, "greeting.txt", false)
	if !d.Truncated || d.Hash != worktree.DiffHash(full) {
		t.Errorf("truncated diff: truncated=%v, hash of the full diff=%v", d.Truncated, d.Hash == worktree.DiffHash(full))
	}
}

func TestApplyHunkSelectionDiscardAndUndo(t *testing.T) {
	a, r, id, _ := newAIMergeApp(t, "http://127.0.0.1:0")
	r.WriteFile("greeting.txt", "hey\n")
	d, err := a.GetWorktreeDiff(id, "greeting.txt", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.ApplyHunkSelection(id, "greeting.txt", false, d.Hash, worktree.Selection{{Hunk: 0}}, "discard"); err != nil {
		t.Fatal(err)
	}
	if got := r.Git("show", "HEAD:greeting.txt"); got != "hi" {
		t.Fatalf("HEAD content = %q", got)
	}
	data, _ := os.ReadFile(filepath.Join(r.Dir, "greeting.txt"))
	if string(data) != "hi\n" {
		t.Fatalf("after discard = %q, want the committed content", data)
	}

	if err := a.UndoDiscard(id); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(filepath.Join(r.Dir, "greeting.txt"))
	if string(data) != "hey\n" {
		t.Fatalf("after undo = %q", data)
	}
	if err := a.UndoDiscard(id); err == nil {
		t.Error("a second undo succeeded; there is nothing left to undo")
	}
}

func TestApplyHunkSelectionStageKeepsNothingToUndo(t *testing.T) {
	a, r, id, _ := newAIMergeApp(t, "http://127.0.0.1:0")
	r.WriteFile("greeting.txt", "hey\n")
	d, _ := a.GetWorktreeDiff(id, "greeting.txt", false)
	if err := a.ApplyHunkSelection(id, "greeting.txt", false, d.Hash, worktree.Selection{{Hunk: 0}}, "stage"); err != nil {
		t.Fatal(err)
	}
	if cached := r.Git("diff", "--cached"); !strings.Contains(cached, "+hey") {
		t.Fatalf("staged diff = %s", cached)
	}
	if err := a.UndoDiscard(id); err == nil {
		t.Error("undo after a stage succeeded")
	}
}

// Opening a diff reads the status once: the path check and the patchable
// flag share it.
func TestGetWorktreeDiffReadsTheStatusOnce(t *testing.T) {
	a, r, id := newMergeApp(t)
	r.WriteFile("greeting.txt", "hey\n")
	statuses := func() int {
		v, err := a.CommandLog(id)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, e := range v.Entries {
			if slices.Contains(e.Args, "status") {
				n++
			}
		}
		return n
	}
	before := statuses()
	if _, err := a.GetWorktreeDiff(id, "greeting.txt", false); err != nil {
		t.Fatal(err)
	}
	if n := statuses() - before; n != 1 {
		t.Errorf("git status ran %d times, want 1", n)
	}
}

// Turning the AI off while a commit message is being prepared (after the
// settings were read, before the run registers) must stop it from streaming.
func TestGenerateCommitMessageNotStartedWhenTurnedOffBeforeItRegisters(t *testing.T) {
	srv := fakeOllama(t, nil)
	a, r, id, ev := newAIMergeApp(t, srv.URL)
	r.WriteFile("a.txt", "changed\n")
	if err := a.StageFile(id, "a.txt"); err != nil {
		t.Fatal(err)
	}
	a.ai.afterSettings = func(string) {
		a.ai.afterSettings = nil
		if err := a.SaveRepoAISettings(id, reposettings.Override{AIOff: true}); err != nil {
			t.Error(err)
		}
	}
	if err := a.GenerateCommitMessage(id, "run1"); !errors.Is(err, ErrAIOff) {
		t.Fatalf("got %v", err)
	}
	a.ai.mu.Lock()
	left := len(a.ai.commits)
	a.ai.mu.Unlock()
	if left != 0 {
		t.Fatalf("%d commit runs left registered", left)
	}
	for _, n := range ev.names() {
		if n == EventCommitDelta || n == EventCommitDone {
			t.Fatalf("the generation started: %v", ev.names())
		}
	}
}
