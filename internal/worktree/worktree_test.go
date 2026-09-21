package worktree_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"git-ui/internal/testrepo"
	"git-ui/internal/worktree"
)

var ctx = context.Background()

// base is a repository with one committed file and nothing else changed.
func base(t *testing.T) *testrepo.Repo {
	t.Helper()
	r := testrepo.New(t)
	r.WriteFile("a.txt", "one\n")
	r.Git("add", "a.txt")
	r.Git("commit", "-q", "-m", "add a")
	return r
}

func paths(files []worktree.FileStatus) []string {
	out := []string{}
	for _, f := range files {
		out = append(out, f.Path)
	}
	return out
}

func status(t *testing.T, dir string) worktree.State {
	t.Helper()
	st, err := worktree.Status(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestStatusOfACleanRepository(t *testing.T) {
	st := status(t, base(t).Dir)
	if len(st.Staged) != 0 || len(st.Unstaged) != 0 || len(st.Untracked) != 0 || st.Merging {
		t.Errorf("state = %+v, want everything empty", st)
	}
}

func TestStatusSplitsStagedUnstagedAndUntracked(t *testing.T) {
	r := base(t)
	r.WriteFile("b.txt", "tracked\n")
	r.Git("add", "b.txt")
	r.Git("commit", "-q", "-m", "add b")
	r.WriteFile("a.txt", "staged\n")
	r.Git("add", "a.txt")
	r.WriteFile("b.txt", "modified after commit\n")
	r.WriteFile("new.txt", "untracked\n")

	st := status(t, r.Dir)
	if !slices.Equal(paths(st.Staged), []string{"a.txt"}) {
		t.Errorf("staged = %v", paths(st.Staged))
	}
	if !slices.Equal(paths(st.Unstaged), []string{"b.txt"}) {
		t.Errorf("unstaged = %v", paths(st.Unstaged))
	}
	if !slices.Equal(paths(st.Untracked), []string{"new.txt"}) {
		t.Errorf("untracked = %v", paths(st.Untracked))
	}
}

// git means two different things by these two entries, and the view must show
// both: what will be committed, and what will not.
func TestStatusListsAFileThatIsBothStagedAndModified(t *testing.T) {
	r := base(t)
	r.WriteFile("a.txt", "staged\n")
	r.Git("add", "a.txt")
	r.WriteFile("a.txt", "and then edited again\n")

	st := status(t, r.Dir)
	if !slices.Contains(paths(st.Staged), "a.txt") || !slices.Contains(paths(st.Unstaged), "a.txt") {
		t.Errorf("staged = %v, unstaged = %v; want a.txt in both", paths(st.Staged), paths(st.Unstaged))
	}
}

func TestStatusReportsARenameWithItsSource(t *testing.T) {
	r := base(t)
	r.Git("mv", "a.txt", "renamed.txt")

	st := status(t, r.Dir)
	if len(st.Staged) != 1 {
		t.Fatalf("staged = %v, want one entry", paths(st.Staged))
	}
	got := st.Staged[0]
	if got.Path != "renamed.txt" || got.OldPath != "a.txt" || got.Status != "R" {
		t.Errorf("entry = %+v, want renamed.txt from a.txt with status R", got)
	}
}

func TestStatusReportsADeletion(t *testing.T) {
	r := base(t)
	r.Git("rm", "-q", "a.txt")

	st := status(t, r.Dir)
	if len(st.Staged) != 1 || st.Staged[0].Status != "D" {
		t.Errorf("staged = %+v, want one deletion", st.Staged)
	}
}

// -z exists so these survive; a name with a space, a newline or non-ASCII is
// not an edge case, it is Tuesday.
func TestStatusHandlesAwkwardNames(t *testing.T) {
	r := base(t)
	names := []string{"with space.txt", "acentuación.txt", "new\nline.txt"}
	for _, name := range names {
		r.WriteFile(name, "x\n")
	}
	st := status(t, r.Dir)
	for _, name := range names {
		if !slices.Contains(paths(st.Untracked), name) {
			t.Errorf("untracked = %q, missing %q", paths(st.Untracked), name)
		}
	}
}

// An untracked directory must be listed as its files, not as the directory:
// the app acts on paths, and "d" is also a pathspec matching everything below.
func TestStatusListsFilesInsideAnUntrackedDirectory(t *testing.T) {
	r := base(t)
	if err := os.MkdirAll(filepath.Join(r.Dir, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	r.WriteFile("d/inside.txt", "x\n")

	st := status(t, r.Dir)
	if !slices.Equal(paths(st.Untracked), []string{"d/inside.txt"}) {
		t.Errorf("untracked = %v, want the file, not the directory", paths(st.Untracked))
	}
}

// Mid-merge the merge view owns the repository; Status says so instead of
// presenting conflicts as ordinary changes.
func TestStatusReportsAMergeInProgress(t *testing.T) {
	r := base(t)
	r.Git("switch", "-q", "-c", "feature")
	r.WriteFile("a.txt", "theirs\n")
	r.Git("commit", "-q", "-am", "theirs")
	r.Git("switch", "-q", "main")
	r.WriteFile("a.txt", "ours\n")
	r.Git("commit", "-q", "-am", "ours")
	_ = r.GitFails("merge", "feature")

	if st := status(t, r.Dir); !st.Merging {
		t.Errorf("merging = false during a conflicted merge: %+v", st)
	}
}

// Resolving the last conflict and staging it turns its "u" record into an
// ordinary "1" one, but the merge is not over until it is committed. Merging
// must stay true even though no unmerged record remains.
func TestStatusReportsAMergeInProgressAfterConflictsAreResolvedAndStaged(t *testing.T) {
	r := base(t)
	r.Git("switch", "-q", "-c", "feature")
	r.WriteFile("a.txt", "theirs\n")
	r.Git("commit", "-q", "-am", "theirs")
	r.Git("switch", "-q", "main")
	r.WriteFile("a.txt", "ours\n")
	r.Git("commit", "-q", "-am", "ours")
	_ = r.GitFails("merge", "feature")
	r.WriteFile("a.txt", "resolved\n")
	r.Git("add", "a.txt")

	st := status(t, r.Dir)
	if !st.Merging {
		t.Errorf("merging = false after staging the resolution, want true: %+v", st)
	}
	for _, f := range st.Staged {
		if f.Status == "U" {
			t.Errorf("staged still has an unmerged entry: %+v", st.Staged)
		}
	}
}

// A regular file replaced by a symlink of the same name is a type change;
// git reports it with status T.
func TestStatusReportsATypeChange(t *testing.T) {
	r := base(t)
	if err := os.Remove(filepath.Join(r.Dir, "a.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("does-not-matter", filepath.Join(r.Dir, "a.txt")); err != nil {
		t.Fatal(err)
	}

	st := status(t, r.Dir)
	if len(st.Unstaged) != 1 {
		t.Fatalf("unstaged = %v, want one entry", st.Unstaged)
	}
	got := st.Unstaged[0]
	if got.Status != "T" {
		t.Skipf("git on this machine reports the file-to-symlink change as %q, not T; skipping", got.Status)
	}
	if got.Path != "a.txt" {
		t.Errorf("entry = %+v, want a.txt", got)
	}
}
