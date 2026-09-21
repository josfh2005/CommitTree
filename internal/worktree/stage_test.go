package worktree_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"testing"

	"git-ui/internal/testrepo"
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

// A staged new file whose worktree copy is gone has its only surviving copy
// in the index. Unstaging it would drop the index entry and leave the path
// in no list and nowhere on disk, so it must be refused instead.
func TestUnstageRefusesANewFileWhoseWorktreeCopyIsGone(t *testing.T) {
	r := base(t)
	r.WriteFile("n.txt", "only copy\n")
	r.Git("add", "n.txt")
	if err := os.Remove(filepath.Join(r.Dir, "n.txt")); err != nil {
		t.Fatal(err)
	}

	if err := worktree.Unstage(ctx, r.Dir, "n.txt"); err == nil {
		t.Error("want a refusal; unstaging would lose the only copy")
	}
	st := status(t, r.Dir)
	if !slices.Contains(paths(st.Staged), "n.txt") {
		t.Errorf("staged = %v, want n.txt still staged after the refusal", paths(st.Staged))
	}
}

// Discarding a staged rename must restore the original, not delete the file:
// restoring only the new path leaves it with no HEAD entry to fall back to.
func TestDiscardOfAStagedRenameRestoresTheOriginal(t *testing.T) {
	r := base(t)
	r.Git("mv", "a.txt", "b.txt")

	if err := worktree.Discard(ctx, r.Dir, "b.txt"); err != nil {
		t.Fatal(err)
	}
	if got := read(t, r.Dir, "a.txt"); got != "one\n" {
		t.Errorf("a.txt = %q, want the committed content restored", got)
	}
	if _, err := os.Lstat(filepath.Join(r.Dir, "b.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("b.txt still exists: %v", err)
	}
	st := status(t, r.Dir)
	if len(st.Staged) != 0 || len(st.Unstaged) != 0 || len(st.Untracked) != 0 {
		t.Errorf("state = %+v, want everything clean", st)
	}
}

// Unstaging a rename must leave the whole rename together: restoring only the
// new path from HEAD (which doesn't have it) used to drop it from the index
// entirely, splitting the rename into a deletion plus an untracked file.
func TestUnstageOfARenameKeepsBothPathsTogether(t *testing.T) {
	r := base(t)
	r.Git("mv", "a.txt", "b.txt")

	if err := worktree.Unstage(ctx, r.Dir, "b.txt"); err != nil {
		t.Fatal(err)
	}
	if got := read(t, r.Dir, "b.txt"); got != "one\n" {
		t.Errorf("b.txt = %q, want the renamed file kept in the working tree", got)
	}
	st := status(t, r.Dir)
	if len(st.Staged) != 0 {
		t.Errorf("staged = %v, want nothing staged after Unstage", paths(st.Staged))
	}
	// One entry for the new path with status "R", carrying its OldPath too
	// (see worktree.State.add): Discard needs that source to restore the
	// rename rather than delete the file (see TestDiscardOfAnUnstagedRename
	// in this file), and it also lets the rename be re-staged as one rename.
	if len(st.Unstaged) != 1 || st.Unstaged[0].Path != "b.txt" || st.Unstaged[0].Status != "R" || st.Unstaged[0].OldPath != "a.txt" {
		t.Errorf("unstaged = %+v, want one R entry for b.txt with OldPath a.txt", st.Unstaged)
	}
	if len(st.Untracked) != 0 {
		t.Errorf("untracked = %v, want none; the rename must not fall apart into an untracked file", paths(st.Untracked))
	}
}

// Discarding an UNSTAGED rename (git mv, then Unstage — a state Unstage
// deliberately creates) must restore the original, exactly like discarding a
// staged one: the old path is only ever passed to git when Discard consults
// the unstaged entry's OldPath, since findStaged alone never sees it.
// Reproduces the bug where this emptied the whole working tree: b.txt
// deleted, a.txt never restored.
func TestDiscardOfAnUnstagedRenameRestoresTheOriginal(t *testing.T) {
	r := base(t)
	r.Git("mv", "a.txt", "b.txt")
	if err := worktree.Unstage(ctx, r.Dir, "b.txt"); err != nil {
		t.Fatal(err)
	}

	if err := worktree.Discard(ctx, r.Dir, "b.txt"); err != nil {
		t.Fatal(err)
	}
	if got := read(t, r.Dir, "a.txt"); got != "one\n" {
		t.Errorf("a.txt = %q, want the committed content restored", got)
	}
	if _, err := os.Lstat(filepath.Join(r.Dir, "b.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("b.txt still exists: %v", err)
	}
	st := status(t, r.Dir)
	if len(st.Staged) != 0 || len(st.Unstaged) != 0 || len(st.Untracked) != 0 {
		t.Errorf("state = %+v, want everything clean", st)
	}
}

// In a repository with no commits yet, Unstage must not fail with raw
// plumbing text from `ls-tree HEAD` — an unborn HEAD is "not in HEAD", the
// same way Commit and Preview treat it, and the file goes back to untracked.
func TestUnstageInARepositoryWithNoCommitsYet(t *testing.T) {
	r := testrepo.New(t)
	r.WriteFile("new.txt", "hello\n")
	if err := worktree.Stage(ctx, r.Dir, "new.txt"); err != nil {
		t.Fatal(err)
	}

	if err := worktree.Unstage(ctx, r.Dir, "new.txt"); err != nil {
		t.Fatal(err)
	}
	st := status(t, r.Dir)
	if len(st.Staged) != 0 || !slices.Equal(paths(st.Untracked), []string{"new.txt"}) {
		t.Errorf("after Unstage: staged = %v, untracked = %v, want new.txt untracked", paths(st.Staged), paths(st.Untracked))
	}
	if got := read(t, r.Dir, "new.txt"); got != "hello\n" {
		t.Errorf("new.txt = %q, want its content kept", got)
	}
}

// Discarding an untracked symlink removes the link itself and never follows
// it, so whatever it points at is untouched.
func TestDiscardOfAnUntrackedSymlinkRemovesOnlyTheLink(t *testing.T) {
	r := base(t)
	target := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(target, []byte("leave me alone\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(r.Dir, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	if err := worktree.Discard(ctx, r.Dir, "link.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(link); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("link.txt still exists: %v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "leave me alone\n" {
		t.Errorf("target = %q, the symlink's target was reached", data)
	}
}
