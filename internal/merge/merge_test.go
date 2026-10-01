package merge

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git-ui/internal/gitcmd"
	"git-ui/internal/testrepo"
)

// conflicting builds a repo where main and feature both changed greeting.txt.
func conflicting(t *testing.T) *testrepo.Repo {
	t.Helper()
	r := testrepo.New(t)
	r.WriteFile("greeting.txt", "hello\n")
	r.Git("add", "greeting.txt")
	r.Git("commit", "-q", "-m", "add greeting")
	r.Git("switch", "-q", "-c", "feature")
	r.WriteFile("greeting.txt", "hola\n")
	r.Git("commit", "-q", "-am", "greet in spanish")
	r.Git("switch", "-q", "main")
	r.WriteFile("greeting.txt", "hi\n")
	r.Git("commit", "-q", "-am", "greet informally")
	return r
}

func TestStartCleanMerge(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	r.Git("switch", "-q", "-c", "feature")
	r.Commit("on feature")
	r.Git("switch", "-q", "main")

	got, err := Start(context.Background(), r.Dir, "feature")
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != Merged {
		t.Fatalf("outcome = %v, want Merged", got.Outcome)
	}
	// --no-ff means a merge commit even when a fast-forward was possible.
	parents := r.Git("rev-list", "--parents", "-n", "1", "HEAD")
	if len(strings.Fields(parents)) != 3 {
		t.Errorf("want a merge commit with two parents, got %q", parents)
	}
}

func TestStartConflicted(t *testing.T) {
	r := conflicting(t)
	got, err := Start(context.Background(), r.Dir, "feature")
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != Conflicted {
		t.Fatalf("outcome = %v, want Conflicted", got.Outcome)
	}
	if len(got.Conflicts) != 1 || got.Conflicts[0] != "greeting.txt" {
		t.Fatalf("conflicts = %v", got.Conflicts)
	}
	// zdiff3 was requested, so the markers carry the common ancestor.
	data, err := os.ReadFile(filepath.Join(r.Dir, "greeting.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "|||||||") {
		t.Errorf("want a zdiff3 base section, got:\n%s", data)
	}
}

func TestStartAlreadyUpToDate(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	r.Git("branch", "feature")

	got, err := Start(context.Background(), r.Dir, "feature")
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != UpToDate {
		t.Fatalf("outcome = %v, want UpToDate", got.Outcome)
	}
}

func TestStartRejectsAnOptionLikeRef(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	_, err := Start(context.Background(), r.Dir, "--exec=rm -rf /")
	if !errors.Is(err, ErrInvalidRef) {
		t.Fatalf("err = %v, want ErrInvalidRef", err)
	}
}

func TestAbortRestoresTheBranch(t *testing.T) {
	r := conflicting(t)
	before := r.Git("rev-parse", "HEAD")
	if _, err := Start(context.Background(), r.Dir, "feature"); err != nil {
		t.Fatal(err)
	}
	if err := Abort(context.Background(), r.Dir); err != nil {
		t.Fatal(err)
	}
	if got := r.Git("rev-parse", "HEAD"); got != before {
		t.Errorf("HEAD = %s, want %s", got, before)
	}
	if data, _ := os.ReadFile(filepath.Join(r.Dir, "greeting.txt")); string(data) != "hi\n" {
		t.Errorf("greeting.txt = %q, want the pre-merge content", data)
	}
}

func TestCommitClosesTheMerge(t *testing.T) {
	r := conflicting(t)
	if _, err := Start(context.Background(), r.Dir, "feature"); err != nil {
		t.Fatal(err)
	}
	r.WriteFile("greeting.txt", "hi there\n")
	r.Git("add", "greeting.txt")
	if err := Commit(context.Background(), r.Dir); err != nil {
		t.Fatal(err)
	}
	parents := r.Git("rev-list", "--parents", "-n", "1", "HEAD")
	if len(strings.Fields(parents)) != 3 {
		t.Errorf("want two parents, got %q", parents)
	}
}

func TestStartFailsOnAnUnknownBranch(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	if _, err := Start(context.Background(), r.Dir, "no-such-branch"); err == nil {
		t.Fatal("want an error for a branch that does not exist")
	}
}

func TestStartRefusesWhenAMergeIsAlreadyInProgress(t *testing.T) {
	r := conflicting(t)
	if _, err := Start(context.Background(), r.Dir, "feature"); err != nil {
		t.Fatal(err)
	}
	if _, err := Start(context.Background(), r.Dir, "feature"); !errors.Is(err, ErrMergeInProgress) {
		t.Fatalf("err = %v, want ErrMergeInProgress", err)
	}
}

// A cherry-pick that stopped on a conflict leaves unmerged paths but no
// MERGE_HEAD. git then refuses the merge, and Start must say so rather than
// hand back the cherry-pick's conflicts as this merge's.
func TestStartDoesNotClaimSomeoneElsesConflicts(t *testing.T) {
	r := conflicting(t)
	r.Git("switch", "-q", "-c", "unrelated", "main")
	r.Commit("unrelated work")
	r.Git("switch", "-q", "main")
	pick := exec.Command("git", "-C", r.Dir, "cherry-pick", "feature")
	pick.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_EDITOR=true")
	if out, err := pick.CombinedOutput(); err == nil {
		t.Fatalf("cherry-pick applied cleanly, want a conflict:\n%s", out)
	}
	if u, _ := Unmerged(context.Background(), r.Dir); len(u) == 0 {
		t.Fatal("the cherry-pick left no unmerged paths")
	}

	got, err := Start(context.Background(), r.Dir, "unrelated")
	if err == nil {
		t.Fatalf("result = %+v, want an error: the unmerged paths belong to the cherry-pick", got)
	}
	if got.Outcome == Conflicted {
		t.Errorf("outcome = Conflicted with %v, want no result", got.Conflicts)
	}
}

// Message is git's prepared merge message without its comment lines (the
// "# Conflicts:" block), ready to edit.
func TestMessageDropsGitComments(t *testing.T) {
	r := conflicting(t)
	if _, err := Start(context.Background(), r.Dir, "feature"); err != nil {
		t.Fatal(err)
	}
	r.WriteFile(".git/MERGE_MSG", "Merge branch 'feature'\n\n# Conflicts:\n#\tgreeting.txt\n\n")
	got, err := Message(context.Background(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != "Merge branch 'feature'" {
		t.Errorf("message = %q", got)
	}
}

// Continue closes a merge with the message it is given, git's cleanup
// applied (comment lines and surrounding blank lines removed).
func TestContinueCommitsAMergeWithTheGivenMessage(t *testing.T) {
	r := conflicting(t)
	if _, err := Start(context.Background(), r.Dir, "feature"); err != nil {
		t.Fatal(err)
	}
	r.WriteFile("greeting.txt", "hi there\n")
	r.Git("add", "greeting.txt")
	if err := Continue(context.Background(), r.Dir, "Merge feature: greet warmly\n\nKeeps both greetings.\n# a comment\n"); err != nil {
		t.Fatal(err)
	}
	if got := r.Git("log", "-1", "--format=%B"); strings.TrimSpace(got) != "Merge feature: greet warmly\n\nKeeps both greetings." {
		t.Errorf("message = %q", got)
	}
}

// An empty message (or one made only of comments) is refused and the merge
// stays open.
func TestContinueRefusesAnEmptyMergeMessage(t *testing.T) {
	r := conflicting(t)
	if _, err := Start(context.Background(), r.Dir, "feature"); err != nil {
		t.Fatal(err)
	}
	r.WriteFile("greeting.txt", "hi there\n")
	r.Git("add", "greeting.txt")
	for _, msg := range []string{"", "  \n", "# only a comment\n"} {
		if err := Continue(context.Background(), r.Dir, msg); !errors.Is(err, ErrEmptyMessage) {
			t.Fatalf("Continue(%q) = %v, want ErrEmptyMessage", msg, err)
		}
	}
	if st := status(t, r.Dir); !st.Merging {
		t.Fatalf("state = %+v, want the merge still open", st)
	}
}

// Continue on a plain merge closes it as a merge commit.
func TestContinueClosesAMergeLikeCommit(t *testing.T) {
	r := conflicting(t)
	if _, err := Start(context.Background(), r.Dir, "feature"); err != nil {
		t.Fatal(err)
	}
	r.WriteFile("greeting.txt", "hi there\n")
	r.Git("add", "greeting.txt")
	if err := Continue(context.Background(), r.Dir, "Merge branch 'feature'"); err != nil {
		t.Fatal(err)
	}
	if st := status(t, r.Dir); st.Merging {
		t.Fatalf("state = %+v, want the merge closed", st)
	}
	parents := r.Git("rev-list", "--parents", "-n", "1", "HEAD")
	if len(strings.Fields(parents)) != 3 {
		t.Errorf("want two parents, got %q", parents)
	}
}

// Continue on a rebase runs rebase --continue; resolving the only conflict
// finishes it and the branch ends up on top of main, not merged into it.
func TestContinueFinishesARebase(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	r.Git("switch", "-q", "-c", "feature")
	r.WriteFile("a.txt", "feature change\n")
	r.Git("add", "a.txt")
	r.Git("commit", "-q", "-am", "feature change")
	r.Git("switch", "-q", "main")
	r.WriteFile("a.txt", "main change\n")
	r.Git("add", "a.txt")
	r.Git("commit", "-q", "-am", "main change")
	r.Git("switch", "-q", "feature")
	r.GitFails("rebase", "main")

	r.WriteFile("a.txt", "resolved\n")
	r.Git("add", "a.txt")
	// A rebase keeps the commit's own message; the one given is for merges.
	if err := Continue(context.Background(), r.Dir, "ignored"); err != nil {
		t.Fatal(err)
	}
	if st := status(t, r.Dir); st.Merging {
		t.Fatalf("state = %+v, want the rebase finished", st)
	}
	if got := r.Git("log", "-1", "--format=%s"); got != "feature change" {
		t.Errorf("subject = %q, want the rebased commit's own", got)
	}
	if got := r.Git("log", "-1", "--format=%P"); strings.Contains(got, " ") {
		t.Errorf("HEAD has more than one parent: %q, want a rebase, not a merge commit", got)
	}
}

// Continue must not swallow a real failure: calling it while the current
// commit's conflict is still unresolved leaves the sequencer exactly where
// it was, and that must come back as an error, not a silent success.
func TestContinueFailsWithAnUnresolvedConflict(t *testing.T) {
	r := conflicting(t)
	r.Git("switch", "-q", "feature")
	r.GitFails("rebase", "main")

	if err := Continue(context.Background(), r.Dir, ""); err == nil {
		t.Fatal("want an error: the conflict was never resolved")
	}
	if st := status(t, r.Dir); !st.Merging || st.Step != 1 {
		t.Fatalf("state = %+v, want still stopped on step 1", st)
	}
}

// TestContinueCancelledAfterAdvancingReportsCancelNotSuccess covers I2b: a
// cancel landing after --continue has committed the resolved step but while
// it is still working on the next one changes the fingerprint exactly the
// way a real "moved on to the next conflict" success would — the case the
// fingerprint check exists for — so it must be told apart before that check
// runs. A post-commit hook that sleeps only for the second commit's file
// guarantees the interrupt lands there, with the sequencer genuinely still
// in progress (not yet stopped on a real conflict).
func TestContinueCancelledAfterAdvancingReportsCancelNotSuccess(t *testing.T) {
	r := testrepo.New(t)
	r.WriteFile("f.txt", "base\n")
	r.Git("add", "f.txt")
	r.Git("commit", "-q", "-m", "base")
	r.Git("switch", "-q", "-c", "feature")
	r.WriteFile("f.txt", "feature1\n")
	r.Git("commit", "-q", "-am", "feature1")
	r.WriteFile("b.txt", "local2\n")
	r.Git("add", "b.txt")
	r.Git("commit", "-q", "-m", "local2")
	r.Git("switch", "-q", "main")
	r.WriteFile("f.txt", "mainchange\n")
	r.Git("commit", "-q", "-am", "mainchange")
	r.Git("switch", "-q", "feature")
	r.GitFails("rebase", "main")

	r.WriteFile("f.txt", "resolved\n")
	r.Git("add", "f.txt")

	hooks := t.TempDir()
	script := "#!/bin/sh\n" +
		"files=$(git diff-tree --no-commit-id --name-only -r HEAD)\n" +
		"if echo \"$files\" | grep -q '^b.txt$'; then\n  sleep 30\nfi\n"
	if err := os.WriteFile(filepath.Join(hooks, "post-commit"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	r.Git("config", "core.hooksPath", hooks)

	gitcmd.SetRecorder(&gitcmd.Recorder{Begin: func(s gitcmd.Start) int64 {
		if len(s.Args) > 0 && s.Args[len(s.Args)-1] == "--continue" {
			time.AfterFunc(300*time.Millisecond, s.Cancel)
		}
		return 0
	}})
	t.Cleanup(func() { gitcmd.SetRecorder(nil) })

	started := time.Now()
	err := Continue(context.Background(), r.Dir, "")
	if !errors.Is(err, gitcmd.ErrCancelled) {
		t.Fatalf("got %v, want an error wrapping ErrCancelled", err)
	}
	if d := time.Since(started); d > 10*time.Second {
		t.Fatalf("took %v: the hook was not interrupted", d)
	}
	// Whatever git's own sequencer bookkeeping now calls it, being cancelled
	// mid-cleanup must leave something for the conflict banner to show —
	// not the clean, fully-finished state a real success would leave.
	if st := status(t, r.Dir); !st.Merging {
		t.Fatalf("state = %+v, want the operation left in progress by the cancel", st)
	}
}

// Abort on a rebase restores the branch to where it was.
func TestAbortStopsARebase(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	r.Git("switch", "-q", "-c", "feature")
	before := r.Git("rev-parse", "HEAD")
	r.WriteFile("a.txt", "feature change\n")
	r.Git("add", "a.txt")
	r.Git("commit", "-q", "-am", "feature change")
	r.Git("switch", "-q", "main")
	r.WriteFile("a.txt", "main change\n")
	r.Git("add", "a.txt")
	r.Git("commit", "-q", "-am", "main change")
	r.Git("switch", "-q", "feature")
	r.GitFails("rebase", "main")

	if err := Abort(context.Background(), r.Dir); err != nil {
		t.Fatal(err)
	}
	if st := status(t, r.Dir); st.Merging {
		t.Fatalf("state = %+v, want the rebase aborted", st)
	}
	if got := r.Git("rev-parse", "HEAD"); got == before {
		t.Errorf("HEAD = %s, want the feature commit still on top, not the pre-rebase base", got)
	}
}

// A cherry-pick is continued and aborted by its own subcommand. Without
// this, the "no MERGE_HEAD, no rebase dir" shortcut would call it a stash
// conflict and both buttons would be silent no-ops.
func TestContinueAndAbortWorkOnACherryPick(t *testing.T) {
	pick := func(t *testing.T) *testrepo.Repo {
		t.Helper()
		r := testrepo.New(t)
		r.Commit("base")
		r.Git("switch", "-q", "-c", "feature")
		r.WriteFile("a.txt", "feature change\n")
		r.Git("add", "a.txt")
		r.Git("commit", "-q", "-am", "feature change")
		r.Git("switch", "-q", "main")
		r.WriteFile("a.txt", "main change\n")
		r.Git("add", "a.txt")
		r.Git("commit", "-q", "-am", "main change")
		r.GitFails("cherry-pick", "feature")
		return r
	}

	t.Run("continue", func(t *testing.T) {
		r := pick(t)
		r.WriteFile("a.txt", "resolved\n")
		r.Git("add", "a.txt")
		// This is the call that hangs forever if the editor is not
		// overridden through the environment: a cherry-pick --continue
		// opens one for the commit message.
		if err := Continue(context.Background(), r.Dir, ""); err != nil {
			t.Fatal(err)
		}
		if st := status(t, r.Dir); st.Merging {
			t.Fatalf("state = %+v, want the cherry-pick finished", st)
		}
	})

	t.Run("abort", func(t *testing.T) {
		r := pick(t)
		before := r.Git("rev-parse", "HEAD")
		if err := Abort(context.Background(), r.Dir); err != nil {
			t.Fatal(err)
		}
		if st := status(t, r.Dir); st.Merging {
			t.Fatalf("state = %+v, want the cherry-pick aborted", st)
		}
		if got := r.Git("rev-parse", "HEAD"); got != before {
			t.Errorf("HEAD = %s, want %s — abort must not move the branch", got, before)
		}
	})
}

// Continue and Abort on a stash conflict do nothing at the git level — there
// is nothing to continue or abort, only files to resolve or leave.
func TestContinueAndAbortOnAStashConflictAreNoOps(t *testing.T) {
	r := testrepo.New(t)
	r.WriteFile("a.txt", "one\n")
	r.Git("add", "a.txt")
	r.Git("commit", "-q", "-m", "base")
	r.WriteFile("a.txt", "stashed\n")
	r.Git("stash", "push", "-q", "-m", "wip")
	r.WriteFile("a.txt", "conflicting\n")
	r.Git("commit", "-q", "-am", "conflicting")
	r.GitFails("stash", "pop")

	if err := Continue(context.Background(), r.Dir, ""); err != nil {
		t.Fatal(err)
	}
	if err := Abort(context.Background(), r.Dir); err != nil {
		t.Fatal(err)
	}
	if st := status(t, r.Dir); st.Kind != KindStash {
		t.Fatalf("state = %+v, want the stash conflict untouched", st)
	}
}
