package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git-ui/internal/worktree"
)

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
	out, err := a.GetWorktreeDiff(id, "a.txt", false)
	if err != nil {
		t.Fatal(err)
	}
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

	out, err := a.GetWorktreeDiff(id, "a.txt", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "changed") {
		t.Errorf("diff = %q, want the file's contents in it", out)
	}

	if err := os.Remove(filepath.Join(r.Dir, "a.txt")); err != nil {
		t.Fatal(err)
	}
	if out, err := a.GetWorktreeDiff(id, "a.txt", false); err == nil {
		t.Errorf("want an error for a path that vanished before the diff, got out = %q", out)
	}
}
