package app

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"git-ui/internal/refs"
	"git-ui/internal/repos"
)

// removeTestWorktree adds a linked worktree of id's repository on a new
// branch named name, at the given path, and returns the new worktree's list
// item id (as ListRepos would report it).
func removeTestWorktree(t *testing.T, a *App, id, name string) (wtID, wtDir string) {
	t.Helper()
	dir, err := a.dir(id)
	if err != nil {
		t.Fatal(err)
	}
	wtDir = filepath.Join(realPath(t, t.TempDir()), name)
	if out, err := exec.Command("git", "-C", dir, "worktree", "add", "-q", "-b", name, wtDir).CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %v %s", err, out)
	}
	items := a.ListRepos()
	for _, it := range items {
		if it.Worktree && it.Path == wtDir {
			return it.ID, wtDir
		}
	}
	t.Fatalf("worktree %s not found in %+v", wtDir, items)
	return "", ""
}

func TestWorktreeRemovalInfoReportsState(t *testing.T) {
	a, id := newTestApp(t)
	wtID, wtDir := removeTestWorktree(t, a, id, "wtbranch")

	info, err := a.WorktreeRemovalInfo(wtID)
	if err != nil {
		t.Fatal(err)
	}
	if info.Branch != "wtbranch" || info.Detached || info.Locked {
		t.Fatalf("info = %+v", info)
	}

	if err := os.WriteFile(filepath.Join(wtDir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err = a.WorktreeRemovalInfo(wtID)
	if err != nil {
		t.Fatal(err)
	}
	if info.Changes != 1 {
		t.Fatalf("info = %+v, want 1 change", info)
	}
}

func TestWorktreeRemovalInfoRefusesANonWorktreeID(t *testing.T) {
	a, id := newTestApp(t)
	if _, err := a.WorktreeRemovalInfo(id); !errors.Is(err, repos.ErrUnknownRepo) {
		t.Fatalf("err = %v, want ErrUnknownRepo for the main repository itself", err)
	}
}

func TestRemoveWorktreeRemovesACleanWorktree(t *testing.T) {
	a, id := newTestApp(t)
	WithAI(a, AIDeps{Emit: newEvents().emit})
	wtID, wtDir := removeTestWorktree(t, a, id, "wtbranch")

	if err := a.RemoveWorktree(wtID, false, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(wtDir); !os.IsNotExist(err) {
		t.Fatalf("worktree dir still present: %v", err)
	}
	items := a.ListRepos()
	for _, it := range items {
		if it.ID == wtID {
			t.Fatalf("removed worktree still listed: %+v", it)
		}
	}
}

func TestRemoveWorktreeRefusesUncommittedChangesUnlessForced(t *testing.T) {
	a, id := newTestApp(t)
	WithAI(a, AIDeps{Emit: newEvents().emit})
	wtID, wtDir := removeTestWorktree(t, a, id, "wtbranch")
	if err := os.WriteFile(filepath.Join(wtDir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := a.RemoveWorktree(wtID, false, false); err == nil {
		t.Fatal("want an error removing a worktree with uncommitted changes, unforced")
	}
	if _, err := os.Stat(wtDir); err != nil {
		t.Fatalf("worktree dir gone after a refused removal: %v", err)
	}

	if err := a.RemoveWorktree(wtID, true, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(wtDir); !os.IsNotExist(err) {
		t.Fatalf("worktree dir still present after forced removal: %v", err)
	}
}

func TestRemoveWorktreeDeletesAMergedBranch(t *testing.T) {
	a, id := newTestApp(t)
	WithAI(a, AIDeps{Emit: newEvents().emit})
	wtID, _ := removeTestWorktree(t, a, id, "wtbranch")

	if err := a.RemoveWorktree(wtID, false, true); err != nil {
		t.Fatal(err)
	}
	dir, err := a.dir(id)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", dir, "rev-parse", "--verify", "--quiet", "refs/heads/wtbranch").CombinedOutput(); err == nil {
		t.Fatalf("feature branch still exists: %s", out)
	}
}

// An unmerged branch is kept, but identifiable as such (refs.ErrNotMerged),
// so the frontend can offer the same force-delete confirmation it already
// uses for an ordinary branch delete — the worktree itself is still removed,
// since losing the branch's commits is a separate, explicit decision.
func TestRemoveWorktreeKeepsAnUnmergedBranchButIdentifiesTheRefusal(t *testing.T) {
	a, id := newTestApp(t)
	WithAI(a, AIDeps{Emit: newEvents().emit})
	wtID, wtDir := removeTestWorktree(t, a, id, "wtbranch")
	if out, err := exec.Command("git", "-C", wtDir, "commit", "--allow-empty", "-q", "-m", "ahead").CombinedOutput(); err != nil {
		t.Fatalf("commit: %v %s", err, out)
	}

	err := a.RemoveWorktree(wtID, false, true)
	if !errors.Is(err, refs.ErrNotMerged) {
		t.Fatalf("err = %v, want ErrNotMerged", err)
	}
	if _, statErr := os.Stat(wtDir); !os.IsNotExist(statErr) {
		t.Fatalf("worktree dir still present: %v", statErr)
	}
	dir, err2 := a.dir(id)
	if err2 != nil {
		t.Fatal(err2)
	}
	if out, verr := exec.Command("git", "-C", dir, "rev-parse", "--verify", "--quiet", "refs/heads/wtbranch").CombinedOutput(); verr != nil {
		t.Fatalf("feature branch should still exist: %s", out)
	}
}

func TestRemoveWorktreeRefusesALockedWorktree(t *testing.T) {
	a, id := newTestApp(t)
	WithAI(a, AIDeps{Emit: newEvents().emit})
	wtID, wtDir := removeTestWorktree(t, a, id, "wtbranch")
	dir, err := a.dir(id)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", dir, "worktree", "lock", wtDir).CombinedOutput(); err != nil {
		t.Fatalf("lock: %v %s", err, out)
	}

	info, err := a.WorktreeRemovalInfo(wtID)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Locked {
		t.Fatalf("info = %+v, want locked", info)
	}
	if err := a.RemoveWorktree(wtID, false, false); err == nil {
		t.Fatal("want an error removing a locked worktree")
	}
}
