# Repository Toolbar Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the header's three small remote icons with a labelled toolbar (Commit, Stash · Fetch, Pull, Push · Branch, Merge · Terminal, Finder, Chat) and a searchable Merge branch picker.

**Architecture:** Pure rules in `frontend/src/lib/toolbar.ts` (`toolbarItems`, `mergeCandidates`) and `frontend/src/lib/pick.ts` (`filterPick`), unit-tested; a new `pick` kind in the existing dialog host; `Toolbar.svelte` draws the items and dispatches to existing actions plus two small new ones; `LogView.svelte` gets the taller two-line header. No backend change.

**Tech Stack:** Svelte 5 (legacy `$:` syntax), TypeScript, Vitest.

**Spec:** `docs/superpowers/specs/2026-09-25-repo-toolbar-design.md`

## Global Constraints

- Never add `Co-Authored-By` lines to commits.
- Every behaviour change updates `docs/spec/*.md` in the same commit (Task 4 carries the visible change and its spec text).
- Bash hook blocks commands containing "git-ui" in paths: use relative paths; `/usr/bin/git` if `git` is refused.
- Frontend tests from `frontend/`: `/Users/josfh/.nvm/versions/node/v22.23.1/bin/node node_modules/.bin/vitest run`; type check: same node with `node_modules/.bin/svelte-check` (0 errors; the 2 existing a11y warnings in BlameView/ContextMenu are known).
- qartez hook: call `qartez_impact` before editing `frontend/src/lib/types.ts`, `frontend/src/lib/stores.ts`, `frontend/src/lib/ui.ts`, `frontend/src/lib/actions.ts`.
- Disabled-reason texts, verbatim: "Nothing to commit", "Nothing to stash", "Resolve the conflict first", "Check out a branch first"; while busy the reason is the busy label itself (e.g. "Pushing…").
- Picker texts: title "Merge into <current branch>", placeholder "Search branches…", empty "No branches match", button "Merge"; groups "Local" and "Remote".
- Header ~60 px; icons 20 px; labels hide below a header width of 860 px (container query).

## Review Focus

1. **Detached HEAD** — Merge must be disabled with "Check out a branch first", and Branch must create from the detached commit. Pinned in Task 1 tests (`detached HEAD`).
2. **A remote `HEAD` symref or the current branch in the picker** — neither may be offered. Pinned in Task 2 (`mergeCandidates` tests).
3. **Clicking Commit when the Changes view is not mounted yet** — the focus request must survive until the commit box mounts, and fire once. Pinned by the one-shot store design in Task 3 and checked manually in Task 4.
4. **A stash conflict dismissed with "Done"** — the "Resolve conflicts" button must still appear, and Pull/Push stay disabled (merge.merging is still true). Pinned in Task 1 (`conflict` cases use `merging: true` for kind `stash`).
5. **Narrow window** — labels hide, buttons stay clickable and keep their tooltip. Manual in Task 4.

---

### Task 1: Toolbar rules

**Files:**
- Create: `frontend/src/lib/toolbar.ts`
- Test: `frontend/src/lib/toolbar.test.ts`

**Interfaces:**
- Produces:

```ts
export type ToolbarId = 'commit' | 'stash' | 'fetch' | 'pull' | 'push' | 'branch' | 'merge' | 'terminal' | 'folder' | 'chat'
export type ToolbarGroup = 'work' | 'sync' | 'refs' | 'tools'
export interface ToolbarInput {
  refs: Refs | null
  worktree: WorktreeState | null
  merge: MergeState | null
  busy: string
  remote: AheadBehind | null
  terminalOpen: boolean
  chatOpen: boolean
  platform: string
}
export interface ToolbarItem { id: ToolbarId; label: string; icon: string; group: ToolbarGroup; enabled: boolean; title: string; active: boolean; badge: number }
export function toolbarItems(input: ToolbarInput): ToolbarItem[]
```
`title` is the tooltip: the disabled reason when disabled, otherwise a description.

- [ ] **Step 1: Failing tests** — `frontend/src/lib/toolbar.test.ts`:

```ts
import { describe, expect, it } from 'vitest'
import { toolbarItems, type ToolbarInput } from './toolbar'
import type { MergeState, Refs, WorktreeState } from './types'

const refs = (over: Partial<Refs> = {}): Refs => ({ head: 'main', headHash: 'abc123', detached: false, local: [], remotes: [], tags: [], ...over })
const clean: WorktreeState = { staged: [], unstaged: [], untracked: [], merging: false }
const dirty: WorktreeState = { staged: [], unstaged: [{ path: 'a.txt', status: 'M' }], untracked: [], merging: false }
const conflict = (kind: MergeState['kind']): MergeState => ({ kind, merging: true, from: 'x', into: 'main', conflicts: ['a.txt'], manual: [], staged: [], unstaged: [] })
const input = (over: Partial<ToolbarInput> = {}): ToolbarInput => ({
  refs: refs(), worktree: clean, merge: null, busy: '', remote: null, terminalOpen: false, chatOpen: true, platform: 'darwin', ...over,
})
const item = (over: Partial<ToolbarInput>, id: string) => toolbarItems(input(over)).find((i) => i.id === id)!

describe('toolbarItems', () => {
  it('lists the ten buttons in their groups and order', () => {
    expect(toolbarItems(input()).map((i) => `${i.group}:${i.id}`)).toEqual([
      'work:commit', 'work:stash', 'sync:fetch', 'sync:pull', 'sync:push', 'refs:branch', 'refs:merge', 'tools:terminal', 'tools:folder', 'tools:chat',
    ])
  })

  it('disables Commit and Stash with nothing changed, enables them with changes', () => {
    expect(item({}, 'commit')).toMatchObject({ enabled: false, title: 'Nothing to commit' })
    expect(item({}, 'stash')).toMatchObject({ enabled: false, title: 'Nothing to stash' })
    expect(item({ worktree: dirty }, 'commit').enabled).toBe(true)
    expect(item({ worktree: dirty }, 'stash').enabled).toBe(true)
  })

  it('blocks writes during any conflict, including a dismissed stash conflict', () => {
    for (const kind of ['merge', 'rebase', 'stash'] as const) {
      const over = { worktree: dirty, merge: conflict(kind) }
      for (const id of ['commit', 'stash', 'pull', 'push', 'merge']) {
        expect(item(over, id), `${kind}/${id}`).toMatchObject({ enabled: false, title: 'Resolve the conflict first' })
      }
      expect(item(over, 'fetch').enabled).toBe(true)
      expect(item(over, 'branch').enabled).toBe(true)
    }
  })

  it('shows the busy label as the reason while an operation runs', () => {
    for (const id of ['commit', 'stash', 'fetch', 'pull', 'push', 'branch', 'merge']) {
      expect(item({ worktree: dirty, busy: 'Pushing…' }, id), id).toMatchObject({ enabled: false, title: 'Pushing…' })
    }
    for (const id of ['terminal', 'folder', 'chat']) expect(item({ busy: 'Pushing…' }, id).enabled).toBe(true)
  })

  it('disables Merge on a detached HEAD but keeps Branch', () => {
    const over = { refs: refs({ detached: true, head: 'abc1234' }) }
    expect(item(over, 'merge')).toMatchObject({ enabled: false, title: 'Check out a branch first' })
    expect(item(over, 'branch').enabled).toBe(true)
  })

  it('badges Pull and Push with behind and ahead', () => {
    const over = { remote: { ahead: 1, behind: 2 } }
    expect(item(over, 'pull').badge).toBe(2)
    expect(item(over, 'push').badge).toBe(1)
    expect(item({}, 'pull').badge).toBe(0)
  })

  it('marks Terminal and Chat active while open and says what a click does', () => {
    expect(item({ terminalOpen: true }, 'terminal')).toMatchObject({ active: true, title: 'Hide terminal (⌘J or Ctrl+`)' })
    expect(item({ terminalOpen: false, platform: 'linux' }, 'terminal')).toMatchObject({ active: false, title: 'Show terminal (Ctrl+J or Ctrl+`)' })
    expect(item({ chatOpen: true }, 'chat')).toMatchObject({ active: true, title: 'Hide chat' })
    expect(item({ chatOpen: false }, 'chat')).toMatchObject({ active: false, title: 'Show chat' })
  })

  it('names the folder button after the platform', () => {
    expect(item({ platform: 'darwin' }, 'folder')).toMatchObject({ label: 'Finder', title: 'Show in Finder' })
    expect(item({ platform: 'linux' }, 'folder').label).toBe('Folder')
  })
})
```

- [ ] **Step 2: Run** vitest on `src/lib/toolbar.test.ts` → FAIL (module not found).

- [ ] **Step 3: Implement** `frontend/src/lib/toolbar.ts`:

```ts
import { revealLabel } from './platform'
import { terminalShortcutLabel } from './terminal'
import type { AheadBehind, MergeState, Refs, WorktreeState } from './types'
import { uncommittedCount } from './uncommitted'

export type ToolbarId = 'commit' | 'stash' | 'fetch' | 'pull' | 'push' | 'branch' | 'merge' | 'terminal' | 'folder' | 'chat'
export type ToolbarGroup = 'work' | 'sync' | 'refs' | 'tools'

export interface ToolbarInput {
  refs: Refs | null
  worktree: WorktreeState | null
  merge: MergeState | null
  busy: string
  remote: AheadBehind | null
  terminalOpen: boolean
  chatOpen: boolean
  platform: string
}

// title is the tooltip: why the button is disabled, or what it does.
export interface ToolbarItem { id: ToolbarId; label: string; icon: string; group: ToolbarGroup; enabled: boolean; title: string; active: boolean; badge: number }

const CONFLICT = 'Resolve the conflict first'

/** toolbarItems is the repository toolbar: every button with whether it can
 *  run now and, when not, why. A conflict of any kind blocks what writes to
 *  the working tree or moves the branch; Fetch and Branch stay available. */
export function toolbarItems(i: ToolbarInput): ToolbarItem[] {
  const conflict = !!i.merge?.merging
  const changes = uncommittedCount(i.worktree) > 0
  // first returns the first reason that applies, or '' when none does.
  const first = (...rules: [boolean, string][]) => rules.find(([when]) => when)?.[1] ?? ''
  const item = (id: ToolbarId, label: string, icon: string, group: ToolbarGroup, reason: string, title: string, extra: Partial<ToolbarItem> = {}): ToolbarItem =>
    ({ id, label, icon, group, enabled: reason === '', title: reason || title, active: false, badge: 0, ...extra })
  const shortcut = `${terminalShortcutLabel(i.platform)} or Ctrl+\``
  return [
    item('commit', 'Commit', 'commit', 'work', first([!!i.busy, i.busy], [conflict, CONFLICT], [!changes, 'Nothing to commit']), 'Commit the changes'),
    item('stash', 'Stash', 'stash', 'work', first([!!i.busy, i.busy], [conflict, CONFLICT], [!changes, 'Nothing to stash']), 'Stash the changes'),
    item('fetch', 'Fetch', 'refresh', 'sync', first([!!i.busy, i.busy]), 'Fetch from all remotes'),
    item('pull', 'Pull', 'download', 'sync', first([!!i.busy, i.busy], [conflict, CONFLICT]), 'Pull', { badge: i.remote?.behind ?? 0 }),
    item('push', 'Push', 'upload', 'sync', first([!!i.busy, i.busy], [conflict, CONFLICT]), 'Push', { badge: i.remote?.ahead ?? 0 }),
    item('branch', 'Branch', 'branch', 'refs', first([!!i.busy, i.busy]), 'New branch from HEAD'),
    item('merge', 'Merge', 'merge', 'refs', first([!!i.busy, i.busy], [conflict, CONFLICT], [!!i.refs?.detached, 'Check out a branch first']), 'Merge a branch into the current one'),
    item('terminal', 'Terminal', 'terminal', 'tools', '', `${i.terminalOpen ? 'Hide' : 'Show'} terminal (${shortcut})`, { active: i.terminalOpen }),
    item('folder', i.platform === 'darwin' ? 'Finder' : 'Folder', 'folder', 'tools', '', revealLabel(i.platform)),
    item('chat', 'Chat', 'chat', 'tools', '', i.chatOpen ? 'Hide chat' : 'Show chat', { active: i.chatOpen }),
  ]
}
```
(Check `revealLabel`'s non-darwin text in `lib/platform.ts`; the test only pins the darwin one.)

- [ ] **Step 4: Run** vitest (whole suite) → PASS; svelte-check → 0 errors.

- [ ] **Step 5: Commit**

```bash
/usr/bin/git add frontend/src/lib/toolbar.ts frontend/src/lib/toolbar.test.ts
/usr/bin/git commit -m "feat(toolbar): rules for the repository toolbar buttons"
```

---

### Task 2: Branch picker dialog

**Files:**
- Create: `frontend/src/lib/pick.ts`, `frontend/src/lib/pick.test.ts`
- Modify: `frontend/src/lib/toolbar.ts` (add `mergeCandidates`), `frontend/src/lib/toolbar.test.ts`
- Modify: `frontend/src/lib/ui.ts` (dialog kind + `pickDialog`)
- Modify: `frontend/src/components/DialogHost.svelte` (render `pick`)

**Interfaces:**
- Produces:

```ts
// pick.ts
export interface PickItem { key: string; label: string; group?: string }
export function filterPick<T extends PickItem>(items: T[], query: string): T[]
// toolbar.ts
export interface MergeCandidate extends PickItem { group: 'Local' | 'Remote'; branch: Branch }
export function mergeCandidates(refs: Refs | null): MergeCandidate[]
// ui.ts
export interface PickOptions { title: string; placeholder: string; empty: string; submitLabel: string; items: PickItem[] }
export const pickDialog: (options: PickOptions) => Promise<string | null> // the chosen key
```

- [ ] **Step 1: Failing tests.** `frontend/src/lib/pick.test.ts`:

```ts
import { describe, expect, it } from 'vitest'
import { filterPick } from './pick'

const items = [{ key: 'a', label: 'feature/login' }, { key: 'b', label: 'fix/typo' }, { key: 'c', label: 'origin/develop' }]

describe('filterPick', () => {
  it('keeps everything for an empty query', () => {
    expect(filterPick(items, '  ')).toEqual(items)
  })
  it('matches any part of the label, ignoring case', () => {
    expect(filterPick(items, 'LOG').map((i) => i.key)).toEqual(['a'])
    expect(filterPick(items, 'o').map((i) => i.key)).toEqual(['a', 'b', 'c'])
  })
  it('gives nothing when nothing matches', () => {
    expect(filterPick(items, 'zzz')).toEqual([])
  })
})
```

Append to `toolbar.test.ts` (and import `mergeCandidates` there):

```ts
describe('mergeCandidates', () => {
  const b = (name: string, remote = '', current = false) => ({ name, remote, hash: 'h', current, upstream: '' })
  it('offers local branches then remote ones, never the current branch or a remote HEAD', () => {
    const r = refs({
      local: [b('main', '', true), b('feature/login'), b('fix/typo')],
      remotes: [{ name: 'origin', branches: [b('HEAD', 'origin'), b('main', 'origin'), b('develop', 'origin')] }],
    })
    expect(mergeCandidates(r).map((c) => `${c.group}:${c.label}`)).toEqual([
      'Local:feature/login', 'Local:fix/typo', 'Remote:origin/main', 'Remote:origin/develop',
    ])
    expect(mergeCandidates(r)[2].branch).toMatchObject({ name: 'main', remote: 'origin' })
  })
  it('is empty without refs', () => {
    expect(mergeCandidates(null)).toEqual([])
  })
})
```
(If `Branch` has other required fields in `types.ts`, add them to `b`.)

- [ ] **Step 2: Run** vitest → FAIL (`filterPick`/`mergeCandidates` missing).

- [ ] **Step 3: Implement.** `frontend/src/lib/pick.ts`:

```ts
export interface PickItem { key: string; label: string; group?: string }

/** filterPick keeps the items whose label contains query, ignoring case;
 *  an empty query keeps them all. */
export function filterPick<T extends PickItem>(items: T[], query: string): T[] {
  const q = query.trim().toLowerCase()
  return q ? items.filter((i) => i.label.toLowerCase().includes(q)) : items
}
```

In `toolbar.ts` (import `Branch` and `PickItem`):

```ts
export interface MergeCandidate extends PickItem { group: 'Local' | 'Remote'; branch: Branch }

/** mergeCandidates is what the Merge picker offers: local branches, then
 *  remote-tracking ones as remote/name, leaving out the current branch and
 *  a remote's HEAD symref. */
export function mergeCandidates(refs: Refs | null): MergeCandidate[] {
  if (!refs) return []
  const local = refs.local.filter((b) => !b.current).map((b): MergeCandidate => ({ key: b.name, label: b.name, group: 'Local', branch: b }))
  const remote = refs.remotes.flatMap((r) =>
    r.branches.filter((b) => b.name !== 'HEAD').map((b): MergeCandidate => ({ key: `${r.name}/${b.name}`, label: `${r.name}/${b.name}`, group: 'Remote', branch: b })),
  )
  return [...local, ...remote]
}
```

In `ui.ts`: add `import type { PickItem } from './pick'`, and

```ts
/** A searchable list; resolves to the chosen item's key, or null. */
export interface PickOptions { title: string; placeholder: string; empty: string; submitLabel: string; items: PickItem[] }
```
a `| (PickOptions & { kind: 'pick'; resolve: (key: string | null) => void })` member in `Dialog`, and

```ts
export const pickDialog = (options: PickOptions) =>
  new Promise<string | null>((resolve) => dialog.set({ ...options, kind: 'pick', resolve }))
```

In `DialogHost.svelte`:
  - script: `import { filterPick } from '../lib/pick'`; state `let query = ''` and `let index = 0`; `$: if ($dialog?.kind === 'pick') { query = ''; index = 0 }`; `$: shown = $dialog?.kind === 'pick' ? filterPick($dialog.items, query) : []`.
  - `submitLabel` expression: add the `pick` case (`$dialog.submitLabel`).
  - `finish`: add `else if (current.kind === 'pick') current.resolve(ok && shown[index] ? shown[index].key : null)` before the final `else`.
  - a key handler:

```ts
  function pickKey(e: KeyboardEvent) {
    if (e.key === 'ArrowDown') index = Math.min(index + 1, shown.length - 1)
    else if (e.key === 'ArrowUp') index = Math.max(index - 1, 0)
    else return
    e.preventDefault()
  }
```
  - markup, a new branch before `{:else}` (the prompt branch):

```svelte
      {:else if $dialog.kind === 'pick'}
        <input class="search" placeholder={$dialog.placeholder} bind:value={query} on:input={() => (index = 0)} on:keydown={pickKey} use:focus />
        <div class="pick-list" role="listbox">
          {#each shown as item, i (item.key)}
            {#if item.group && item.group !== shown[i - 1]?.group}<div class="pick-group">{item.group}</div>{/if}
            <button type="button" class="pick-item" class:selected={i === index} role="option" aria-selected={i === index} on:click={() => (index = i)} on:dblclick={() => finish(true)}>{item.label}</button>
          {:else}
            <p class="pick-empty">{$dialog.empty}</p>
          {/each}
        </div>
```
  - the submit button gets `disabled={$dialog.kind === 'pick' && shown.length === 0}`.
  - styles:

```css
  .search { font-size: 13px; }
  .pick-list { max-height: 260px; overflow-y: auto; display: flex; flex-direction: column; border: 1px solid var(--border); border-radius: 8px; padding: 4px; }
  .pick-group { padding: 6px 8px 2px; font-size: 11px; color: var(--faint); }
  .pick-item { text-align: left; padding: 5px 8px; border-radius: 6px; }
  .pick-item:hover { background: var(--hover); }
  .pick-item.selected { background: var(--active); }
  .pick-empty { padding: 8px; font-size: 12px; }
```
  - scroll the selected item into view: `$: if ($dialog?.kind === 'pick') tick().then(() => document.querySelector('.pick-item.selected')?.scrollIntoView({ block: 'nearest' }))` referencing `index` (import `tick` from svelte), so arrowing past the fold keeps it visible.

- [ ] **Step 4: Run** vitest → PASS; svelte-check → 0 errors (fix any narrowing errors in `finish`/`submitLabel` with the new kind).

- [ ] **Step 5: Commit**

```bash
/usr/bin/git add frontend/src/lib/pick.ts frontend/src/lib/pick.test.ts frontend/src/lib/toolbar.ts frontend/src/lib/toolbar.test.ts frontend/src/lib/ui.ts frontend/src/components/DialogHost.svelte
/usr/bin/git commit -m "feat(ui): searchable pick dialog and the Merge candidates"
```

---

### Task 3: Actions and commit-box focus

**Files:**
- Modify: `frontend/src/lib/stores.ts` (one-shot focus flag)
- Modify: `frontend/src/lib/actions.ts` (`startCommit`, `pickAndMerge`)
- Modify: `frontend/src/components/CommitBox.svelte`
- Test: `frontend/src/lib/stores.test.ts` (or a new `toolbarActions.test.ts` if stores tests are awkward)

**Interfaces:**
- Consumes: `mergeCandidates` (Task 2), `pickDialog` (Task 2), existing `mergeBranch(id, branch, into)`, `selectUncommitted()`.
- Produces: `export const focusCommitBox = writable(false)` (stores.ts); `export function startCommit(): void`; `export async function pickAndMerge(id: string): Promise<void>` (actions.ts).

- [ ] **Step 1: Failing test** — append to `frontend/src/lib/stores.test.ts` (check its existing imports/`get` usage first):

```ts
describe('startCommit', () => {
  it('selects the uncommitted row and asks the commit box for focus', async () => {
    const { startCommit } = await import('./actions')
    selectedHash.set('abc')
    startCommit()
    expect(get(uncommittedSelected)).toBe(true)
    expect(get(selectedHash)).toBe('')
    expect(get(focusCommitBox)).toBe(true)
  })
})
```
(import `focusCommitBox`, `selectedHash`, `uncommittedSelected` from `./stores` and `get` from `svelte/store` if not already.) If importing `./actions` in this test pulls Wails bindings that break under vitest, put the test in `frontend/src/lib/actions.test.ts` where actions are already tested and follow its mocking.

- [ ] **Step 2: Run** vitest → FAIL.

- [ ] **Step 3: Implement.**
  - `stores.ts`, next to `uncommittedSelected`:

```ts
/** Set by the toolbar's Commit; the commit box focuses its message once it
 *  is on screen and clears the flag, so the request survives the Changes
 *  view mounting and fires once. */
export const focusCommitBox = writable(false)
```
  - `actions.ts`:

```ts
/** The toolbar's Commit: open the Changes view on the uncommitted row and
 *  put the cursor in the commit message. */
export function startCommit() {
  selectUncommitted()
  focusCommitBox.set(true)
}

/** The toolbar's Merge: pick a branch, then the usual merge confirmation. */
export async function pickAndMerge(id: string) {
  const current = get(refs)
  if (!current || current.detached) return
  const candidates = mergeCandidates(current)
  const key = await pickDialog({
    title: `Merge into ${current.head}`,
    placeholder: 'Search branches…',
    empty: 'No branches match',
    submitLabel: 'Merge',
    items: candidates,
  })
  const chosen = candidates.find((c) => c.key === key)
  if (chosen) await mergeBranch(id, chosen.branch, current.head)
}
```
  (import `get` from `svelte/store`, `refs`, `focusCommitBox`, `selectUncommitted` from `./stores`, `mergeCandidates` from `./toolbar`, `pickDialog` from `./ui` — check which are already imported.)
  - `CommitBox.svelte`: `let box: HTMLTextAreaElement`; `bind:this={box}` on the textarea; import `focusCommitBox`; and

```ts
  $: if ($focusCommitBox && box) {
    box.focus()
    focusCommitBox.set(false)
  }
```

- [ ] **Step 4: Run** vitest → PASS; svelte-check → 0 errors.

- [ ] **Step 5: Commit**

```bash
/usr/bin/git add frontend/src/lib/stores.ts frontend/src/lib/actions.ts frontend/src/components/CommitBox.svelte frontend/src/lib/stores.test.ts
/usr/bin/git commit -m "feat(toolbar): Commit focuses the message box; Merge picks a branch first"
```

---

### Task 4: Icons, toolbar, header and spec

**Files:**
- Modify: `frontend/src/components/Icon.svelte`
- Rewrite: `frontend/src/components/Toolbar.svelte`
- Modify: `frontend/src/components/LogView.svelte`
- Modify: `docs/spec/05-remote-and-stash.md`, `docs/spec/01-repositories-and-sidebar.md`, `docs/spec/08-terminal.md`, `docs/spec/06-ai.md`

**Interfaces:**
- Consumes: `toolbarItems`, `ToolbarId` (Task 1); `startCommit`, `pickAndMerge` (Task 3); existing `stashChanges`, `fetchRemote`, `pull`, `push`, `newBranch`, `openRepoFolder`.

- [ ] **Step 1: Icons** — add to `paths` in `Icon.svelte` (16×16 grid, drawn at 20 px):

```ts
    commit: 'M10.5 8a2.5 2.5 0 1 1-5 0 2.5 2.5 0 0 1 5 0ZM1.5 8h4M10.5 8h4',
    stash: 'M2.5 6.5h11v7h-11zM3.5 3.5h9l1 3h-11zM6 9.5h4',
    branch: 'M5 2.5v11M12.5 4.5a1.5 1.5 0 1 1-3 0 1.5 1.5 0 0 1 3 0ZM11 6c0 3-6 2.5-6 5.5',
    merge: 'M5 2.5v11M12.5 11.5a1.5 1.5 0 1 1-3 0 1.5 1.5 0 0 1 3 0ZM5 4.5c0 3 6 2.5 6 5.5',
    folder: 'M2 4.5h4l1.5 1.5H14v7H2z',
    chat: 'M2.5 3.5h11v7h-6l-3 2.5v-2.5h-2z',
```

- [ ] **Step 2: `Toolbar.svelte`** — replace the component:

```svelte
<script lang="ts">
  import Icon from './Icon.svelte'
  import { fetchRemote, newBranch, openRepoFolder, pickAndMerge, pull, push, startCommit, stashChanges } from '../lib/actions'
  import { toolbarItems, type ToolbarGroup, type ToolbarId } from '../lib/toolbar'
  import { busy, chatOpen, mergeState, platform, refs, remoteInfo, stashConflictDismissed, terminalOpen, worktreeState } from '../lib/stores'

  export let repoId: string

  $: items = toolbarItems({ refs: $refs, worktree: $worktreeState, merge: $mergeState, busy: $busy, remote: $remoteInfo, terminalOpen: $terminalOpen, chatOpen: $chatOpen, platform: $platform })
  const GROUPS: ToolbarGroup[] = ['work', 'sync', 'refs', 'tools']

  function act(id: ToolbarId) {
    switch (id) {
      case 'commit': return startCommit()
      case 'stash': return stashChanges(repoId)
      case 'fetch': return fetchRemote(repoId)
      case 'pull': return pull(repoId)
      case 'push': return push(repoId)
      case 'branch': return newBranch(repoId, $refs?.headHash ?? 'HEAD', $refs?.head ?? 'HEAD')
      case 'merge': return pickAndMerge(repoId)
      case 'terminal': return terminalOpen.update((open) => !open)
      case 'folder': return openRepoFolder(repoId)
      case 'chat': return chatOpen.update((open) => !open)
    }
  }
</script>

<div class="toolbar">
  <!-- The only way back into a stash conflict the user dismissed with
       "Done": without it the conflict view would be unreachable until the
       files happen to resolve. -->
  {#if $mergeState?.kind === 'stash' && $stashConflictDismissed}
    <button class="btn" on:click={() => stashConflictDismissed.set(false)}>Resolve conflicts</button>
  {/if}
  {#each GROUPS as group, g}
    {#if g > 0}<span class="sep"></span>{/if}
    {#each items.filter((i) => i.group === group) as item (item.id)}
      <button class="tool" class:active={item.active} title={item.title} aria-label={item.label} disabled={!item.enabled} on:click={() => act(item.id)}>
        <span class="icon"><Icon name={item.icon} size={20} />{#if item.badge}<span class="badge">{item.badge}</span>{/if}</span>
        <span class="label">{item.label}</span>
      </button>
    {/each}
  {/each}
</div>

<style>
  .toolbar { display: flex; align-items: center; gap: 2px; flex: none; }
  .tool { display: flex; flex-direction: column; align-items: center; gap: 2px; min-width: 50px; padding: 4px 6px; border-radius: 8px; color: var(--muted); font-size: 11px; }
  .tool:hover:not(:disabled) { background: var(--hover); color: var(--text); }
  .tool:disabled { opacity: 0.4; }
  .tool.active { color: var(--accent); }
  .icon { position: relative; display: inline-flex; }
  .badge { position: absolute; top: -4px; right: -8px; font-size: 9px; line-height: 1; padding: 1px 3px; border-radius: 6px; background: var(--accent); color: white; }
  .sep { width: 1px; height: 28px; margin: 0 6px; background: var(--border); }
  /* The header is the container (LogView). Too narrow for labelled buttons:
     icons only, the name stays in the tooltip. */
  @container repo-header (max-width: 860px) {
    .label { display: none; }
    .tool { min-width: 32px; }
  }
</style>
```
(`var(--accent)`, `--hover`, `--muted`, `--border` exist in `theme.css`. Keep `.btn` for Resolve conflicts, which is global.)

- [ ] **Step 3: `LogView.svelte` header** —
  - remove the Terminal button and the "Show chat" button blocks (and now-unused imports: `Icon` if unused, `terminalShortcutLabel`, `platform`, `chatOpen`, `terminalOpen` only if unused elsewhere in the file);
  - make the title two lines: name on the first line, path under it (`.title { flex-direction: column; align-items: flex-start; gap: 0 }`, keep `.path` and the submodule breadcrumb);
  - `header { height: 60px; container-type: inline-size; container-name: repo-header; }`.
  - Keep `class="drag"` on the header; verify in the app that the toolbar buttons still receive clicks (the old buttons did in the same header).

- [ ] **Step 4: Spec.**
  - `docs/spec/05-remote-and-stash.md`: replace the "The toolbar offers three remote actions…" paragraph and table, and the badges paragraph, with the whole toolbar: the ten buttons in their groups, the table from the design spec's Behaviour section (disabled rules and the verbatim reasons), the narrow-window rule, the active state of Terminal/Chat, and a "Merge branch picker" subsection (list, search, keys, then the existing confirmation). Keep the Fetch/Pull/Push subsections below as they are.
  - `docs/spec/01-repositories-and-sidebar.md`: next to the branch menu's "Merge `<branch>` into `<head>`" row, note the toolbar's Merge picker offers the same merge for any local or remote branch.
  - `docs/spec/08-terminal.md` (around line 53, the header "Terminal" button): the button is now the toolbar's Terminal toggle, highlighted while open; shortcuts unchanged.
  - `docs/spec/06-ai.md`: where the chat panel is shown/hidden, say the toolbar's Chat toggle does it (find the current wording with grep for "Show chat"/"Hide chat").

- [ ] **Step 5: Verify** — vitest PASS, svelte-check 0 errors, `go test ./internal/...` PASS (untouched, sanity).

- [ ] **Step 6: Manual check in the app** (after the owner-approved merge, `make dev` from the main checkout): every button's action; disabled states and tooltips (clean repo, with changes, during a merge conflict, detached HEAD); Merge picker keyboard (type, ↑↓, Enter, Escape, double-click, no match); Commit focuses the message; narrow the window until labels hide; light, dark and high-contrast themes; window still drags by the header.

- [ ] **Step 7: Commit**

```bash
/usr/bin/git add frontend/src/components/Icon.svelte frontend/src/components/Toolbar.svelte frontend/src/components/LogView.svelte docs/spec
/usr/bin/git commit -m "feat(toolbar): labelled repository toolbar with Commit, Stash, Branch, Merge and tool toggles"
```
