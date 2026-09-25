# git-ui — Repository toolbar — Design

Date: 2026-09-25
Status: Approved design, not implemented
Builds on: `2026-09-21-working-tree-design.md` (Changes view, commit box),
`2026-09-21-push-pull-stash-design.md` (fetch/pull/push, stash),
`2026-09-17-merge-agent-design.md` (merging a branch and its conflicts),
`2026-09-22-embedded-terminal-design.md` and the AI chat panel.

## Goal

Put the repository's main git actions one big, labelled click away, in the
style of SourceTree's toolbar. Today the header has three 14 px unlabelled
icons (Fetch, Pull, Push) plus separate Terminal and chat buttons; commit,
stash, new branch and merge live in the Changes view or in context menus.

## Decisions

- **Actions, in three groups:** Commit, Stash · Fetch, Pull, Push · Branch,
  Merge — then, on the right, Terminal, Finder (Folder on Linux), Chat. Tag,
  rebase and cherry-pick stay in context menus: they act on a selected commit.
- **Layout B:** one taller header (~60 px): repository name and path on two
  lines on the left, the toolbar on the right; 20 px outline icons with the
  label under each, in the app's own style.
- **Narrow window:** the title and path ellipsize first; below a width where
  the labelled toolbar no longer fits, the labels hide and the buttons become
  icon-only with the name in the tooltip. A CSS container query on the
  header, no JavaScript.
- **Merge picks with a searchable dialog** (not a drop-down menu), then the
  existing "Merge X into Y?" confirmation.
- **Branch uses the existing "New branch" dialog**, from HEAD.
- **Terminal and Chat become toggles** in the toolbar, highlighted while
  open; the header's separate Terminal button and chat button go away.
- **Every disabled button says why** in its tooltip.
- Approach: the rules live in a pure, tested `lib/toolbar.ts`; the
  component only draws them. Not configurable (no reorder/hide) — later if
  asked. No native macOS NSToolbar (Wails v2 does not support it).

## Behaviour

| Button | Action | Disabled when (tooltip reason) |
|---|---|---|
| Commit | Selects the "Uncommitted changes" row, which opens the Changes view, and focuses the commit message box | no uncommitted changes ("Nothing to commit"); a merge, rebase or stash conflict is in progress ("Resolve the conflict first"); another operation is running (the busy label itself, e.g. "Pushing…") |
| Stash | The existing "Stash changes" dialog (message, include untracked) | no uncommitted changes ("Nothing to stash"); a conflict is in progress; busy |
| Fetch | Unchanged | busy |
| Pull | Unchanged; behind count badge | busy; a conflict is in progress (today's `canSync`) |
| Push | Unchanged; ahead count badge | same as Pull |
| Branch | The existing "New branch" dialog from HEAD (name, "Check out after creating") | busy |
| Merge | The branch picker dialog, then the existing merge confirmation and flow | detached HEAD ("Check out a branch first"); a conflict is in progress; busy |
| Terminal | Shows/hides the terminal panel; active while open | never |
| Finder / Folder | Opens the repository folder | never |
| Chat | Shows/hides the chat panel; active while open | never |

"Uncommitted changes" counts staged, unstaged and untracked files, as the
log's uncommitted row does. The "Resolve conflicts" button (a dismissed
stash conflict) stays where it is, left of the toolbar. A missing
(moved/deleted) repository shows no toolbar, as today.

### Merge branch picker

- Title "Merge into <current branch>"; a search field (focused), a list, and
  Cancel / Merge.
- The list: local branches, then remote-tracking branches as
  `remote/name`; the current branch is left out.
- The search filters as you type: case-insensitive, matching any part of the
  name (`log` finds `feature/login`). No match shows "No branches match".
- ↑/↓ move the selection (the first item is selected initially and after
  each filter change), Enter or double-click confirms, Escape or a click
  outside cancels. Merge is disabled with nothing selected.
- Confirming runs the existing `mergeBranch(id, branch, into)`: its
  confirmation, its "already up to date" toast, conflicts and submodule
  warnings are unchanged.

## Architecture

- `frontend/src/lib/toolbar.ts` (new, pure):
  - `toolbarItems(input: ToolbarInput): ToolbarItem[]` where `ToolbarInput`
    is `{ refs, worktree, merge, busy, remote, terminalOpen, chatOpen,
    platform }` (the existing stores' values) and `ToolbarItem` is
    `{ id, label, icon, group, enabled, reason, active, badge }`.
  - `mergeCandidates(refs: Refs | null): MergeCandidate[]` — the picker's
    items (local, then remote; current branch left out), each keeping its
    `Branch`.
- `frontend/src/lib/pick.ts` (new, pure): `filterPick(items, query)`, the
  case-insensitive substring filter the picker applies as you type.
- `frontend/src/lib/ui.ts`: a `pick` dialog kind and `pickDialog(options)`
  returning the chosen item or null.
- `frontend/src/components/DialogHost.svelte`: renders `pick` (search field,
  list, keyboard handling) with the existing backdrop/Escape/focus.
- `frontend/src/lib/actions.ts`: `startCommit(id)` (select the uncommitted
  row and request focus on the commit box) and `pickAndMerge(id)` (picker →
  `mergeBranch`).
- `frontend/src/components/Toolbar.svelte`: rewritten to draw
  `toolbarItems` and dispatch each id to its action.
- `frontend/src/components/LogView.svelte`: taller header, two-line title,
  the old Terminal/chat buttons removed, container query.
- `frontend/src/components/CommitBox.svelte`: focuses its message box when
  `startCommit` asks (a small store flag).
- `frontend/src/components/Icon.svelte`: the new icons.

No backend change: every action already exists.

## Testing

- `toolbarItems`: clean repo; with changes; merge/rebase/stash conflict;
  detached HEAD; busy; ahead/behind badges; Terminal/Chat active; Finder vs
  Folder by platform; the reason text of each disabled button.
- `mergeCandidates`: excludes the current branch and a remote `HEAD`; local
  before remote. `filterPick`: case-insensitive substring match; empty query
  keeps all; no match gives an empty list.
- Manual, in the app: the picker's keyboard flow; narrow window; light, dark
  and high-contrast themes.

## Spec updates

`docs/spec/05-remote-and-stash.md`'s "three remote actions" toolbar section
becomes the whole toolbar (buttons, disabled rules and reasons, narrow
window, Merge picker); `docs/spec/01-repositories-and-sidebar.md`, whose branch
menu offers "Merge <branch> into <head>", notes Merge is also on the toolbar, and the terminal
and chat specs note their toggles moved into it.

## Out of scope

- Reordering or hiding toolbar buttons.
- Tag, rebase, cherry-pick, discard on the toolbar.
- Any backend change.
