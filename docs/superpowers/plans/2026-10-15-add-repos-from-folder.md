# Add Repositories From a Folder Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** "Add repo → Add from folder…" scans a parent folder's direct children for git repositories and adds the ticked ones in one step, optionally into a group.

**Architecture:** A new Go package `internal/scanrepos` lists candidate children with `os.Stat` only (no git). `repos.Store.AddMany` adds a batch with one `repos.json` write and rollback. Four app bindings expose scan, add, folder picker and default folder. On the frontend, pure logic lives in `lib/addRepos.ts` (vitest), the Clone dialog's parent field is extracted into `FolderField.svelte` and reused by a new `AddReposDialog.svelte`.

**Tech Stack:** Go 1.x (Wails v2 app), Svelte 5 in legacy syntax (`$:`, `on:`, `export let`), TypeScript, vitest 0.34.

**Spec:** `docs/superpowers/specs/2026-10-15-add-repos-from-folder-design.md` (read it before starting any task).

## Global Constraints

- Worktree: `/Users/josfh/playground/git-ui/.claude/worktrees/committest-multi-repo-add-54fcef`. Prefix **every** Bash command with `cd <worktree> &&`.
- git is `/usr/bin/git`. Commit messages: conventional style (`feat(scope): …`, `refactor(ui): …`), **never** a `Co-Authored-By` line.
- Go tests: `go test ./internal/<pkg>/...`; whole suite `go test ./...` from the worktree root.
- Frontend commands need Node 22: `cd <worktree>/frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && <cmd>`; tests `npx vitest run <file>`, types `npm run check`.
- Wails bindings: after adding Go bindings run `cd <worktree> && ~/go/bin/wails generate module`, then `/usr/bin/git checkout -- frontend/wailsjs/runtime 2>/dev/null || true`; commit only `frontend/wailsjs/go/**` changes.
- Exact user-visible strings (copy verbatim):
  - Menu item: `Add from folder…` (between `Open folder…` and `Clone…`).
  - Dialog title: `Add repositories from a folder`.
  - Relative parent: `Parent folder must be a full path (or start with ~).` (existing clone error, reused).
  - Scan errors: `That folder doesn't exist.` / `That path isn't a folder.` / `That folder can't be read: <reason>`.
  - Empty: `No git repositories in this folder.`
  - Skipped: `N folders skipped (not git repositories).` / `1 folder skipped (not a git repository).`
  - Row label for listed repos: `Already added`.
  - Buttons: `Scan` / `Scanning…`, `Choose…`, `Select all`, `None`, `Cancel`, `Add repositories` (0 ticked), `Add 1 repository`, `Add N repositories`, `Adding…`.
  - Group select: `No group`, existing names sorted, `New group…`.
  - Toasts: `Added 1 repository.` / `Added N repositories.`; partial: `Added N repositories; M couldn't be added: a (reason), b (reason), c (reason), and K more.`; all already listed: `Nothing added: those repositories are already in the list.`; all failed (inline, dialog stays open): `No repository could be added: a (reason), ….`
- No write lock, no command-log entry for adding (only `repos.json` changes).
- Every behaviour change updates `docs/spec/01-repositories-and-sidebar.md` in the same commit (Task 6 owns it).

## Review Focus

1. **Parent typed with trailing slash or spaces** (`" ~/projects/ "`): scans the same folder as `~/projects`. Pinned in Task 1 (`TestScanTrimsAndExpands`).
2. **Parent that is itself a repository, with a submodule/worktree checkout inside**: the parent is not offered, `.git`-file children are skipped and counted. Pinned in Task 1 (`TestScanParentIsRepo`).
3. **Every ticked repository was already added in the meantime**: nothing is added, info toast `Nothing added: those repositories are already in the list.`, dialog closes. Pinned in Task 2 (`TestAddManyAllListed`) and Task 4 (`summarize` case).
4. **Field edited while a scan is in flight**: the late answer is dropped; the list stays cleared. Pinned in Task 4 (`createLatest`).
5. **Folder names with spaces or non-ASCII letters, mixed case**: listed, sorted case-insensitively, added with that name. Pinned in Task 1 (`TestScanListsSortsAndSkips`).

---

### Task 1: `scanrepos.Scan` and the shared parent expansion

**Files:**
- Modify: `internal/clone/clone.go` (rename `expandParent` → `ExpandParent`, all call sites in that file)
- Create: `internal/scanrepos/scanrepos.go`
- Test: `internal/scanrepos/scanrepos_test.go`

**Interfaces:**
- Consumes: `clone.ExpandParent(parent string) (string, error)` (renamed here; errors `clone.ErrParentNotAbsolute`, `clone.ErrHomeUnknown`).
- Produces:
  ```go
  package scanrepos
  type Candidate struct { Name string `json:"name"`; Path string `json:"path"` }
  type Result struct { Parent string `json:"parent"`; Repos []Candidate `json:"repos"`; Skipped int `json:"skipped"` }
  var ErrParentMissing, ErrParentNotDir, ErrParentUnreadable error
  func Scan(parent string) (Result, error)
  ```

- [ ] **Step 1: Rename `expandParent` to `ExpandParent` in `internal/clone/clone.go`**

Rename the function and every call in the file (`grep -n expandParent internal/clone/clone.go`). Keep its doc comment, starting it with `ExpandParent`. Then run `go build ./... && go test ./internal/clone/...` — expect PASS (pure rename).

- [ ] **Step 2: Write the failing tests** — `internal/scanrepos/scanrepos_test.go`

```go
package scanrepos_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"git-ui/internal/clone"
	"git-ui/internal/scanrepos"
)

// mkdirs creates each relative path (folders) under root.
func mkdirs(t *testing.T, root string, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if err := os.MkdirAll(filepath.Join(root, p), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func names(r scanrepos.Result) []string {
	out := []string{}
	for _, c := range r.Repos {
		out = append(out, c.Name)
	}
	return out
}

func TestScanListsSortsAndSkips(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root,
		"beta/.git", "Alpha/.git", "My Repo/.git", "ñandú/.git", // repos
		"plain",              // folder without .git: skipped
		".hidden/.git",       // hidden: neither listed nor counted
		"nested/inner/.git",  // only direct children count: skipped
	)
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A child whose .git is a file (linked worktree / submodule checkout): skipped.
	mkdirs(t, root, "linked")
	if err := os.WriteFile(filepath.Join(root, "linked", ".git"), []byte("gitdir: /elsewhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A symlink to a repository is listed; a broken symlink is skipped.
	other := t.TempDir()
	mkdirs(t, other, "target/.git")
	if err := os.Symlink(filepath.Join(other, "target"), filepath.Join(root, "linkrepo")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(other, "gone"), filepath.Join(root, "broken")); err != nil {
		t.Fatal(err)
	}

	got, err := scanrepos.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Alpha", "beta", "linkrepo", "My Repo", "ñandú"}
	if !reflect.DeepEqual(names(got), want) {
		t.Fatalf("repos = %v, want %v", names(got), want)
	}
	// plain, nested, notes.txt, linked, broken
	if got.Skipped != 5 {
		t.Fatalf("skipped = %d, want 5", got.Skipped)
	}
	if got.Parent != filepath.Clean(root) {
		t.Fatalf("parent = %q", got.Parent)
	}
	if got.Repos[0].Path != filepath.Join(root, "Alpha") {
		t.Fatalf("path = %q", got.Repos[0].Path)
	}
}

func TestScanTrimsAndExpands(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	mkdirs(t, home, "projects/app/.git")

	for _, typed := range []string{"~/projects", " ~/projects/ ", home + "/projects/"} {
		got, err := scanrepos.Scan(typed)
		if err != nil {
			t.Fatalf("%q: %v", typed, err)
		}
		if got.Parent != filepath.Join(home, "projects") || !reflect.DeepEqual(names(got), []string{"app"}) {
			t.Fatalf("%q: %+v", typed, got)
		}
	}
}

func TestScanParentIsRepo(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root, ".git", "src", "vendor/lib")
	if err := os.WriteFile(filepath.Join(root, "vendor", "lib", ".git"), []byte("gitdir: ../../.git/modules/lib\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := scanrepos.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Repos) != 0 || got.Skipped != 2 {
		t.Fatalf("got %+v, want no repos and 2 skipped", got)
	}
}

func TestScanEmptyFolderGivesNonNilList(t *testing.T) {
	got, err := scanrepos.Scan(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got.Repos == nil || len(got.Repos) != 0 || got.Skipped != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestScanRefusals(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "f")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		parent string
		want   error
	}{
		{"relative/path", clone.ErrParentNotAbsolute},
		{filepath.Join(root, "missing"), scanrepos.ErrParentMissing},
		{file, scanrepos.ErrParentNotDir},
	}
	for _, c := range cases {
		if _, err := scanrepos.Scan(c.parent); !errors.Is(err, c.want) {
			t.Errorf("Scan(%q) = %v, want %v", c.parent, err, c.want)
		}
	}
	if scanrepos.ErrParentMissing.Error() != "That folder doesn't exist." ||
		scanrepos.ErrParentNotDir.Error() != "That path isn't a folder." {
		t.Fatal("messages changed")
	}
}

func TestScanUnreadable(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permissions not enforced")
	}
	root := filepath.Join(t.TempDir(), "locked")
	mkdirs(t, root, "a/.git")
	if err := os.Chmod(root, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(root, 0o755) })
	_, err := scanrepos.Scan(root)
	if !errors.Is(err, scanrepos.ErrParentUnreadable) || !strings.HasPrefix(err.Error(), "That folder can't be read: ") {
		t.Fatalf("err = %v", err)
	}
}

// Scanning never runs git: with no git on PATH it still works.
func TestScanRunsNoGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed anyway")
	}
	root := t.TempDir()
	mkdirs(t, root, "a/.git")
	t.Setenv("PATH", t.TempDir())
	got, err := scanrepos.Scan(root)
	if err != nil || len(got.Repos) != 1 {
		t.Fatalf("got %+v, %v", got, err)
	}
}
```

- [ ] **Step 3: Run the tests to see them fail**

Run: `go test ./internal/scanrepos/...`
Expected: FAIL — `package git-ui/internal/scanrepos` has no non-test Go files / undefined: `scanrepos.Scan`.

- [ ] **Step 4: Implement** — `internal/scanrepos/scanrepos.go`

```go
// Package scanrepos finds the git repositories directly inside a folder, for
// adding several to the sidebar at once. It only looks at the file system
// (no git), so it is instant even for a folder with hundreds of entries.
package scanrepos

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"git-ui/internal/clone"
)

// Candidate is a direct child of the scanned folder that is a repository.
type Candidate struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// Result is what Scan found. Repos is sorted by name (case-insensitive) and
// never nil. Skipped counts the non-hidden children that are not candidates.
type Result struct {
	Parent  string      `json:"parent"`
	Repos   []Candidate `json:"repos"`
	Skipped int         `json:"skipped"`
}

var (
	ErrParentMissing    = errors.New("That folder doesn't exist.")
	ErrParentNotDir     = errors.New("That path isn't a folder.")
	ErrParentUnreadable = errors.New("That folder can't be read")
)

// Scan lists the repositories among parent's direct children. parent is
// taken as typed: trimmed, with a leading ~ expanded (clone.ExpandParent);
// a relative path is refused. A child is a candidate when its name does not
// start with ".", it is a folder (or a symlink to one) and its .git is a
// folder; a .git file (linked worktree, submodule checkout) does not count.
// The parent itself is never a candidate.
func Scan(parent string) (Result, error) {
	dir, err := clone.ExpandParent(parent)
	if err != nil {
		return Result{}, err
	}
	info, err := os.Stat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return Result{}, ErrParentMissing
	case err != nil:
		return Result{}, fmt.Errorf("%w: %v", ErrParentUnreadable, err)
	case !info.IsDir():
		return Result{}, ErrParentNotDir
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrParentUnreadable, err)
	}
	res := Result{Parent: dir, Repos: []Candidate{}}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		path := filepath.Join(dir, name)
		if isRepo(path) {
			res.Repos = append(res.Repos, Candidate{Name: name, Path: path})
		} else {
			res.Skipped++
		}
	}
	sort.SliceStable(res.Repos, func(i, j int) bool {
		a, b := strings.ToLower(res.Repos[i].Name), strings.ToLower(res.Repos[j].Name)
		if a != b {
			return a < b
		}
		return res.Repos[i].Name < res.Repos[j].Name
	})
	return res, nil
}

// isRepo: path is a folder (symlinks followed) whose .git is a folder.
func isRepo(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return false
	}
	git, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil && git.IsDir()
}
```

Note on the unreadable message: `fmt.Errorf("%w: %v", ErrParentUnreadable, err)` gives `That folder can't be read: open …: permission denied`.

- [ ] **Step 5: Run the tests to see them pass**

Run: `go test ./internal/scanrepos/... ./internal/clone/...`
Expected: PASS. (If `TestScanListsSortsAndSkips` disagrees on the order of `linkrepo` vs `My Repo`: lowercase compare gives `alpha < beta < linkrepo < my repo < ñandú` — fix the code, not the test.)

- [ ] **Step 6: Commit**

```bash
cd <worktree> && /usr/bin/git add internal/clone/clone.go internal/scanrepos && /usr/bin/git commit -m "feat(scanrepos): list the git repositories directly inside a folder; clone.ExpandParent exported for it"
```

---

### Task 2: `repos.Store.AddMany`

**Files:**
- Modify: `internal/repos/repos.go` (`Add` at ~line 88; add `AddFailure`, `AddMany`, helper `findPath`)
- Test: `internal/repos/repos_test.go` (append)

**Interfaces:**
- Consumes: existing `toplevel(ctx, path)`, `s.uniqueID`, `s.save`.
- Produces:
  ```go
  type AddFailure struct { Path string `json:"path"`; Name string `json:"name"`; Reason string `json:"reason"` }
  func (s *Store) AddMany(ctx context.Context, paths []string, group string) (added []Repo, failed []AddFailure, err error)
  ```
  On success `added` and `failed` are never nil. On a save error both are nil and the in-memory list is unchanged.

- [ ] **Step 1: Write the failing tests** (append to `internal/repos/repos_test.go`; `ctx`, `realPath`, `testrepo` already exist there)

```go
func newRepo(t *testing.T) *testrepo.Repo {
	t.Helper()
	r := testrepo.New(t)
	r.Commit("init")
	return r
}

func openStore(t *testing.T) (*repos.Store, string) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "cfg", "repos.json")
	s, err := repos.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	return s, file
}

func TestAddManyAddsInOrderWithGroupAndPersists(t *testing.T) {
	a, b := newRepo(t), newRepo(t)
	s, file := openStore(t)

	added, failed, err := s.AddMany(ctx, []string{a.Dir, b.Dir}, "  Work ")
	if err != nil || len(failed) != 0 || len(added) != 2 {
		t.Fatalf("added %v failed %v err %v", added, failed, err)
	}
	if added[0].Path != realPath(t, a.Dir) || added[1].Path != realPath(t, b.Dir) {
		t.Fatalf("order/paths: %+v", added)
	}
	for _, r := range added {
		if r.Group != "Work" || r.ID == "" || r.Name != filepath.Base(r.Path) {
			t.Fatalf("entry %+v", r)
		}
	}
	reopened, err := repos.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.List(); len(got) != 2 || got[0].Group != "Work" {
		t.Fatalf("persisted %+v", got)
	}
}

func TestAddManySkipsListedAndDuplicatesAndReportsFailures(t *testing.T) {
	listed, fresh := newRepo(t), newRepo(t)
	sub := filepath.Join(fresh.Dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	notRepo := t.TempDir()
	s, _ := openStore(t)
	before, err := s.Add(ctx, listed.Dir)
	if err != nil {
		t.Fatal(err)
	}

	added, failed, err := s.AddMany(ctx, []string{listed.Dir, fresh.Dir, sub, notRepo}, "G")
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 1 || added[0].Path != realPath(t, fresh.Dir) {
		t.Fatalf("added %+v", added)
	}
	if len(failed) != 1 || failed[0].Path != notRepo || failed[0].Name != filepath.Base(notRepo) || failed[0].Reason == "" {
		t.Fatalf("failed %+v", failed)
	}
	// The already-listed entry is untouched: same ID, still ungrouped.
	got, _ := s.Get(before.ID)
	if got.Group != "" {
		t.Fatalf("listed repo regrouped: %+v", got)
	}
	if n := len(s.List()); n != 2 {
		t.Fatalf("list has %d entries, want 2", n)
	}
}

func TestAddManyAllListed(t *testing.T) {
	a := newRepo(t)
	s, _ := openStore(t)
	if _, err := s.Add(ctx, a.Dir); err != nil {
		t.Fatal(err)
	}
	added, failed, err := s.AddMany(ctx, []string{a.Dir}, "")
	if err != nil || added == nil || failed == nil || len(added) != 0 || len(failed) != 0 {
		t.Fatalf("added %v failed %v err %v", added, failed, err)
	}
}

func TestAddManyRollsBackWhenSaveFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores permissions")
	}
	a, b := newRepo(t), newRepo(t)
	dir := t.TempDir()
	s, err := repos.Open(filepath.Join(dir, "repos.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add(ctx, a.Dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })

	added, failed, err := s.AddMany(ctx, []string{b.Dir}, "")
	if err == nil || added != nil || failed != nil {
		t.Fatalf("added %v failed %v err %v", added, failed, err)
	}
	if n := len(s.List()); n != 1 {
		t.Fatalf("list has %d entries after rollback, want 1", n)
	}
}

func TestAddStillReturnsExistingEntry(t *testing.T) {
	a := newRepo(t)
	s, _ := openStore(t)
	first, err := s.Add(ctx, a.Dir)
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.Add(ctx, a.Dir)
	if err != nil || again.ID != first.ID || len(s.List()) != 1 {
		t.Fatalf("again %+v err %v", again, err)
	}
}
```

If `newRepo` / `openStore` names already exist in the test file, pick unused names (`grep -n "func newRepo\|func openStore" internal/repos/repos_test.go`).

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/repos/...`
Expected: FAIL — `s.AddMany undefined`.

- [ ] **Step 3: Implement** in `internal/repos/repos.go`

Replace `Add` and add the new code after it:

```go
// AddFailure is a path AddMany could not add: Name is the path's last
// element, Reason the error's text.
type AddFailure struct {
	Path   string `json:"path"`
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// Add registers the work tree containing path. Adding a repo twice returns
// the existing entry.
func (s *Store) Add(ctx context.Context, path string) (Repo, error) {
	top, err := toplevel(ctx, path)
	if err != nil {
		return Repo{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.findPath(top); ok {
		return r, nil
	}
	r := Repo{ID: s.uniqueID(top), Name: filepath.Base(top), Path: top}
	s.repos = append(s.repos, r)
	if err := s.save(); err != nil {
		s.repos = s.repos[:len(s.repos)-1]
		return Repo{}, err
	}
	return r, nil
}

// AddMany registers the work trees containing paths, in order, in group
// (trimmed; "" is ungrouped), with a single persisted write. A path whose
// work tree is already listed, or was already added earlier in this call, is
// left as it is (its group untouched) and reported neither as added nor as
// failed. A path that is not in a work tree is reported in failed and the
// others are still added. When the write fails nothing is added: the list is
// restored and only err is returned.
func (s *Store) AddMany(ctx context.Context, paths []string, group string) ([]Repo, []AddFailure, error) {
	group = strings.TrimSpace(group)
	failed := []AddFailure{}
	tops := make([]string, 0, len(paths))
	// git runs outside the lock, as in Add.
	for _, p := range paths {
		top, err := toplevel(ctx, p)
		if err != nil {
			failed = append(failed, AddFailure{Path: p, Name: filepath.Base(p), Reason: err.Error()})
			continue
		}
		tops = append(tops, top)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.repos)
	for _, top := range tops {
		if _, ok := s.findPath(top); ok {
			continue
		}
		s.repos = append(s.repos, Repo{ID: s.uniqueID(top), Name: filepath.Base(top), Path: top, Group: group})
	}
	if len(s.repos) == n {
		return []Repo{}, failed, nil
	}
	if err := s.save(); err != nil {
		s.repos = s.repos[:n]
		return nil, nil, err
	}
	return append([]Repo{}, s.repos[n:]...), failed, nil
}

// findPath is the entry whose path is top. Callers must hold s.mu.
func (s *Store) findPath(top string) (Repo, bool) {
	for _, r := range s.repos {
		if r.Path == top {
			return r, true
		}
	}
	return Repo{}, false
}
```

(`strings` is already imported.)

- [ ] **Step 4: Run the tests to see them pass**

Run: `go test ./internal/repos/... ./internal/app/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd <worktree> && /usr/bin/git add internal/repos && /usr/bin/git commit -m "feat(repos): AddMany adds a batch of repositories in one write, skipping listed ones and reporting failures"
```

---

### Task 3: App bindings, generated Wails bindings, `api.ts` and types

**Files:**
- Create: `internal/app/addrepos.go`
- Test: `internal/app/addrepos_test.go`
- Modify: `frontend/wailsjs/go/app/App.d.ts`, `App.js`, `frontend/wailsjs/go/models.ts` (generated)
- Modify: `frontend/src/lib/types.ts` (append types), `frontend/src/lib/api.ts` (4 entries after `listDirs`)

**Interfaces:**
- Consumes: `scanrepos.Scan` (Task 1), `repos.Store.AddMany`, `repos.AddFailure` (Task 2), existing `canonical(path)` in `internal/app/app.go`, `(*App).DefaultCloneParent()`.
- Produces (Go):
  ```go
  type ScanCandidate struct { Name string `json:"name"`; Path string `json:"path"`; Added bool `json:"added"` }
  type ScanResult struct { Parent string `json:"parent"`; Repos []ScanCandidate `json:"repos"`; Skipped int `json:"skipped"` }
  type AddReposResult struct { Added []repos.Repo `json:"added"`; Failed []repos.AddFailure `json:"failed"` }
  func (a *App) ScanReposIn(parent string) (ScanResult, error)
  func (a *App) AddRepos(paths []string, group string) (AddReposResult, error)
  func (a *App) PickReposParent(start string) (string, error)
  func (a *App) DefaultReposParent() string
  ```
- Produces (TS, `types.ts`):
  ```ts
  export interface ScanCandidate { name: string; path: string; added: boolean }
  export interface ScanResult { parent: string; repos: ScanCandidate[]; skipped: number }
  export interface AddFailure { path: string; name: string; reason: string }
  export interface AddReposResult { added: Repo[]; failed: AddFailure[] }
  ```
- Produces (`api`): `scanReposIn(parent)`, `addRepos(paths, group)`, `pickReposParent(start)`, `defaultReposParent()`.

- [ ] **Step 1: Write the failing tests** — `internal/app/addrepos_test.go`

```go
package app

import (
	"os"
	"path/filepath"
	"testing"

	"git-ui/internal/repos"
	"git-ui/internal/testrepo"
)

func newEmptyApp(t *testing.T) *App {
	t.Helper()
	store, err := repos.Open(filepath.Join(t.TempDir(), "repos.json"))
	if err != nil {
		t.Fatal(err)
	}
	return New(store)
}

func TestScanReposInMarksListedThroughSymlinks(t *testing.T) {
	a := newEmptyApp(t)
	listed := testrepo.New(t)
	listed.Commit("init")
	if _, err := a.store.Add(a.ctx, listed.Dir); err != nil {
		t.Fatal(err)
	}
	parent := t.TempDir()
	// The listed repo appears in parent under another spelling (a symlink);
	// the stored path is git's resolved top-level.
	if err := os.Symlink(listed.Dir, filepath.Join(parent, "a")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(parent, "b", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := a.ScanReposIn(parent)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Repos) != 2 || !got.Repos[0].Added || got.Repos[1].Added {
		t.Fatalf("got %+v", got)
	}
}

func TestScanReposInPassesErrors(t *testing.T) {
	a := newEmptyApp(t)
	if _, err := a.ScanReposIn("relative"); err == nil {
		t.Fatal("relative parent accepted")
	}
}

func TestAddReposNeverNilSlices(t *testing.T) {
	a := newEmptyApp(t)
	r := testrepo.New(t)
	r.Commit("init")

	res, err := a.AddRepos([]string{r.Dir}, "")
	if err != nil || len(res.Added) != 1 || res.Failed == nil {
		t.Fatalf("res %+v err %v", res, err)
	}
	res, err = a.AddRepos([]string{r.Dir}, "")
	if err != nil || res.Added == nil || res.Failed == nil || len(res.Added) != 0 {
		t.Fatalf("second add %+v err %v", res, err)
	}
}
```

`New(store)` sets `a.ctx` to `context.Background()`, so `a.ctx` is safe in tests.

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/app/ -run 'ScanReposIn|AddRepos'`
Expected: FAIL — `a.ScanReposIn undefined`.

- [ ] **Step 3: Implement** — `internal/app/addrepos.go`

```go
package app

import (
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"git-ui/internal/repos"
	"git-ui/internal/scanrepos"
)

// ScanCandidate is a repository found by ScanReposIn; Added when the
// sidebar already lists it.
type ScanCandidate struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Added bool   `json:"added"`
}

// ScanResult is scanrepos.Result with each candidate marked Added or not.
type ScanResult struct {
	Parent  string          `json:"parent"`
	Repos   []ScanCandidate `json:"repos"`
	Skipped int             `json:"skipped"`
}

// AddReposResult is what AddRepos added and what it could not; both never nil.
type AddReposResult struct {
	Added  []repos.Repo       `json:"added"`
	Failed []repos.AddFailure `json:"failed"`
}

// ScanReposIn lists the repositories directly inside parent (as typed: ~
// expanded, relative refused) for "Add from folder…". A candidate is Added
// when a stored entry has the same path once symlinks are resolved.
func (a *App) ScanReposIn(parent string) (ScanResult, error) {
	res, err := scanrepos.Scan(parent)
	if err != nil {
		return ScanResult{}, err
	}
	listed := map[string]bool{}
	for _, r := range a.store.List() {
		listed[canonical(r.Path)] = true
	}
	out := ScanResult{Parent: res.Parent, Repos: make([]ScanCandidate, 0, len(res.Repos)), Skipped: res.Skipped}
	for _, c := range res.Repos {
		out.Repos = append(out.Repos, ScanCandidate{Name: c.Name, Path: c.Path, Added: listed[canonical(c.Path)]})
	}
	return out, nil
}

// AddRepos adds paths to the list in group ("" for none) in one write. See
// repos.Store.AddMany; a failed write is the error and nothing is added.
func (a *App) AddRepos(paths []string, group string) (AddReposResult, error) {
	added, failed, err := a.store.AddMany(a.ctx, paths, group)
	if err != nil {
		return AddReposResult{}, err
	}
	return AddReposResult{Added: added, Failed: failed}, nil
}

// PickReposParent asks for the folder to scan, starting at start; "" when
// the user cancels.
func (a *App) PickReposParent(start string) (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "Add repositories from folder", DefaultDirectory: start})
}

// DefaultReposParent is the folder offered before any scan: home.
func (a *App) DefaultReposParent() string {
	return a.DefaultCloneParent()
}
```

- [ ] **Step 4: Run the Go tests**

Run: `go test ./internal/app/ -run 'ScanReposIn|AddRepos' && go vet ./internal/app/`
Expected: PASS.

- [ ] **Step 5: Regenerate the Wails bindings**

Run: `cd <worktree> && ~/go/bin/wails generate module && /usr/bin/git checkout -- frontend/wailsjs/runtime 2>/dev/null || true; /usr/bin/git status --short frontend/wailsjs`
Expected: `App.d.ts`, `App.js`, `models.ts` modified with `ScanReposIn`, `AddRepos`, `PickReposParent`, `DefaultReposParent` and the `app.ScanResult`, `app.ScanCandidate`, `app.AddReposResult`, `repos.AddFailure` models. If the generator fails, add the four functions by hand following the exact shape of `ListDirs` in `App.d.ts`/`App.js` (models are not needed by `api.ts`, which casts).

- [ ] **Step 6: Add the TS types and API entries**

Append to `frontend/src/lib/types.ts`:

```ts
/** A repository directly inside the scanned folder (Add from folder…). */
export interface ScanCandidate {
  name: string
  path: string
  /** Already in the sidebar. */
  added: boolean
}

export interface ScanResult {
  /** The scanned folder, ~ expanded. */
  parent: string
  repos: ScanCandidate[]
  /** Non-hidden children that are not repositories. */
  skipped: number
}

export interface AddFailure {
  path: string
  name: string
  reason: string
}

export interface AddReposResult {
  added: Repo[]
  failed: AddFailure[]
}
```

In `frontend/src/lib/api.ts`, add `AddReposResult, ScanResult` to the `./types` import and these entries after `listDirs`:

```ts
  scanReposIn: (parent: string) => call<ScanResult>(Go.ScanReposIn(parent)),
  addRepos: (paths: string[], group: string) => call<AddReposResult>(Go.AddRepos(paths, group)),
  pickReposParent: (start: string) => call<string>(Go.PickReposParent(start)),
  defaultReposParent: () => call<string>(Go.DefaultReposParent()),
```

- [ ] **Step 7: Type-check**

Run: `cd <worktree>/frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npm run check`
Expected: 0 errors (warnings that existed before are fine — compare with `git stash`-free baseline by noting the count before your change).

- [ ] **Step 8: Commit**

```bash
cd <worktree> && /usr/bin/git add internal/app/addrepos.go internal/app/addrepos_test.go frontend/wailsjs/go frontend/src/lib/types.ts frontend/src/lib/api.ts && /usr/bin/git commit -m "feat(app): ScanReposIn, AddRepos, PickReposParent and DefaultReposParent bindings"
```

---

### Task 4: `lib/addRepos.ts` — the dialog's pure logic

**Files:**
- Create: `frontend/src/lib/addRepos.ts`
- Test: `frontend/src/lib/addRepos.test.ts`

**Interfaces:**
- Consumes: types `ScanResult`, `AddReposResult`, `Repo` (Task 3).
- Produces:
  ```ts
  export const addReposDialogOpen: Writable<boolean>
  export interface ScanRow { name: string; path: string; added: boolean; ticked: boolean }
  export interface ScanView { parent: string; rows: ScanRow[]; skipped: number }
  export function fromScan(r: ScanResult): ScanView
  export function toggle(v: ScanView, path: string): ScanView
  export function selectAll(v: ScanView): ScanView
  export function selectNone(v: ScanView): ScanView
  export function ticked(v: ScanView | null): string[]
  export function addLabel(n: number): string
  export function skippedLine(n: number): string
  export const NEW_GROUP: string
  export function groupOptions(repos: Repo[]): string[]
  export function chosenGroup(choice: string, newName: string): string | null
  export function folderName(path: string): string
  export interface AddSummary { kind: 'info' | 'error'; text: string; keepOpen: boolean }
  export function summarize(r: AddReposResult): AddSummary
  export interface Latest { next(): number; isCurrent(n: number): boolean; invalidate(): void }
  export function createLatest(): Latest
  ```

- [ ] **Step 1: Write the failing tests** — `frontend/src/lib/addRepos.test.ts`

```ts
import { describe, expect, it } from 'vitest'
import {
  addLabel, chosenGroup, createLatest, folderName, fromScan, groupOptions, NEW_GROUP, selectAll, selectNone, skippedLine, summarize, ticked, toggle,
} from './addRepos'
import type { AddFailure, Repo, ScanResult } from './types'

const scan: ScanResult = {
  parent: '/p',
  skipped: 2,
  repos: [
    { name: 'a', path: '/p/a', added: false },
    { name: 'b', path: '/p/b', added: true },
    { name: 'c', path: '/p/c', added: false },
  ],
}

describe('rows and ticks', () => {
  it('ticks only repositories not yet added', () => {
    const v = fromScan(scan)
    expect(v.rows.map((r) => r.ticked)).toEqual([true, false, true])
    expect(ticked(v)).toEqual(['/p/a', '/p/c'])
    expect(v.skipped).toBe(2)
    expect(v.parent).toBe('/p')
  })
  it('toggle flips an enabled row and ignores an added one', () => {
    const v = fromScan(scan)
    expect(ticked(toggle(v, '/p/a'))).toEqual(['/p/c'])
    expect(ticked(toggle(v, '/p/b'))).toEqual(['/p/a', '/p/c'])
  })
  it('select all / none act on enabled rows only', () => {
    const none = selectNone(fromScan(scan))
    expect(ticked(none)).toEqual([])
    expect(none.rows[1].ticked).toBe(false)
    const all = selectAll(none)
    expect(ticked(all)).toEqual(['/p/a', '/p/c'])
    expect(all.rows[1].ticked).toBe(false)
  })
  it('no view means nothing ticked', () => expect(ticked(null)).toEqual([]))
})

describe('labels', () => {
  it.each([[0, 'Add repositories'], [1, 'Add 1 repository'], [7, 'Add 7 repositories']])('addLabel(%i)', (n, want) =>
    expect(addLabel(n)).toBe(want))
  it.each([
    [0, ''],
    [1, '1 folder skipped (not a git repository).'],
    [3, '3 folders skipped (not git repositories).'],
  ])('skippedLine(%i)', (n, want) => expect(skippedLine(n)).toBe(want))
})

describe('groups', () => {
  const repo = (group?: string) => ({ id: group ?? 'x', name: 'n', path: '/n', missing: false, branch: '', group }) as Repo
  it('lists existing groups once, sorted', () => {
    expect(groupOptions([repo('work'), repo(), repo('Apps'), repo('work')])).toEqual(['Apps', 'work'])
  })
  it('chosenGroup', () => {
    expect(chosenGroup('', 'ignored')).toBe('')
    expect(chosenGroup('work', '')).toBe('work')
    expect(chosenGroup(NEW_GROUP, '  projects ')).toBe('projects')
    expect(chosenGroup(NEW_GROUP, '   ')).toBeNull()
  })
  it.each([
    ['/Users/me/projects', 'projects'],
    ['/Users/me/projects/', 'projects'],
    ['~/code', 'code'],
    ['/', ''],
  ])('folderName(%s)', (p, want) => expect(folderName(p)).toBe(want))
})

describe('summarize', () => {
  const repo = { id: 'i', name: 'n', path: '/n', missing: false, branch: '' } as Repo
  const fail = (name: string): AddFailure => ({ path: '/p/' + name, name, reason: 'not a git repository' })

  it('all added', () => {
    expect(summarize({ added: [repo], failed: [] })).toEqual({ kind: 'info', text: 'Added 1 repository.', keepOpen: false })
    expect(summarize({ added: [repo, repo], failed: [] }).text).toBe('Added 2 repositories.')
  })
  it('nothing new: everything was already listed', () => {
    expect(summarize({ added: [], failed: [] })).toEqual({
      kind: 'info', text: 'Nothing added: those repositories are already in the list.', keepOpen: false,
    })
  })
  it('partial failure names at most three', () => {
    expect(summarize({ added: [repo], failed: [fail('x')] })).toEqual({
      kind: 'error', text: "Added 1 repository; 1 couldn't be added: x (not a git repository).", keepOpen: false,
    })
    const five = ['a', 'b', 'c', 'd', 'e'].map(fail)
    expect(summarize({ added: [repo, repo], failed: five }).text).toBe(
      "Added 2 repositories; 5 couldn't be added: a (not a git repository), b (not a git repository), c (not a git repository), and 2 more.",
    )
  })
  it('all failed keeps the dialog open', () => {
    expect(summarize({ added: [], failed: [fail('a'), fail('b'), fail('c')] })).toEqual({
      kind: 'error',
      text: 'No repository could be added: a (not a git repository), b (not a git repository), c (not a git repository).',
      keepOpen: true,
    })
  })
})

describe('createLatest', () => {
  it('only the newest request is current', () => {
    const l = createLatest()
    const first = l.next()
    const second = l.next()
    expect(l.isCurrent(first)).toBe(false)
    expect(l.isCurrent(second)).toBe(true)
  })
  it('invalidate drops the request in flight (field edited during a scan)', () => {
    const l = createLatest()
    const n = l.next()
    l.invalidate()
    expect(l.isCurrent(n)).toBe(false)
  })
})
```

- [ ] **Step 2: Run them to see them fail**

Run: `cd <worktree>/frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npx vitest run src/lib/addRepos.test.ts`
Expected: FAIL — cannot resolve `./addRepos`.

- [ ] **Step 3: Implement** — `frontend/src/lib/addRepos.ts`

```ts
// "Add from folder…": the dialog's state and wording, kept pure for tests.
import { writable } from 'svelte/store'
import type { AddReposResult, Repo, ScanResult } from './types'

export const addReposDialogOpen = writable(false)

export interface ScanRow {
  name: string
  path: string
  /** Already in the sidebar: shown disabled, never ticked. */
  added: boolean
  ticked: boolean
}

export interface ScanView {
  parent: string
  rows: ScanRow[]
  skipped: number
}

export function fromScan(r: ScanResult): ScanView {
  return {
    parent: r.parent,
    skipped: r.skipped,
    rows: r.repos.map((c) => ({ name: c.name, path: c.path, added: c.added, ticked: !c.added })),
  }
}

const setTicks = (v: ScanView, f: (row: ScanRow) => boolean): ScanView => ({
  ...v,
  rows: v.rows.map((row) => (row.added ? row : { ...row, ticked: f(row) })),
})

export const toggle = (v: ScanView, path: string) => setTicks(v, (row) => (row.path === path ? !row.ticked : row.ticked))
export const selectAll = (v: ScanView) => setTicks(v, () => true)
export const selectNone = (v: ScanView) => setTicks(v, () => false)

/** The ticked paths, in list order. */
export function ticked(v: ScanView | null): string[] {
  return v ? v.rows.filter((r) => r.ticked && !r.added).map((r) => r.path) : []
}

export function addLabel(n: number): string {
  if (n === 0) return 'Add repositories'
  return n === 1 ? 'Add 1 repository' : `Add ${n} repositories`
}

export function skippedLine(n: number): string {
  if (n === 0) return ''
  return n === 1 ? '1 folder skipped (not a git repository).' : `${n} folders skipped (not git repositories).`
}

export const NEW_GROUP = '__new_group__'

/** Every group name in use, once each, sorted. */
export function groupOptions(repos: Repo[]): string[] {
  return [...new Set(repos.map((r) => r.group).filter((g): g is string => !!g))].sort((a, b) => a.localeCompare(b))
}

/** The group to add into: "" for none; null when "New group…" has no name yet. */
export function chosenGroup(choice: string, newName: string): string | null {
  if (choice !== NEW_GROUP) return choice
  const name = newName.trim()
  return name === '' ? null : name
}

/** The last segment of a folder path, as suggested for "New group…". */
export function folderName(path: string): string {
  const trimmed = path.replace(/\/+$/, '')
  return trimmed.slice(trimmed.lastIndexOf('/') + 1)
}

export interface AddSummary {
  kind: 'info' | 'error'
  text: string
  /** Nothing was added and something failed: show text in the dialog instead of a toast. */
  keepOpen: boolean
}

const repositories = (n: number) => (n === 1 ? '1 repository' : `${n} repositories`)

function failures(r: AddReposResult): string {
  const named = r.failed.slice(0, 3).map((f) => `${f.name} (${f.reason})`)
  const more = r.failed.length - named.length
  return named.join(', ') + (more > 0 ? `, and ${more} more` : '') + '.'
}

export function summarize(r: AddReposResult): AddSummary {
  const added = r.added.length
  if (r.failed.length === 0) {
    if (added === 0) return { kind: 'info', text: 'Nothing added: those repositories are already in the list.', keepOpen: false }
    return { kind: 'info', text: `Added ${repositories(added)}.`, keepOpen: false }
  }
  if (added === 0) return { kind: 'error', text: `No repository could be added: ${failures(r)}`, keepOpen: true }
  return { kind: 'error', text: `Added ${repositories(added)}; ${r.failed.length} couldn't be added: ${failures(r)}`, keepOpen: false }
}

/** Numbers requests so only the latest answer is used; invalidate() drops the one in flight. */
export interface Latest {
  next(): number
  isCurrent(n: number): boolean
  invalidate(): void
}

export function createLatest(): Latest {
  let current = 0
  return {
    next: () => ++current,
    isCurrent: (n) => n === current,
    invalidate: () => void ++current,
  }
}
```

- [ ] **Step 4: Run the tests to see them pass**

Run: `cd <worktree>/frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npx vitest run src/lib/addRepos.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd <worktree> && /usr/bin/git add frontend/src/lib/addRepos.ts frontend/src/lib/addRepos.test.ts && /usr/bin/git commit -m "feat(ui): addRepos logic for the Add from folder dialog (ticks, labels, groups, summary, latest scan)"
```

---

### Task 5: Extract `FolderField.svelte` from the Clone dialog (no behaviour change)

**Files:**
- Create: `frontend/src/components/FolderField.svelte`
- Modify: `frontend/src/components/CloneDialog.svelte` (parent field block, its script state and its CSS)

**Interfaces:**
- Consumes: `lib/pathcomplete.ts` (`applyPick, closedList, createCompleter, handleKey, markStale, openList, splitPath`), `api.listDirs`.
- Produces: `<FolderField bind:value bind:this={field} pickFolder={fn} idPrefix="…" onPicked={fn?} onEdit={fn?} autofocus={bool?} />` with
  - `export let value: string` (bindable)
  - `export let pickFolder: (start: string) => Promise<string>` (Choose…)
  - `export let idPrefix: string` (listbox id `${idPrefix}-list`, options `${idPrefix}-opt-${i}`)
  - `export let onPicked: (path: string) => void = () => {}` (after Choose… returns a folder)
  - `export let onEdit: () => void = () => {}` (after every user keystroke that changes the text)
  - `export let autofocus = false`
  - `export function closeList(): void`
  - Enter with no highlighted suggestion is left unhandled, so it submits the surrounding `<form>` (as today in Clone).

- [ ] **Step 1: Create `frontend/src/components/FolderField.svelte`**

Move the parent-field markup, the suggestion logic and the related CSS out of `CloneDialog.svelte` verbatim, parameterised by the props above:

```svelte
<script lang="ts">
  import { onDestroy, tick } from 'svelte'
  import { api } from '../lib/api'
  import { applyPick, closedList, createCompleter, handleKey, markStale, openList, splitPath } from '../lib/pathcomplete'

  /** The folder as typed (bindable); the backend trims it and expands a leading ~. */
  export let value: string
  /** Opens the system folder picker at the given folder; "" when cancelled. */
  export let pickFolder: (start: string) => Promise<string>
  /** Prefix for the listbox and option ids (unique per dialog). */
  export let idPrefix: string
  /** Called after Choose… put a folder in the field. */
  export let onPicked: (path: string) => void = () => {}
  /** Called after the user changed the text by typing. */
  export let onEdit: () => void = () => {}
  export let autofocus = false

  // Folder suggestions: looked up while typing (after a short pause), only the latest answer counts.
  // Programmatic changes (a remembered folder, Choose…) don't open them.
  let list = closedList
  const completer = createCompleter(api.listDirs, (items) => (list = openList(items)), 150)
  const optionID = (i: number) => `${idPrefix}-opt-${i}`
  $: if (list.selected >= 0) tick().then(() => document.getElementById(optionID(list.selected))?.scrollIntoView({ block: 'nearest' }))
  onDestroy(() => completer.cancel())

  function pick(item: string) {
    value = applyPick(item)
    list = closedList
    onEdit()
    // The next level, straight away.
    completer.now(value)
  }

  function onKey(e: KeyboardEvent) {
    // keyCode 229: the key that confirms an IME composition (Safari reports no isComposing then).
    if (e.isComposing || e.keyCode === 229) return
    const r = handleKey(list, e.key, e.shiftKey, value, e.ctrlKey || e.altKey || e.metaKey)
    list = r.state
    if (!r.handled) return
    e.preventDefault()
    if (e.key === 'Escape') {
      // Esc closes the suggestions only, not the dialog, and no lookup may reopen them.
      e.stopPropagation()
      closeList()
    }
    if (r.pick !== null) pick(r.pick)
  }

  export function closeList() {
    completer.cancel()
    list = closedList
  }

  async function choose() {
    closeList()
    try {
      const picked = await pickFolder(value)
      if (picked) {
        value = picked
        onPicked(picked)
      }
    } catch {
      // The picker failed or was dismissed: keep the current folder.
    }
  }
</script>

<span class="parent">
  <span class="field">
    <!-- svelte-ignore a11y_autofocus -->
    <input {autofocus} spellcheck="false" autocomplete="off" placeholder="/path/to/folder or ~/folder" bind:value
      role="combobox" aria-label="Parent folder" aria-autocomplete="list" aria-expanded={list.open} aria-controls={list.open ? `${idPrefix}-list` : undefined}
      aria-activedescendant={list.selected >= 0 ? optionID(list.selected) : undefined}
      on:input={(e) => { list = markStale(list); completer.request(e.currentTarget.value); onEdit() }}
      on:keydown={onKey} on:blur={closeList} />
    {#if list.open}
      <!-- mousedown is kept off the input's blur, scrollbar included. -->
      <ul id="{idPrefix}-list" class="suggest" role="listbox" aria-label="Folders" on:mousedown|preventDefault>
        {#each list.items as item, i (item)}
          {@const parts = splitPath(item)}
          <li id={optionID(i)} role="option" aria-selected={i === list.selected} class:selected={i === list.selected}
            title={item} on:mousedown={(e) => e.button === 0 && pick(item)}>
            <span class="dir">{parts.dir}</span><span class="name">{parts.name}</span>
          </li>
        {/each}
      </ul>
    {/if}
  </span>
  <button type="button" class="btn" on:click={choose}>Choose…</button>
</span>

<style>
  .parent { display: flex; gap: 8px; }
  .field { position: relative; flex: 1; min-width: 0; display: flex; }
  .field input { flex: 1; min-width: 0; font-size: 13px; }
  .suggest {
    position: absolute; top: calc(100% + 4px); left: 0; right: 0; z-index: 5;
    margin: 0; padding: 4px; list-style: none; max-height: 200px; overflow-y: auto;
    background: var(--surface); border: 1px solid var(--border); border-radius: 8px; box-shadow: var(--shadow);
  }
  .suggest li {
    padding: 4px 8px; border-radius: 6px; cursor: pointer; font-family: var(--mono); font-size: 12px;
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  }
  .suggest li:hover { background: var(--hover); }
  .suggest li.selected { background: var(--active); }
  .suggest .dir { color: var(--muted); }
  .suggest .name { color: var(--text); font-weight: 600; }
</style>
```

Note `pick()` now calls `onEdit()`: picking a suggestion changes the text just like typing. In the Clone dialog `onEdit` is a no-op, so nothing changes there.

- [ ] **Step 2: Switch `CloneDialog.svelte` to it**

In the script: delete `tick` from the svelte import, the `pathcomplete` import, `list`, `completer`, `optionID`, the scroll `$:` line, `pick`, `onParentKey`, `closeList`, `choose`; in the `$cloneDialogOpen` block delete `completer.cancel()` and `list = closedList` (the field is destroyed with the dialog). Add:

```ts
  import FolderField from './FolderField.svelte'
  let field: FolderField | undefined
```

In `submit()`, replace `closeList()` with `field?.closeList()`.

Replace the whole `<label>Parent folder …</label>` block with:

```svelte
          <label>Parent folder
            <FolderField bind:this={field} bind:value={parent} pickFolder={api.pickCloneParent} idPrefix="clone-parent" />
          </label>
```

Delete from `<style>` the rules now in FolderField: `.parent`, `.field`, `.field input`, `.suggest` and all `.suggest …` rules. Keep everything else.

- [ ] **Step 3: Verify no regression**

Run: `cd <worktree>/frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npm run check && npx vitest run`
Expected: `npm run check` with no new errors or warnings (in particular no "unused CSS selector" and no unused import in CloneDialog); all vitest suites PASS (`pathcomplete.test.ts`, `clone.test.ts` unchanged).

Then read the diff of `CloneDialog.svelte` once more against the checklist: same placeholder, same aria attributes, same ids (`clone-parent-list`, `clone-parent-opt-N`), Esc in the field still closes only the suggestions, Enter with no suggestion still submits the clone form.

- [ ] **Step 4: Commit**

```bash
cd <worktree> && /usr/bin/git add frontend/src/components/FolderField.svelte frontend/src/components/CloneDialog.svelte && /usr/bin/git commit -m "refactor(ui): FolderField, the Clone dialog's parent folder field with suggestions, as its own component"
```

---

### Task 6: `AddReposDialog`, menu entry, actions and the behaviour spec

**Files:**
- Create: `frontend/src/components/AddReposDialog.svelte`
- Modify: `frontend/src/lib/stores.ts` (add `reposParent` after `cloneParent`, line ~48)
- Modify: `frontend/src/lib/actions.ts` (after `openCloneDialog`, ~line 107)
- Modify: `frontend/src/components/Sidebar.svelte` (Add repo menu, ~line 140; import at line 4)
- Modify: `frontend/src/App.svelte` (import + mount next to `<CloneDialog />`, ~line 207)
- Modify: `docs/spec/01-repositories-and-sidebar.md` (Repository list paragraph at line ~33; new subsection after "Cloning a repository", before "### Worktrees")

**Interfaces:**
- Consumes: Task 3 `api.scanReposIn/addRepos/pickReposParent/defaultReposParent`; Task 4 everything in `lib/addRepos.ts`; Task 5 `FolderField`.
- Produces: `openAddReposDialog(): void`, `addReposFromFolder(paths: string[], group: string): Promise<string>` in `actions.ts` (returns '' when the dialog may close, else the inline error).

- [ ] **Step 1: Store and actions**

`frontend/src/lib/stores.ts`, after `cloneParent`:

```ts
/** The folder "Add from folder…" last scanned successfully, as typed. */
export const reposParent = persisted<string>('reposParent', '')
```

`frontend/src/lib/actions.ts`: import `addReposDialogOpen, summarize` from `./addRepos` and `AddReposResult` type from `./types` (merge into existing import lines), then after `openCloneDialog`:

```ts
export function openAddReposDialog() {
  addReposDialogOpen.set(true)
}

/** Adds the ticked repositories from Add from folder…. Returns '' when the
 *  dialog may close (the toast says what happened), else the error to show
 *  in it (the store write failed, or nothing could be added). */
export async function addReposFromFolder(paths: string[], group: string): Promise<string> {
  let result: AddReposResult
  try {
    result = await api.addRepos(paths, group)
  } catch (e) {
    return errorMessage(e)
  }
  const s = summarize(result)
  if (s.keepOpen) return s.text
  try {
    await loadRepos()
  } catch (e) {
    toast(errorMessage(e), 'error')
  }
  toast(s.text, s.kind)
  return ''
}
```

- [ ] **Step 2: The dialog** — `frontend/src/components/AddReposDialog.svelte`

```svelte
<script lang="ts">
  import { api } from '../lib/api'
  import { addReposFromFolder } from '../lib/actions'
  import {
    addLabel, addReposDialogOpen, chosenGroup, createLatest, folderName, fromScan, groupOptions, NEW_GROUP, selectAll, selectNone,
    skippedLine, ticked, toggle, type ScanView,
  } from '../lib/addRepos'
  import { repos, reposParent } from '../lib/stores'
  import { dialog, errorMessage } from '../lib/ui'
  import FolderField from './FolderField.svelte'

  let parent = ''
  let opened = false
  let field: FolderField | undefined
  let view: ScanView | null = null
  let scanning = false
  let scanError = ''
  let groupChoice = ''
  let newGroup = ''
  let adding = false
  let addError = ''
  const scans = createLatest()

  // Every opening starts afresh at the remembered folder (else home); nothing is scanned yet.
  $: if ($addReposDialogOpen !== opened) {
    opened = $addReposDialogOpen
    if (opened) init()
  }
  async function init() {
    scans.invalidate()
    parent = ''
    view = null
    scanning = false
    scanError = ''
    groupChoice = ''
    newGroup = ''
    adding = false
    addError = ''
    try {
      const initial = $reposParent || (await api.defaultReposParent())
      // Typing before the default arrived wins.
      if (parent === '') parent = initial
    } catch {
      // No default: the user types or picks a folder.
    }
  }

  $: dir = parent.trim()
  $: groups = groupOptions($repos)
  $: paths = ticked(view)
  $: group = chosenGroup(groupChoice, newGroup)
  $: canAdd = !adding && paths.length > 0 && group !== null

  // The list described the folder that was scanned; once the field changes it no longer applies.
  function edited() {
    scans.invalidate()
    view = null
    scanning = false
    scanError = ''
    addError = ''
  }

  async function scan() {
    if (dir === '' || scanning) return
    field?.closeList()
    const n = scans.next()
    scanning = true
    view = null
    scanError = ''
    addError = ''
    try {
      const result = await api.scanReposIn(dir)
      if (!scans.isCurrent(n)) return
      view = fromScan(result)
      // Only a folder that scanned is remembered.
      reposParent.set(dir)
    } catch (e) {
      if (scans.isCurrent(n)) scanError = errorMessage(e)
    } finally {
      if (scans.isCurrent(n)) scanning = false
    }
  }

  function onGroupChange() {
    if (groupChoice === NEW_GROUP && newGroup === '') newGroup = folderName(view?.parent ?? dir)
  }

  async function add() {
    if (!canAdd || group === null) return
    adding = true
    addError = ''
    const err = await addReposFromFolder(paths, group)
    adding = false
    if (err) addError = err
    else addReposDialogOpen.set(false)
  }

  const close = () => addReposDialogOpen.set(false)
  // Escape closes a confirm opened on top of this dialog first.
  const onKey = (e: KeyboardEvent) => { if ($addReposDialogOpen && e.key === 'Escape' && !$dialog && !e.defaultPrevented) close() }
</script>

<svelte:window on:keydown={onKey} />

{#if $addReposDialogOpen}
  <div class="backdrop" on:click|self={close} role="presentation">
    <div class="dialog" role="dialog" aria-label="Add repositories from a folder">
      <h3>Add repositories from a folder</h3>
      <!-- Enter in the field (no suggestion highlighted) submits this form: it scans. -->
      <form on:submit|preventDefault={scan}>
        <label>Parent folder
          <span class="scanrow">
            <FolderField bind:this={field} bind:value={parent} autofocus pickFolder={api.pickReposParent} idPrefix="repos-parent"
              onEdit={edited} onPicked={() => { edited(); scan() }} />
            <button type="submit" class="btn" disabled={dir === '' || scanning}>{scanning ? 'Scanning…' : 'Scan'}</button>
          </span>
        </label>
      </form>

      {#if scanError}
        <p class="problem" role="alert">{scanError}</p>
      {:else if view}
        {#if view.rows.length === 0}
          <p class="muted">No git repositories in this folder.</p>
        {:else}
          <div class="listhead">
            <button type="button" class="link" on:click={() => view && (view = selectAll(view))}>Select all</button>
            <button type="button" class="link" on:click={() => view && (view = selectNone(view))}>None</button>
          </div>
          <ul class="rows" aria-label="Repositories">
            {#each view.rows as row (row.path)}
              <li title={row.path}>
                <label class="row" class:disabled={row.added}>
                  <input type="checkbox" checked={row.ticked} disabled={row.added}
                    on:change={() => view && (view = toggle(view, row.path))} />
                  <span class="name ellipsis">{row.name}</span>
                  {#if row.added}<span class="tag">Already added</span>{/if}
                </label>
              </li>
            {/each}
          </ul>
        {/if}
        {#if view.skipped}<p class="muted">{skippedLine(view.skipped)}</p>{/if}
      {/if}

      <label>Group
        <select bind:value={groupChoice} on:change={onGroupChange}>
          <option value="">No group</option>
          {#each groups as g (g)}<option value={g}>{g}</option>{/each}
          <option value={NEW_GROUP}>New group…</option>
        </select>
      </label>
      {#if groupChoice === NEW_GROUP}
        <label>Group name
          <input spellcheck="false" bind:value={newGroup} />
        </label>
      {/if}

      {#if addError}<p class="problem" role="alert">{addError}</p>{/if}
      <div class="buttons">
        <button type="button" class="btn" on:click={close}>Cancel</button>
        <button type="button" class="btn primary" disabled={!canAdd} on:click={add}>
          {adding ? 'Adding…' : addLabel(paths.length)}
        </button>
      </div>
    </div>
  </div>
{/if}

<style>
  .backdrop { position: fixed; inset: 0; z-index: 40; display: grid; place-items: center; background: rgba(0, 0, 0, 0.25); }
  .dialog {
    width: min(480px, 92vw);
    max-height: 86vh;
    padding: 18px 20px 16px;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 12px;
    box-shadow: var(--shadow);
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  h3 { margin: 0; font-size: 14px; font-weight: 600; }
  p { margin: 0; line-height: 1.5; }
  label { display: flex; flex-direction: column; gap: 5px; color: var(--muted); font-size: 12px; }
  label input:not([type='checkbox']), select { font-size: 13px; }
  .scanrow { display: flex; gap: 8px; }
  .scanrow :global(.parent) { flex: 1; min-width: 0; }
  .listhead { display: flex; gap: 12px; font-size: 12px; }
  .link { background: none; border: 0; padding: 0; color: var(--accent); cursor: pointer; font-size: 12px; }
  .rows {
    margin: 0; padding: 4px; list-style: none; overflow-y: auto; max-height: 260px; min-height: 0;
    border: 1px solid var(--border); border-radius: 8px;
  }
  .row { flex-direction: row; align-items: center; gap: 8px; padding: 4px 6px; border-radius: 6px; color: var(--text); font-size: 13px; cursor: pointer; }
  .row:hover { background: var(--hover); }
  .row.disabled { color: var(--muted); cursor: default; }
  .name { flex: 1; min-width: 0; }
  .tag { font-size: 11px; color: var(--muted); }
  .muted { color: var(--muted); font-size: 12px; }
  .problem { color: var(--danger); font-size: 12px; overflow-wrap: anywhere; white-space: pre-line; }
  .buttons { display: flex; justify-content: flex-end; gap: 8px; margin-top: 4px; }
</style>
```

`--accent` and the global `.ellipsis` class are defined in `frontend/src/theme.css`.

- [ ] **Step 3: Menu entry and mount**

`Sidebar.svelte`: add `openAddReposDialog` to the `../lib/actions` import and make the menu:

```svelte
    on:click={(e) => openMenu(e, [
      { label: 'Open folder…', action: addRepo },
      { label: 'Add from folder…', action: openAddReposDialog },
      { label: 'Clone…', action: openCloneDialog },
    ])}
```

`App.svelte`: `import AddReposDialog from './components/AddReposDialog.svelte'` next to the CloneDialog import, and mount `<AddReposDialog />` right after `<CloneDialog />` (before `<DialogHost />`, per the comment there).

- [ ] **Step 4: Behaviour spec** — `docs/spec/01-repositories-and-sidebar.md`

In "## Repository list", change the sentence `"Add repo" opens a menu with **Open folder…** and **Clone…**.` to:

```markdown
"Add repo" opens a menu with **Open folder…**, **Add from folder…** and
**Clone…**.
```

Insert this subsection right before `### Worktrees`:

```markdown
### Adding repositories from a folder

**Add from folder…** opens a dialog for adding several repositories at once
from one parent folder. The parent folder field behaves like the Clone
dialog's (typed or picked with Choose…; trimmed; a leading `~` expanded; a
relative path refused with "Parent folder must be a full path (or start with
~)."; folder suggestions while typing). It starts at the folder last scanned
successfully (remembered across launches), else the home folder, and nothing
is scanned when the dialog opens. A scan runs on **Scan**, on Enter in the
field when no suggestion is highlighted, and right after Choose… returns a
folder; Scan is disabled while the field is empty and reads "Scanning…"
while a scan runs. Editing the field clears the result, and only the latest
scan's answer is shown.

Only the folder's **direct children** are considered; the scan never goes
deeper and the folder itself is never offered. A child is listed when its
name does not start with `.`, it is a folder (or a symlink to one) and its
`.git` is a folder. A child whose `.git` is a file (a linked worktree or a
submodule checkout) is not listed. Every other child that is not hidden
counts as skipped, shown below the list as "N folders skipped (not git
repositories)." ("1 folder skipped (not a git repository)."). The scan reads
the file system only; it runs no git command.

The list is sorted by name, ignoring case, one row per repository with a
checkbox; the full path is the row's tooltip. Repositories not yet in the
sidebar start ticked; those already in it (same path once symlinks are
resolved) are unticked, disabled and labelled "Already added". **Select
all** and **None** act on the enabled rows. With no repositories the dialog
says "No git repositories in this folder.". A missing parent says "That
folder doesn't exist.", a parent that is a file "That path isn't a folder.",
and an unreadable one "That folder can't be read: " followed by the system's
reason.

A **Group** select offers "No group" (the default), every group in use
(sorted) and "New group…", which shows a name field prefilled with the
scanned folder's name; Add is disabled while that name is empty.

**Add N repositories** ("Add 1 repository"; "Add repositories", disabled,
when nothing is ticked) adds the ticked rows in list order, each by the same
rule as Open folder… (its top-level directory), with the chosen group, in a
single write of the repository list; it reads "Adding…" meanwhile. A ticked
repository already in the list by then is left as it is, group included, and
two rows with the same top-level are added once. A row that can no longer be
added (for example a folder deleted since the scan) does not stop the
others. Selection does not change. Afterwards:

- everything added: the dialog closes and a toast says "Added N
  repositories." ("Added 1 repository.");
- nothing new (all already listed): the dialog closes and a toast says
  "Nothing added: those repositories are already in the list.";
- some added, some failed: the dialog closes and an error toast says
  "Added N repositories; M couldn't be added: " followed by at most three
  "name (reason)" entries, then ", and K more" when there are more;
- none added and some failed, or the list could not be written: the dialog
  stays open with the error; when the write fails nothing at all is added.

Adding takes no repository lock and leaves no command-log entry: only the
repository list changes.
```

- [ ] **Step 5: Verify**

Run: `cd <worktree>/frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npm run check && npx vitest run` then `cd <worktree> && go test ./... && go vet ./...`
Expected: no new check errors/warnings; all tests PASS.

Then build once to be sure the bundle compiles: `cd <worktree>/frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npm run build`. Expected: success. Restore anything the build touched outside `frontend/dist` (`/usr/bin/git status --short`; `frontend/dist` is ignored or untracked — do not commit it).

- [ ] **Step 6: Commit**

```bash
cd <worktree> && /usr/bin/git add frontend/src docs/spec/01-repositories-and-sidebar.md && /usr/bin/git commit -m "feat(ui): Add from folder… dialog adds several repositories from a parent folder; spec 01 updated"
```

---

## After the tasks

- Final whole-branch review (Opus), including a spec-compliance pass of the
  whole feature against the design spec and `docs/spec/01-…` (not only the
  diff), and a check that the Clone dialog's parent field behaves as before.
- Manual pass by the owner: `make dev` from the worktree (Node 22), a real
  folder with several repos, one already added, one hidden, one worktree;
  then the Clone dialog's suggestions. Restore `frontend/wailsjs/runtime`
  before merging. Merge `--no-ff` only when the owner says.
