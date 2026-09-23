# Submodules Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A "Submodules" section in each expanded repository listing every submodule flat (recursive), submodules that open like repositories, Initialise / Update / Sync actions, and gitlink changes rendered as "moved a → b" in Changes and commit details.

**Architecture:** A new `internal/submodules` package reads submodules without `git submodule status` (which aborts on a gitlink missing from `.gitmodules`): gitlinks from `ls-files -s`, names/URLs from `.gitmodules`, content state from `status --porcelain=v2`, recursing into each initialised submodule. `App.ListRepos` appends initialised submodules as detected items (`submodule: true`, `parentId` = top repository) exactly as it does worktrees, and `a.repo(id)` resolves them third. The frontend never renders submodule items as sidebar rows; a new `SubmoduleSection` renders them under their parent, and an opened submodule's sections render under the parent row. Diffs gain `--submodule=log`, parsed by `lib/submodules.ts` into a `SubmoduleDiff` component.

**Tech Stack:** Go, Wails v2 bindings, Svelte (legacy `$:`), TypeScript, vitest.

**Spec:** `docs/superpowers/specs/2026-09-23-submodules-design.md` (approved 2026-09-23). Read it before any task.

## Global Constraints

- Submodules are detected on every list read and never written to `repos.json`; a submodule's id is `repos.IDFor(<absolute path>)`.
- Only initialised submodules become repository items (openable); uninitialised and "not configured" ones exist only in `GetSubmodules` rows.
- Flat list, recursive, paths relative to the top repository, slash-separated, sorted by path (plain byte order).
- The section is last (after Stash), shown only when `submoduleCount > 0`, header count always visible, starts collapsed, expanded state persisted per repository id (`expandedSubmoduleSections`).
- An opened submodule shows Changes/Branches/Remotes/Tags/Stash (under its parent's row) but no Submodules section of its own.
- Markers: moved `↕` + 7-char checked-out hash; modified `●`; untracked-only `○`; not initialised: dimmed row + label `not initialised`; conflict `!`; not configured: label `not configured`.
- Row menu: Open, Initialise, Update to recorded commit, Sync URL, Show in Finder, Open terminal here — each shown only in the states the spec gives. Header menu: Initialise all, Update all — both confirmed, listing the paths.
- Submodule writes use `gitcmd.NetworkTimeout`, and take the parent's write lock AND each affected submodule id's lock (all `TryLock`; any held → `ErrBusy`).
- Update over local changes returns `ErrSubmoduleDirty` with text: `<path> has local changes that updating would overwrite. Commit or stash them inside the submodule first.`
- Diffs (Changes, commit details, AI tools) use `--submodule=log`.
- The log list itself never changes for submodules.
- Living spec (`docs/spec/*.md`) updated in the same commit as each behaviour — the files are named in each task. Design docs do NOT count.
- Commits: conventional; NO `Co-Authored-By` or any other trailer; never `git stash`.
- Tests building submodules from local paths need file transport: `git -c protocol.file.allow=always submodule add …`; app tests that clone set `GIT_CONFIG_COUNT=1`, `GIT_CONFIG_KEY_0=protocol.file.allow`, `GIT_CONFIG_VALUE_0=always` with `t.Setenv`.

## Review Focus

1. A gitlink with no `.gitmodules` entry (or the reverse) — the list must still load and show it `not configured`, never fail the whole section. (Task 1 test `TestListNotConfigured`.)
2. A path with a space (`third party/lib`) through list, diff parse and actions. (Task 1 `TestListPathWithSpace`, Task 5 parse test.)
3. The selected submodule is deinitialised/removed in a terminal — selection moves to the parent, no error loop. (Task 6 `loadRepos` change + vitest `nextSelection`.)
4. Update while the submodule is being written from its own view (or vice versa) — one of them gets `ErrBusy`, never both run. (Task 4 `TestSubmoduleLocks`.)
5. A submodule moved backwards (rewind) and one whose commits are not fetched — the diff pane still shows the two hashes, not raw text. (Task 5 parse tests.)

---

### Task 1: `internal/submodules` — read the flat recursive list

**Files:**
- Create: `internal/submodules/submodules.go`
- Test: `internal/submodules/submodules_test.go`

**Interfaces:**
- Produces:
  ```go
  type Submodule struct {
      Name        string `json:"name"`        // .gitmodules name; "" when not configured
      Path        string `json:"path"`        // relative to the top repository, slash-separated
      URL         string `json:"url"`
      Recorded    string `json:"recorded"`    // commit the direct parent's index records; "" when no gitlink
      CheckedOut  string `json:"checkedOut"`  // "" when not initialised
      Branch      string `json:"branch"`      // "" when detached or not initialised
      Initialised bool   `json:"initialised"`
      Configured  bool   `json:"configured"`  // has both a gitlink and a .gitmodules entry
      Moved       bool   `json:"moved"`
      Modified    bool   `json:"modified"`
      Untracked   bool   `json:"untracked"`
      Conflict    bool   `json:"conflict"`
  }
  func List(ctx context.Context, dir string) ([]Submodule, error)
  func HasAny(dir string) bool // cheap: .gitmodules exists in dir
  ```

- [ ] **Step 1: Write the failing tests**

```go
package submodules_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"git-ui/internal/submodules"
	"git-ui/internal/testrepo"
)

// withSub returns a parent repo with lib (from a separate repo) added at path.
func withSub(t *testing.T, path string) (*testrepo.Repo, *testrepo.Repo) {
	t.Helper()
	lib := testrepo.New(t)
	lib.WriteFile("a.txt", "a")
	lib.Commit("lib one")
	parent := testrepo.New(t)
	parent.WriteFile("p.txt", "p")
	parent.Commit("parent one")
	parent.Git("-c", "protocol.file.allow=always", "submodule", "add", "-q", lib.Dir, path)
	parent.Git("commit", "-q", "-m", "add submodule")
	return parent, lib
}

func find(t *testing.T, list []submodules.Submodule, path string) submodules.Submodule {
	t.Helper()
	for _, s := range list {
		if s.Path == path {
			return s
		}
	}
	t.Fatalf("%s not in %+v", path, list)
	return submodules.Submodule{}
}

func TestListInSync(t *testing.T) {
	parent, _ := withSub(t, "vendor/lib")
	list, err := submodules.List(context.Background(), parent.Dir)
	if err != nil {
		t.Fatal(err)
	}
	s := find(t, list, "vendor/lib")
	if !s.Initialised || !s.Configured || s.Moved || s.Modified || s.Untracked || s.Recorded == "" || s.CheckedOut != s.Recorded || s.Name != "vendor/lib" {
		t.Fatalf("unexpected %+v", s)
	}
}

func TestListMovedModifiedUntracked(t *testing.T) {
	parent, _ := withSub(t, "lib")
	sub := filepath.Join(parent.Dir, "lib")
	os.WriteFile(filepath.Join(sub, "a.txt"), []byte("changed"), 0o644)
	// The submodule clone has no identity of its own; pass one per command.
	run := func(args ...string) { parent.Git(append([]string{"-C", sub, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...) }
	run("commit", "-q", "-am", "moved")
	os.WriteFile(filepath.Join(sub, "a.txt"), []byte("dirty"), 0o644)
	os.WriteFile(filepath.Join(sub, "new.txt"), []byte("n"), 0o644)
	list, _ := submodules.List(context.Background(), parent.Dir)
	s := find(t, list, "lib")
	if !s.Moved || !s.Modified || !s.Untracked || s.Branch == "" {
		t.Fatalf("unexpected %+v", s)
	}
}

func TestListUninitialised(t *testing.T) {
	parent, _ := withSub(t, "lib")
	clone := testrepo.Clone(t, parent.Dir) // clones without --recurse-submodules
	list, err := submodules.List(context.Background(), clone.Dir)
	if err != nil {
		t.Fatal(err)
	}
	s := find(t, list, "lib")
	if s.Initialised || s.CheckedOut != "" || s.Recorded == "" || !s.Configured {
		t.Fatalf("unexpected %+v", s)
	}
}

func TestListNestedIsFlat(t *testing.T) {
	inner := testrepo.New(t)
	inner.WriteFile("z.txt", "z")
	inner.Commit("inner")
	mid := testrepo.New(t)
	mid.WriteFile("m.txt", "m")
	mid.Commit("mid")
	mid.Git("-c", "protocol.file.allow=always", "submodule", "add", "-q", inner.Dir, "deps/zlib")
	mid.Git("commit", "-q", "-m", "add inner")
	top := testrepo.New(t)
	top.WriteFile("t.txt", "t")
	top.Commit("top")
	top.Git("-c", "protocol.file.allow=always", "submodule", "add", "-q", mid.Dir, "vendor/lib")
	top.Git("-c", "protocol.file.allow=always", "submodule", "update", "--init", "--recursive", "-q")
	top.Git("commit", "-q", "-m", "add mid")
	list, _ := submodules.List(context.Background(), top.Dir)
	if len(list) != 2 || list[0].Path != "vendor/lib" || list[1].Path != "vendor/lib/deps/zlib" || !list[1].Initialised {
		t.Fatalf("unexpected %+v", list)
	}
}

func TestListPathWithSpace(t *testing.T) {
	parent, _ := withSub(t, "third party/lib")
	list, _ := submodules.List(context.Background(), parent.Dir)
	if s := find(t, list, "third party/lib"); !s.Initialised {
		t.Fatalf("unexpected %+v", s)
	}
}

func TestListNotConfigured(t *testing.T) {
	parent, _ := withSub(t, "lib")
	parent.Git("config", "-f", ".gitmodules", "--remove-section", "submodule.lib")
	list, err := submodules.List(context.Background(), parent.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if s := find(t, list, "lib"); s.Configured {
		t.Fatalf("unexpected %+v", s)
	}
}

func TestListNone(t *testing.T) {
	r := testrepo.New(t)
	r.WriteFile("a", "a")
	r.Commit("one")
	list, err := submodules.List(context.Background(), r.Dir)
	if err != nil || len(list) != 0 || submodules.HasAny(r.Dir) {
		t.Fatalf("%v %v", list, err)
	}
}
```

Also add `TestListConflict`: in `withSub(t, "lib")`, make two commits in `lib` (A, B, using the `run` helper above); on branch `x` of the parent record A (`git -C lib checkout -q A`, `git add lib`, commit), on `main` record B the same way; `parent.GitFails("merge", "x")` → `List` returns `lib` with `Conflict: true` and `Recorded == ""`.

- [ ] **Step 2:** `go test ./internal/submodules/` → FAIL (package missing).

- [ ] **Step 3: Implement**

```go
// Package submodules reads a repository's git submodules as one flat,
// recursive list. It does not use `git submodule status`, which aborts on a
// gitlink that has no .gitmodules entry; each piece is read directly.
package submodules

// List reads dir's submodules and, recursively, those of every initialised
// one, with paths relative to dir, sorted by path.
func List(ctx context.Context, dir string) ([]Submodule, error) {
	var out []Submodule
	if err := walk(ctx, dir, "", &out); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func HasAny(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".gitmodules"))
	return err == nil
}

// walk lists the direct submodules of the repository at root/prefix.
func walk(ctx context.Context, root, prefix string, out *[]Submodule) error {
	here := filepath.Join(root, filepath.FromSlash(prefix))
	links, conflicts, err := gitlinks(ctx, here)      // ls-files -s -z → path → recorded; conflicted paths
	if err != nil {
		return err
	}
	mods := gitmodules(ctx, here)                      // path → {name, url}; missing file → empty map
	flags := contentFlags(ctx, here)                   // path → {commit, modified, untracked}; errors → empty
	paths := union(keys(links), keys(mods), conflicts)
	for _, p := range paths {
		abs := filepath.Join(here, filepath.FromSlash(p))
		m, inMods := mods[p]
		s := Submodule{Name: m.name, URL: m.url, Path: join(prefix, p), Recorded: links[p], Conflict: conflicts[p]}
		s.Configured = inMods && (links[p] != "" || conflicts[p])
		if head, ok := checkedOut(ctx, abs); ok {       // <abs>/.git exists AND rev-parse --show-toplevel == abs
			s.Initialised, s.CheckedOut = true, head
			s.Branch = branch(ctx, abs)                 // symbolic-ref --short -q HEAD; "" on exit 1
			s.Moved = s.Recorded != "" && head != s.Recorded
			f := flags[p]
			s.Modified, s.Untracked = f.modified, f.untracked
		}
		*out = append(*out, s)
		if s.Initialised {
			if err := walk(ctx, root, s.Path, out); err != nil {
				return err
			}
		}
	}
	return nil
}
```

Helper details (write them in full):
- `gitlinks`: `gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "ls-files", "-s", "-z")`; each record `"<mode> <sha> <stage>\t<path>"`; keep mode `160000`; stage `0` → `links[path]=sha`; stage `1|2|3` → `conflicts[path]=true` (no recorded commit).
- `gitmodules`: `git config -f .gitmodules -z --get-regexp '^submodule\..*\.(path|url)$'` run in dir; exit code 1 (no file / no match) → empty map, any other error → empty map too. Each record is `key\nvalue`; name = key without the `submodule.` prefix and the last `.path`/`.url` suffix (names may contain dots). Build name→{path,url}, then index by path.
- `contentFlags`: plain `status --porcelain=v2 -z` in dir (the `sub` field reports untracked content inside a submodule on its own). For records starting `1 ` or `2 `, field 3 (`parts[2]`) is `N...` or `S<c><m><u>`; for `S` records take the path exactly as `internal/worktree.parseChange` does (fields after the 8th; for `2` skip the score) and set `commit = c=='C'`, `modified = m=='M'`, `untracked = u=='U'`. Skip the rename source field that follows a `2` record.
- `checkedOut`: `os.Stat(<abs>/.git)`; then `git -C abs rev-parse --show-toplevel HEAD`; accept only when the first line, after `filepath.EvalSymlinks` on both sides, equals `abs` (an empty directory inside the parent resolves to the parent's toplevel — that is "not initialised").
- `join(prefix, p)`: `p` when prefix is "", else `prefix + "/" + p`.

- [ ] **Step 4:** `go test ./internal/submodules/ && go vet ./internal/submodules/ && gofmt -l internal` → PASS, no files listed.
- [ ] **Step 5: Commit** — `git add internal/submodules && git commit -m "feat(submodules): read a repository's submodules as a flat recursive list"` (no behaviour visible yet, so no spec change).

---

### Task 2: Working-tree status and diffs know gitlinks

**Files:**
- Modify: `internal/worktree/worktree.go` (`FileStatus`, `Status`, `parseChange` caller)
- Modify: `internal/gitlog/details.go` (`FileChange`, `GetDetails`, `Diff`)
- Modify: `internal/app/worktree.go` (`GetWorktreeDiff` args)
- Modify: any diff invocation under `internal/ai/tools/` that shows a working-tree diff (grep `"diff"` in `internal/ai`)
- Test: `internal/worktree/worktree_test.go`, `internal/gitlog/details_test.go`, `internal/app/worktree_test.go`
- Spec: `docs/spec/03-working-tree.md`, `docs/spec/02-log-and-history.md`, `docs/spec/06-ai.md`

**Interfaces:**
- Produces:
  ```go
  // worktree.FileStatus gains:
  Submodule    bool `json:"submodule,omitempty"`
  SubCommit    bool `json:"subCommit,omitempty"`    // the pointer moved (porcelain c flag)
  SubModified  bool `json:"subModified,omitempty"`
  SubUntracked bool `json:"subUntracked,omitempty"`
  // gitlog.FileChange gains:
  Submodule bool `json:"submodule,omitempty"`
  func ParseRaw(out string) []FileChange // `diff-tree -r --raw -z -M` output
  ```

- [ ] **Step 1: Failing tests**
  - `worktree`: build a parent with a submodule (copy the `withSub` helper from Task 1 into the test file); commit inside the submodule → `Status` Unstaged has `{Path:"lib", Status:"M", Submodule:true, SubCommit:true}`; only dirty the submodule → `SubCommit:false, SubModified:true`; stage the moved pointer → Staged entry has `Submodule:true`.
  - `gitlog`: `ParseRaw(":160000 160000 aaa bbb M\x00lib\x00:100644 100644 ccc ddd M\x00a.txt\x00:100644 100644 eee fff R090\x00old\x00new\x00")` → `[{M lib submodule}, {M a.txt}, {R new old}]`; `GetDetails` of the "move submodule" commit has `Submodule:true` for `lib`; `Diff(parent, hash, ["lib"])` contains `"Submodule lib "` and not `"Subproject commit"`.
  - `app`: `GetWorktreeDiff(id, "lib", false)` for a moved pointer contains `"Submodule lib "`.
- [ ] **Step 2:** `go test ./internal/worktree/ ./internal/gitlog/ ./internal/app/` → FAIL.
- [ ] **Step 3: Implement**
  - `Status`: pass the `sub` field (`parts[2]` from the same split `parseChange` does — return it as a 4th value `sub string`) into `add`; when `sub[0]=='S'` set the four flags on both the staged and unstaged entry it creates.
  - `GetDetails`: switch to `diff-tree --no-commit-id -r --raw -z -M` and `ParseRaw`. Raw records: a header field `":<srcmode> <dstmode> <srcsha> <dstsha> <status>"` then one path (two for `R`/`C`: old, new). `Submodule = srcmode=="160000" || dstmode=="160000"`. Keep `ParseNameStatus` (used/referenced by stash).
  - `Diff`: add `"--submodule=log"` right after `"--no-color"` in both branches.
  - `GetWorktreeDiff`: `args := []string{"--literal-pathspecs", "diff", "--submodule=log"}`.
  - AI tools: every `diff`/`show` they run gets `--submodule=log`.
- [ ] **Step 4:** `go test ./... && go vet ./... && gofmt -l internal` → PASS.
- [ ] **Step 5: Docs** —
  - `03-working-tree.md`, "The diff": a submodule's diff is git's `--submodule=log` summary (old → new commit plus the subjects between), not a `Subproject commit` line; status reports whether the pointer moved and whether the submodule has modified/untracked content.
  - `02-log-and-history.md`, details pane: file list flags submodule changes; their diff is the same summary.
  - `06-ai.md`, read tools: diffs the tools return use the same summary for submodules.
- [ ] **Step 6: Commit** — `feat(diff): show submodule pointer changes as a commit summary`.

---

### Task 3: App — submodules as detected repositories

**Files:**
- Modify: `internal/app/app.go` (`App`, `RepoItem`, `repo`, `ListRepos`)
- Create: `internal/app/submodules.go` (`GetSubmodules`)
- Test: `internal/app/submodules_test.go`
- Spec: `docs/spec/01-repositories-and-sidebar.md`, `docs/spec/07-conventions-and-constraints.md`

**Interfaces:**
- Consumes: `submodules.List`, `submodules.HasAny` (Task 1).
- Produces:
  ```go
  // RepoItem gains:
  Submodule      bool   `json:"submodule,omitempty"`      // detected, initialised submodule
  SubPath        string `json:"subPath,omitempty"`        // path relative to ParentID's repository
  SubmoduleCount int    `json:"submoduleCount,omitempty"` // on the top repository item
  // App gains: smMu sync.Mutex; submodules map[string]repos.Repo
  func (a *App) GetSubmodules(id string) ([]submodules.Submodule, error)
  ```

- [ ] **Step 1: Failing tests** (reuse the fixtures pattern of `internal/app/worktrees_test.go` to build an App over a store with one repository):
  - A stored repo with an initialised submodule `vendor/lib` and a nested `vendor/lib/deps/zlib`: `ListRepos` returns the top item with `SubmoduleCount: 2`, plus two items with `Submodule:true`, `ParentID: top.ID`, `SubPath` equal to the paths, `ID == repos.IDFor(abs)`, `Name` = last path segment, `Branch` from `refs.CurrentLabel`.
  - An uninitialised submodule counts in `SubmoduleCount` but yields no item.
  - `a.dir(subID)` returns the absolute path; after the submodule's directory is emptied (`git submodule deinit -f vendor/lib`) and `ListRepos` runs again, `a.dir(subID)` is `ErrUnknownRepo`.
  - `RemoveRepo(subID)` → `ErrUnknownRepo`.
  - `GetSubmodules(top.ID)` returns the Task 1 list; `GetSubmodules("nope")` → `ErrUnknownRepo`.
  - A detected worktree of the top repository also gets its submodules (parent = worktree id) — only if that worktree has initialised them; otherwise `SubmoduleCount` only.
- [ ] **Step 2:** `go test ./internal/app/ -run Submodule` → FAIL.
- [ ] **Step 3: Implement**
  - In `ListRepos`, after the worktree pass, for every non-missing item that is not itself a submodule and whose path `submodules.HasAny`: `list, err := submodules.List(a.ctx, path)`; on error skip. Set `items[i].SubmoduleCount = len(list)`; for each `s.Initialised`: `abs := filepath.Join(path, filepath.FromSlash(s.Path))`, append `RepoItem{Repo: repos.Repo{ID: repos.IDFor(abs), Name: filepath.Base(abs), Path: abs}, Branch: refs.CurrentLabel(a.ctx, abs), ParentID: item.ID, Submodule: true, SubPath: s.Path}` and record it in `foundSub`.
  - Replace `a.submodules` wholesale under `smMu`; ids that vanished (and are not stored) get `a.term.CloseRepo` + `a.forgetLog`, same loop as worktrees.
  - `repo(id)`: stored → worktrees → submodules.
  - `GetSubmodules`: `dir, err := a.dir(id)` → `submodules.List(a.ctx, dir)`.
- [ ] **Step 4:** `go test ./... && go vet ./... && gofmt -l internal` → PASS.
- [ ] **Step 5:** Regenerate bindings: `source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && ~/go/bin/wails generate module`; revert unrelated touched files.
- [ ] **Step 6: Docs** — `07-conventions-and-constraints.md`: repository identity also covers detected submodules (path-derived id, not a list entry, resolved after stored entries and worktrees). `01-repositories-and-sidebar.md`, "Repository list": the list read also detects initialised submodules (recursively), which are never sidebar rows; the top item carries the submodule count.
- [ ] **Step 7: Commit** — `feat(app): detect submodules as openable repositories`.

---

### Task 4: App — Initialise, Update, Sync with the two-lock rule

**Files:**
- Modify: `internal/app/submodules.go`, `internal/app/app.go` (`writeAll` next to `write`)
- Create: `internal/submodules/ops.go`
- Test: `internal/submodules/ops_test.go`, `internal/app/submodules_test.go`
- Spec: `docs/spec/01-repositories-and-sidebar.md`, `docs/spec/07-conventions-and-constraints.md`

**Interfaces:**
- Produces:
  ```go
  // package submodules
  type ErrDirty struct{ Path string }
  func (e *ErrDirty) Error() string // "<path> has local changes that updating would overwrite. Commit or stash them inside the submodule first."
  func Init(ctx context.Context, dir, path string) error      // submodule update --init -- <path>
  func Update(ctx context.Context, dir, path string) error    // submodule update -- <path>; ErrDirty on "would be overwritten"
  func Sync(ctx context.Context, dir, path string) error      // submodule sync -- <path>
  func InitAll(ctx context.Context, dir string) error         // submodule update --init --recursive
  func UpdateAll(ctx context.Context, dir string) error       // submodule update --recursive; ErrDirty (Path "") on overwrite
  // App
  func (a *App) InitSubmodule(id, path string) error
  func (a *App) UpdateSubmodule(id, path string) error
  func (a *App) SyncSubmodule(id, path string) error
  func (a *App) InitAllSubmodules(id string) error
  func (a *App) UpdateAllSubmodules(id string) error
  func (a *App) writeAll(ids []string, fn func(ctx context.Context) error) error
  ```

- [ ] **Step 1: Failing tests**
  - `submodules`: `Init` on a clone's uninitialised `lib` → `List` shows it initialised at the recorded commit; `Update` on a moved `lib` → back to recorded; `Update` with a dirty file that the checkout would overwrite → `*ErrDirty` via `errors.As`; `Sync` after changing the URL in `.gitmodules` → `git -C lib config remote.origin.url` equals the new URL. Paths passed as git pathspecs with `--literal-pathspecs` so `third party/lib` works. Tests set the `GIT_CONFIG_*` env from the Global Constraints.
  - `app`:
    - `InitSubmodule(id, "not-a-submodule")` → error `"not-a-submodule" is not a submodule of this repository`.
    - `TestSubmoduleLocks`: hold the submodule id's lock (`a.writes.LoadOrStore(subID, &sync.Mutex{})` then `Lock`) → `UpdateSubmodule(top, "lib")` returns `ErrBusy`; release; hold the top's lock → `ErrBusy`; after either failure both locks are free again (a second call succeeds).
- [ ] **Step 2:** run both packages → FAIL.
- [ ] **Step 3: Implement**
  - Every command: `gitcmd.Run(ctx, dir, gitcmd.NetworkTimeout, "--literal-pathspecs", "submodule", …, "--", path)`. `Update`/`UpdateAll` map a `*gitcmd.Error` whose stderr contains `would be overwritten` to `&ErrDirty{Path: path}`.
  - `writeAll`:
    ```go
    func (a *App) writeAll(ids []string, fn func(ctx context.Context) error) error {
    	var held []*sync.Mutex
    	defer func() {
    		for _, mu := range held {
    			mu.Unlock()
    		}
    	}()
    	for _, id := range ids {
    		m, _ := a.writes.LoadOrStore(id, &sync.Mutex{})
    		mu := m.(*sync.Mutex)
    		if !mu.TryLock() {
    			return ErrBusy
    		}
    		held = append(held, mu)
    	}
    	return fn(a.ctx)
    }
    ```
  - Per-path methods: `dir := a.dir(id)`; `list := submodules.List`; refuse a path not in `list`; ids = `[id, repos.IDFor(filepath.Join(dir, path))]`. All-methods: ids = `id` + every listed submodule's id. Then `writeAll(ids, …)`.
- [ ] **Step 4:** `go test ./... && go vet ./... && gofmt -l internal` → PASS; regenerate bindings (as Task 3 Step 5).
- [ ] **Step 5: Docs** — `01-repositories-and-sidebar.md`: the actions and what each runs; Update's refusal text. `07-conventions-and-constraints.md`: a submodule write holds the parent's lock and the lock of every submodule it touches.
- [ ] **Step 6: Commit** — `feat(submodules): initialise, update and sync under both write locks`.

---

### Task 5: Frontend lib — types, api, grouping, markers, diff parsing

**Files:**
- Modify: `frontend/src/lib/types.ts`, `frontend/src/lib/api.ts`, `frontend/src/lib/repoGroups.ts`
- Create: `frontend/src/lib/submodules.ts`
- Test: `frontend/src/lib/submodules.test.ts`, `frontend/src/lib/repoGroups.test.ts`

**Interfaces:**
- Consumes: Tasks 2–4 bindings.
- Produces:
  ```ts
  // types.ts
  interface Repo { …; submodule?: boolean; subPath?: string; submoduleCount?: number }
  interface FileStatus { …; submodule?: boolean; subCommit?: boolean; subModified?: boolean; subUntracked?: boolean }
  interface FileChange { …; submodule?: boolean }
  export interface Submodule { name: string; path: string; url: string; recorded: string; checkedOut: string; branch: string; initialised: boolean; configured: boolean; moved: boolean; modified: boolean; untracked: boolean; conflict: boolean }
  // api.ts
  getSubmodules(id): Promise<Submodule[]>; initSubmodule(id, path); updateSubmodule(id, path); syncSubmodule(id, path); initAllSubmodules(id); updateAllSubmodules(id)
  // submodules.ts
  export interface Marker { kind: 'moved' | 'modified' | 'untracked' | 'conflict'; text: string }
  export function markers(s: Submodule): Marker[]
  export function rowLabel(s: Submodule): '' | 'not initialised' | 'not configured'
  export function splitPath(path: string): { dir: string; leaf: string } // 'vendor/lib/zlib' → { dir: 'vendor/lib/', leaf: 'zlib' }
  export function tooltip(s: Submodule): string
  export interface SubmoduleCommit { dir: '>' | '<'; subject: string }
  export interface SubmoduleDiff { path: string; from: string; to: string; note: string; commits: SubmoduleCommit[]; content: string[] }
  export function parseSubmoduleDiff(diff: string): SubmoduleDiff | null
  export function updateMessage(s: Submodule): string
  export function nextSelection(prevSelected: Repo | null, list: Repo[]): string | null // id to select, '' to clear, null to keep
  ```

- [ ] **Step 1: Failing tests** (`submodules.test.ts`):

```ts
import { describe, expect, it } from 'vitest'
import { markers, nextSelection, parseSubmoduleDiff, rowLabel, splitPath, updateMessage } from './submodules'
import type { Repo, Submodule } from './types'

const base: Submodule = { name: 'lib', path: 'vendor/lib', url: 'u', recorded: 'a'.repeat(40), checkedOut: 'a'.repeat(40), branch: '', initialised: true, configured: true, moved: false, modified: false, untracked: false, conflict: false }

describe('markers', () => {
  it('is empty in sync', () => expect(markers(base)).toEqual([]))
  it('moved shows the checked-out short hash', () =>
    expect(markers({ ...base, moved: true, checkedOut: 'b'.repeat(40) })).toEqual([{ kind: 'moved', text: '↕ bbbbbbb' }]))
  it('modified wins over untracked-only', () =>
    expect(markers({ ...base, modified: true, untracked: true }).map((m) => m.kind)).toEqual(['modified']))
  it('untracked only', () => expect(markers({ ...base, untracked: true })).toEqual([{ kind: 'untracked', text: '○' }]))
  it('moved and modified combine', () =>
    expect(markers({ ...base, moved: true, modified: true }).map((m) => m.kind)).toEqual(['moved', 'modified']))
  it('conflict', () => expect(markers({ ...base, conflict: true })).toEqual([{ kind: 'conflict', text: '!' }]))
})

describe('rowLabel', () => {
  it('uninitialised', () => expect(rowLabel({ ...base, initialised: false, checkedOut: '' })).toBe('not initialised'))
  it('not configured wins', () => expect(rowLabel({ ...base, configured: false, initialised: false })).toBe('not configured'))
})

it('splitPath', () => {
  expect(splitPath('vendor/lib/zlib')).toEqual({ dir: 'vendor/lib/', leaf: 'zlib' })
  expect(splitPath('lib')).toEqual({ dir: '', leaf: 'lib' })
})

describe('parseSubmoduleDiff', () => {
  it('forward with commits', () =>
    expect(parseSubmoduleDiff('Submodule third party/lib 1234567..89abcde:\n  > Fix overflow\n  > Bump version\n')).toEqual({
      path: 'third party/lib', from: '1234567', to: '89abcde', note: '', content: [],
      commits: [{ dir: '>', subject: 'Fix overflow' }, { dir: '>', subject: 'Bump version' }],
    }))
  it('rewind', () => expect(parseSubmoduleDiff('Submodule lib 89abcde...1234567 (rewind):\n  < Newer\n')?.commits).toEqual([{ dir: '<', subject: 'Newer' }]))
  it('commits not present', () =>
    expect(parseSubmoduleDiff('Submodule lib 1234567..89abcde (commits not present)\n')).toMatchObject({ from: '1234567', to: '89abcde', note: 'commits not present', commits: [] }))
  it('new submodule', () => expect(parseSubmoduleDiff('Submodule lib 0000000...1234567 (new submodule)\n')?.note).toBe('new submodule'))
  it('content only', () =>
    expect(parseSubmoduleDiff('Submodule lib contains modified content\nSubmodule lib contains untracked content\n')).toMatchObject({ from: '', to: '', content: ['modified', 'untracked'] }))
  it('ordinary diff is null', () => expect(parseSubmoduleDiff('diff --git a/x b/x\n')).toBeNull())
})

it('updateMessage names the commit left and the branch', () => {
  const s = { ...base, moved: true, checkedOut: 'b'.repeat(40), branch: 'main' }
  expect(updateMessage(s)).toBe('vendor/lib will leave bbbbbbb and check out the recorded commit aaaaaaa on a detached HEAD. Branch main itself is not changed.')
})

describe('nextSelection', () => {
  const top: Repo = { id: 't', name: 't', path: '/t', missing: false, branch: 'main' }
  const sub: Repo = { id: 's', name: 'lib', path: '/t/lib', missing: false, branch: 'main', submodule: true, parentId: 't', subPath: 'lib' }
  it('keeps a listed selection', () => expect(nextSelection(sub, [top, sub])).toBeNull())
  it('falls back to the parent of a vanished submodule', () => expect(nextSelection(sub, [top])).toBe('t'))
  it('clears a vanished repository', () => expect(nextSelection(top, [])).toBe(''))
})
```

  Add to `repoGroups.test.ts`: an item with `submodule: true, parentId: 't'` never appears in `loose`, any group, or any `children`.
- [ ] **Step 2:** `cd frontend && npx vitest run src/lib/submodules.test.ts src/lib/repoGroups.test.ts` → FAIL.
- [ ] **Step 3: Implement.** `parseSubmoduleDiff` scans lines: `^Submodule (.+) ([0-9a-f]+)\.\.\.?([0-9a-f]+)(?: \((.+)\))?:?$` → path/from/to/note; `^Submodule (.+) contains (modified|untracked) content$` → push to `content` (set path); `^\s+([<>]) (.*)$` → commits. Return null when no `Submodule ` line matched. `tooltip` lines: `Recorded: <7>`, `Checked out: <7>` (+ ` on <branch>`), `URL: <url>`, `Name: <name>` only when `name !== path`. `groupRepos` drops `repo.submodule` items first.
- [ ] **Step 4:** `npx vitest run && npm run check` → PASS.
- [ ] **Step 5: Commit** — `feat(frontend): submodule types, api and helpers` (no visible behaviour yet).

---

### Task 6: Sidebar — the Submodules section, opening, actions

**Files:**
- Create: `frontend/src/components/SubmoduleSection.svelte`
- Modify: `frontend/src/components/RepoRow.svelte`, `frontend/src/components/RepoRefs.svelte`, `frontend/src/lib/stores.ts`, `frontend/src/lib/actions.ts`
- Spec: `docs/spec/01-repositories-and-sidebar.md`

**Interfaces:**
- Consumes: Task 5.
- Produces: `expandedSubmoduleSections` (persisted `string[]`), `toggleSubmodulesExpanded(repoId)`; actions `openSubmodule(parentId, s)`, `initSubmodule(parentId, s)`, `updateSubmodule(parentId, s)`, `syncSubmodule(parentId, s)`, `initAllSubmodules(parentId, list)`, `updateAllSubmodules(parentId, list)` — each runs through `run()` then `await loadRepos()`.

- [ ] **Step 1: Stores.** Add `expandedSubmoduleSections` next to `expandedStashSections` with the same validator and toggle. In `loadRepos`, keep the previous `selectedRepo` value before `repos.set(list)`, then use `nextSelection(prev, list)`: `null` → nothing; a non-empty id → `selectRepo(id)`; `''` → the existing clearing block.
- [ ] **Step 2: `SubmoduleSection.svelte`** (props `parentId: string`, `count: number`):
  - Header like Stash's: title button toggles `expandedSubmoduleSections`, `count`, and an `icon-btn` (`more` icon) + `on:contextmenu` opening the header menu (Initialise all — disabled when none uninitialised; Update all — disabled when none moved; both disabled while `$busy`).
  - When expanded: `rows = await api.getSubmodules(parentId)`, reloaded reactively on `$repos` and `$logVersion` changes (both change after writes and on focus); an error shows `<div class="none">Could not read submodules: {message}</div>`.
  - Row: `button.row-item.ref`, `class:active={$selectedRepoId === idOf(s)}` where `idOf` looks up the `$repos` item with `parentId === parentId && subPath === s.path`; dimmed (`class:dim`) when `rowLabel(s)`; mark icon `package` (add to `Icon.svelte` if missing — a simple box); text `<span class="dir">{dir}</span>{leaf}`; label and markers right-aligned; `title={tooltip(s)}`.
  - Click: initialised → `openSubmodule(parentId, s)` (selects the submodule id; if already selected, selects `parentId`); otherwise nothing.
  - Row menu per the Global Constraints: Open (initialised), Initialise (!initialised && configured), Update to recorded commit (initialised && moved), Sync URL (configured), Show in Finder (initialised: `openRepoFolder(subId)`), Open terminal here (initialised: select it, `terminalOpen.set(true)`).
- [ ] **Step 3: Actions.** `updateSubmodule` confirms with `confirmDialog({ title: 'Update submodule', message: updateMessage(s), confirmLabel: 'Update' })`; `initAll`/`updateAll` confirm with the affected paths, one per line. Errors surface through `run()`'s toast (the backend's `ErrDirty` text is already plain).
- [ ] **Step 4: `RepoRow.svelte`.** `$: openSub = $selectedRepo?.submodule && $selectedRepo.parentId === repo.id ? $selectedRepo : null`. Render `<RepoRefs repoId={openSub?.id ?? repo.id} submodulePath={openSub?.subPath ?? ''} />`, then `{#if !repo.submodule && repo.submoduleCount}<SubmoduleSection parentId={repo.id} count={repo.submoduleCount} />{/if}` inside the `{#if expanded}`.
- [ ] **Step 5: `RepoRefs.svelte`.** New prop `submodulePath = ''`; when set, a first row `↳ {submodulePath}` (muted, with a `title="Back to the repository"` button that calls `selectRepo(parentId)`), so the user can tell the sections below belong to the submodule.
- [ ] **Step 6:** `npx vitest run && npm run check && npm run build` → PASS. Launch (`make dev`) against a repository with a nested submodule and check: section count, collapsed by default, markers, click opens (row highlighted, sections under the parent now the submodule's), click again returns, uninitialised row does nothing, each menu item.
- [ ] **Step 7: Docs** — `01-repositories-and-sidebar.md`: "Per-repository sections" order gains Submodules (last, only when present); a new "### Submodules" subsection: flat recursive list, row text, markers and labels table, tooltip, click/second click, menus, confirmations, the error row, the selection moving to the parent when an opened submodule vanishes; "Rules": Submodules starts collapsed and is remembered per repository.
- [ ] **Step 8: Commit** — `feat(sidebar): Submodules section with open, initialise, update and sync`.

---

### Task 7: Changes view and commit details render submodules

**Files:**
- Create: `frontend/src/components/SubmoduleDiff.svelte`
- Modify: `frontend/src/components/ChangesView.svelte`, `frontend/src/components/FileList.svelte`, `frontend/src/components/CommitDetails.svelte`
- Spec: `docs/spec/03-working-tree.md`, `docs/spec/02-log-and-history.md`

**Interfaces:**
- Consumes: `parseSubmoduleDiff`, `SubmoduleDiff` type, `openSubmodule` (Tasks 5–6). `openSubmodule` needs the `Submodule`; from the Changes view, look up the `$repos` item by `parentId`/`subPath` and select it directly with `selectRepo`.

- [ ] **Step 1: `SubmoduleDiff.svelte`** (props `diff: SubmoduleDiff`, `onOpen: (() => void) | null`): heading `Submodule <code>{path}</code>` — `from` → `to` (7 chars each; `new submodule` / `submodule deleted` / `commits not present` note shown muted); list of commits as `> subject` / `< subject` lines using `lineClass`' add/del colours; when `content` is non-empty, a note "Contains modified content" / "untracked content" joined, plus an **Open submodule** link when `onOpen`.
- [ ] **Step 2: `ChangesView.svelte`.**
  - Diff pane: `$: sub = selectedFile?.submodule ? parseSubmoduleDiff(diff) : null`; render `SubmoduleDiff` when `sub`, else today's lines.
  - `actionsFor(file)` when `file.submodule`:
    - staged → `[Unstage, Update to recorded commit]`;
    - unstaged with `subCommit` → `[Stage, Update to recorded commit]`;
    - unstaged content-only → `[Open submodule]` only, and the pane shows "Commit inside the submodule first" (pass it as a `SubmoduleDiff` note).
    - Update to recorded commit calls `updateSubmodule(repoId, s)` with `s` from `api.getSubmodules(repoId)` matched by path.
- [ ] **Step 3: `FileList.svelte` / `CommitDetails.svelte`.** Rows whose item has `submodule` show the `package` icon before the status letter. In `CommitDetails`, a selected file with `submodule` renders `SubmoduleDiff` (with `onOpen` only when the `$repos` has an item for that path).
- [ ] **Step 4:** `npx vitest run && npm run check && npm run build`; in the app: move a submodule's pointer, dirty another, stage/unstage the moved one, Update from the Changes view, open a commit that moved a submodule.
- [ ] **Step 5: Docs** — `03-working-tree.md`: submodule rows (icon), the summary pane, no Stage for content-only changes and the reason, Update in place of Discard. `02-log-and-history.md`: the details pane's icon and summary; the log list itself is unchanged.
- [ ] **Step 6: Commit** — `feat(changes): submodule pointer changes shown and updated in place`.

---

### Task 8: Breadcrumb and the "not at the recorded commit" toast

**Files:**
- Modify: `frontend/src/components/LogView.svelte`, `frontend/src/lib/ui.ts`, `frontend/src/components/Toasts.svelte`, `frontend/src/lib/actions.ts`
- Test: `frontend/src/lib/submodules.test.ts` (add `movedMessage`)
- Spec: `docs/spec/01-repositories-and-sidebar.md`, `docs/spec/02-log-and-history.md`, `docs/spec/05-remote-and-stash.md` (pull)

**Interfaces:**
- Produces: `toast(message, kind, action?: { label: string; run: () => void })` — a toast with an action is not auto-dismissed; `movedMessage(n: number): string` → `"1 submodule is not at the recorded commit"` / `"N submodules are not at the recorded commit"`; `warnMovedSubmodules(id: string): Promise<void>`.

- [ ] **Step 1: Failing test** for `movedMessage` (1 and 3) → FAIL → implement → PASS.
- [ ] **Step 2: Toast action.** Extend `Toast` with `action?`; `Toasts.svelte` renders a `btn` that runs it and dismisses the toast.
- [ ] **Step 3: `warnMovedSubmodules(id)`** in `actions.ts`: when the repo item has `submoduleCount`, `api.getSubmodules(id)`, count `moved`; if > 0 → `toast(movedMessage(n), 'info', { label: 'Update all', run: () => updateAllSubmodules(id, list) })`. Call it after a successful `checkoutBranch`, `checkoutCommit`, `resetBranch`, `mergeBranch`, `pull` (only when the action succeeded; `run()` returns a boolean — use it; for `mergeBranch`/`pull` call it in the success path).
- [ ] **Step 4: Breadcrumb.** In `LogView.svelte`'s title: when `$selectedRepo?.submodule`, render `<button class="crumb" on:click={() => selectRepo(parent.id)}>{parent.name}</button> › <span>{$selectedRepo.subPath}</span>` (parent from `$repos` by `parentId`), else today's name + path.
- [ ] **Step 5:** `npx vitest run && npm run check && npm run build`; in the app: check out an older commit of the parent that recorded a different submodule commit → toast → Update all → markers clear.
- [ ] **Step 6: Docs** — `02-log-and-history.md`: the header breadcrumb for a submodule. `01-repositories-and-sidebar.md` (checkout, reset, merge) and `05-remote-and-stash.md` (pull): the toast after the write, never an automatic update; `submodule.recurse` is honoured because git honours it.
- [ ] **Step 7: Commit** — `feat(submodules): breadcrumb and warning when submodules are left behind`.

---

### Task 9: Whole-branch verification

- [ ] `go test ./... && go vet ./... && gofmt -l internal` and `cd frontend && npx vitest run && npm run check && npm run build` — all pass.
- [ ] Every `docs/spec/` file named in Tasks 2–8 has changed on the branch: `git diff --stat main -- docs/spec`.
- [ ] Set the design doc's status to `Implemented (manual pass pending)`.
- [ ] Manual pass list for the owner (put it in the final summary): nested submodule repo; open/back/breadcrumb; Initialise on a fresh clone; Update after an older checkout; dirty Update refusal text; Changes and commit-details summaries; chat in a submodule is its own conversation; a submodule deinitialised in the terminal while selected moves the selection to the parent on focus.
