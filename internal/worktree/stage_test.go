package worktree_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"testing"

	"git-ui/internal/worktree"
)

func read(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestStageAndUnstageATrackedFile(t *testing.T) {
	r := base(t)
	r.WriteFile("a.txt", "changed\n")

	if err := worktree.Stage(ctx, r.Dir, "a.txt"); err != nil {
		t.Fatal(err)
	}
	if st := status(t, r.Dir); !slices.Equal(paths(st.Staged), []string{"a.txt"}) || len(st.Unstaged) != 0 {
		t.Fatalf("after Stage: staged = %v, unstaged = %v", paths(st.Staged), paths(st.Unstaged))
	}
	if err := worktree.Unstage(ctx, r.Dir, "a.txt"); err != nil {
		t.Fatal(err)
	}
	st := status(t, r.Dir)
	if len(st.Staged) != 0 || !slices.Equal(paths(st.Unstaged), []string{"a.txt"}) {
		t.Errorf("after Unstage: staged = %v, unstaged = %v", paths(st.Staged), paths(st.Unstaged))
	}
	if got := read(t, r.Dir, "a.txt"); got != "changed\n" {
		t.Errorf("a.txt = %q, want the content kept", got)
	}
}

func TestStageAnUntrackedFileThenUnstageReturnsItToUntracked(t *testing.T) {
	r := base(t)
	r.WriteFile("new.txt", "hello\n")

	if err := worktree.Stage(ctx, r.Dir, "new.txt"); err != nil {
		t.Fatal(err)
	}
	if st := status(t, r.Dir); !slices.Equal(paths(st.Staged), []string{"new.txt"}) {
		t.Fatalf("staged = %v", paths(st.Staged))
	}
	if err := worktree.Unstage(ctx, r.Dir, "new.txt"); err != nil {
		t.Fatal(err)
	}
	st := status(t, r.Dir)
	if !slices.Equal(paths(st.Untracked), []string{"new.txt"}) {
		t.Errorf("untracked = %v, want new.txt back", paths(st.Untracked))
	}
	if got := read(t, r.Dir, "new.txt"); got != "hello\n" {
		t.Errorf("new.txt = %q, want the file kept", got)
	}
}

func TestStageADeletion(t *testing.T) {
	r := base(t)
	if err := os.Remove(filepath.Join(r.Dir, "a.txt")); err != nil {
		t.Fatal(err)
	}
	if err := worktree.Stage(ctx, r.Dir, "a.txt"); err != nil {
		t.Fatal(err)
	}
	st := status(t, r.Dir)
	if len(st.Staged) != 1 || st.Staged[0].Status != "D" {
		t.Errorf("staged = %+v, want the deletion staged", st.Staged)
	}
}

func TestDiscardRestoresAnUnstagedChange(t *testing.T) {
	r := base(t)
	r.WriteFile("a.txt", "changed\n")

	if err := worktree.Discard(ctx, r.Dir, "a.txt"); err != nil {
		t.Fatal(err)
	}
	if got := read(t, r.Dir, "a.txt"); got != "one\n" {
		t.Errorf("a.txt = %q, want the committed content", got)
	}
	if st := status(t, r.Dir); len(st.Unstaged) != 0 {
		t.Errorf("unstaged = %v, want none", paths(st.Unstaged))
	}
}

// Discarding a file that is staged AND modified throws away both, which is
// why the dialog says so.
func TestDiscardThrowsAwayTheStagedChangeToo(t *testing.T) {
	r := base(t)
	r.WriteFile("a.txt", "staged\n")
	r.Git("add", "a.txt")
	r.WriteFile("a.txt", "and edited\n")

	if err := worktree.Discard(ctx, r.Dir, "a.txt"); err != nil {
		t.Fatal(err)
	}
	if got := read(t, r.Dir, "a.txt"); got != "one\n" {
		t.Errorf("a.txt = %q, want the committed content", got)
	}
	st := status(t, r.Dir)
	if len(st.Staged) != 0 || len(st.Unstaged) != 0 {
		t.Errorf("staged = %v, unstaged = %v, want both empty", paths(st.Staged), paths(st.Unstaged))
	}
}

func TestDiscardDeletesAnUntrackedFile(t *testing.T) {
	r := base(t)
	r.WriteFile("new.txt", "unrecoverable\n")

	if err := worktree.Discard(ctx, r.Dir, "new.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(r.Dir, "new.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("new.txt still exists: %v", err)
	}
}

// Pathspec magic, a stranger path and an empty path are refused by every
// entry point, and nothing changes.
func TestOperationsAcceptOnlyListedPaths(t *testing.T) {
	r := base(t)
	r.WriteFile("a.txt", "changed\n")
	r.WriteFile("untouched.txt", "x\n") // untracked, but never listed to the op under test

	for _, op := range []struct {
		name string
		fn   func(path string) error
	}{
		{"Stage", func(p string) error { return worktree.Stage(ctx, r.Dir, p) }},
		{"Unstage", func(p string) error { return worktree.Unstage(ctx, r.Dir, p) }},
		{"Discard", func(p string) error { return worktree.Discard(ctx, r.Dir, p) }},
	} {
		for _, path := range []string{":(glob)*", "*", "", "../escape.txt", "nonexistent.txt"} {
			if err := op.fn(path); !errors.Is(err, worktree.ErrNotInWorktree) {
				t.Errorf("%s(%q) = %v, want ErrNotInWorktree", op.name, path, err)
			}
		}
	}
	if got := read(t, r.Dir, "untouched.txt"); got != "x\n" {
		t.Errorf("untouched.txt = %q, an operation reached it", got)
	}
}

// Unstage only applies to what is staged, Stage to what is not.
func TestOperationsRefuseAPathFromTheWrongList(t *testing.T) {
	r := base(t)
	r.WriteFile("a.txt", "changed\n")
	if err := worktree.Unstage(ctx, r.Dir, "a.txt"); !errors.Is(err, worktree.ErrNotInWorktree) {
		t.Errorf("Unstage of an unstaged file = %v, want ErrNotInWorktree", err)
	}
}

// A literal pathspec still matches everything under a directory of that name.
//
// CONTROLLER RULING: the brief's version of this test also wrote a file named
// "d" right after creating a directory named "d", which cannot exist (a
// directory and a file cannot share one path) and would abort the test before
// the assertion. That step is dropped; only the directory case is asserted.
func TestDiscardRefusesAPathThatIsAlsoADirectory(t *testing.T) {
	r := base(t)
	if err := os.MkdirAll(filepath.Join(r.Dir, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	r.WriteFile("d/inside.txt", "keep me\n")

	if err := worktree.Discard(ctx, r.Dir, "d"); !errors.Is(err, worktree.ErrNotInWorktree) {
		t.Errorf("Discard(d) = %v, want ErrNotInWorktree", err)
	}
	if got := read(t, r.Dir, "d/inside.txt"); got != "keep me\n" {
		t.Errorf("d/inside.txt = %q, it was reached", got)
	}
}

// A FIFO must never be opened or followed; deleting is fine, reading is not.
func TestDiscardRefusesSomethingThatIsNotARegularFile(t *testing.T) {
	r := base(t)
	fifo := filepath.Join(r.Dir, "pipe")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	if err := worktree.Discard(ctx, r.Dir, "pipe"); err == nil {
		t.Error("want a refusal for a FIFO")
	}
	if _, err := os.Lstat(fifo); err != nil {
		t.Errorf("the FIFO was removed: %v", err)
	}
}
