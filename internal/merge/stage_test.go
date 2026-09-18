package merge

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"git-ui/internal/testrepo"
)

// resolvedMerge is a merge of feature into main whose one conflict,
// greeting.txt, has been resolved and staged, and which also brings in a file
// only feature added, new.txt, staged cleanly by git.
func resolvedMerge(t *testing.T) *testrepo.Repo {
	t.Helper()
	r := conflicting(t)
	r.Git("switch", "-q", "feature")
	r.WriteFile("new.txt", "brand new\n")
	r.Git("add", "new.txt")
	r.Git("commit", "-q", "-m", "add new")
	r.Git("switch", "-q", "main")
	if _, err := Start(context.Background(), r.Dir, "feature"); err != nil {
		t.Fatal(err)
	}
	r.WriteFile("greeting.txt", "hi there\n")
	r.Git("add", "greeting.txt")
	return r
}

func status(t *testing.T, dir string) State {
	t.Helper()
	st, err := Status(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestStatusListsStagedFiles(t *testing.T) {
	r := resolvedMerge(t)
	st := status(t, r.Dir)
	if !slices.Equal(st.Staged, []string{"greeting.txt", "new.txt"}) {
		t.Errorf("staged = %v, want greeting.txt and new.txt", st.Staged)
	}
	if len(st.Unstaged) != 0 {
		t.Errorf("unstaged = %v, want none", st.Unstaged)
	}
}

// An unmerged file is a conflict, not a staged or unstaged one.
func TestStatusKeepsUnmergedFilesOutOfStagedAndUnstaged(t *testing.T) {
	r := conflicting(t)
	if _, err := Start(context.Background(), r.Dir, "feature"); err != nil {
		t.Fatal(err)
	}
	st := status(t, r.Dir)
	if len(st.Staged) != 0 || len(st.Unstaged) != 0 {
		t.Errorf("staged = %v, unstaged = %v, want both empty", st.Staged, st.Unstaged)
	}
}

func TestUnstageKeepsTheContent(t *testing.T) {
	r := resolvedMerge(t)
	if err := Unstage(context.Background(), r.Dir, "greeting.txt"); err != nil {
		t.Fatal(err)
	}
	st := status(t, r.Dir)
	if !slices.Equal(st.Unstaged, []string{"greeting.txt"}) || !slices.Equal(st.Staged, []string{"new.txt"}) {
		t.Errorf("staged = %v, unstaged = %v", st.Staged, st.Unstaged)
	}
	data, err := os.ReadFile(filepath.Join(r.Dir, "greeting.txt"))
	if err != nil || string(data) != "hi there\n" {
		t.Errorf("greeting.txt = %q, %v; want the resolution kept", data, err)
	}
}

// A file the merge adds is not in HEAD, so unstaging it would make it
// untracked and drop it from the view — and from the merge commit — unseen.
func TestUnstageKeepsANewFileVisible(t *testing.T) {
	r := resolvedMerge(t)
	if err := Unstage(context.Background(), r.Dir, "new.txt"); err != nil {
		t.Fatal(err)
	}
	st := status(t, r.Dir)
	if !slices.Contains(st.Unstaged, "new.txt") {
		t.Errorf("unstaged = %v, want new.txt listed", st.Unstaged)
	}
	if _, err := os.Stat(filepath.Join(r.Dir, "new.txt")); err != nil {
		t.Errorf("new.txt is gone: %v", err)
	}
}

func TestStageAfterUnstageRestoresIt(t *testing.T) {
	r := resolvedMerge(t)
	for _, path := range []string{"greeting.txt", "new.txt"} {
		if err := Unstage(context.Background(), r.Dir, path); err != nil {
			t.Fatal(err)
		}
		if err := Stage(context.Background(), r.Dir, path); err != nil {
			t.Fatal(err)
		}
	}
	st := status(t, r.Dir)
	if !slices.Equal(st.Staged, []string{"greeting.txt", "new.txt"}) || len(st.Unstaged) != 0 {
		t.Errorf("staged = %v, unstaged = %v", st.Staged, st.Unstaged)
	}
}

func TestStageRefusesAFileWithMarkers(t *testing.T) {
	r := resolvedMerge(t)
	if err := Unstage(context.Background(), r.Dir, "greeting.txt"); err != nil {
		t.Fatal(err)
	}
	r.WriteFile("greeting.txt", "<<<<<<< HEAD\nhi\n=======\nhola\n>>>>>>> feature\n")
	if err := Stage(context.Background(), r.Dir, "greeting.txt"); !errors.Is(err, ErrMarkersLeft) {
		t.Fatalf("err = %v, want ErrMarkersLeft", err)
	}
	if st := status(t, r.Dir); slices.Contains(st.Staged, "greeting.txt") {
		t.Error("greeting.txt was staged with markers in it")
	}
}

// Paths must be ones git itself listed. Pathspec magic, a path from the wrong
// list and a path outside the merge are all refused, and nothing changes.
func TestStageAndUnstageAcceptOnlyListedPaths(t *testing.T) {
	r := resolvedMerge(t)
	r.WriteFile("other.txt", "untracked\n")
	for _, path := range []string{":(glob)*", "*", "greeting.txt", "other.txt", "../escape.txt", ""} {
		if err := Stage(context.Background(), r.Dir, path); !errors.Is(err, ErrNotInMerge) {
			t.Errorf("Stage(%q) = %v, want ErrNotInMerge", path, err)
		}
	}
	for _, path := range []string{":(glob)*", "*", "other.txt", "../escape.txt", ""} {
		if err := Unstage(context.Background(), r.Dir, path); !errors.Is(err, ErrNotInMerge) {
			t.Errorf("Unstage(%q) = %v, want ErrNotInMerge", path, err)
		}
	}
	st := status(t, r.Dir)
	if !slices.Equal(st.Staged, []string{"greeting.txt", "new.txt"}) || len(st.Unstaged) != 0 {
		t.Errorf("state changed: staged = %v, unstaged = %v", st.Staged, st.Unstaged)
	}
}

func TestStageRefusesWhenNotMerging(t *testing.T) {
	r := testrepo.New(t)
	r.WriteFile("a.txt", "a\n")
	r.Git("add", "a.txt")
	r.Git("commit", "-q", "-m", "a")
	r.WriteFile("a.txt", "changed\n")
	if err := Stage(context.Background(), r.Dir, "a.txt"); !errors.Is(err, ErrNotInMerge) {
		t.Errorf("err = %v, want ErrNotInMerge", err)
	}
}

// A merge may start with unrelated uncommitted work. It is not the merge's,
// so it must not be listed as a merge file waiting to be staged.
func TestStatusLeavesUnrelatedWorkOutOfUnstaged(t *testing.T) {
	r := conflicting(t)
	r.WriteFile("wip.txt", "committed\n")
	r.Git("add", "wip.txt")
	r.Git("commit", "-q", "-m", "wip file")
	r.WriteFile("wip.txt", "work in progress\n")
	if _, err := Start(context.Background(), r.Dir, "feature"); err != nil {
		t.Fatal(err)
	}
	if st := status(t, r.Dir); slices.Contains(st.Unstaged, "wip.txt") {
		t.Errorf("unstaged = %v, lists unrelated work", st.Unstaged)
	}
}

// conflictingWith is conflicting (greeting.txt conflicts) with extra files
// committed on the common base by base, and extra changes made on feature by
// theirs, then starts the merge of feature into main.
func conflictingWith(t *testing.T, base, theirs func(r *testrepo.Repo)) *testrepo.Repo {
	t.Helper()
	r := testrepo.New(t)
	r.WriteFile("greeting.txt", "hello\n")
	base(r)
	r.Git("add", "-A")
	r.Git("commit", "-q", "-m", "base")
	r.Git("switch", "-q", "-c", "feature")
	r.WriteFile("greeting.txt", "hola\n")
	theirs(r)
	r.Git("add", "-A")
	r.Git("commit", "-q", "-m", "theirs")
	r.Git("switch", "-q", "main")
	r.WriteFile("greeting.txt", "hi\n")
	r.Git("commit", "-q", "-am", "ours")
	if _, err := Start(context.Background(), r.Dir, "feature"); err != nil {
		t.Fatal(err)
	}
	return r
}

// A rename is a deletion plus an addition; both sides must be listed, or the
// deletion goes into the merge commit unseen.
func TestStatusListsBothSidesOfARename(t *testing.T) {
	r := conflictingWith(t,
		func(r *testrepo.Repo) { r.WriteFile("old.txt", "some content that stays the same\n") },
		func(r *testrepo.Repo) { r.Git("mv", "old.txt", "new-name.txt") })
	st := status(t, r.Dir)
	if !slices.Contains(st.Staged, "old.txt") || !slices.Contains(st.Staged, "new-name.txt") {
		t.Errorf("staged = %v, want both old.txt and new-name.txt", st.Staged)
	}
}

// dirToFile is a merge in which feature replaced directory d/ with a file d.
func dirToFile(t *testing.T) *testrepo.Repo {
	t.Helper()
	return conflictingWith(t,
		func(r *testrepo.Repo) { r.WriteFile("d/x", "inside\n") },
		func(r *testrepo.Repo) {
			r.Git("rm", "-q", "d/x")
			r.WriteFile("d", "now a file\n")
		})
}

// A literal pathspec "d" still matches everything under d/, so acting on it
// would reach d/x too. It is refused rather than guessed at.
func TestStageAndUnstageRefuseAPathThatIsAlsoADirectoryInTheMerge(t *testing.T) {
	r := dirToFile(t)
	before := status(t, r.Dir)
	if !slices.Contains(before.Staged, "d") || !slices.Contains(before.Staged, "d/x") {
		t.Fatalf("staged = %v, want d and d/x", before.Staged)
	}
	if err := Unstage(context.Background(), r.Dir, "d"); !errors.Is(err, ErrNotInMerge) {
		t.Errorf("Unstage(d) = %v, want ErrNotInMerge", err)
	}
	if after := status(t, r.Dir); !slices.Equal(after.Staged, before.Staged) {
		t.Errorf("staged changed: %v -> %v", before.Staged, after.Staged)
	}
}

// Unstaging a new file whose worktree copy is gone would leave its content
// nowhere but the dropped index entry.
func TestUnstageRefusesANewFileWithNoWorktreeCopy(t *testing.T) {
	r := resolvedMerge(t)
	if err := os.Remove(filepath.Join(r.Dir, "new.txt")); err != nil {
		t.Fatal(err)
	}
	if err := Unstage(context.Background(), r.Dir, "new.txt"); err == nil {
		t.Error("want an error: unstaging would lose new.txt's only copy")
	}
	if st := status(t, r.Dir); !slices.Contains(st.Staged, "new.txt") {
		t.Errorf("staged = %v, want new.txt still staged", st.Staged)
	}
}

// With unrelated histories there is no merge base for HEAD...MERGE_HEAD;
// Status must still answer rather than take the merge view down.
func TestStatusWorksWithoutAMergeBase(t *testing.T) {
	r := testrepo.New(t)
	r.WriteFile("a.txt", "ours\n")
	r.Git("add", "a.txt")
	r.Git("commit", "-q", "-m", "ours")
	r.Git("switch", "-q", "--orphan", "other")
	r.WriteFile("a.txt", "theirs\n")
	r.WriteFile("b.txt", "theirs only\n")
	r.Git("add", "a.txt", "b.txt")
	r.Git("commit", "-q", "-m", "theirs")
	r.Git("switch", "-q", "main")
	cmd := exec.Command("git", "-C", r.Dir, "merge", "--allow-unrelated-histories", "other")
	_ = cmd.Run() // conflicts on a.txt, which is the point
	st := status(t, r.Dir)
	if !st.Merging || !slices.Contains(st.Staged, "b.txt") {
		t.Errorf("state = %+v, want merging with b.txt staged", st)
	}
}
