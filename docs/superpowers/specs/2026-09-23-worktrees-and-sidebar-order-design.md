# git-ui — Git worktrees in the sidebar, repos sorted by name, click to deselect a commit — Design

Date: 2026-09-23
Status: Implemented (manual pass pending)
Builds on: `docs/spec/01-repositories-and-sidebar.md`, `docs/spec/02-log-and-history.md`.

Three independent changes asked for together. Worktrees are the large one;
the other two are small and ride along.

## 1. Git worktrees

### Goal

The owner works in git worktrees every day (Claude Code sessions run in
them). Today a worktree can be added as a repository and works, but git-ui
does not know worktrees exist: a worktree and its main repository show as two
unrelated entries, worktrees created elsewhere never appear, and checking out
a branch that is checked out in another worktree fails with git's raw error.

This design lets the user **see and open** a repository's worktrees and see
which branches are checked out elsewhere. Creating and removing worktrees is
out of scope for now.

### Decisions

- **Detected, not added.** A listed repository's linked worktrees are read
  from git every time the repository list is read; nothing about them is
  stored in `repos.json`. A worktree removed elsewhere (e.g. by Claude Code)
  simply stops appearing — there is nothing to clean up.
- **Child rows under the repository.** Each linked worktree is a row indented
  under its main repository's row, marked `↳`, showing the worktree
  directory's name and its current branch (or `HEAD (<short>)` when
  detached). Child rows are visible whether or not the main repository is
  expanded, and sorted by name.
- **A worktree is a repository for everything else.** Selecting a child row
  selects that worktree exactly as selecting a repository does: its own log,
  Changes, branches/remotes/tags/stash when expanded, chat conversation,
  terminal tabs, write lock. It can be expanded like a repository.
- **Stable identity.** A worktree's identifier is derived from its path with
  the same function that derives a repository's, so its chat history,
  terminal tabs and remembered UI state survive restarts and come back if a
  worktree is recreated at the same path.
- **Its context menu** offers Fetch, Pull, Push and "Show in Finder" (or the
  platform's equivalent) — not "Remove from list", "Move to group…" or
  "Locate…": a worktree is not a list entry and follows its main repository.
- **What is not listed:** the main working tree itself (it is the repository's
  own row), a bare repository's entry, and any worktree git reports as
  `prunable` or whose directory no longer exists.
- **A repository added by hand that is a worktree** of another listed
  repository is shown nested under that repository (keeping its own
  identifier, so nothing it remembers is lost), and it keeps "Remove from
  list" since it *is* a list entry. It is never shown twice. If its main
  repository is not listed, it stays an ordinary top-level entry.
- **Branches checked out in another worktree.** In the Branches section, a
  local branch that is checked out in a different worktree of the same
  repository shows a worktree marker with the tooltip "Checked out in
  <directory name>". Its Checkout action (menu item and double-click) and
  "Delete…" are disabled with that reason. If git still refuses a checkout
  for this reason (e.g. from the chat's write tools), the error shown is
  "<branch> is checked out in another worktree (<path>)" rather than git's
  raw text.
- **Shared stash.** A worktree's Stash section header carries a tooltip
  saying the stash is shared with the main repository and its other
  worktrees (git keeps one stash per repository).

### Architecture

#### Reading worktrees — `internal/repos` (or a small `internal/worktrees` package)

```go
type Worktree struct {
    Path     string // absolute
    Head     string // commit hash
    Branch   string // short name, "" when detached
    Detached bool
    Prunable bool
    Main     bool   // the main working tree (first entry)
}

func List(ctx context.Context, dir string) ([]Worktree, error)
```

Parsed from `git worktree list --porcelain -z` (NUL-separated, so paths with
newlines survive). Records are separated by an empty field; keys: `worktree`,
`HEAD`, `branch refs/heads/<x>`, `detached`, `bare`, `prunable [reason]`,
`locked [reason]`. The first record is the main working tree.

#### App — `ListRepos` and resolution

- `RepoItem` gains `ParentID string` (json `parentId`, empty for top-level)
  and `Worktree bool` (true for a detected worktree that is not a list
  entry).
- `ListRepos`: for each present stored repository, `worktrees.List`; for each
  linked, non-prunable worktree whose directory exists: if a stored entry has
  the same path, set that entry's `ParentID`; otherwise append a detected
  item with `ID = idFor(path)`, `Name = base(path)`, `Path`, `Branch`,
  `ParentID`, `Worktree: true`. Children are computed only for a stored
  repository that is itself a main working tree (its path equals the first
  record's path): git lists every worktree from any of them, so a stored
  entry that is a linked worktree gets no children of its own — it is itself
  nested under its main repository when that one is listed.
- The detected items are remembered in memory (`a.worktrees map[id]path`,
  replaced wholesale on every `ListRepos`) so `a.dir(id)` resolves a stored
  repository first and then a detected worktree. An id that is neither is
  `ErrUnknownRepo` as today.
- Refusals for list-entry actions (`RemoveRepo`, `RelocateRepo`,
  `SetRepoGroup`) on a detected worktree id: `ErrUnknownRepo` (they are not
  list entries), and the frontend never offers them.
- Branches checked out elsewhere: `GetRefs` gains, per local branch,
  `Worktree string` — the path of the other worktree that has it checked out
  (empty when none, and empty for the current worktree's own branch), from
  the same `worktrees.List` call.
- The checkout error: `ops.Checkout` recognises git's "is already used by
  worktree at" / "is already checked out at" message and returns
  `ErrCheckedOutElsewhere{Branch, Path}` with the friendly text.

#### Frontend

- `lib/repoGroups.ts`: grouping places items with a `parentId` under their
  parent (in whichever group or loose area the parent is), sorted by name;
  a `parentId` naming a repository not in the list falls back to top level.
- `Sidebar`/`RepoRow`: child rows with a depth one deeper than their parent,
  `↳` marker, the reduced context menu for `worktree` items, "Remove from
  list" kept for a stored entry that is nested.
- `RepoRefs`: the branch marker, tooltip and disabled actions; the Stash
  header tooltip for worktrees.
- Selection, chat, terminal, log: unchanged — they already key on the id.

### Error handling

- `git worktree list` failing for one repository (corrupt metadata, very old
  git) leaves that repository without child rows and does not fail the list.
- A worktree deleted while selected: the next list read drops it; the
  selection then points at an unknown id, handled as the removal of a
  selected repository is today (selection cleared).

### Testing

- Go: `worktrees.List` against real repositories built with
  `git worktree add` (a branch worktree, a detached one, a removed-directory
  one reported `prunable`, a path with a space); `ListRepos` returns child
  items with the right parent, never lists the main tree twice, nests a
  hand-added worktree entry without duplicating it; `a.dir` resolves a
  detected worktree; `GetRefs` reports the other worktree's path for its
  branch; `ops.Checkout` of such a branch returns `ErrCheckedOutElsewhere`.
- vitest: grouping with parents in a group and loose, orphan parent ids,
  name ordering of children.
- Manual: the owner's `git-ui` repo shows its Claude Code worktrees as
  children; selecting one shows its log and chat; a Claude worktree removed
  in the terminal disappears on focus; the branch marker and disabled
  checkout; the chat's `checkout_branch` on such a branch gives the friendly
  error.

## 2. Repositories sorted by name

Within the loose area, within every group, and among a repository's worktree
children, rows are sorted by display name, case-insensitively (locale
compare with base sensitivity), ties broken by path. Groups keep their
existing name order. Manual drag reordering is out of scope (noted for
later if name order is not enough).

## 3. Clicking the selected commit deselects it

In the log, a click on the row (or its graph dot) of the commit that is
already selected clears the selection, which closes the details pane — the
same state as before anything was selected. The same applies to the
"Uncommitted changes" row: clicking it while selected deselects it. The
context menu never deselects (right-click on the selected row keeps it
selected). Selecting via a jump arrow or from elsewhere (search, chat links)
always selects, never toggles.

## Docs

`docs/spec/01-repositories-and-sidebar.md` (worktree rows, order, branch
marker, shared stash), `docs/spec/02-log-and-history.md` (click to
deselect), `docs/spec/07-conventions-and-constraints.md` (worktree ids are
not list entries; the write lock is per worktree id, while git's own
per-repository locks still serialise ref updates across worktrees) — each
in the same commit as the behaviour.

## Out of scope

Creating, removing, locking, moving or pruning worktrees; manual drag
reordering of repositories; worktrees of a repository that is not in the
list.
