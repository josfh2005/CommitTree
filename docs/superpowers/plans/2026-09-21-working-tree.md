# Working Tree (Sub-project 2a) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** See the working tree's changes, stage and unstage by file, discard, and commit — with the commit message written by the task model — as a third mode in the main pane.

**Architecture:** A new `internal/worktree` package reads `git status --porcelain=v2 -z` and performs the mutations, mirroring `internal/merge`'s shape and its path rules. The app layer wraps each mutation in the per-repo write lock and emits `worktree:changed`. The frontend gains a shared `FileList` component (extracted from the merge view) and a `ChangesView` that uses it, plus a commit box that streams an AI-written message.

**Tech Stack:** Go 1.26, Wails v2, Svelte 5, vitest.

**Spec:** `docs/superpowers/specs/2026-09-21-working-tree-design.md`

## Global Constraints

- A path from the renderer is accepted only when it exactly equals one that `worktree.Status` just returned; every git command taking such a path passes `--literal-pathspecs`. A bare path is a pathspec — `:(glob)*` and `*` must never match.
- `Discard` refuses a path that is a directory prefix of another listed path, and refuses anything that is not a regular file or a symlink (never follow, never recurse).
- A commit message reaches git through a temporary file with `-F`, never on the command line.
- Every mutation runs under the app's per-repo write lock (`a.write`) and emits `worktree:changed`.
- The AI commit message uses the **task** provider and model (`responderFor`), never the chat one, and only ever summarises the **staged** diff.
- Commit messages must NOT contain `Co-Authored-By` lines.
- Go: `go vet ./... && go test ./...` and `gofmt -l internal` clean.
- Frontend: prefix every command with `export PATH=$HOME/.nvm/versions/node/v22.23.1/bin:$PATH` (the default shell node is v14 and fails). `npm run check` must end with 0 ERRORS; the 3 a11y warnings in ContextMenu/Splitter/Sidebar are pre-existing.
- Never commit `frontend/wailsjs/runtime`; `make build` restores it.

---

### Task 1: Read the working tree's status

**Files:**
- Create: `internal/worktree/worktree.go`
- Create: `internal/worktree/worktree_test.go`

**Interfaces:**
- Consumes: `internal/gitcmd` (`gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, args...)`), `internal/testrepo` in tests.
- Produces:
  ```go
  package worktree
  type FileStatus struct {
      Path    string `json:"path"`
      OldPath string `json:"oldPath,omitempty"`
      Status  string `json:"status"` // M A D R T ?
  }
  type State struct {
      Staged    []FileStatus `json:"staged"`
      Unstaged  []FileStatus `json:"unstaged"`
      Untracked []FileStatus `json:"untracked"`
      Merging   bool         `json:"merging"`
  }
  func Status(ctx context.Context, dir string) (State, error)
  ```

- [ ] **Step 1: Write the failing test**

Create `internal/worktree/worktree_test.go`:

```go
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
	r.WriteFile("a.txt", "staged\n")
	r.Git("add", "a.txt")
	r.WriteFile("b.txt", "tracked\n")
	r.Git("add", "b.txt")
	r.Git("commit", "-q", "-m", "add b")
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
```

`testrepo.Repo` has no helper for a command that is expected to fail. Add one next to `Git` in `internal/testrepo/testrepo.go`:

```go
// GitFails runs git and returns its combined output without failing the test
// when git exits non-zero — for commands whose failure is the point, such as
// a merge that conflicts.
func (r *Repo) GitFails(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.Dir
	out, _ := cmd.CombinedOutput()
	return string(out)
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/worktree/`
Expected: FAIL — the package does not exist.

- [ ] **Step 3: Write the implementation**

Create `internal/worktree/worktree.go`:

```go
// Package worktree reads and changes the working tree: what has changed,
// what is staged, and the commit that closes it. Conflicts are the merge
// package's business; this one only reports that a merge is in progress.
package worktree

import (
	"context"
	"strings"

	"git-ui/internal/gitcmd"
)

// FileStatus is one changed path. Status is git's letter for it: M modified,
// A added, D deleted, R renamed, T type-changed, ? untracked.
type FileStatus struct {
	Path    string `json:"path"`
	OldPath string `json:"oldPath,omitempty"`
	Status  string `json:"status"`
}

// State is what the Changes view shows. A file that is both staged and
// modified appears in Staged and in Unstaged, which is what git means.
type State struct {
	Staged    []FileStatus `json:"staged"`
	Unstaged  []FileStatus `json:"unstaged"`
	Untracked []FileStatus `json:"untracked"`
	Merging   bool         `json:"merging"`
}

// Status reads the working tree with porcelain v2, which reports the staged
// and unstaged state of every path in one call, names a rename's source, and
// with -z survives spaces, newlines and non-ASCII in paths.
func Status(ctx context.Context, dir string) (State, error) {
	st := State{Staged: []FileStatus{}, Unstaged: []FileStatus{}, Untracked: []FileStatus{}}
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout,
		"status", "--porcelain=v2", "-z", "--untracked-files=all")
	if err != nil {
		return State{}, err
	}
	fields := strings.Split(out, "\x00")
	for i := 0; i < len(fields); i++ {
		rec := fields[i]
		if rec == "" {
			continue
		}
		switch rec[0] {
		case '1': // ordinary change: "1 XY ... <path>"
			x, y, path := parseChange(rec)
			st.add(x, y, path, "")
		case '2': // rename or copy: "2 XY ... <path>" then the source in the next field
			x, y, path := parseChange(rec)
			old := ""
			if i+1 < len(fields) {
				i++
				old = fields[i]
			}
			st.add(x, y, path, old)
		case 'u': // unmerged: the merge view owns this repository
			st.Merging = true
		case '?':
			st.Untracked = append(st.Untracked, FileStatus{Path: strings.TrimPrefix(rec, "? "), Status: "?"})
		}
	}
	return st, nil
}

// parseChange pulls the two status letters and the path out of a v2 entry.
// The path is the ninth space-separated field for an ordinary change and the
// tenth for a rename, but it may itself contain spaces, so it is taken as
// everything after the known count of fields.
func parseChange(rec string) (x, y byte, path string) {
	parts := strings.SplitN(rec, " ", 9)
	if len(parts) < 9 || len(parts[1]) != 2 {
		return '.', '.', ""
	}
	x, y = parts[1][0], parts[1][1]
	path = parts[8]
	if rec[0] == '2' {
		// A rename entry has one more field (the similarity score) before the path.
		if sub := strings.SplitN(path, " ", 2); len(sub) == 2 {
			path = sub[1]
		}
	}
	return x, y, path
}

// add files one entry into the staged and unstaged lists. '.' means that side
// is unchanged; git reports both in one entry.
func (s *State) add(x, y byte, path, old string) {
	if path == "" {
		return
	}
	if x != '.' {
		s.Staged = append(s.Staged, FileStatus{Path: path, OldPath: old, Status: string(x)})
	}
	if y != '.' {
		// The unstaged side of a rename is a change to the new path; the old
		// path only exists staged.
		s.Unstaged = append(s.Unstaged, FileStatus{Path: path, Status: string(y)})
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/worktree/ -v`
Expected: PASS for all eight tests. If the rename test fails on the path, print one raw record (`fmt.Printf("%q", rec)`) in the test to see the real field layout before adjusting `parseChange` — the field counts differ between entry kinds.

- [ ] **Step 5: Commit**

```bash
git add internal/worktree internal/testrepo
git commit -m "feat(worktree): read the working tree's status"
```

---

### Task 2: Stage, unstage and discard

**Files:**
- Create: `internal/worktree/stage.go`
- Create: `internal/worktree/stage_test.go`

**Interfaces:**
- Consumes: `Status`, `FileStatus`, `State` from Task 1.
- Produces:
  ```go
  var ErrNotInWorktree = errors.New("worktree: not a changed file of this repository")
  func Stage(ctx context.Context, dir, path string) error
  func Unstage(ctx context.Context, dir, path string) error
  func Discard(ctx context.Context, dir, path string) error
  ```

- [ ] **Step 1: Write the failing test**

Create `internal/worktree/stage_test.go`:

```go
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
func TestDiscardRefusesAPathThatIsAlsoADirectory(t *testing.T) {
	r := base(t)
	if err := os.MkdirAll(filepath.Join(r.Dir, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	r.WriteFile("d/inside.txt", "keep me\n")
	r.WriteFile("d", "") // cannot exist as both; assert on the directory case only
	_ = os.Remove(filepath.Join(r.Dir, "d"))

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
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/worktree/ -run 'Stage|Unstage|Discard|Operations'`
Expected: FAIL — `worktree.Stage` is undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/worktree/stage.go`:

```go
package worktree

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"git-ui/internal/gitcmd"
)

// ErrNotInWorktree refuses a path that is not one Status just listed. Paths
// are compared as exact strings against what git printed, so pathspec magic
// such as ":(glob)*" can never match one.
var ErrNotInWorktree = errors.New("worktree: not a changed file of this repository")

// Stage adds one unstaged or untracked path to the index.
func Stage(ctx context.Context, dir, path string) error {
	st, err := Status(ctx, dir)
	if err != nil {
		return err
	}
	if err := listed(st, path, append(paths(st.Unstaged), paths(st.Untracked)...)); err != nil {
		return err
	}
	_, err = gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "--literal-pathspecs", "add", "--", path)
	return err
}

// Unstage takes one staged path out of the index and keeps the worktree as
// it is. A path HEAD does not have becomes untracked again, which is where it
// came from and where the view still lists it.
func Unstage(ctx context.Context, dir, path string) error {
	st, err := Status(ctx, dir)
	if err != nil {
		return err
	}
	if err := listed(st, path, paths(st.Staged)); err != nil {
		return err
	}
	_, err = gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "--literal-pathspecs", "restore", "--staged", "--", path)
	return err
}

// Discard throws a path's changes away. For a tracked path it restores from
// HEAD, taking the staged change with it; for an untracked one there is
// nothing to restore to, so the file is deleted — which the caller confirms
// first, because git cannot undo it.
func Discard(ctx context.Context, dir, path string) error {
	st, err := Status(ctx, dir)
	if err != nil {
		return err
	}
	all := append(append(paths(st.Staged), paths(st.Unstaged)...), paths(st.Untracked)...)
	if err := listed(st, path, all); err != nil {
		return err
	}
	if slices.Contains(paths(st.Untracked), path) {
		return deleteUntracked(dir, path)
	}
	_, err = gitcmd.Run(ctx, dir, gitcmd.ReadTimeout,
		"--literal-pathspecs", "restore", "--source=HEAD", "--staged", "--worktree", "--", path)
	return err
}

// deleteUntracked removes a file the repository never tracked. It refuses
// anything that is not a regular file or a symlink, so a planted directory or
// FIFO cannot make this delete outside the repository or block on open.
func deleteUntracked(dir, path string) error {
	full := filepath.Join(dir, path)
	info, err := os.Lstat(full)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("worktree: %s is not a regular file; remove it in a terminal", path)
	}
	return os.Remove(full)
}

// listed checks that path is one git printed, and that it is not also a
// directory of this repository: a literal pathspec still matches everything
// under it, so "d" would reach "d/inside.txt" too.
func listed(st State, path string, allowed []string) error {
	if !slices.Contains(allowed, path) {
		return fmt.Errorf("%w: %q", ErrNotInWorktree, path)
	}
	for _, list := range [][]string{paths(st.Staged), paths(st.Unstaged), paths(st.Untracked)} {
		for _, p := range list {
			if strings.HasPrefix(p, path+"/") {
				return fmt.Errorf("%w: %q is also a directory here; act on its files", ErrNotInWorktree, path)
			}
		}
	}
	return nil
}

func paths(files []FileStatus) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		out = append(out, f.Path)
	}
	return out
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/worktree/ -v`
Expected: PASS for every test in the package.

- [ ] **Step 5: Commit**

```bash
git add internal/worktree
git commit -m "feat(worktree): stage, unstage and discard one listed path"
```

---

### Task 3: Commit and amend

**Files:**
- Create: `internal/worktree/commit.go`
- Create: `internal/worktree/commit_test.go`

**Interfaces:**
- Consumes: `Status`, `State` from Task 1.
- Produces:
  ```go
  var ErrNothingStaged = errors.New("worktree: nothing is staged")
  type CommitInfo struct {
      StagedCount int    `json:"stagedCount"`
      CanAmend    bool   `json:"canAmend"`
      LastMessage string `json:"lastMessage"`
      Pushed      bool   `json:"pushed"`
      Upstream    string `json:"upstream"`
  }
  func Commit(ctx context.Context, dir, message string, amend bool) error
  func Preview(ctx context.Context, dir string) (CommitInfo, error)
  ```

- [ ] **Step 1: Write the failing test**

Create `internal/worktree/commit_test.go`:

```go
package worktree_test

import (
	"errors"
	"strings"
	"testing"

	"git-ui/internal/testrepo"
	"git-ui/internal/worktree"
)

func TestCommitStagedChanges(t *testing.T) {
	r := base(t)
	r.WriteFile("a.txt", "changed\n")
	r.Git("add", "a.txt")

	if err := worktree.Commit(ctx, r.Dir, "change a", false); err != nil {
		t.Fatal(err)
	}
	if got := r.Git("log", "-1", "--pretty=%s"); got != "change a" {
		t.Errorf("subject = %q", got)
	}
	if st := status(t, r.Dir); len(st.Staged) != 0 {
		t.Errorf("staged = %v after committing", paths(st.Staged))
	}
}

// A real message has a subject, a blank line, a body, and often quotes —
// none of which may reach git through a command line.
func TestCommitKeepsAMultiLineMessageIntact(t *testing.T) {
	r := base(t)
	r.WriteFile("a.txt", "changed\n")
	r.Git("add", "a.txt")
	message := "fix: don't \"quote\" the shell\n\nIt broke on `backticks` and $VARS.\nSecond line.\n"

	if err := worktree.Commit(ctx, r.Dir, message, false); err != nil {
		t.Fatal(err)
	}
	got := r.Git("log", "-1", "--pretty=%B")
	for _, want := range []string{`don't "quote" the shell`, "`backticks` and $VARS", "Second line."} {
		if !strings.Contains(got, want) {
			t.Errorf("message = %q, missing %q", got, want)
		}
	}
}

func TestCommitRefusesWithNothingStaged(t *testing.T) {
	r := base(t)
	r.WriteFile("a.txt", "changed but not staged\n")
	if err := worktree.Commit(ctx, r.Dir, "nope", false); !errors.Is(err, worktree.ErrNothingStaged) {
		t.Errorf("err = %v, want ErrNothingStaged", err)
	}
}

func TestCommitRefusesAnEmptyMessage(t *testing.T) {
	r := base(t)
	r.WriteFile("a.txt", "changed\n")
	r.Git("add", "a.txt")
	if err := worktree.Commit(ctx, r.Dir, "   \n", false); err == nil {
		t.Error("want an error for a blank message")
	}
}

func TestAmendReplacesTheLastCommit(t *testing.T) {
	r := base(t)
	before := r.Git("rev-parse", "HEAD")
	r.WriteFile("a.txt", "amended\n")
	r.Git("add", "a.txt")

	if err := worktree.Commit(ctx, r.Dir, "add a, properly", true); err != nil {
		t.Fatal(err)
	}
	if got := r.Git("log", "-1", "--pretty=%s"); got != "add a, properly" {
		t.Errorf("subject = %q", got)
	}
	if got := r.Git("rev-list", "--count", "HEAD"); got != "1" {
		t.Errorf("commit count = %s, want the commit replaced, not added", got)
	}
	if r.Git("rev-parse", "HEAD") == before {
		t.Error("HEAD did not move")
	}
}

// Amending with nothing staged only rewrites the message, which is the most
// common use of it.
func TestAmendWithNothingStagedRewritesTheMessage(t *testing.T) {
	r := base(t)
	if err := worktree.Commit(ctx, r.Dir, "better subject", true); err != nil {
		t.Fatal(err)
	}
	if got := r.Git("log", "-1", "--pretty=%s"); got != "better subject" {
		t.Errorf("subject = %q", got)
	}
}

func TestPreviewCountsStagedFilesAndOffersTheLastMessage(t *testing.T) {
	r := base(t)
	r.WriteFile("a.txt", "changed\n")
	r.Git("add", "a.txt")
	r.WriteFile("b.txt", "new\n")
	r.Git("add", "b.txt")

	info, err := worktree.Preview(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.StagedCount != 2 || !info.CanAmend || info.LastMessage != "add a" {
		t.Errorf("info = %+v", info)
	}
	if info.Pushed || info.Upstream != "" {
		t.Errorf("info = %+v, want no upstream", info)
	}
}

func TestPreviewCannotAmendAnEmptyRepository(t *testing.T) {
	r := testrepo.New(t)
	info, err := worktree.Preview(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.CanAmend {
		t.Errorf("info = %+v, want CanAmend false with no commits", info)
	}
}

// Amending a commit the upstream already has rewrites shared history; the
// dialog needs to know.
func TestPreviewReportsAPushedCommit(t *testing.T) {
	src := testrepo.New(t)
	src.Commit("base")
	bare := testrepo.NewBareFrom(t, src)
	clone := testrepo.Clone(t, bare)

	info, err := worktree.Preview(ctx, clone.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Pushed || info.Upstream != "origin/main" {
		t.Errorf("info = %+v, want pushed on origin/main", info)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/worktree/ -run 'Commit|Amend|Preview'`
Expected: FAIL — `worktree.Commit` is undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/worktree/commit.go`:

```go
package worktree

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"git-ui/internal/gitcmd"
)

// ErrNothingStaged refuses a commit with an empty index, which git would
// reject anyway, with a message about the working tree instead of the index.
var ErrNothingStaged = errors.New("worktree: nothing is staged")

// CommitInfo is what the commit box needs to decide what it can offer.
type CommitInfo struct {
	StagedCount int    `json:"stagedCount"`
	CanAmend    bool   `json:"canAmend"`    // false in a repository with no commits
	LastMessage string `json:"lastMessage"` // the message Amend starts from
	Pushed      bool   `json:"pushed"`      // the commit Amend would rewrite is on the upstream
	Upstream    string `json:"upstream"`
}

// Commit closes the staged changes. An amend rewrites the last commit, and is
// allowed with nothing staged because rewriting only the message is the
// common case.
func Commit(ctx context.Context, dir, message string, amend bool) error {
	if strings.TrimSpace(message) == "" {
		return errors.New("worktree: the commit message is empty")
	}
	if !amend {
		st, err := Status(ctx, dir)
		if err != nil {
			return err
		}
		if len(st.Staged) == 0 {
			return ErrNothingStaged
		}
	}
	// The message goes through a file: on a command line a long body with
	// quotes, newlines or a leading dash is at the mercy of quoting and of
	// ARG_MAX.
	path, cleanup, err := messageFile(ctx, dir, message)
	if err != nil {
		return err
	}
	defer cleanup()

	args := []string{"commit", "-F", path}
	if amend {
		args = append(args, "--amend")
	}
	_, err = gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, args...)
	return err
}

// messageFile writes the message inside the repository's own git directory,
// so it never lands in the working tree and never shows up as untracked.
func messageFile(ctx context.Context, dir, message string) (string, func(), error) {
	gitDir, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return "", func() {}, err
	}
	f, err := os.CreateTemp(strings.TrimSpace(gitDir), "git-ui-commit-*")
	if err != nil {
		return "", func() {}, err
	}
	name := f.Name()
	cleanup := func() { os.Remove(name) }
	if _, err := f.WriteString(message); err != nil {
		f.Close()
		cleanup()
		return "", func() {}, err
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return name, cleanup, nil
}

// Preview reports what the commit box may offer: how much is staged, whether
// there is a commit to amend, its message, and whether amending it would
// rewrite something the upstream already has.
func Preview(ctx context.Context, dir string) (CommitInfo, error) {
	st, err := Status(ctx, dir)
	if err != nil {
		return CommitInfo{}, err
	}
	info := CommitInfo{StagedCount: len(st.Staged)}

	if _, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--verify", "--quiet", "HEAD"); err != nil {
		return info, nil // no commits yet: nothing to amend
	}
	info.CanAmend = true
	if msg, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "log", "-1", "--pretty=%B"); err == nil {
		info.LastMessage = strings.TrimRight(msg, "\n")
	}

	up, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	if err != nil {
		return info, nil // no upstream: nothing can have been pushed
	}
	info.Upstream = strings.TrimSpace(up)
	// HEAD is on the upstream when it is an ancestor of it — counted rather
	// than compared, so a fast-forwarded upstream still counts.
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-list", "--count", "HEAD", "^@{upstream}", "--")
	if err != nil {
		return info, nil
	}
	ahead, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return info, fmt.Errorf("worktree: count ahead: %w", err)
	}
	info.Pushed = ahead == 0
	return info, nil
}
```

Drop the `path/filepath` import if nothing in the file ends up using it.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/worktree/ -v`
Expected: PASS for every test. If `TestPreviewReportsAPushedCommit` fails on the branch name, check what `testrepo.Clone` calls the default branch and use that.

- [ ] **Step 5: Commit**

```bash
git add internal/worktree
git commit -m "feat(worktree): commit and amend with the message in a file"
```

---

### Task 4: App layer

**Files:**
- Create: `internal/app/worktree.go`
- Create: `internal/app/worktree_test.go`
- Modify: `internal/app/app.go` (nothing structural — only if a helper needs exporting)

**Interfaces:**
- Consumes: `internal/worktree` (Tasks 1-3); `a.write(id, fn)` and `a.emit(name, data)` from `internal/app`.
- Produces:
  ```go
  const EventWorktreeChanged = "worktree:changed"
  type WorktreeChangedEvent struct{ RepoID string `json:"repoID"` }
  func (a *App) GetWorktreeState(id string) (worktree.State, error)
  func (a *App) StageFile(id, path string) error
  func (a *App) UnstageFile(id, path string) error
  func (a *App) DiscardFile(id, path string) error
  func (a *App) CommitChanges(id, message string, amend bool) error
  func (a *App) GetCommitPreview(id string) (worktree.CommitInfo, error)
  func (a *App) GetWorktreeDiff(id, path string, staged bool) (string, error)
  ```

- [ ] **Step 1: Write the failing test**

Create `internal/app/worktree_test.go`. Reuse the existing helper in `internal/app/merge_test.go` that builds an app around a temp repository — read it first and follow its shape; it returns `(a *App, r *testrepo.Repo, id string)`.

```go
package app

import (
	"errors"
	"strings"
	"testing"

	"git-ui/internal/worktree"
)

// The wrappers refuse a path git did not list, change nothing and announce
// nothing; a real change is announced once as worktree:changed.
func TestWorktreeActionsRejectOrAnnounce(t *testing.T) {
	a, r, id, ev := newAIMergeApp(t, "http://127.0.0.1:0")
	r.WriteFile("a.txt", "changed\n")

	for _, err := range []error{
		a.StageFile(id, ":(glob)*"),
		a.UnstageFile(id, "*"),
		a.DiscardFile(id, "../escape.txt"),
	} {
		if !errors.Is(err, worktree.ErrNotInWorktree) {
			t.Errorf("err = %v, want ErrNotInWorktree", err)
		}
	}
	if n := countEvents(ev, EventWorktreeChanged); n != 0 {
		t.Fatalf("%d worktree:changed events after refusals, want 0", n)
	}

	if err := a.StageFile(id, "a.txt"); err != nil {
		t.Fatal(err)
	}
	if n := countEvents(ev, EventWorktreeChanged); n != 1 {
		t.Errorf("%d worktree:changed events, want 1 per change", n)
	}
	st, err := a.GetWorktreeState(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Staged) != 1 || st.Staged[0].Path != "a.txt" {
		t.Errorf("staged = %+v", st.Staged)
	}
}

func TestCommitChangesAnnouncesAndClearsTheIndex(t *testing.T) {
	a, r, id, ev := newAIMergeApp(t, "http://127.0.0.1:0")
	r.WriteFile("a.txt", "changed\n")
	if err := a.StageFile(id, "a.txt"); err != nil {
		t.Fatal(err)
	}
	if err := a.CommitChanges(id, "change a", false); err != nil {
		t.Fatal(err)
	}
	if got := r.Git("log", "-1", "--pretty=%s"); got != "change a" {
		t.Errorf("subject = %q", got)
	}
	if n := countEvents(ev, EventWorktreeChanged); n < 2 {
		t.Errorf("%d worktree:changed events, want one for the stage and one for the commit", n)
	}
}

func TestGetWorktreeDiffRefusesAnUnlistedPath(t *testing.T) {
	a, r, id, _ := newAIMergeApp(t, "http://127.0.0.1:0")
	r.WriteFile("a.txt", "changed\n")
	if _, err := a.GetWorktreeDiff(id, "../secrets.txt", false); err == nil {
		t.Error("want a refusal for a path outside the listed changes")
	}
	out, err := a.GetWorktreeDiff(id, "a.txt", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "changed") {
		t.Errorf("diff = %q, want the change in it", out)
	}
}
```

`newAIMergeApp` builds a repository with a conflicting history. If its shape does not fit, add a smaller helper beside it in this file that creates an app over a plain repository with one commit — do not change `newAIMergeApp` itself, other tests depend on it.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/app/ -run Worktree`
Expected: FAIL — `a.StageFile` is undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/app/worktree.go`:

```go
package app

import (
	"context"
	"fmt"
	"slices"

	"git-ui/internal/gitcmd"
	"git-ui/internal/worktree"
)

// EventWorktreeChanged tells the frontend the working tree moved, so the
// Changes view refreshes without polling.
const EventWorktreeChanged = "worktree:changed"

type WorktreeChangedEvent struct {
	RepoID string `json:"repoID"`
}

func (a *App) GetWorktreeState(id string) (worktree.State, error) {
	dir, err := a.dir(id)
	if err != nil {
		return worktree.State{}, err
	}
	return worktree.Status(a.ctx, dir)
}

func (a *App) StageFile(id, path string) error {
	return a.writeWorktree(id, func(ctx context.Context, dir string) error {
		return worktree.Stage(ctx, dir, path)
	})
}

func (a *App) UnstageFile(id, path string) error {
	return a.writeWorktree(id, func(ctx context.Context, dir string) error {
		return worktree.Unstage(ctx, dir, path)
	})
}

func (a *App) DiscardFile(id, path string) error {
	return a.writeWorktree(id, func(ctx context.Context, dir string) error {
		return worktree.Discard(ctx, dir, path)
	})
}

func (a *App) CommitChanges(id, message string, amend bool) error {
	return a.writeWorktree(id, func(ctx context.Context, dir string) error {
		return worktree.Commit(ctx, dir, message, amend)
	})
}

func (a *App) GetCommitPreview(id string) (worktree.CommitInfo, error) {
	dir, err := a.dir(id)
	if err != nil {
		return worktree.CommitInfo{}, err
	}
	return worktree.Preview(a.ctx, dir)
}

// GetWorktreeDiff returns what one changed file shows in the pane: the staged
// diff against HEAD, or the unstaged diff against the index. Only a path the
// status just listed can be read, so a path from the renderer cannot be used
// to read the disk.
func (a *App) GetWorktreeDiff(id, path string, staged bool) (string, error) {
	dir, err := a.dir(id)
	if err != nil {
		return "", err
	}
	st, err := worktree.Status(a.ctx, dir)
	if err != nil {
		return "", err
	}
	known := []string{}
	for _, list := range [][]worktree.FileStatus{st.Staged, st.Unstaged, st.Untracked} {
		for _, f := range list {
			known = append(known, f.Path)
		}
	}
	if !slices.Contains(known, path) {
		return "", fmt.Errorf("%q is not a changed file of this repository", path)
	}
	args := []string{"--literal-pathspecs", "diff"}
	if staged {
		args = append(args, "--cached")
	}
	args = append(args, "--", path)
	return gitcmd.Run(a.ctx, dir, gitcmd.ReadTimeout, args...)
}

// writeWorktree runs fn under the repository's write lock, so it cannot
// interleave with a merge action or an agent tool call, then tells the
// frontend the working tree moved.
func (a *App) writeWorktree(id string, fn func(ctx context.Context, dir string) error) error {
	if err := a.write(id, fn); err != nil {
		return err
	}
	a.emit(EventWorktreeChanged, WorktreeChangedEvent{RepoID: id})
	return nil
}
```

- [ ] **Step 4: Run the whole Go suite**

Run: `go vet ./... && go test ./...`
Expected: everything passes.

- [ ] **Step 5: Regenerate the bindings and commit**

```bash
export PATH=$HOME/.nvm/versions/node/v22.23.1/bin:$HOME/go/bin:$PATH
wails generate module
git checkout -- frontend/wailsjs/runtime
grep -n "StageFile\|CommitChanges\|GetWorktreeState" frontend/wailsjs/go/app/App.d.ts
git add internal/app frontend/wailsjs/go
git commit -m "feat(app): expose the working tree under the write lock"
```

---

### Task 5: The AI commit message

**Files:**
- Create: `internal/ai/prompts/defaults/commit-message.md`
- Modify: `internal/ai/prompts/prompts.go` (add the `CommitMessage` name)
- Modify: `internal/ai/tasks/explain.go` → add `CommitContext` (or a new `internal/ai/tasks/commit.go`)
- Create: `internal/ai/tasks/commit_test.go`
- Modify: `internal/ai/settings/settings.go` (the `CommitMessage` setting)
- Modify: `internal/ai/settings/settings_test.go`
- Modify: `internal/app/worktree.go` (add `GenerateCommitMessage`)
- Modify: `internal/app/worktree_test.go`

**Interfaces:**
- Consumes: `responderFor(provider, model string, cfg settings.Settings) (ai.Responder, error)` and `a.aiSettings()` from `internal/app/ai.go`; `prompts.Store.Get(name, vars)`; `tasks.OllamaDiffBudget`; `tools.Truncate`.
- Produces:
  ```go
  // prompts
  const CommitMessage = "commit-message"
  // tasks
  func CommitContext(ctx context.Context, dir string, budget int) (string, error)
  // settings
  const (
      CommitAutoLocal = "auto-local" // default
      CommitAuto      = "auto"
      CommitManual    = "manual"
  )
  // Settings gains: CommitMessage string `json:"commitMessage"`
  // app
  const EventCommitDelta = "commit:delta"
  const EventCommitDone  = "commit:done"
  func (a *App) GenerateCommitMessage(id, runID string) error
  ```

- [ ] **Step 1: Write the failing tests**

Create `internal/ai/tasks/commit_test.go`:

```go
package tasks_test

import (
	"context"
	"strings"
	"testing"

	"git-ui/internal/ai/tasks"
	"git-ui/internal/testrepo"
)

// The message must describe what is being committed, so the context carries
// the staged diff and not the unstaged one.
func TestCommitContextCarriesOnlyTheStagedDiff(t *testing.T) {
	r := testrepo.New(t)
	r.WriteFile("a.txt", "one\n")
	r.Git("add", "a.txt")
	r.Git("commit", "-q", "-m", "base")
	r.WriteFile("a.txt", "STAGED CHANGE\n")
	r.Git("add", "a.txt")
	r.WriteFile("b.txt", "UNSTAGED CHANGE\n")

	got, err := tasks.CommitContext(context.Background(), r.Dir, 6000)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "STAGED CHANGE") {
		t.Errorf("context = %q, missing the staged change", got)
	}
	if strings.Contains(got, "UNSTAGED CHANGE") {
		t.Errorf("context = %q, leaks the unstaged change", got)
	}
}

// A truncated diff must say so, or the model invents what it could not see.
func TestCommitContextSaysWhenItTruncated(t *testing.T) {
	r := testrepo.New(t)
	r.WriteFile("a.txt", "one\n")
	r.Git("add", "a.txt")
	r.Git("commit", "-q", "-m", "base")
	r.WriteFile("a.txt", strings.Repeat("a long line of change\n", 500))
	r.Git("add", "a.txt")

	got, err := tasks.CommitContext(context.Background(), r.Dir, 200)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) > 2000 {
		t.Errorf("context is %d bytes, want it cut to the budget", len(got))
	}
	if !strings.Contains(strings.ToLower(got), "truncated") {
		t.Errorf("context = %q, does not say it was truncated", got)
	}
}
```

Append to `internal/ai/settings/settings_test.go`:

```go
func TestCommitMessageModeDefaultsAndValidates(t *testing.T) {
	if got := settings.Defaults().CommitMessage; got != settings.CommitAutoLocal {
		t.Errorf("default = %q, want auto-local", got)
	}
	path := filepath.Join(t.TempDir(), "ai.json")
	s := settings.Defaults()
	s.CommitMessage = "sometimes"
	if err := settings.Save(path, s); !errors.Is(err, settings.ErrInvalid) {
		t.Errorf("err = %v, want ErrInvalid for an unknown mode", err)
	}
	// A file written before this setting existed must still load.
	if err := os.WriteFile(path, []byte(`{"ollamaURL":"http://localhost:11434","chatProvider":"ollama","chatModel":"m","taskProvider":"ollama","taskModel":"m"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := settings.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.CommitMessage != settings.CommitAutoLocal {
		t.Errorf("loaded = %q, want the default filled in", loaded.CommitMessage)
	}
}
```

Append to `internal/app/worktree_test.go`:

```go
// The generated message streams as commit:delta events and ends with
// commit:done; nothing is committed by generating one.
func TestGenerateCommitMessageStreams(t *testing.T) {
	srv := fakeOllama(t, nil) // its canned reply is enough; we assert on events
	a, r, id, ev := newAIMergeApp(t, srv.URL)
	r.WriteFile("a.txt", "changed\n")
	if err := a.StageFile(id, "a.txt"); err != nil {
		t.Fatal(err)
	}
	if err := a.GenerateCommitMessage(id, "run1"); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, EventCommitDelta)
	ev.wait(t, EventCommitDone)
	if got := r.Git("rev-list", "--count", "HEAD"); got != "1" {
		t.Errorf("commit count = %s; generating a message committed something", got)
	}
}

func TestGenerateCommitMessageRefusesWithNothingStaged(t *testing.T) {
	a, _, id, _ := newAIMergeApp(t, "http://127.0.0.1:0")
	if err := a.GenerateCommitMessage(id, "run1"); err == nil {
		t.Error("want an error with nothing staged")
	}
}
```

`fakeOllama(t *testing.T, onChat func(req map[string]any)) *httptest.Server` already exists in `internal/app/ai_test.go` and serves a canned reply; pass `nil` for the hook. `newAIMergeApp` and `countEvents` are in `internal/app/merge_test.go`, and `ev.wait(t, name)` returns the event. Assert on the events, not on the reply's text.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/ai/tasks/ ./internal/ai/settings/ ./internal/app/ -run 'Commit'`
Expected: FAIL — `tasks.CommitContext`, `settings.CommitAutoLocal` and `a.GenerateCommitMessage` are undefined.

- [ ] **Step 3: Write the prompt default**

Create `internal/ai/prompts/defaults/commit-message.md`:

```markdown
You write git commit messages for a developer working in the repository "{{repo}}", on the branch "{{branch}}".

Write the message for the staged changes below. A subject line of at most 72 characters in the imperative mood, then, only when the change needs it, a blank line and two or three short lines saying why. No bullet points, no markdown, no closing remarks, no quotes around the message. Follow the repository's existing convention if the diff shows one, such as a "feat(scope):" prefix. If the diff is marked as truncated, describe only what you can see and do not guess at the rest.

Answer with the commit message and nothing else.
```

Add the name to `internal/ai/prompts/prompts.go`:

```go
const (
	Chat             = "chat"
	ExplainCommit    = "explain-commit"
	ResolveConflicts = "resolve-conflicts"
	CommitMessage    = "commit-message"
)
```

- [ ] **Step 4: Write `CommitContext`**

Create `internal/ai/tasks/commit.go`:

```go
package tasks

import (
	"context"
	"fmt"
	"strings"

	"git-ui/internal/ai/tools"
	"git-ui/internal/gitcmd"
	"git-ui/internal/worktree"
)

// CommitContext describes the staged changes for the model: the file list and
// the staged diff, cut to budget bytes. The unstaged changes are deliberately
// absent — the message must describe what is being committed.
func CommitContext(ctx context.Context, dir string, budget int) (string, error) {
	st, err := worktree.Status(ctx, dir)
	if err != nil {
		return "", err
	}
	diff, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "diff", "--cached")
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString("Staged files:\n")
	for _, f := range st.Staged {
		if f.OldPath != "" {
			fmt.Fprintf(&b, "%s %s -> %s\n", f.Status, f.OldPath, f.Path)
			continue
		}
		fmt.Fprintf(&b, "%s %s\n", f.Status, f.Path)
	}
	cut := tools.Truncate(diff, budget)
	b.WriteString("\nStaged diff:\n")
	if len(cut) < len(diff) {
		b.WriteString("(truncated: the diff is longer than the budget)\n")
	}
	b.WriteString(cut)
	return b.String(), nil
}
```

- [ ] **Step 5: Add the setting**

In `internal/ai/settings/settings.go`, add the constants, the field and the validation:

```go
const (
	// CommitAutoLocal generates the message automatically when the task
	// provider is local and free, and offers a button otherwise.
	CommitAutoLocal = "auto-local"
	CommitAuto      = "auto"
	CommitManual    = "manual"
)
```

```go
type Settings struct {
	OllamaURL     string `json:"ollamaURL"`
	ChatProvider  string `json:"chatProvider"`
	ChatModel     string `json:"chatModel"`
	TaskProvider  string `json:"taskProvider"`
	TaskModel     string `json:"taskModel"`
	CommitMessage string `json:"commitMessage"`
}
```

`Defaults()` sets `CommitMessage: CommitAutoLocal`. `Load` fills an empty value with the default, the way it already does for the providers. `validate` rejects anything that is not one of the three.

- [ ] **Step 6: Add `GenerateCommitMessage`**

In `internal/app/worktree.go`:

```go
// EventCommitDelta streams the generated commit message into the box, and
// EventCommitDone closes it. They mirror the chat's delta/done pair.
const (
	EventCommitDelta = "commit:delta"
	EventCommitDone  = "commit:done"
)

type CommitDeltaEvent struct {
	RepoID string `json:"repoID"`
	RunID  string `json:"runID"`
	Text   string `json:"text"`
}

type CommitDoneEvent struct {
	RepoID string `json:"repoID"`
	RunID  string `json:"runID"`
	Error  string `json:"error,omitempty"`
}

// GenerateCommitMessage streams a commit message for the staged changes. It
// uses the task provider and model — the cheap one — and never commits.
func (a *App) GenerateCommitMessage(id, runID string) error {
	if a.ai == nil {
		return ErrAIDisabled
	}
	if runID == "" {
		return errors.New("run id is required")
	}
	repo, ok := a.store.Get(id)
	if !ok {
		return fmt.Errorf("unknown repository %q", id)
	}
	st, err := worktree.Status(a.ctx, repo.Path)
	if err != nil {
		return err
	}
	if len(st.Staged) == 0 {
		return worktree.ErrNothingStaged
	}
	cfg, err := a.aiSettings()
	if err != nil {
		return err
	}
	responder, err := a.responderFor(cfg.TaskProvider, cfg.TaskModel, cfg)
	if err != nil {
		return err
	}
	instructions, err := a.ai.deps.Prompts.Get(prompts.CommitMessage, prompts.Vars{
		Repo: repo.Name, Path: repo.Path, Branch: refs.CurrentLabel(a.ctx, repo.Path), Date: time.Now().Format("2006-01-02"),
	})
	if err != nil {
		return err
	}
	prompt, err := tasks.CommitContext(a.ctx, repo.Path, tasks.OllamaDiffBudget)
	if err != nil {
		return err
	}

	go func() {
		stream, err := responder.Respond(a.ctx, instructions, prompt)
		if err != nil {
			a.emit(EventCommitDone, CommitDoneEvent{RepoID: id, RunID: runID, Error: err.Error()})
			return
		}
		done := CommitDoneEvent{RepoID: id, RunID: runID}
		for chunk := range stream {
			switch {
			case chunk.Err != nil:
				done.Error = chunk.Err.Error()
			case chunk.Delta != "":
				a.emit(EventCommitDelta, CommitDeltaEvent{RepoID: id, RunID: runID, Text: chunk.Delta})
			}
		}
		a.emit(EventCommitDone, done)
	}()
	return nil
}
```

Add the imports this needs: `errors`, `time`, `git-ui/internal/ai/prompts`, `git-ui/internal/ai/tasks`, `git-ui/internal/refs`.

- [ ] **Step 7: Run everything and commit**

Run: `go vet ./... && go test ./... && gofmt -l internal`
Expected: passes, `gofmt` prints nothing.

```bash
export PATH=$HOME/.nvm/versions/node/v22.23.1/bin:$HOME/go/bin:$PATH
wails generate module && git checkout -- frontend/wailsjs/runtime
git add internal frontend/wailsjs/go
git commit -m "feat(ai): write the commit message from the staged diff"
```

---

### Task 6: Extract the shared file list

**Files:**
- Create: `frontend/src/components/FileList.svelte`
- Modify: `frontend/src/components/MergeView.svelte`
- Modify: `frontend/src/lib/merge.ts` (only if a type moves)

**Interfaces:**
- Produces:
  ```svelte
  <!-- FileList.svelte -->
  export let sections: { title: string; files: { path: string; status: string; oldPath?: string }[] }[]
  export let selected: string
  export let onSelect: (path: string) => void
  export let actions: (file) => { label: string; run: () => void; danger?: boolean }[] = () => []
  export let onMenu: (event: MouseEvent, file) => void = () => {}
  export let glyph: (status: string) => string = (s) => s
  ```

This task changes working merge code. The merge view's existing vitest tests and `npm run check` are the safety net: they must pass unchanged at the end.

- [ ] **Step 1: Read what you are extracting**

Read `frontend/src/components/MergeView.svelte` in full, and `frontend/src/lib/merge.ts`. The list already has: section headers with counts, a row per file with a status glyph, a hover action button (Stage/Unstage), a context menu on Manual rows, and the selected-row styling. Those are exactly the parts that move.

- [ ] **Step 2: Write `FileList.svelte`**

Move the markup and the styles for the sections, rows, glyphs and hover actions into `frontend/src/components/FileList.svelte`, driven by the props above. Keep the styles with the component; do not leave a copy behind in `MergeView.svelte`.

The row stays a `<button>` for selection with a separate `<button>` per action, as it is today — a button inside a button is invalid HTML and `npm run check` will say so.

- [ ] **Step 3: Rewrite `MergeView.svelte` to use it**

Replace the inline list with `<FileList sections={...} selected={selected} onSelect={open} actions={actionsFor} onMenu={manualMenu} />`, where `actionsFor(file)` returns Stage for an unstaged file, Unstage for a staged one, and nothing for a conflict or a Manual file.

Behaviour must not change: same sections, same buttons, same context menu on Manual rows, same selection.

- [ ] **Step 4: Verify nothing moved**

```bash
export PATH=$HOME/.nvm/versions/node/v22.23.1/bin:$PATH
cd frontend && npm run check && npx vitest run
```
Expected: 0 errors, every existing test passes. The merge tests exercise `mergeSections`, not the component, so also open the app and click through a merge if one is available — or accept the check plus the review.

- [ ] **Step 5: Commit**

```bash
git add frontend/src
git commit -m "refactor(ui): extract the merge view's file list into FileList"
```

---

### Task 7: The Changes view

**Files:**
- Create: `frontend/src/components/ChangesView.svelte`
- Create: `frontend/src/lib/worktree.ts`
- Create: `frontend/src/lib/worktree.test.ts`
- Modify: `frontend/src/lib/types.ts`, `frontend/src/lib/api.ts`, `frontend/src/lib/stores.ts`, `frontend/src/lib/actions.ts`
- Modify: `frontend/src/components/Sidebar.svelte` (or `RepoRefs.svelte` — whichever owns the rows above the branches; read both)
- Modify: the component that chooses between the log and the merge view (read `frontend/src/App.svelte`)

**Interfaces:**
- Consumes: bindings from Task 4 (`GetWorktreeState`, `StageFile`, `UnstageFile`, `DiscardFile`, `GetWorktreeDiff`), `FileList` from Task 6.
- Produces:
  ```ts
  export interface FileStatus { path: string; oldPath?: string; status: string }
  export interface WorktreeState { staged: FileStatus[]; unstaged: FileStatus[]; untracked: FileStatus[]; merging: boolean }
  export function worktreeSections(state: WorktreeState): { title: string; files: FileStatus[] }[]
  export function changedCount(state: WorktreeState | null): number
  export function discardMessage(file: FileStatus, staged: boolean): string
  export const worktreeState: Writable<WorktreeState | null>
  export async function loadWorktreeState(): Promise<void>
  ```

- [ ] **Step 1: Write the failing test**

Create `frontend/src/lib/worktree.test.ts`:

```ts
import { describe, expect, it } from 'vitest'
import { changedCount, discardMessage, worktreeSections } from './worktree'
import type { WorktreeState } from './types'

const state = (over: Partial<WorktreeState> = {}): WorktreeState => ({
  staged: [],
  unstaged: [],
  untracked: [],
  merging: false,
  ...over,
})

describe('worktreeSections', () => {
  it('groups staged, unstaged and untracked in that order', () => {
    const sections = worktreeSections(
      state({
        staged: [{ path: 'a.ts', status: 'M' }],
        unstaged: [{ path: 'b.ts', status: 'M' }],
        untracked: [{ path: 'c.ts', status: '?' }],
      }),
    )
    expect(sections.map((s) => s.title)).toEqual(['Staged', 'Unstaged', 'Untracked'])
  })

  it('leaves out empty sections', () => {
    expect(worktreeSections(state({ staged: [{ path: 'a.ts', status: 'M' }] })).map((s) => s.title)).toEqual(['Staged'])
  })

  it('is empty when nothing changed', () => {
    expect(worktreeSections(state())).toEqual([])
  })
})

describe('changedCount', () => {
  it('counts a file that is both staged and modified once', () => {
    const s = state({ staged: [{ path: 'a.ts', status: 'M' }], unstaged: [{ path: 'a.ts', status: 'M' }] })
    expect(changedCount(s)).toBe(1)
  })

  it('is zero with no state', () => {
    expect(changedCount(null)).toBe(0)
  })
})

describe('discardMessage', () => {
  it('warns that an untracked file is deleted for good', () => {
    const m = discardMessage({ path: 'notes.txt', status: '?' }, false)
    expect(m).toContain('notes.txt')
    expect(m).toContain('delete')
    expect(m).toMatch(/cannot be undone|unrecoverable/i)
  })

  it('says a staged change is thrown away too', () => {
    expect(discardMessage({ path: 'a.ts', status: 'M' }, true)).toContain('staged')
  })
})
```

- [ ] **Step 2: Run it and see it fail**

```bash
export PATH=$HOME/.nvm/versions/node/v22.23.1/bin:$PATH
cd frontend && npx vitest run src/lib/worktree.test.ts
```
Expected: FAIL — `./worktree` does not exist.

- [ ] **Step 3: Write `worktree.ts` and the types**

```ts
import type { FileStatus, WorktreeState } from './types'

export interface WorktreeSection {
  title: string
  files: FileStatus[]
}

/** worktreeSections groups the change lists for the file list, in the order
 *  the user acts on them: what will be committed, then what will not. */
export function worktreeSections(state: WorktreeState): WorktreeSection[] {
  return [
    { title: 'Staged', files: state.staged },
    { title: 'Unstaged', files: state.unstaged },
    { title: 'Untracked', files: state.untracked },
  ].filter((s) => s.files.length > 0)
}

/** changedCount is what the sidebar row shows: distinct paths, so a file that
 *  is both staged and modified counts once. */
export function changedCount(state: WorktreeState | null): number {
  if (!state) return 0
  const paths = new Set<string>()
  for (const list of [state.staged, state.unstaged, state.untracked]) {
    for (const f of list) paths.add(f.path)
  }
  return paths.size
}

/** discardMessage is the confirmation before throwing changes away — the only
 *  destructive action here, and for an untracked file git cannot undo it. */
export function discardMessage(file: FileStatus, staged: boolean): string {
  if (file.status === '?') {
    return `Delete ${file.path}? It was never committed, so this cannot be undone.`
  }
  const also = staged ? ' Its staged changes are thrown away too.' : ''
  return `Discard your changes to ${file.path}?${also} This cannot be undone.`
}
```

Add to `frontend/src/lib/types.ts`:

```ts
export interface FileStatus { path: string; oldPath?: string; status: string }
export interface WorktreeState { staged: FileStatus[]; unstaged: FileStatus[]; untracked: FileStatus[]; merging: boolean }
export interface CommitInfo { stagedCount: number; canAmend: boolean; lastMessage: string; pushed: boolean; upstream: string }
```

Add to `frontend/src/lib/api.ts`, next to the merge wrappers:

```ts
  getWorktreeState: (id: string) => call<WorktreeState>(Go.GetWorktreeState(id)),
  stageFile: (id: string, path: string) => call<void>(Go.StageFile(id, path)),
  unstageFile: (id: string, path: string) => call<void>(Go.UnstageFile(id, path)),
  discardFile: (id: string, path: string) => call<void>(Go.DiscardFile(id, path)),
  getWorktreeDiff: (id: string, path: string, staged: boolean) => call<string>(Go.GetWorktreeDiff(id, path, staged)),
  getCommitPreview: (id: string) => call<CommitInfo>(Go.GetCommitPreview(id)),
  commitChanges: (id: string, message: string, amend: boolean) => call<void>(Go.CommitChanges(id, message, amend)),
  generateCommitMessage: (id: string, runID: string) => call<void>(Go.GenerateCommitMessage(id, runID)),
```

Add to `frontend/src/lib/stores.ts`, modelled on `loadMergeState` — including its guard, which exists because a slow answer for a repository the user has left must not overwrite the one now on screen:

```ts
export const worktreeState = writable<WorktreeState | null>(null)

export async function loadWorktreeState() {
  const repo = get(selectedRepo)
  if (!repo || repo.missing) {
    worktreeState.set(null)
    return
  }
  try {
    const state = await api.getWorktreeState(repo.id)
    if (get(selectedRepoId) !== repo.id) return
    worktreeState.set(state)
  } catch {
    if (get(selectedRepoId) === repo.id) worktreeState.set(null)
  }
}
```

Call `loadWorktreeState()` wherever `loadMergeState()` is already called: on repository selection, on refresh, and on window focus.

- [ ] **Step 4: Write the actions**

In `frontend/src/lib/actions.ts`, following the existing `run(...)` helper:

```ts
export const stageFile = (id: string, path: string) => run('Staging…', () => api.stageFile(id, path))
export const unstageFile = (id: string, path: string) => run('Unstaging…', () => api.unstageFile(id, path))

export async function discardFile(id: string, file: FileStatus, staged: boolean) {
  const ok = await confirmDialog({
    title: file.status === '?' ? 'Delete file' : 'Discard changes',
    message: discardMessage(file, staged),
    confirmLabel: file.status === '?' ? 'Delete' : 'Discard',
    danger: true,
  })
  if (ok) await run('Discarding…', () => api.discardFile(id, file.path))
}
```

- [ ] **Step 5: Write `ChangesView.svelte`**

Two panes, like `MergeView.svelte`: `FileList` on the left with `worktreeSections($worktreeState)`, the diff on the right. Reuse `MergeView`'s `lineClass` for the diff colouring — if it is still local to that component, move it to `frontend/src/lib/merge.ts` (or a new `lib/diff.ts`) and import it in both, rather than copying it.

Selecting a file calls `api.getWorktreeDiff(repoId, path, staged)`, where `staged` is true when the row came from the Staged section. Guard the response with a request counter, as `MergeView` does, so a slow answer cannot overwrite a newer selection.

Subscribe to `worktree:changed` for this repository and reload the state, and re-read the open file whenever the state changes — the same shape the merge view uses.

Row actions: **Stage** on an unstaged or untracked row, **Unstage** on a staged one, and **Discard** on every row.

- [ ] **Step 6: Route to it**

In the sidebar, add a row above the branches reading "Changes" with `changedCount($worktreeState)` when it is not zero. Clicking it sets the main pane's mode to `changes`; clicking a commit in the log sets it back to `log`. While `$mergeState?.merging` is true the merge view still wins — the sidebar row then routes there instead.

- [ ] **Step 7: Verify**

```bash
export PATH=$HOME/.nvm/versions/node/v22.23.1/bin:$PATH
cd frontend && npm run check && npx vitest run
```
Expected: 0 errors, all tests pass including the new ones.

- [ ] **Step 8: Commit**

```bash
git add frontend/src
git commit -m "feat(ui): a Changes view for the working tree"
```

---

### Task 8: The commit box

**Files:**
- Create: `frontend/src/components/CommitBox.svelte`
- Modify: `frontend/src/components/ChangesView.svelte`
- Modify: `frontend/src/lib/worktree.ts`, `frontend/src/lib/worktree.test.ts`
- Modify: `frontend/src/lib/actions.ts`, `frontend/src/lib/api.ts` (if a wrapper is still missing)
- Modify: `frontend/src/components/SettingsDialog.svelte` (the commit-message mode)

**Interfaces:**
- Consumes: `CommitInfo`, `commitChanges`, `getCommitPreview`, `GenerateCommitMessage` and the `commit:delta` / `commit:done` events from Tasks 4-5.
- Produces:
  ```ts
  export function canCommit(info: CommitInfo | null, message: string, amend: boolean): boolean
  export function shouldAutoGenerate(mode: string, taskProvider: string, message: string, touched: boolean): boolean
  export function amendWarning(info: CommitInfo): string | null
  ```

- [ ] **Step 1: Write the failing tests**

Append to `frontend/src/lib/worktree.test.ts`:

```ts
import { amendWarning, canCommit, shouldAutoGenerate } from './worktree'
import type { CommitInfo } from './types'

const info = (over: Partial<CommitInfo> = {}): CommitInfo => ({
  stagedCount: 1,
  canAmend: true,
  lastMessage: 'previous',
  pushed: false,
  upstream: '',
  ...over,
})

describe('canCommit', () => {
  it('needs a message and something staged', () => {
    expect(canCommit(info(), 'a message', false)).toBe(true)
    expect(canCommit(info(), '   ', false)).toBe(false)
    expect(canCommit(info({ stagedCount: 0 }), 'a message', false)).toBe(false)
  })

  it('allows an amend with nothing staged, which only rewrites the message', () => {
    expect(canCommit(info({ stagedCount: 0 }), 'better subject', true)).toBe(true)
  })

  it('is false without a preview', () => {
    expect(canCommit(null, 'a message', false)).toBe(false)
  })
})

describe('shouldAutoGenerate', () => {
  it('generates for a local provider in auto-local', () => {
    expect(shouldAutoGenerate('auto-local', 'ollama', '', false)).toBe(true)
    expect(shouldAutoGenerate('auto-local', 'anthropic', '', false)).toBe(false)
  })

  it('generates for any provider in auto, and never in manual', () => {
    expect(shouldAutoGenerate('auto', 'anthropic', '', false)).toBe(true)
    expect(shouldAutoGenerate('manual', 'ollama', '', false)).toBe(false)
  })

  it('never overwrites what the user typed', () => {
    expect(shouldAutoGenerate('auto', 'ollama', 'my own message', false)).toBe(false)
    expect(shouldAutoGenerate('auto', 'ollama', '', true)).toBe(false)
  })
})

describe('amendWarning', () => {
  it('warns when the commit is already on the upstream', () => {
    const m = amendWarning(info({ pushed: true, upstream: 'origin/main' }))
    expect(m).toContain('origin/main')
    expect(m).toMatch(/force push/i)
  })

  it('is null for a commit that was never pushed', () => {
    expect(amendWarning(info())).toBeNull()
  })
})
```

- [ ] **Step 2: Run them and see them fail**

```bash
export PATH=$HOME/.nvm/versions/node/v22.23.1/bin:$PATH
cd frontend && npx vitest run src/lib/worktree.test.ts
```
Expected: FAIL — `canCommit` is not exported.

- [ ] **Step 3: Write the three functions**

Append to `frontend/src/lib/worktree.ts`:

```ts
import type { CommitInfo } from './types'

/** canCommit gates the Commit button. An amend may have nothing staged: it
 *  then only rewrites the message, which is the common use of it. */
export function canCommit(info: CommitInfo | null, message: string, amend: boolean): boolean {
  if (!info || message.trim() === '') return false
  if (amend) return info.canAmend
  return info.stagedCount > 0
}

/** shouldAutoGenerate decides whether to write the message without being
 *  asked. It never overwrites what the user typed: `touched` stays true once
 *  they edit the box, until they clear it. */
export function shouldAutoGenerate(mode: string, taskProvider: string, message: string, touched: boolean): boolean {
  if (message.trim() !== '' || touched) return false
  if (mode === 'manual') return false
  if (mode === 'auto') return true
  return taskProvider === 'ollama' // auto-local: only the local, free provider
}

/** amendWarning is the confirmation before rewriting a commit the upstream
 *  already has, or null when there is nothing to warn about. */
export function amendWarning(info: CommitInfo): string | null {
  if (!info.pushed || !info.upstream) return null
  return `This commit is already on ${info.upstream}. Amending rewrites it, so pushing afterwards will need a force push. Amend anyway?`
}
```

- [ ] **Step 4: Write `CommitBox.svelte`**

- A `<textarea>` bound to the message; typing sets `touched = true`, clearing it sets `touched = false`.
- A **Write with AI** button calling `api.generateCommitMessage(repoId, crypto.randomUUID())`, which appends `commit:delta` text for its own run id and stops on `commit:done`; while streaming the button reads **Stop** and calls the existing chat-stop path for that repository. Ignore events whose `runID` is not the current one.
- Auto-generation: whenever the staged set changes, call `shouldAutoGenerate($aiSettings.commitMessage, $aiSettings.taskProvider, message, touched)` and generate when it is true.
- An **Amend** checkbox: ticking it loads `info.lastMessage` into an untouched box; unticking restores what was there before.
- A **Commit** button, `disabled={!canCommit(info, message, amend) || !!$busy}`. On click, when `amend && amendWarning(info)` is non-null, confirm first; then `api.commitChanges(...)`, clear the box, and refresh.
- Errors from git go to a toast with `errorMessage(e)`, unchanged — git's own wording is what the user searches for.

- [ ] **Step 5: Add the setting to the dialog**

In `SettingsDialog.svelte`, add a select for the commit-message mode bound to `settings.commitMessage`, with the three options labelled: "Automatic for local models (default)", "Always automatic", "Only when I ask". Put it next to the task provider, since that is what it depends on.

- [ ] **Step 6: Verify**

```bash
export PATH=$HOME/.nvm/versions/node/v22.23.1/bin:$PATH
cd frontend && npm run check && npx vitest run
```
Expected: 0 errors, all tests pass.

- [ ] **Step 7: Commit**

```bash
git add frontend/src
git commit -m "feat(ui): commit box with an AI-written message and amend"
```

---

### Task 9: Build, verify and document

**Files:**
- Modify: `README.md` (if it lists what the app can do)
- Modify: `docs/superpowers/specs/2026-09-21-working-tree-design.md` (the `Status:` line)

- [ ] **Step 1: Build and launch**

```bash
export PATH=$HOME/.nvm/versions/node/v22.23.1/bin:$PATH
make build && pkill -x git-ui; sleep 1; open build/bin/git-ui.app
```
If `git status` then shows `frontend/wailsjs/runtime` modified, run `git checkout -- frontend/wailsjs/runtime`.

- [ ] **Step 2: Walk the checklist by hand**

In a scratch repository (create one under your temp directory — do NOT use the user's repositories):

- [ ] The sidebar shows "Changes" with a count that matches `git status`.
- [ ] Staged, Unstaged and Untracked list the right files; a file edited after staging appears in two sections.
- [ ] Clicking a file shows its diff; an untracked file shows its contents.
- [ ] Stage, Unstage and Discard each do what they say, and the list refreshes.
- [ ] Discarding an untracked file asks first and then deletes it.
- [ ] Editing a file in a terminal updates the view when the window regains focus.
- [ ] Commit writes the commit and it appears in the log.
- [ ] Amend rewrites the last commit rather than adding one.
- [ ] A commit with a multi-line message keeps its body.
- [ ] Starting a merge routes the sidebar row to the merge view.
- [ ] With Ollama as the task provider, the message is written automatically; typing stops it being overwritten.

- [ ] **Step 3: Document**

If `README.md` lists features, add the working tree. Set the spec's `Status:` to `Implemented`.

- [ ] **Step 4: Commit**

```bash
git add README.md docs/superpowers/specs/2026-09-21-working-tree-design.md
git commit -m "docs: the working tree view"
```

---

## Notes for the executor

- **The path rules are not optional.** They come from a review that found a bare path is a pathspec, so `:(glob)*` from the renderer could reach files the user never selected. Every mutating entry point re-reads the status and compares exact strings.
- **`internal/merge` is the sibling to copy from**, in package shape, error style and tests: real temporary repositories, awkward names, no mocks of git.
- **Do not widen the scope.** No hunks, no push, no stash: they are 2b, and the spec says so.
- **The AI never commits.** It writes text into a box the user edits and submits.
