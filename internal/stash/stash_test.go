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

func TestFilesListsATrackedChange(t *testing.T) {
	r := base(t)
	r.WriteFile("a.txt", "changed\n")
	if err := stash.Push(ctx, r.Dir, "wip", false); err != nil {
		t.Fatal(err)
	}
	files, err := stash.Files(ctx, r.Dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("files = %+v, want one", files)
	}
	if files[0].Path != "a.txt" {
		t.Errorf("path = %q, want a.txt", files[0].Path)
	}
	if files[0].Status != "M" {
		t.Errorf("status = %q, want M", files[0].Status)
	}
	if files[0].Untracked {
		t.Error("Untracked = true, want false for a tracked change")
	}
}

func TestFilesIncludesAnUntrackedFile(t *testing.T) {
	r := base(t)
	r.WriteFile("a.txt", "changed\n")
	r.WriteFile("new.txt", "brand new\n")
	if err := stash.Push(ctx, r.Dir, "with untracked", true); err != nil {
		t.Fatal(err)
	}
	files, err := stash.Files(ctx, r.Dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("files = %+v, want two", files)
	}
	var tracked, untracked *stash.File
	for i := range files {
		switch files[i].Path {
		case "a.txt":
			tracked = &files[i]
		case "new.txt":
			untracked = &files[i]
		}
	}
	if tracked == nil || tracked.Untracked {
		t.Errorf("tracked = %+v, want a.txt marked tracked", tracked)
	}
	if untracked == nil || !untracked.Untracked {
		t.Errorf("untracked = %+v, want new.txt marked untracked", untracked)
	}
}

func TestFilesOnAStashOfOnlyUntrackedFiles(t *testing.T) {
	r := base(t)
	r.WriteFile("new.txt", "brand new\n")
	if err := stash.Push(ctx, r.Dir, "only untracked", true); err != nil {
		t.Fatal(err)
	}
	files, err := stash.Files(ctx, r.Dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("files = %+v, want one", files)
	}
	if files[0].Path != "new.txt" || !files[0].Untracked {
		t.Errorf("files[0] = %+v, want new.txt marked untracked", files[0])
	}
}

func TestFileDiffOfATrackedFile(t *testing.T) {
	r := base(t)
	r.WriteFile("a.txt", "changed\n")
	if err := stash.Push(ctx, r.Dir, "wip", false); err != nil {
		t.Fatal(err)
	}
	out, err := stash.FileDiff(ctx, r.Dir, 0, "a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "changed") {
		t.Errorf("diff = %q, missing the change", out)
	}
}

func TestFileDiffOfAnUntrackedFile(t *testing.T) {
	r := base(t)
	r.WriteFile("new.txt", "brand new content\n")
	if err := stash.Push(ctx, r.Dir, "with untracked", true); err != nil {
		t.Fatal(err)
	}
	out, err := stash.FileDiff(ctx, r.Dir, 0, "new.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "brand new content") {
		t.Errorf("diff = %q, missing the untracked file's content", out)
	}
}

// git diff --name-status reports a rename as three tab-separated fields
// ("R070\told.txt\tnew.txt"), not two — Files must not fold the old and new
// paths together into one garbled Path, and the similarity score must not
// leak into Status.
func TestFilesParsesARename(t *testing.T) {
	// Git only detects a rename above its default 50% similarity threshold —
	// base(t)'s one-line a.txt is too small a base for that once a line is
	// added, so this writes a longer file of its own to stay well past it.
	r := base(t)
	r.WriteFile("a.txt", "one\ntwo\nthree\n")
	r.Git("commit", "-q", "-am", "grow a.txt")
	r.Git("mv", "a.txt", "renamed.txt")
	r.WriteFile("renamed.txt", "one\ntwo\nthree\nmore\n")
	if err := stash.Push(ctx, r.Dir, "wip", false); err != nil {
		t.Fatal(err)
	}
	files, err := stash.Files(ctx, r.Dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("files = %+v, want one", files)
	}
	f := files[0]
	if f.Path != "renamed.txt" {
		t.Errorf("path = %q, want renamed.txt (not the tab-joined old+new)", f.Path)
	}
	if f.OldPath != "a.txt" {
		t.Errorf("oldPath = %q, want a.txt", f.OldPath)
	}
	if f.Status != "R" {
		t.Errorf("status = %q, want a bare R, not the similarity score", f.Status)
	}
}

// FileDiff of a renamed file must show the actual content delta, not the
// whole new file rendered as a fresh addition — which is what a pathspec of
// only the new path gives once git has already decided it's a rename.
func TestFileDiffOfARename(t *testing.T) {
	r := base(t)
	r.WriteFile("a.txt", "one\ntwo\nthree\n")
	r.Git("commit", "-q", "-am", "grow a.txt")
	r.Git("mv", "a.txt", "renamed.txt")
	r.WriteFile("renamed.txt", "one\ntwo\nthree\nmore\n")
	if err := stash.Push(ctx, r.Dir, "wip", false); err != nil {
		t.Fatal(err)
	}
	out, err := stash.FileDiff(ctx, r.Dir, 0, "renamed.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "+more") {
		t.Errorf("diff = %q, missing the actual added line", out)
	}
	if strings.Contains(out, "+one") {
		t.Errorf("diff = %q, rendered the whole file as an addition instead of a rename delta", out)
	}
}

// A stash created outside the app (a bare terminal `git stash push`, no -m)
// gets git's own "WIP on <branch>: <hash> <subject>" message instead of the
// "On <branch>: <message>" shape Push always produces. List must still split
// out the right branch and a usable message for it.
func TestListParsesAStashWithoutACustomMessage(t *testing.T) {
	r := base(t)
	r.WriteFile("a.txt", "changed\n")
	r.Git("stash", "push", "-q")

	entries, err := stash.List(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %+v, want one", entries)
	}
	if entries[0].Branch != "main" {
		t.Errorf("branch = %q, want main", entries[0].Branch)
	}
	if entries[0].Message == "" {
		t.Errorf("message = %q, want a non-empty message", entries[0].Message)
	}
}
