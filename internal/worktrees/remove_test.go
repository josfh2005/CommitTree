package worktrees

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"git-ui/internal/gitcmd"
	"git-ui/internal/testrepo"
)

// addWorktree adds a linked worktree at tmp/name on a new branch of the same
// name, off r's current HEAD.
func addWorktree(t *testing.T, r *testrepo.Repo, name string) string {
	t.Helper()
	wt := filepath.Join(t.TempDir(), name)
	r.Git("worktree", "add", "-q", wt, "-b", name)
	return wt
}

func TestInfoReportsACleanWorktree(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	wt := addWorktree(t, r, "feature")

	info, err := Info(context.Background(), r.Dir, wt)
	if err != nil {
		t.Fatal(err)
	}
	if info.Branch != "feature" || info.Detached || info.Changes != 0 || info.Locked {
		t.Fatalf("info = %+v", info)
	}
}

func TestInfoCountsUncommittedChanges(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	wt := addWorktree(t, r, "feature")
	if err := os.WriteFile(filepath.Join(wt, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, "b.txt"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}

	info, err := Info(context.Background(), r.Dir, wt)
	if err != nil {
		t.Fatal(err)
	}
	if info.Changes != 2 {
		t.Fatalf("changes = %d, want 2: %+v", info.Changes, info)
	}
}

func TestInfoReportsAMergedBranch(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	wt := addWorktree(t, r, "feature")
	// feature has no commits of its own beyond base, so it is merged into main.
	info, err := Info(context.Background(), r.Dir, wt)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Merged {
		t.Fatalf("info = %+v, want merged", info)
	}
}

func TestInfoReportsAnUnmergedBranch(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	wt := addWorktree(t, r, "feature")
	r.Git("-C", wt, "commit", "--allow-empty", "-q", "-m", "ahead")

	info, err := Info(context.Background(), r.Dir, wt)
	if err != nil {
		t.Fatal(err)
	}
	if info.Merged {
		t.Fatalf("info = %+v, want not merged", info)
	}
}

func TestInfoReportsALockedWorktree(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	wt := addWorktree(t, r, "feature")
	r.Git("worktree", "lock", wt, "--reason", "busy")

	info, err := Info(context.Background(), r.Dir, wt)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Locked {
		t.Fatalf("info = %+v, want locked", info)
	}
}

func TestInfoReportsADetachedWorktree(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	wt := filepath.Join(t.TempDir(), "det")
	r.Git("worktree", "add", "-q", "--detach", wt)

	info, err := Info(context.Background(), r.Dir, wt)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Detached || info.Branch != "" || info.Merged {
		t.Fatalf("info = %+v", info)
	}
}

func TestRemoveDeletesACleanWorktree(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	wt := addWorktree(t, r, "feature")

	if err := Remove(context.Background(), r.Dir, wt, false); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(wt); !os.IsNotExist(statErr) {
		t.Fatalf("worktree dir still present: %v", statErr)
	}
	list, err := List(context.Background(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("list = %+v, want only the main tree left", list)
	}
}

func TestRemoveRefusesWithUncommittedChangesUnlessForced(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	wt := addWorktree(t, r, "feature")
	if err := os.WriteFile(filepath.Join(wt, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := Remove(context.Background(), r.Dir, wt, false)
	var gerr *gitcmd.Error
	if !errors.As(err, &gerr) {
		t.Fatalf("err = %v, want a gitcmd.Error refusal", err)
	}
	if _, statErr := os.Stat(wt); statErr != nil {
		t.Fatalf("worktree dir gone after a refused removal: %v", statErr)
	}

	if err := Remove(context.Background(), r.Dir, wt, true); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(wt); !os.IsNotExist(statErr) {
		t.Fatalf("worktree dir still present after a forced removal: %v", statErr)
	}
}

func TestRemoveRefusesALockedWorktree(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	wt := addWorktree(t, r, "feature")
	r.Git("worktree", "lock", wt, "--reason", "busy")

	if err := Remove(context.Background(), r.Dir, wt, false); err == nil {
		t.Fatal("want an error removing a locked worktree")
	}
	if _, statErr := os.Stat(wt); statErr != nil {
		t.Fatalf("worktree dir gone after a refused removal: %v", statErr)
	}
}
