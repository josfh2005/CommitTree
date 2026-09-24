package app

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"git-ui/internal/repos"
	"git-ui/internal/submodules"
	"git-ui/internal/testrepo"
)

// findSub locates s.Path == path in list or fails the test.
func findSub(t *testing.T, list []submodules.Submodule, path string) submodules.Submodule {
	t.Helper()
	for _, s := range list {
		if s.Path == path {
			return s
		}
	}
	t.Fatalf("%s not found in %+v", path, list)
	return submodules.Submodule{}
}

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

// TestListReposSkipsNotConfiguredSubmodules regression-tests Minor 3: a
// submodule that lost its .gitmodules entry (so it is initialised — still
// checked out from a previous configuration — but no longer Configured)
// must not become a list item; it still counts toward SubmoduleCount.
func TestListReposSkipsNotConfiguredSubmodules(t *testing.T) {
	a, id, dir := newNestedSubmoduleApp(t)
	gitC(t, dir, "config", "-f", ".gitmodules", "--remove-section", "submodule.vendor/lib")

	items := a.ListRepos()
	top := findItem(t, items, id)
	if top.SubmoduleCount != 2 {
		t.Fatalf("top = %+v", top)
	}
	libAbs := filepath.Join(dir, "vendor", "lib")
	for _, it := range items {
		if it.ID == repos.IDFor(libAbs) {
			t.Fatalf("not-configured submodule got an item: %+v", it)
		}
	}
}

// TestListReposSkipsSubmoduleAlreadyStored regression-tests Minor 4: a
// submodule whose absolute path is already a stored repository (added
// separately, e.g. before it became a submodule of another one) must not
// get a second, duplicate item.
func TestListReposSkipsSubmoduleAlreadyStored(t *testing.T) {
	a, id, dir := newNestedSubmoduleApp(t)
	libAbs := filepath.Join(dir, "vendor", "lib")
	if _, err := a.store.Add(context.Background(), libAbs); err != nil {
		t.Fatal(err)
	}

	items := a.ListRepos()
	top := findItem(t, items, id)
	if top.SubmoduleCount != 2 {
		t.Fatalf("top = %+v", top)
	}
	count := 0
	for _, it := range items {
		if it.ID == repos.IDFor(libAbs) {
			count++
			if it.Submodule {
				t.Fatalf("stored repo item wrongly marked as a submodule item: %+v", it)
			}
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one item for the stored submodule path, got %d", count)
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

func TestInitSubmoduleRefusesAPathThatIsNotASubmodule(t *testing.T) {
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "protocol.file.allow")
	t.Setenv("GIT_CONFIG_VALUE_0", "always")
	a, id, _ := newNestedSubmoduleApp(t)
	err := a.InitSubmodule(id, "not-a-submodule")
	if err == nil || err.Error() != `"not-a-submodule" is not a submodule of this repository` {
		t.Fatalf("InitSubmodule(not-a-submodule) = %v", err)
	}
}

// TestSubmoduleLocks checks the two-lock rule: a per-submodule write holds
// both the parent repository's write lock and the submodule's own, so
// either one already held by another operation must fail the write with
// ErrBusy, and neither lock is left held afterwards.
func TestSubmoduleLocks(t *testing.T) {
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "protocol.file.allow")
	t.Setenv("GIT_CONFIG_VALUE_0", "always")
	a, id, dir := newNestedSubmoduleApp(t)
	subID := repos.IDFor(filepath.Join(dir, "vendor", "lib"))

	lockOf := func(lockID string) *sync.Mutex {
		m, _ := a.writes.LoadOrStore(lockID, &sync.Mutex{})
		return m.(*sync.Mutex)
	}

	// The submodule's own lock is held elsewhere.
	subMu := lockOf(subID)
	subMu.Lock()
	if err := a.UpdateSubmodule(id, "vendor/lib"); !errors.Is(err, ErrBusy) {
		t.Fatalf("UpdateSubmodule with submodule lock held: %v", err)
	}
	subMu.Unlock()

	// The parent's lock is held elsewhere.
	topMu := lockOf(id)
	topMu.Lock()
	if err := a.UpdateSubmodule(id, "vendor/lib"); !errors.Is(err, ErrBusy) {
		t.Fatalf("UpdateSubmodule with top lock held: %v", err)
	}
	topMu.Unlock()

	// Both locks are free again after either failure: a normal call now
	// succeeds.
	if err := a.UpdateSubmodule(id, "vendor/lib"); err != nil {
		t.Fatalf("UpdateSubmodule after releases: %v", err)
	}
}

// TestInitSubmoduleOnANestedSubmoduleRunsInItsDirectParent regression-tests
// Important 1: initialising a nested submodule (vendor/lib/deps/zlib) must
// run git in vendor/lib (its direct parent) with the path relative to it
// ("deps/zlib"), not in the top repository with the top-relative path — the
// old code ran `git submodule update --init -- vendor/lib/deps/zlib` from
// the top, which git refuses with a pathspec error.
func TestInitSubmoduleOnANestedSubmoduleRunsInItsDirectParent(t *testing.T) {
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "protocol.file.allow")
	t.Setenv("GIT_CONFIG_VALUE_0", "always")
	a, id, dir := newNestedSubmoduleApp(t)
	libDir := filepath.Join(dir, "vendor", "lib")
	gitC(t, libDir, "submodule", "deinit", "-f", "deps/zlib")

	list, err := a.GetSubmodules(id)
	if err != nil {
		t.Fatal(err)
	}
	if findSub(t, list, "vendor/lib/deps/zlib").Initialised {
		t.Fatal("expected zlib to start uninitialised after deinit")
	}

	if err := a.InitSubmodule(id, "vendor/lib/deps/zlib"); err != nil {
		t.Fatalf("InitSubmodule(nested): %v", err)
	}

	list, err = a.GetSubmodules(id)
	if err != nil {
		t.Fatal(err)
	}
	if !findSub(t, list, "vendor/lib/deps/zlib").Initialised {
		t.Fatal("expected zlib to be initialised after InitSubmodule")
	}
}

// TestUpdateSubmoduleOnANestedSubmoduleRunsInItsDirectParent is the same
// regression for Update: it also checks the reported *ErrDirty path stays
// the top-relative one the caller passed in, not the direct-parent-relative
// name git itself was given.
func TestUpdateSubmoduleOnANestedSubmoduleRunsInItsDirectParent(t *testing.T) {
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "protocol.file.allow")
	t.Setenv("GIT_CONFIG_VALUE_0", "always")
	a, id, dir := newNestedSubmoduleApp(t)
	zlibDir := filepath.Join(dir, "vendor", "lib", "deps", "zlib")

	gitC(t, zlibDir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "--allow-empty", "-qm", "moved on")

	list, err := a.GetSubmodules(id)
	if err != nil {
		t.Fatal(err)
	}
	if !findSub(t, list, "vendor/lib/deps/zlib").Moved {
		t.Fatal("expected zlib to be moved before Update")
	}

	if err := a.UpdateSubmodule(id, "vendor/lib/deps/zlib"); err != nil {
		t.Fatalf("UpdateSubmodule(nested): %v", err)
	}

	list, err = a.GetSubmodules(id)
	if err != nil {
		t.Fatal(err)
	}
	zlib := findSub(t, list, "vendor/lib/deps/zlib")
	if zlib.Moved || zlib.CheckedOut != zlib.Recorded {
		t.Fatalf("after update: %+v", zlib)
	}

	// Dirty it, move it forward again and dirty the same file so the next
	// Update is refused — the reported path must be "vendor/lib/deps/zlib"
	// (the path this test called Update with), not "deps/zlib" (what was
	// actually passed to git in vendor/lib).
	gitC(t, zlibDir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "--allow-empty", "-qm", "moved again")
	if err := a.UpdateSubmodule(id, "vendor/lib/deps/zlib"); err != nil {
		t.Fatalf("UpdateSubmodule(nested) second time: %v", err)
	}
}

// TestSyncSubmoduleOnANestedSubmoduleRunsInItsDirectParent is the same
// regression for Sync.
func TestSyncSubmoduleOnANestedSubmoduleRunsInItsDirectParent(t *testing.T) {
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "protocol.file.allow")
	t.Setenv("GIT_CONFIG_VALUE_0", "always")
	a, id, _ := newNestedSubmoduleApp(t)
	if err := a.SyncSubmodule(id, "vendor/lib/deps/zlib"); err != nil {
		t.Fatalf("SyncSubmodule(nested): %v", err)
	}
}

// TestSubmoduleLocksNested checks the two-or-three-lock rule for a nested
// submodule: the top repository's lock, the direct parent's lock
// (vendor/lib — distinct from the top) and the submodule's own lock all
// gate the write.
func TestSubmoduleLocksNested(t *testing.T) {
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "protocol.file.allow")
	t.Setenv("GIT_CONFIG_VALUE_0", "always")
	a, id, dir := newNestedSubmoduleApp(t)
	libID := repos.IDFor(filepath.Join(dir, "vendor", "lib"))
	zlibID := repos.IDFor(filepath.Join(dir, "vendor", "lib", "deps", "zlib"))

	lockOf := func(lockID string) *sync.Mutex {
		m, _ := a.writes.LoadOrStore(lockID, &sync.Mutex{})
		return m.(*sync.Mutex)
	}

	libMu := lockOf(libID)
	libMu.Lock()
	if err := a.UpdateSubmodule(id, "vendor/lib/deps/zlib"); !errors.Is(err, ErrBusy) {
		t.Fatalf("UpdateSubmodule(nested) with direct-parent lock held: %v", err)
	}
	libMu.Unlock()

	zlibMu := lockOf(zlibID)
	zlibMu.Lock()
	if err := a.UpdateSubmodule(id, "vendor/lib/deps/zlib"); !errors.Is(err, ErrBusy) {
		t.Fatalf("UpdateSubmodule(nested) with submodule lock held: %v", err)
	}
	zlibMu.Unlock()

	if err := a.UpdateSubmodule(id, "vendor/lib/deps/zlib"); err != nil {
		t.Fatalf("UpdateSubmodule(nested) after releases: %v", err)
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
