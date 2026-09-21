package worktree_test

import (
	"errors"
	"strings"
	"testing"

	"git-ui/internal/testrepo"
	"git-ui/internal/worktree"
)

func TestCommitStagedChanges(t *testing.T) {
	r := base(t)
	r.WriteFile("a.txt", "changed\n")
	r.Git("add", "a.txt")

	if err := worktree.Commit(ctx, r.Dir, "change a", false); err != nil {
		t.Fatal(err)
	}
	if got := r.Git("log", "-1", "--pretty=%s"); got != "change a" {
		t.Errorf("subject = %q", got)
	}
	if st := status(t, r.Dir); len(st.Staged) != 0 {
		t.Errorf("staged = %v after committing", paths(st.Staged))
	}
}

// A real message has a subject, a blank line, a body, and often quotes —
// none of which may reach git through a command line.
func TestCommitKeepsAMultiLineMessageIntact(t *testing.T) {
	r := base(t)
	r.WriteFile("a.txt", "changed\n")
	r.Git("add", "a.txt")
	message := "fix: don't \"quote\" the shell\n\nIt broke on `backticks` and $VARS.\nSecond line.\n"

	if err := worktree.Commit(ctx, r.Dir, message, false); err != nil {
		t.Fatal(err)
	}
	got := r.Git("log", "-1", "--pretty=%B")
	for _, want := range []string{`don't "quote" the shell`, "`backticks` and $VARS", "Second line."} {
		if !strings.Contains(got, want) {
			t.Errorf("message = %q, missing %q", got, want)
		}
	}
}

func TestCommitRefusesWithNothingStaged(t *testing.T) {
	r := base(t)
	r.WriteFile("a.txt", "changed but not staged\n")
	if err := worktree.Commit(ctx, r.Dir, "nope", false); !errors.Is(err, worktree.ErrNothingStaged) {
		t.Errorf("err = %v, want ErrNothingStaged", err)
	}
}

func TestCommitRefusesAnEmptyMessage(t *testing.T) {
	r := base(t)
	r.WriteFile("a.txt", "changed\n")
	r.Git("add", "a.txt")
	if err := worktree.Commit(ctx, r.Dir, "   \n", false); err == nil {
		t.Error("want an error for a blank message")
	}
}

func TestAmendReplacesTheLastCommit(t *testing.T) {
	r := base(t)
	before := r.Git("rev-parse", "HEAD")
	r.WriteFile("a.txt", "amended\n")
	r.Git("add", "a.txt")

	if err := worktree.Commit(ctx, r.Dir, "add a, properly", true); err != nil {
		t.Fatal(err)
	}
	if got := r.Git("log", "-1", "--pretty=%s"); got != "add a, properly" {
		t.Errorf("subject = %q", got)
	}
	if got := r.Git("rev-list", "--count", "HEAD"); got != "1" {
		t.Errorf("commit count = %s, want the commit replaced, not added", got)
	}
	if r.Git("rev-parse", "HEAD") == before {
		t.Error("HEAD did not move")
	}
}

// Amending with nothing staged only rewrites the message, which is the most
// common use of it.
func TestAmendWithNothingStagedRewritesTheMessage(t *testing.T) {
	r := base(t)
	if err := worktree.Commit(ctx, r.Dir, "better subject", true); err != nil {
		t.Fatal(err)
	}
	if got := r.Git("log", "-1", "--pretty=%s"); got != "better subject" {
		t.Errorf("subject = %q", got)
	}
}

func TestPreviewCountsStagedFilesAndOffersTheLastMessage(t *testing.T) {
	r := base(t)
	r.WriteFile("a.txt", "changed\n")
	r.Git("add", "a.txt")
	r.WriteFile("b.txt", "new\n")
	r.Git("add", "b.txt")

	info, err := worktree.Preview(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.StagedCount != 2 || !info.CanAmend || info.LastMessage != "add a" {
		t.Errorf("info = %+v", info)
	}
	if info.Pushed || info.Upstream != "" {
		t.Errorf("info = %+v, want no upstream", info)
	}
}

func TestPreviewCannotAmendAnEmptyRepository(t *testing.T) {
	r := testrepo.New(t)
	info, err := worktree.Preview(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.CanAmend {
		t.Errorf("info = %+v, want CanAmend false with no commits", info)
	}
}

// Amending a commit the upstream already has rewrites shared history; the
// dialog needs to know.
func TestPreviewReportsAPushedCommit(t *testing.T) {
	src := testrepo.New(t)
	src.Commit("base")
	bare := testrepo.NewBareFrom(t, src)
	clone := testrepo.Clone(t, bare)

	info, err := worktree.Preview(ctx, clone.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Pushed || info.Upstream != "origin/main" {
		t.Errorf("info = %+v, want pushed on origin/main", info)
	}
}
