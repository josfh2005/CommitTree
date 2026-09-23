# git-ui — "Uncommitted changes" row in the log — Design

Date: 2026-09-23
Status: Approved design, not implemented
Builds on: `2026-09-21-working-tree-design.md` (2a, the Changes view this
row opens) and `2026-09-16-git-ui-design.md` (the log and its graph).

## Goal

Give the log a Sourcetree-style top row, **"Uncommitted changes"**, above the
newest commit. Selecting it shows the working tree in the details pane under
the log — list, diff and commit box — the same way selecting a commit shows
that commit. It is a second, log-centric way into the working tree; the
Changes view reached from the sidebar stays.

2a chose "a third mode in the main pane … rather than … a synthetic row at the
top of the log". This design does not reverse that: the two coexist, and both
show the same store, so staging in one is visible in the other.

## Decisions

- **Details pane content**: the whole `ChangesView` (FileList with
  Stage/Unstage/Discard, the diff, the CommitBox), reused unchanged. You can
  commit without leaving the log.
- **Visibility**: the row exists only while the working tree has changes
  (anything staged, unstaged or untracked) and disappears when it is clean. It
  is shown regardless of the log's filters and ordering — those select
  history, and the row is not history.
- **Graph**: a hollow, dashed circle in HEAD's lane. A dashed line joins it to
  HEAD only when HEAD is the first loaded row; otherwise (newer commits on
  other branches, date ordering) the dot stands alone so the line never
  crosses other lanes. If HEAD is not among the loaded rows (filters, paging,
  unborn branch), the dot goes in lane 0.
- **No context menu** on the row for now.
- **Conflicts still win.** While `conflictOwnsScreen(...)` is true the details
  pane shows `MergeView`, whatever is selected — unchanged behaviour.

## Architecture

Frontend only. The backend, `getLog` and its paging key are untouched: the row
is not a `LogRow` and never enters `rows`.

Rejected alternative: have the backend insert a synthetic `LogRow` into the
first page. It would tie working-tree state into the paging key and the lane
computation, and every stage/unstage would invalidate page 0.

### Selection — `lib/stores.ts`

A new store, `uncommittedSelected = writable(false)`, mutually exclusive with
`selectedHash`: selecting one clears the other. A sentinel value inside
`selectedHash` is rejected because `CommitDetails`, the commit context menu,
`jumpTo` and `App.svelte`'s `mainView` switch all treat it as a real hash.

`selectRepo` clears it along with `selectedHash`. Selecting it also sets
`mainView` to `'log'`, as selecting a commit already does.

### Pure helpers — `lib/uncommitted.ts` (new, with tests)

- `uncommittedCount(state: WorktreeState | null): number` — the number of
  distinct paths across staged, unstaged and untracked. A partially staged
  file (git status `MM`) is listed in both Staged and Unstaged and counts
  once. `null` → 0.
- `uncommittedMarker(rows: LogRow[]): { lane: number; joined: boolean }` —
  the lane of the row with `isHead` (0 if none is loaded), and `joined` true
  only when that row is `rows[0]`.

The row offset itself is `offset = count > 0 ? 1 : 0`, applied in `LogList`.

### `LogList.svelte`

- Computes `count` from `$worktreeState` and `offset`.
- Every place that turns a row index into a y position, or a y position into
  a row, adds or subtracts `offset`: row `top`, the spacer height, the shallow
  note, `visibleRange`, the canvas draw loop (`rowCenterY(i + offset)`,
  `edgeSegment(edge, i + offset)`), graph hit-testing (`arrowAt`, click and
  context menu row lookup), the load-more threshold and `jump()`'s scroll
  target. `graphWidth` takes the visible real rows as today; the marker lives
  in an existing lane so it never widens the graph.
- Draws the marker: a dashed hollow circle at `(laneX(marker.lane),
  rowCenterY(0))` in HEAD's lane colour, and when `joined` a dashed segment
  from it to `rowCenterY(1)`.
- Renders the row as a `button.row` at `top: 0`, text
  `Uncommitted changes (N)` in the subject column, muted author/date/hash
  columns left empty, `class:selected={$uncommittedSelected}`. Click sets
  `uncommittedSelected` and clears `selectedHash`.
- Clicking a commit row clears `uncommittedSelected`.

### `LogView.svelte`

The details pane opens when a conflict owns the screen, a hash is selected,
**or** `$uncommittedSelected` (and the row exists). Order of precedence:
`MergeView` → `ChangesView` for the row → `CommitDetails`.

### Keeping the row fresh

Today only a mounted `ChangesView` listens to `worktree:changed`. That
subscription moves to a global place — beside `startFocusRefresh` in
`lib/actions.ts`, started from `App.svelte` — calling `loadWorktreeState()`
for the selected repository. `ChangesView` drops its own listener; it already
follows `$worktreeState`. Window focus and the embedded terminal's settle
check already call `loadWorktreeState()` and need nothing new.

### When the tree becomes clean while the row is selected

After a commit (or a discard/stash that empties the tree) the row disappears.
If it was selected, the selection moves to the HEAD commit
(`$refs.headHash`), so the user sees what they just committed. If there is no
HEAD hash, both selections are cleared and the details pane closes. A reactive
statement in `LogList` does this, since it owns `count`.

## Error handling

`loadWorktreeState` already sets the store to `null` on failure; `null` counts
as 0, so a failing status read hides the row rather than showing a stale one.
Everything else inside the pane is `ChangesView`'s existing behaviour.

## Testing

- vitest for `uncommittedCount` (empty, each list alone, an `MM` path counted
  once, untracked plus staged, `null`) and `uncommittedMarker` (HEAD first,
  HEAD further down, HEAD not loaded, empty rows).
- vitest for the selection helpers if they end up as functions in
  `lib/uncommitted.ts` (selecting one clears the other).
- `svelte-check`, `go test ./...` untouched but run, `make build`.
- Manual: dirty tree → row with count and dashed dot joined to HEAD; select →
  Changes pane under the log; stage in the row's pane and see it in the
  sidebar's Changes view; commit → row disappears and HEAD is selected;
  `git add` in the embedded terminal updates the count; date ordering with a
  newer commit on another branch → dot without a line; a merge in progress →
  MergeView wins.

## Docs

`2026-09-21-working-tree-design.md`'s scope decision is updated in the same
commit as the implementation to say the synthetic row now coexists with the
Changes view (see `git-ui-spec-in-sync`).

## Out of scope

Checkbox staging in the Sourcetree style, a context menu on the row (stash,
discard all), keyboard navigation onto the row, and any change to the Changes
view reached from the sidebar.
