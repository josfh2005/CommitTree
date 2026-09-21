package stash_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"git-ui/internal/stash"
	"git-ui/internal/testrepo"
)

var ctx = context.Background()

func base(t *testing.T) *testrepo.Repo {
	t.Helper()
	r := testrepo.New(t)
	r.WriteFile("a.txt", "one\n")
	r.Git("add", "a.txt")
	r.Git("commit", "-q", "-m", "base")
	return r
}

func TestPushAndListRoundTrip(t *testing.T) {
	r := base(t)
	r.WriteFile("a.txt", "changed\n")

	if err := stash.Push(ctx, r.Dir, "work in progress", false); err != nil {
		t.Fatal(err)
	}
	if got := r.Git("status", "--porcelain"); got != "" {
		t.Errorf("status = %q, want a clean worktree after stashing", got)
	}
	entries, err := stash.List(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Index != 0 {
		t.Fatalf("entries = %+v, want one at index 0", entries)
	}
	if !strings.Contains(entries[0].Message, "work in progress") {
		t.Errorf("message = %q, missing the stash message", entries[0].Message)
	}
	if entries[0].Branch != "main" {
		t.Errorf("branch = %q, want main", entries[0].Branch)
	}
}

func TestPushIncludesUntrackedWhenAsked(t *testing.T) {
	r := base(t)
	r.WriteFile("new.txt", "untracked\n")

	if err := stash.Push(ctx, r.Dir, "with untracked", true); err != nil {
		t.Fatal(err)
	}
	if got := r.Git("status", "--porcelain"); got != "" {
		t.Errorf("status = %q, want the untracked file stashed away too", got)
	}
}

func TestApplyKeepsTheStashEntry(t *testing.T) {
	r := base(t)
	r.WriteFile("a.txt", "changed\n")
	if err := stash.Push(ctx, r.Dir, "wip", false); err != nil {
		t.Fatal(err)
	}
	if err := stash.Apply(ctx, r.Dir, 0); err != nil {
		t.Fatal(err)
	}
	entries, err := stash.List(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("entries = %+v, want the stash still there after Apply", entries)
	}
}

func TestPopRemovesTheStashEntry(t *testing.T) {
	r := base(t)
	r.WriteFile("a.txt", "changed\n")
	if err := stash.Push(ctx, r.Dir, "wip", false); err != nil {
		t.Fatal(err)
	}
	if err := stash.Pop(ctx, r.Dir, 0); err != nil {
		t.Fatal(err)
	}
	entries, err := stash.List(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("entries = %+v, want none after Pop", entries)
	}
}

func TestDrop(t *testing.T) {
	r := base(t)
	r.WriteFile("a.txt", "changed\n")
	if err := stash.Push(ctx, r.Dir, "wip", false); err != nil {
		t.Fatal(err)
	}
	if err := stash.Drop(ctx, r.Dir, 0); err != nil {
		t.Fatal(err)
	}
	entries, err := stash.List(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("entries = %+v, want none after Drop", entries)
	}
}

func TestDiffShowsTheStashedChange(t *testing.T) {
	r := base(t)
	r.WriteFile("a.txt", "changed\n")
	if err := stash.Push(ctx, r.Dir, "wip", false); err != nil {
		t.Fatal(err)
	}
	out, err := stash.Diff(ctx, r.Dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "changed") {
		t.Errorf("diff = %q, missing the change", out)
	}
}

// A repository that has never stashed has no refs/stash at all; one that
// stashed and dropped everything has an empty one. Both must read as an
// empty list, not as an error.
func TestListOfNoStashesIsEmptyNotNil(t *testing.T) {
	r := base(t)
	entries, err := stash.List(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if entries == nil || len(entries) != 0 {
		t.Errorf("entries = %#v, want an empty, non-nil slice", entries)
	}

	r.WriteFile("a.txt", "changed\n")
	if err := stash.Push(ctx, r.Dir, "wip", false); err != nil {
		t.Fatal(err)
	}
	if err := stash.Drop(ctx, r.Dir, 0); err != nil {
		t.Fatal(err)
	}
	entries, err = stash.List(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("entries = %#v, want an empty list once every stash is dropped", entries)
	}
}

// List must carry a usable hash — the whole reason it reads the reflog
// instead of `git stash list`.
func TestListCarriesTheStashCommitHash(t *testing.T) {
	r := base(t)
	r.WriteFile("a.txt", "changed\n")
	if err := stash.Push(ctx, r.Dir, "wip", false); err != nil {
		t.Fatal(err)
	}
	entries, err := stash.List(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %+v, want one", entries)
	}
	if entries[0].Hash != strings.TrimSpace(r.Git("rev-parse", "refs/stash")) {
		t.Errorf("hash = %q, want refs/stash", entries[0].Hash)
	}
}

func TestPushWithNothingToStash(t *testing.T) {
	r := base(t)
	if err := stash.Push(ctx, r.Dir, "wip", false); !errors.Is(err, stash.ErrNothingToStash) {
		t.Errorf("err = %v, want ErrNothingToStash", err)
	}
	entries, err := stash.List(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("entries = %+v, want none", entries)
	}
}
