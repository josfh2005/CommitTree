# Worktrees, Sidebar Order and Click-to-Deselect Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show each listed repository's git worktrees as child rows that open like repositories, mark branches checked out in another worktree, sort repositories by name, and let a click on the selected commit deselect it.

**Architecture:** A new `internal/worktrees` package parses `git worktree list --porcelain -z`. `App.ListRepos` appends detected worktrees as items with a `parentId`, remembers them in memory, and every id lookup goes through one resolver (`a.repo(id)`) that checks stored entries first and detected worktrees second. The frontend groups children under parents and sorts by name; the log toggles selection on a repeated click.

**Tech Stack:** Go, Wails v2 bindings, Svelte (legacy `$:`), TypeScript, vitest.

**Spec:** `docs/superpowers/specs/2026-09-23-worktrees-and-sidebar-order-design.md`

## Global Constraints

- Worktrees are detected on every list read and never written to `repos.json`.
- A detected worktree's id is the same path-derived id a repository gets (`repos.IDFor(path)`).
- Not listed: the main working tree, bare entries, `prunable` worktrees, worktrees whose directory is gone.
- Children are computed only for a stored repository that is itself the main working tree.
- A stored entry that is a linked worktree of a listed main repository is nested (gets `parentId`), keeps its id and "Remove from list", never duplicated.
- Detected worktree menu: Fetch, Pull, Push, Show in Finder — no Remove/Move/Locate.
- Branch checked out in another worktree: marker + tooltip "Checked out in <directory name>", Checkout and Delete disabled; git's refusal becomes `<branch> is checked out in another worktree (<path>)`.
- Sort: display name, case-insensitive (`localeCompare(b, undefined, { sensitivity: 'base' })`), ties by path — loose area, each group, and children.
- Deselect: left-click on the already-selected commit row/dot or the selected "Uncommitted changes" row clears the selection; right-click, jump arrows and external selection never toggle.
- Living spec updated in the same commit as each behaviour: `docs/spec/01-repositories-and-sidebar.md`, `docs/spec/02-log-and-history.md`, `docs/spec/07-conventions-and-constraints.md`.
- Commits: conventional; NO `Co-Authored-By` or any trailer; never `git stash`.

## Review Focus

1. A Claude Code worktree removed from disk but not pruned — must not appear, must not break the list. (Task 1 prunable test, Task 2 missing-dir test.)
2. The selected repository is a worktree that disappears — the app must not error on every call; the selection clears. (Task 3 frontend: `selectedRepo` derived becomes null → existing "Select or add a repository" state; verify in manual pass.)
3. The same worktree added by hand AND detected — exactly one row, the stored id. (Task 2 test.)
4. A worktree path containing spaces — parsed intact. (Task 1 test.)
5. Chat, merge agent and commit-message generation on a detected worktree id — must resolve the path (they used `a.store.Get` directly). (Task 2 test: `SendChat` on a worktree id does not return unknown repository.)

---

### Task 1: `internal/worktrees` — list a repository's worktrees

**Files:** Create `internal/worktrees/worktrees.go`, `internal/worktrees/worktrees_test.go`.

**Produces:** `type Worktree struct { Path, Head, Branch string; Detached, Prunable, Bare, Main bool }`; `func List(ctx context.Context, dir string) ([]Worktree, error)`; `func parse(out string) []Worktree` (unexported, tested directly).

- [ ] **Step 1: Failing tests** — `worktrees_test.go` (package `worktrees`): (a) `parse` of a hand-written NUL-separated fixture with a main tree on `main`, a branch worktree at a path with a space, a detached one, and a `prunable gitdir file points to non-existent location` one → four entries with the right fields, first `Main`; (b) `List` against a `testrepo` repo: `git worktree add -q <tmp>/wt one -b one` and `git worktree add -q --detach <tmp>/det` → three entries, branch `one` on the second, `Detached` on the third; after `os.RemoveAll(<tmp>/det)` the third is `Prunable`.
- [ ] **Step 2:** `go test ./internal/worktrees/` → FAIL (package missing).
- [ ] **Step 3: Implement.**

```go
// Package worktrees reads a repository's git worktrees.
package worktrees

type Worktree struct {
	Path     string `json:"path"`
	Head     string `json:"head"`
	Branch   string `json:"branch"` // short name; "" when detached
	Detached bool   `json:"detached"`
	Prunable bool   `json:"prunable"`
	Bare     bool   `json:"bare"`
	Main     bool   `json:"main"` // the first record: the main working tree
}

// List runs `git worktree list --porcelain -z` in dir.
func List(ctx context.Context, dir string) ([]Worktree, error) {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return nil, err
	}
	return parse(out), nil
}

// parse reads -z porcelain: fields are NUL-terminated, a record ends with an
// empty field. Keys: worktree, HEAD, branch refs/heads/<x>, detached, bare,
// prunable [reason], locked [reason].
func parse(out string) []Worktree {
	var list []Worktree
	var cur *Worktree
	for _, field := range strings.Split(out, "\x00") {
		if field == "" {
			if cur != nil {
				list = append(list, *cur)
				cur = nil
			}
			continue
		}
		key, value, _ := strings.Cut(field, " ")
		switch key {
		case "worktree":
			cur = &Worktree{Path: value, Main: len(list) == 0}
		case "HEAD":
			if cur != nil { cur.Head = value }
		case "branch":
			if cur != nil { cur.Branch = strings.TrimPrefix(value, "refs/heads/") }
		case "detached":
			if cur != nil { cur.Detached = true }
		case "bare":
			if cur != nil { cur.Bare = true }
		case "prunable":
			if cur != nil { cur.Prunable = true }
		}
	}
	if cur != nil {
		list = append(list, *cur)
	}
	return list
}
```

(`gitcmd.Run` trims output; the trailing empty fields are therefore optional — the final `if cur != nil` handles it. gofmt the `if` bodies onto their own lines.)

- [ ] **Step 4:** `go test ./internal/worktrees/ && go vet ./internal/worktrees/` → PASS.
- [ ] **Step 5:** Commit `feat(worktrees): read a repository's worktrees`.

---

### Task 2: App — detected worktrees, one resolver, branch marker data, friendly checkout error

**Files:** Modify `internal/repos/repos.go` (export `IDFor`), `internal/app/app.go` (`RepoItem`, `ListRepos`, `dir`, new `repo`, `GetRefs`, list-entry refusals), `internal/app/ai.go`, `internal/app/merge.go`, `internal/app/worktree.go` (replace `a.store.Get` with `a.repo`), `internal/refs/list.go` (`Branch.Worktree`), `internal/ops/ops.go` (`ErrCheckedOutElsewhere`), tests in `internal/app/worktrees_test.go`, `internal/ops/ops_test.go`; regenerate `frontend/wailsjs/go`; docs `docs/spec/07-conventions-and-constraints.md`.

**Produces:** `RepoItem{ repos.Repo; Branch string; ParentID string \`json:"parentId,omitempty"\`; Worktree bool \`json:"worktree,omitempty"\` }`; `refs.Branch.Worktree string \`json:"worktree,omitempty"\``; `ops.ErrCheckedOutElsewhere` (type with `Branch`, `Path`, `Error()`); `func (a *App) repo(id string) (repos.Repo, bool)`.

- [ ] **Step 1: Failing tests** — `internal/app/worktrees_test.go`: build with `newTestApp` (repo has `main` and `feature`), get its dir via `a.dir(id)`, run `git -C dir worktree add -q <tmp>/wt feature`. Assert:
  - `ListRepos()` has 2 items; the second has `ParentID == id`, `Worktree == true`, `Name == "wt"`, `Branch == "feature"`, `ID == repos.IDFor(<resolved wt path>)`.
  - `a.dir(thatID)` returns the worktree path; `a.GetLog(thatID, …)` works.
  - After `store.Add` of the worktree path by hand (use `a.store.Add(ctx, wtPath)`), `ListRepos()` still has 2 items, the worktree one now `Worktree == false` with `ParentID == id` and the stored id.
  - `RemoveRepo(detectedID)` and `SetRepoGroup(detectedID, "g")` return `repos.ErrUnknownRepo`.
  - After `os.RemoveAll(wtPath)`, `ListRepos()` has 1 item and `a.dir(oldID)` errors.
  - `GetRefs(id)`: branch `feature` has `Worktree == wtPath`; `main` has `""`. `GetRefs(wtID)`: `main` has the main dir, `feature` has `""`.
  - `a.Checkout(id, "feature")` returns an error whose text is `feature is checked out in another worktree (<wtPath>)` and `errors.As(err, *ops.ErrCheckedOutElsewhere)`.
  - With `newAIApp`, `SendChat(wtID, "hi", "r")` does not return "unknown repository".
  (On macOS `t.TempDir()` sits under `/var` → `/private/var`; compare paths after `filepath.EvalSymlinks`.)
- [ ] **Step 2:** `go test ./internal/app/ -run Worktree` → FAIL.
- [ ] **Step 3: Implement.**
  - `repos.IDFor(path string) string` — rename `idFor` to exported `IDFor` (update callers).
  - `App` gains `wtMu sync.Mutex; worktrees map[string]repos.Repo` (id → a Repo with `Name`, `Path`).
  - `func (a *App) repo(id string) (repos.Repo, bool)`: `a.store.Get(id)`; else look up `a.worktrees` under `wtMu`. `dir` uses it. Replace the four `a.store.Get(` calls in `ai.go`, `merge.go`, `worktree.go` with `a.repo(`.
  - `ListRepos`: build items as today; `byPath := map[string]int` (item index by cleaned path); `found := map[string]repos.Repo{}`; for each stored, present repo: `wts, err := worktrees.List(a.ctx, r.Path)`; skip on err, or when `len(wts) == 0 || !samePath(wts[0].Path, r.Path)` (not a main tree); for each `wt` after the first: skip `Bare || Prunable`, skip when `os.Stat(wt.Path)` fails; if `i, ok := byPath[clean(wt.Path)]` → `items[i].ParentID = r.ID`; else append `RepoItem{Repo: repos.Repo{ID: repos.IDFor(wt.Path), Name: filepath.Base(wt.Path), Path: wt.Path}, Branch: label, ParentID: r.ID, Worktree: true}` where label is `wt.Branch` or `HEAD (<first 7 of Head>)`, and record it in `found`. Replace `a.worktrees = found` under the lock at the end. `samePath` compares `filepath.EvalSymlinks` results (fall back to `filepath.Clean`).
  - `refs.Branch.Worktree`: in `GetRefs`, after `refs.List`, call `worktrees.List(a.ctx, dir)`; for each local branch, if a worktree other than `dir` (by `samePath`) has `Branch == b.Name`, set `b.Worktree = wt.Path`. Errors from `worktrees.List` are ignored (no markers).
  - `ops.Checkout`: on error, if the stderr contains `is already used by worktree at` or `is already checked out at`, extract the quoted path after `at '` and return `&ErrCheckedOutElsewhere{Branch: branch, Path: path}` with `Error() = fmt.Sprintf("%s is checked out in another worktree (%s)", Branch, Path)`. Add `ops_test.go` case with a real worktree.
  - `RemoveRepo`, `RelocateRepo`, `SetRepoGroup` already fail with `ErrUnknownRepo` for ids not in the store — confirm they call the store directly, not `a.repo`.
- [ ] **Step 4:** `go test ./... && go vet ./... && gofmt -l internal` → PASS/clean (run `make build` first if the root package cannot embed `frontend/dist`).
- [ ] **Step 5:** Regenerate bindings: `source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && ~/go/bin/wails generate module`; revert unrelated touched files.
- [ ] **Step 6: Docs** — `docs/spec/07-conventions-and-constraints.md`: repository identity covers detected worktrees (path-derived id, not a list entry, resolved after stored entries); the write lock is per id, so a main repository and each of its worktrees have separate locks while git's own ref locking still serialises ref updates between them. `docs/spec/01-repositories-and-sidebar.md`: the list read also detects worktrees (what is and is not listed), and the checked-out-elsewhere checkout error text.
- [ ] **Step 7:** Commit `feat(worktrees): list a repository's worktrees and open them like repositories`.

---

### Task 3: Frontend — child rows, name order, branch marker, shared stash note

**Files:** Modify `frontend/src/lib/types.ts` (`Repo.parentId?`, `Repo.worktree?`, `Branch.worktree?`), `frontend/src/lib/repoGroups.ts` (+ `repoGroups.test.ts`), `frontend/src/components/Sidebar.svelte`, `RepoRow.svelte`, `RepoRefs.svelte`; docs `docs/spec/01-repositories-and-sidebar.md`.

**Produces:** `groupRepos(repos)` returns `{ loose: RepoNode[]; groups: { name: string; repos: RepoNode[] }[] }` with `interface RepoNode { repo: Repo; children: Repo[] }`, all sorted by `compareRepos(a, b)` (name, base sensitivity, then path).

- [ ] **Step 1: Failing tests** (`repoGroups.test.ts`, update existing cases to the node shape): name order case-insensitive with a path tie-break; a child under a loose parent and under a grouped parent; a child whose `parentId` is not in the list is a top-level node; a child's own `group` is ignored (it follows the parent); children sorted by name.
- [ ] **Step 2:** `npx vitest run src/lib/repoGroups.test.ts` → FAIL.
- [ ] **Step 3: Implement** `groupRepos`: split parents (no `parentId`, or `parentId` not among ids) from children; bucket children by `parentId`; build nodes; loose/group split on the parent's `group`; sort nodes and children with `compareRepos`; groups by name as today.
- [ ] **Step 4:** tests PASS.
- [ ] **Step 5: Components.**
  - `Sidebar.svelte`: `{#each grouped.loose as node (node.repo.id)}<RepoRow repo={node.repo} />{#each node.children as child (child.id)}<RepoRow repo={child} depth={1} child />{/each}{/each}`; same inside groups with depths 1 and 2.
  - `RepoRow.svelte`: `export let child = false`; when `child`, render a `↳` marker before the name and do not make the row draggable; menu: for `repo.worktree` (detected) only Fetch, Pull, Push and the reveal item; for a nested stored entry (`child && !repo.worktree`) the normal menu minus "Move to group…".
  - `RepoRefs.svelte`: a local branch with `worktree` shows a small worktree marker (reuse an existing icon, e.g. `folder`) and `title="Checked out in <basename>"`; its Checkout menu item, double-click and "Delete…" are disabled with that title. The Stash header gets `title="Shared with the main repository and its other worktrees"` when the selected repo has `parentId` (look it up in `$repos`).
- [ ] **Step 6:** `npx vitest run && npm run check` → PASS, 0 errors.
- [ ] **Step 7: Docs** — `docs/spec/01-repositories-and-sidebar.md`: worktree child rows (placement, label, menu, nested stored entry), name ordering everywhere, the branch marker and disabled actions, the shared-stash note.
- [ ] **Step 8:** Commit `feat(sidebar): worktrees as child rows, repositories sorted by name`.

---

### Task 4: Click the selected commit to deselect it

**Files:** Modify `frontend/src/lib/stores.ts` (+ `stores.test.ts`), `frontend/src/components/LogList.svelte`; docs `docs/spec/02-log-and-history.md`.

**Produces:** `toggleCommit(hash: string)` — selects `hash`, or clears the selection when it is already selected; `toggleUncommitted()` — selects the row, or clears it when already selected.

- [ ] **Step 1: Failing tests** (`stores.test.ts`): `toggleCommit('a')` selects `a`; again clears it; `toggleCommit('b')` after `a` selects `b`; `toggleUncommitted()` selects the row and clears `selectedHash`; again clears the row.
- [ ] **Step 2:** FAIL.
- [ ] **Step 3: Implement** in `stores.ts`:

```ts
/** A left click on the log: select the commit, or clear the selection when
 *  it is the one already selected (closing the details pane). */
export function toggleCommit(hash: string) {
  selectedHash.set(get(selectedHash) === hash ? '' : hash)
}

export function toggleUncommitted() {
  if (get(uncommittedSelected)) uncommittedSelected.set(false)
  else selectUncommitted()
}
```

In `LogList.svelte`, the row `on:click` and `onGraphClick`'s row selection call `toggleCommit(row.hash)`; the uncommitted row's click and the graph click above the first row call `toggleUncommitted()`. The context menu (`commitMenu`) and `jump()` keep `selectedHash.set`.
- [ ] **Step 4:** `npx vitest run && npm run check` → PASS.
- [ ] **Step 5: Docs** — `docs/spec/02-log-and-history.md`: selection toggles on a repeated left click (commit rows, graph dots, the uncommitted row); right-click and jumps always select.
- [ ] **Step 6:** Commit `feat(log): click the selected commit to deselect it`.

---

### After the tasks

Mark the design doc `Status: Implemented (manual pass pending)`, run `go test ./...`, `npx vitest run`, `npm run check`, `make build`, and dispatch one final whole-branch review (most capable model) before merging.
