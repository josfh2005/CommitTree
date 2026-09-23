# Uncommitted Changes Row Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A synthetic "Uncommitted changes (N)" row at the top of the log that, when selected, shows the existing Changes view in the details pane under the log.

**Architecture:** Frontend only. Pure helpers in a new `lib/uncommitted.ts`; a new `uncommittedSelected` store mutually exclusive with `selectedHash`; `LogList` shifts every real row down by a pixel `lead` (one `ROW_HEIGHT` when the tree is dirty) and draws the row plus a dashed marker in the graph; `LogView` mounts `ChangesView` in the details pane; the `worktree:changed` listener moves from `ChangesView` to `App.svelte` so the row stays fresh.

**Tech Stack:** Svelte (legacy `$:` syntax in these components), TypeScript, vitest, Wails runtime events.

**Spec:** `docs/superpowers/specs/2026-09-23-uncommitted-row-design.md`

## Global Constraints

- No backend change: `getLog`, its paging key and `LogRow` stay as they are.
- `ChangesView` is reused unchanged except for removing its own `worktree:changed` listener.
- A merge in progress keeps winning the details pane (`conflictOwnsScreen(...)` first).
- The row appears only when `uncommittedCount($worktreeState) > 0`, regardless of filters and log ordering.
- Commit messages: conventional style as in `git log`; **no `Co-Authored-By` line**.
- Frontend commands run from `frontend/` with Node 22: `source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npx vitest run` / `npm run check`.

## Review Focus

1. A partially staged file (`MM`) appears in Staged and Unstaged — the count must say 1, not 2. (Task 1 test.)
2. HEAD filtered out or not yet paged in — the marker must still draw (lane 0, no line) and not throw. (Task 1 test.)
3. The row selected, then the tree cleaned from the embedded terminal (`git stash`) — selection must move to HEAD, not leave an empty details pane. (Task 1 `cleanTreeSelection` tests + Task 3 manual step.)
4. Switching repository with the row selected — the new repo must not open with the row selected. (Task 2 Step 3 resets it in `selectRepo`; `selectRepo` fires backend loads, so this one is pinned by the Task 3 manual checklist rather than a unit test.)
5. Any code path that sets `selectedHash` (graph click, jump arrow, context menu, CommitDetails parent links) — must deselect the row without each call site knowing about it. (Task 2 test.)

---

### Task 1: Pure helpers — `lib/uncommitted.ts`

**Files:**
- Create: `frontend/src/lib/uncommitted.ts`
- Test: `frontend/src/lib/uncommitted.test.ts`

**Interfaces:**
- Consumes: `WorktreeState`, `FileStatus`, `LogRow` from `lib/types.ts`; `GRAPH_PADDING`, `LANE_WIDTH` from `lib/geometry.ts`.
- Produces:
  - `uncommittedCount(state: WorktreeState | null): number`
  - `uncommittedMarker(rows: LogRow[]): { lane: number; color: number; joined: boolean }`
  - `markerWidth(lane: number): number`
  - `cleanTreeSelection(selected: boolean, count: number, headHash: string): string | null` — `null`: nothing to do; `''`: clear the row selection and select nothing; a hash: clear the row selection and select that commit.

- [ ] **Step 1: Write the failing tests**

```ts
// frontend/src/lib/uncommitted.test.ts
import { describe, expect, it } from 'vitest'
import { GRAPH_PADDING, LANE_WIDTH } from './geometry'
import type { FileStatus, LogRow, WorktreeState } from './types'
import { cleanTreeSelection, markerWidth, uncommittedCount, uncommittedMarker } from './uncommitted'

const f = (path: string, status = 'M'): FileStatus => ({ path, status })
const state = (s: Partial<WorktreeState>): WorktreeState => ({ staged: [], unstaged: [], untracked: [], merging: false, ...s })
const row = (hash: string, lane: number, isHead = false, color = lane): LogRow => ({
  hash, short: hash.slice(0, 7), parents: [], author: 'a', email: 'a@x', date: '2026-09-23T00:00:00Z',
  subject: hash, refs: [], lane, color, edges: [], isMerge: false, isHead,
})

describe('uncommittedCount', () => {
  it('is 0 for null and for a clean tree', () => {
    expect(uncommittedCount(null)).toBe(0)
    expect(uncommittedCount(state({}))).toBe(0)
  })
  it('counts each list alone', () => {
    expect(uncommittedCount(state({ staged: [f('a')] }))).toBe(1)
    expect(uncommittedCount(state({ unstaged: [f('a'), f('b')] }))).toBe(2)
    expect(uncommittedCount(state({ untracked: [f('n', '?')] }))).toBe(1)
  })
  it('counts a partially staged path once', () => {
    expect(uncommittedCount(state({ staged: [f('a')], unstaged: [f('a')] }))).toBe(1)
  })
  it('adds distinct paths across lists', () => {
    expect(uncommittedCount(state({ staged: [f('a')], unstaged: [f('b')], untracked: [f('c', '?')] }))).toBe(3)
  })
})

describe('uncommittedMarker', () => {
  it('joins HEAD when it is the first row', () => {
    expect(uncommittedMarker([row('h', 1, true, 5), row('x', 0)])).toEqual({ lane: 1, color: 5, joined: true })
  })
  it('stands alone in HEAD\'s lane when HEAD is further down', () => {
    expect(uncommittedMarker([row('x', 0), row('h', 2, true, 3)])).toEqual({ lane: 2, color: 3, joined: false })
  })
  it('falls back to lane 0 when HEAD is not loaded', () => {
    expect(uncommittedMarker([row('x', 1), row('y', 2)])).toEqual({ lane: 0, color: 0, joined: false })
    expect(uncommittedMarker([])).toEqual({ lane: 0, color: 0, joined: false })
  })
})

describe('markerWidth', () => {
  it('is the graph width needed to show the given lane', () => {
    expect(markerWidth(0)).toBe(GRAPH_PADDING * 2 + LANE_WIDTH)
    expect(markerWidth(3)).toBe(GRAPH_PADDING * 2 + 4 * LANE_WIDTH)
  })
})

describe('cleanTreeSelection', () => {
  it('does nothing while the row is not selected', () => {
    expect(cleanTreeSelection(false, 0, 'abc')).toBeNull()
  })
  it('does nothing while the tree still has changes', () => {
    expect(cleanTreeSelection(true, 2, 'abc')).toBeNull()
  })
  it('moves the selection to HEAD when the tree becomes clean', () => {
    expect(cleanTreeSelection(true, 0, 'abc')).toBe('abc')
  })
  it('clears the selection when there is no HEAD', () => {
    expect(cleanTreeSelection(true, 0, '')).toBe('')
  })
})
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npx vitest run src/lib/uncommitted.test.ts`
Expected: FAIL — cannot resolve `./uncommitted`.

- [ ] **Step 3: Write the implementation**

```ts
// frontend/src/lib/uncommitted.ts
import { GRAPH_PADDING, LANE_WIDTH } from './geometry'
import type { LogRow, WorktreeState } from './types'

/** Distinct paths with any uncommitted change. A partially staged file
 *  (git status "MM") is listed under both Staged and Unstaged, and is still
 *  one file to the person reading "Uncommitted changes (N)". */
export function uncommittedCount(state: WorktreeState | null): number {
  if (!state) return 0
  const paths = new Set<string>()
  for (const f of [...state.staged, ...state.unstaged, ...state.untracked]) paths.add(f.path)
  return paths.size
}

/** Where the "Uncommitted changes" row's dot sits in the graph: HEAD's lane
 *  and colour, joined to HEAD by a dashed line only when HEAD is the first
 *  row — anywhere lower, the line would cross other branches' lanes. With
 *  HEAD not loaded (filters, paging, an unborn branch) it falls back to
 *  lane 0 on its own. */
export function uncommittedMarker(rows: LogRow[]): { lane: number; color: number; joined: boolean } {
  const index = rows.findIndex((r) => r.isHead)
  if (index < 0) return { lane: 0, color: 0, joined: false }
  return { lane: rows[index].lane, color: rows[index].color, joined: index === 0 }
}

/** The graph column width that still shows the marker's lane — HEAD may be
 *  scrolled out of the visible slice that graphWidth measures. */
export function markerWidth(lane: number): number {
  return GRAPH_PADDING * 2 + (lane + 1) * LANE_WIDTH
}

/** What to select once the tree is clean while the row was selected (a
 *  commit, discard or stash emptied it): null = leave things alone, '' =
 *  select nothing, a hash = select that commit (HEAD, so a commit made from
 *  the row is what the details pane shows next). */
export function cleanTreeSelection(selected: boolean, count: number, headHash: string): string | null {
  if (!selected || count > 0) return null
  return headHash
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npx vitest run src/lib/uncommitted.test.ts`
Expected: PASS (all tests).

- [ ] **Step 5: Commit**

```bash
git add frontend/src/lib/uncommitted.ts frontend/src/lib/uncommitted.test.ts
git commit -m "feat(log): helpers for the uncommitted changes row"
```

---

### Task 2: Selection store, details pane and a global worktree listener

**Files:**
- Modify: `frontend/src/lib/stores.ts` (after the `mainView` declaration ~line 77; `selectRepo` ~line 238)
- Modify: `frontend/src/lib/stores.test.ts`
- Modify: `frontend/src/components/LogView.svelte`
- Modify: `frontend/src/components/ChangesView.svelte` (remove its `EventsOn` listener)
- Modify: `frontend/src/App.svelte` (`onMount`, the `mainView` reactive line)

**Interfaces:**
- Consumes: `uncommittedCount` from Task 1.
- Produces:
  - `uncommittedSelected: Writable<boolean>` in `lib/stores.ts`
  - `selectUncommitted(): void` in `lib/stores.ts` — clears `selectedHash`, sets `uncommittedSelected` to true, sets `mainView` to `'log'`.
  - Invariant: any non-empty `selectedHash.set(...)` anywhere clears `uncommittedSelected` (a store subscription, so call sites need no change).

- [ ] **Step 1: Write the failing tests** (append to `frontend/src/lib/stores.test.ts`; extend its import from `./stores`)

```ts
import { mainView, persisted, selectedHash, selectUncommitted, uncommittedSelected } from './stores'

describe('uncommitted row selection', () => {
  afterEach(() => {
    selectedHash.set('')
    uncommittedSelected.set(false)
    mainView.set('log')
  })
  it('selecting the row clears the selected commit and shows the log', () => {
    selectedHash.set('abc')
    mainView.set('changes')
    selectUncommitted()
    expect(get(uncommittedSelected)).toBe(true)
    expect(get(selectedHash)).toBe('')
    expect(get(mainView)).toBe('log')
  })
  it('selecting any commit deselects the row, whoever sets it', () => {
    selectUncommitted()
    selectedHash.set('def')
    expect(get(uncommittedSelected)).toBe(false)
  })
  it('clearing the commit selection leaves the row selected', () => {
    selectUncommitted()
    selectedHash.set('')
    expect(get(uncommittedSelected)).toBe(true)
  })
})
```

(Keep the existing `persisted` import; the line above replaces it. `afterEach` and `get` are already imported in this file.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npx vitest run src/lib/stores.test.ts`
Expected: FAIL — `selectUncommitted` / `uncommittedSelected` not exported.

- [ ] **Step 3: Add the store** — in `frontend/src/lib/stores.ts`, directly after the `mainView` declaration:

```ts
/** The log's synthetic "Uncommitted changes" row is selected, so the
 *  details pane shows the Changes view. Mutually exclusive with
 *  selectedHash: selectUncommitted clears the hash, and the subscription
 *  below clears this whenever anything selects a commit — graph clicks,
 *  jump arrows, context menus — without each of them knowing about it. */
export const uncommittedSelected = writable(false)
selectedHash.subscribe((hash) => {
  if (hash) uncommittedSelected.set(false)
})

export function selectUncommitted() {
  selectedHash.set('')
  uncommittedSelected.set(true)
  mainView.set('log')
}
```

And in `selectRepo`, inside the `if (get(selectedRepoId) !== id) {` block, after `selectedHash.set('')`:

```ts
    uncommittedSelected.set(false)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npx vitest run src/lib/stores.test.ts`
Expected: PASS.

- [ ] **Step 5: Details pane** — replace the script and the `{#key}` block contents in `frontend/src/components/LogView.svelte`:

Script — add imports and one reactive line:

```ts
  import ChangesView from './ChangesView.svelte'
  import { uncommittedCount } from '../lib/uncommitted'
  import { chatOpen, detailsHeight, mergeState, selectedHash, selectedRepo, stashConflictDismissed, terminalOpen, uncommittedSelected, worktreeState } from '../lib/stores'

  // The row can be selected a moment before the tree turns out clean (LogList
  // then moves the selection to HEAD); never open an empty Changes pane.
  $: showUncommitted = $uncommittedSelected && uncommittedCount($worktreeState) > 0
```

Template — the details block becomes:

```svelte
      {#if conflictOwnsScreen($mergeState, $stashConflictDismissed) || $selectedHash || showUncommitted}
        <Splitter direction="horizontal" on:drag={(e) => detailsHeight.set(Math.min(720, Math.max(120, $detailsHeight - e.detail)))} />
        <div class="details" style="height: {$detailsHeight}px">
          {#if conflictOwnsScreen($mergeState, $stashConflictDismissed)}
            <MergeView repoId={$selectedRepo.id} />
          {:else if showUncommitted}
            <ChangesView repoId={$selectedRepo.id} />
          {:else if $selectedHash}
            <CommitDetails repoId={$selectedRepo.id} hash={$selectedHash} />
          {/if}
        </div>
      {/if}
```

- [ ] **Step 6: Move the `worktree:changed` listener to the app** — in `frontend/src/components/ChangesView.svelte` delete the `EventsOn` import, the `WorktreeChangedEvent` type import, the `onDestroy` import, and this block:

```ts
  // The pane follows $worktreeState (below), so the handler only reloads it.
  const off = EventsOn('worktree:changed', (payload: WorktreeChangedEvent) => {
    if (payload?.repoID !== repoId) return
    loadWorktreeState()
  })
  onDestroy(off)
```

(`loadWorktreeState` is then unused in ChangesView — remove it from the stores import too.)

In `frontend/src/App.svelte`: import `EventsOn` from `'../wailsjs/runtime/runtime'`, `loadWorktreeState` and `uncommittedSelected` from `./lib/stores`, and `type WorktreeChangedEvent` from `./lib/types`. Replace `onMount` with:

```ts
  onMount(() => {
    loadRepos().then(loadRefs)
    loadAISettings()
    const stopFocus = startFocusRefresh()
    // Every view of the working tree follows $worktreeState — the Changes
    // view and the log's "Uncommitted changes" row alike — so one app-wide
    // listener keeps them fresh whether or not a Changes pane is mounted.
    const offWorktree = EventsOn('worktree:changed', (payload: WorktreeChangedEvent) => {
      if (payload?.repoID === $selectedRepo?.id) loadWorktreeState()
    })
    return () => {
      stopFocus()
      offWorktree()
    }
  })
```

And change the `mainView` reactive line to:

```ts
  $: if ($selectedHash || $uncommittedSelected) mainView.set('log')
```

(Update its comment: "Selecting a commit — or the log's uncommitted row — means the user wants the log pane, so switch the main pane back.")

- [ ] **Step 7: Verify**

Run: `cd frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npx vitest run && npm run check`
Expected: all vitest suites PASS; svelte-check reports 0 errors and no new warnings in the touched files.

- [ ] **Step 8: Commit**

```bash
git add frontend/src/lib/stores.ts frontend/src/lib/stores.test.ts frontend/src/components/LogView.svelte frontend/src/components/ChangesView.svelte frontend/src/App.svelte
git commit -m "feat(log): select uncommitted changes into the details pane"
```

---

### Task 3: The row and its marker in `LogList`, plus docs

**Files:**
- Modify: `frontend/src/components/LogList.svelte`
- Modify: `docs/superpowers/specs/2026-09-21-working-tree-design.md` (scope decision, line ~23-24)
- Modify: `docs/superpowers/specs/2026-09-23-uncommitted-row-design.md` (Status line)

**Interfaces:**
- Consumes: `uncommittedCount`, `uncommittedMarker`, `markerWidth`, `cleanTreeSelection` (Task 1); `uncommittedSelected`, `selectUncommitted`, `worktreeState` (Task 2 / existing stores); `DOT_RADIUS`, `laneX`, `laneColor`, `rowCenterY`, `ROW_HEIGHT` (existing geometry).
- Produces: nothing other tasks use.

The whole change rests on one number: `lead`, the pixels the real rows are pushed down by (`ROW_HEIGHT` when the row is shown, else 0). Real row `i` keeps index `i` everywhere (the `rows` array, `byHash`, `edgeSegment`, `arrowAt`); only the pixel mapping moves.

- [ ] **Step 1: Script — imports and derived values.** Add to the imports:

```ts
  import { cleanTreeSelection, markerWidth, uncommittedCount, uncommittedMarker } from '../lib/uncommitted'
```

Extend the stores import with `selectUncommitted, uncommittedSelected, worktreeState`. Then, after the `let hover ...` declaration:

```ts
  // The synthetic "Uncommitted changes" row sits above the first commit;
  // every real row is pushed down by `lead` pixels while it is shown. Row
  // indices never change — only the index↔pixel mapping does.
  $: count = uncommittedCount($worktreeState)
  $: lead = count > 0 ? ROW_HEIGHT : 0
  $: marker = uncommittedMarker(rows)

  // A commit, discard or stash that empties the tree removes the row; if it
  // was selected, show HEAD instead of leaving an empty details pane.
  $: {
    const next = cleanTreeSelection($uncommittedSelected, count, $refs?.headHash ?? '')
    if (next !== null) {
      uncommittedSelected.set(false)
      if (next) selectedHash.set(next)
    }
  }
```

- [ ] **Step 2: Scroll, range and width.** Replace `onScroll`'s `nearEnd` line with:

```ts
    const nearEnd = scrollTop - lead + viewport > (rows.length - 100) * ROW_HEIGHT
```

Replace the `range` and `width` reactive lines with:

```ts
  $: range = visibleRange(Math.max(0, scrollTop - lead), viewport, rows.length)
  // Size the graph column to the rows on screen so one wide stretch of history
  // doesn't squeeze the messages everywhere else — plus the marker's lane
  // while the uncommitted row is on screen, since HEAD may be scrolled away.
  $: width = graphVisible
    ? Math.max(graphWidth(rows.slice(range.start, Math.min(rows.length, range.end + 1))), lead && scrollTop < lead ? markerWidth(marker.lane) : 0)
    : 12
  $: draw(canvas, rows, range, width, viewport, scrollTop, graphVisible, lead, marker)
```

- [ ] **Step 3: Drawing.** In `draw`, replace the two transform/clear lines with:

```ts
    ctx.setTransform(dpr, 0, 0, dpr, 0, (lead - scrollTop) * dpr)
    ctx.clearRect(0, scrollTop - lead, width, viewport)
```

At the end of `draw` (after the dots loop), add:

```ts
    // The uncommitted row's marker, one row above the first commit: a hollow
    // dashed dot, joined to HEAD only when HEAD is directly below it.
    if (lead) {
      const x = laneX(marker.lane)
      const y = rowCenterY(-1)
      ctx.setLineDash([2, 2])
      ctx.strokeStyle = laneColor(marker.color)
      ctx.lineWidth = 1.6
      if (marker.joined) {
        ctx.beginPath()
        ctx.moveTo(x, y + DOT_RADIUS)
        ctx.lineTo(x, rowCenterY(0) - DOT_RADIUS)
        ctx.stroke()
      }
      ctx.beginPath()
      ctx.arc(x, y, DOT_RADIUS, 0, Math.PI * 2)
      ctx.fillStyle = surface
      ctx.fill()
      ctx.stroke()
      ctx.setLineDash([])
    }
```

- [ ] **Step 4: Hit-testing.** `graphPoint` returns y in real-row space:

```ts
  function graphPoint(event: MouseEvent) {
    const rect = canvas.getBoundingClientRect()
    return { x: event.clientX - rect.left, y: event.clientY - rect.top + scrollTop - lead }
  }
```

In `onGraphClick`, before `const row = ...`:

```ts
    if (p.y < 0) {
      if (lead) selectUncommitted()
      return
    }
```

In `onGraphContext`, return early when `graphPoint(event).y < 0` (no menu on the row):

```ts
  function onGraphContext(event: MouseEvent) {
    const p = graphPoint(event)
    if (p.y < 0) return
    const row = rows[Math.floor(p.y / ROW_HEIGHT)]
    if (row) commitMenu(event, row)
  }
```

In `jump`, the scroll target becomes:

```ts
    scroller.scrollTop = Math.max(0, index * ROW_HEIGHT + lead - viewport / 2)
```

- [ ] **Step 5: Template.** Spacer height, row tops and the shallow note gain `lead`, and the row is rendered before the `{#each}`:

```svelte
    <div class="spacer" style="height: {(rows.length + (shallow && !hasMore && rows.length ? 1 : 0)) * ROW_HEIGHT + lead}px">
```

Right after the `{/if}` closing the canvas block:

```svelte
      {#if lead}
        <button
          class="row uncommitted"
          class:selected={$uncommittedSelected}
          style="top: 0; padding-left: {width}px"
          on:click={selectUncommitted}
          on:contextmenu|preventDefault
        >
          <span class="subject ellipsis">Uncommitted changes ({count})</span>
          <span></span><span></span><span></span>
        </button>
      {/if}
```

Commit rows: `style="top: {(range.start + i) * ROW_HEIGHT + lead}px; padding-left: {width}px"`. Shallow note: `style="top: {rows.length * ROW_HEIGHT + lead}px"`.

Style, beside `.merge .subject`:

```css
  .uncommitted .subject { font-style: italic; color: var(--muted); }
```

The "No commits yet" overlay stays keyed on `rows.length === 0` — a new repository with untracked files shows the row above that message, which is accurate.

- [ ] **Step 6: Verify automated checks**

Run: `cd frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npx vitest run && npm run check && cd .. && go test ./... && make build`
Expected: vitest PASS, svelte-check 0 errors, go tests PASS (untouched), build succeeds.

- [ ] **Step 7: Docs.** In `docs/superpowers/specs/2026-09-21-working-tree-design.md`, replace the scope bullet

```
- **A third mode in the main pane**, beside the log and the merge view, rather
  than a permanent bottom panel or a synthetic row at the top of the log.
```

with

```
- **A third mode in the main pane**, beside the log and the merge view, rather
  than a permanent bottom panel. Since 2026-09-23 a synthetic "Uncommitted
  changes" row at the top of the log opens the same view in the details pane
  as well — the two coexist (see `2026-09-23-uncommitted-row-design.md`).
```

In `docs/superpowers/specs/2026-09-23-uncommitted-row-design.md` set `Status: Implemented (manual pass pending)`.

- [ ] **Step 8: Commit**

```bash
git add frontend/src/components/LogList.svelte docs/superpowers/specs/2026-09-21-working-tree-design.md docs/superpowers/specs/2026-09-23-uncommitted-row-design.md
git commit -m "feat(log): uncommitted changes row with its graph marker"
```

- [ ] **Step 9: Manual checklist for the owner** (report, do not claim done): dirty tree → row with count and dashed dot joined to HEAD; select → Changes pane under the log; stage there and see it in the sidebar's Changes view; commit → row disappears and HEAD is selected; `git add`/`git stash` in the embedded terminal updates the count / moves the selection to HEAD; date ordering with a newer commit on another branch → dot without a line; a merge in progress → MergeView wins; switch repository with the row selected → nothing selected in the new one.
