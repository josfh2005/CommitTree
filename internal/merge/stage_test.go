package merge

import (
	"context"
	"errors"
	"os"
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
