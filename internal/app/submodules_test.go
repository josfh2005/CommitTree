package app

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"testing"

	"git-ui/internal/repos"
	"git-ui/internal/testrepo"
)

// gitC runs git -C dir <args> and fails the test on error.
func gitC(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// newNestedSubmoduleApp returns an App over a stored repository with an
// initialised submodule at vendor/lib, itself holding an initialised
// submodule at deps/zlib, mirroring internal/submodules.TestListNestedIsFlat.
func newNestedSubmoduleApp(t *testing.T) (a *App, id string, dir string) {
	t.Helper()
	inner := testrepo.New(t)
	inner.WriteFile("z.txt", "z")
	inner.Commit("inner")

	lib := testrepo.New(t)
	lib.WriteFile("a.txt", "a")
	lib.Git("add", "a.txt")
	lib.Git("commit", "-q", "-m", "lib one")
	lib.Git("-c", "protocol.file.allow=always", "submodule", "add", "-q", inner.Dir, "deps/zlib")
	lib.Git("commit", "-q", "-m", "add inner")

	parent := testrepo.New(t)
	parent.WriteFile("p.txt", "p")
	parent.Commit("parent one")
	parent.Git("-c", "protocol.file.allow=always", "submodule", "add", "-q", lib.Dir, "vendor/lib")
	parent.Git("-c", "protocol.file.allow=always", "submodule", "update", "--init", "--recursive", "-q")
	parent.Git("commit", "-q", "-m", "add submodule")

	store, err := repos.Open(filepath.Join(t.TempDir(), "repos.json"))
	if err != nil {
		t.Fatal(err)
	}
	repo, err := store.Add(context.Background(), parent.Dir)
	if err != nil {
		t.Fatal(err)
	}
	return New(store), repo.ID, repo.Path
}

func findItem(t *testing.T, items []RepoItem, id string) RepoItem {
	t.Helper()
	for _, it := range items {
		if it.ID == id {
			return it
		}
	}
	t.Fatalf("%s not found in %+v", id, items)
	return RepoItem{}
}

func TestListReposDetectsSubmodules(t *testing.T) {
	a, id, dir := newNestedSubmoduleApp(t)

	items := a.ListRepos()
	top := findItem(t, items, id)
	if top.SubmoduleCount != 2 {
		t.Fatalf("top = %+v", top)
	}

	libAbs := filepath.Join(dir, "vendor", "lib")
	lib := findItem(t, items, repos.IDFor(libAbs))
	if !lib.Submodule || lib.ParentID != id || lib.SubPath != "vendor/lib" || lib.Name != "lib" || lib.Branch == "" {
		t.Fatalf("lib = %+v", lib)
	}

	zlibAbs := filepath.Join(libAbs, "deps", "zlib")
	zlib := findItem(t, items, repos.IDFor(zlibAbs))
	if !zlib.Submodule || zlib.ParentID != id || zlib.SubPath != "vendor/lib/deps/zlib" || zlib.Name != "zlib" || zlib.Branch == "" {
		t.Fatalf("zlib = %+v", zlib)
	}
}

func TestListReposUninitialisedSubmoduleCountsButHasNoItem(t *testing.T) {
	a, id, dir := newNestedSubmoduleApp(t)
	libDir := filepath.Join(dir, "vendor", "lib")
	gitC(t, libDir, "submodule", "deinit", "-f", "deps/zlib")

	items := a.ListRepos()
	top := findItem(t, items, id)
	if top.SubmoduleCount != 2 {
		t.Fatalf("top = %+v", top)
	}
	zlibAbs := filepath.Join(libDir, "deps", "zlib")
	for _, it := range items {
		if it.ID == repos.IDFor(zlibAbs) {
			t.Fatalf("uninitialised submodule got an item: %+v", it)
		}
	}
}

func TestDirOfASubmoduleThenDeinitialised(t *testing.T) {
	a, id, dir := newNestedSubmoduleApp(t)
	_ = id
	libAbs := filepath.Join(dir, "vendor", "lib")
	subID := repos.IDFor(libAbs)

	a.ListRepos()
	got, err := a.dir(subID)
	if err != nil || got != libAbs {
		t.Fatalf("dir = %q, %v", got, err)
	}

	gitC(t, dir, "submodule", "deinit", "-f", "vendor/lib")
	a.ListRepos()
	if _, err := a.dir(subID); !errors.Is(err, repos.ErrUnknownRepo) {
		t.Fatalf("dir after deinit: %v", err)
	}
}

func TestRemoveRepoRefusesASubmodule(t *testing.T) {
	a, _, dir := newNestedSubmoduleApp(t)
	a.ListRepos()
	subID := repos.IDFor(filepath.Join(dir, "vendor", "lib"))
	if err := a.RemoveRepo(subID); !errors.Is(err, repos.ErrUnknownRepo) {
		t.Fatalf("remove submodule: %v", err)
	}
}

func TestGetSubmodules(t *testing.T) {
	a, id, _ := newNestedSubmoduleApp(t)
	list, err := a.GetSubmodules(id)
	if err != nil || len(list) != 2 {
		t.Fatalf("list = %+v, err %v", list, err)
	}
	if _, err := a.GetSubmodules("nope"); !errors.Is(err, repos.ErrUnknownRepo) {
		t.Fatalf("GetSubmodules(nope): %v", err)
	}
}

func TestListReposDetectsSubmodulesInAWorktree(t *testing.T) {
	a, id, dir := newNestedSubmoduleApp(t)
	_ = id
	wt := filepath.Join(realPath(t, t.TempDir()), "wt")
	gitC(t, dir, "worktree", "add", "-q", "--detach", wt)

	// The worktree's own vendor/lib checkout starts uninitialised (git
	// worktree add does not init submodules), so only it — not the nested
	// deps/zlib, which is only discoverable by recursing into it — is
	// counted, and neither gets an item.
	items := a.ListRepos()
	wtItem := findItem(t, items, repos.IDFor(wt))
	if wtItem.SubmoduleCount != 1 {
		t.Fatalf("worktree item = %+v", wtItem)
	}
	for _, it := range items {
		if it.ParentID == wtItem.ID {
			t.Fatalf("uninitialised worktree submodule got an item: %+v", it)
		}
	}

	gitC(t, wt, "-c", "protocol.file.allow=always", "submodule", "update", "--init", "--recursive", "-q")
	items = a.ListRepos()
	wtItem = findItem(t, items, repos.IDFor(wt))
	if wtItem.SubmoduleCount != 2 {
		t.Fatalf("worktree item after init = %+v", wtItem)
	}
	libAbs := filepath.Join(wt, "vendor", "lib")
	lib := findItem(t, items, repos.IDFor(libAbs))
	if !lib.Submodule || lib.ParentID != wtItem.ID {
		t.Fatalf("lib in worktree = %+v", lib)
	}
	zlibAbs := filepath.Join(libAbs, "deps", "zlib")
	zlib := findItem(t, items, repos.IDFor(zlibAbs))
	if !zlib.Submodule || zlib.ParentID != wtItem.ID {
		t.Fatalf("zlib in worktree = %+v", zlib)
	}
}
