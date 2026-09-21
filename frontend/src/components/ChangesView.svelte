<script lang="ts">
  import { EventsOn } from '../../wailsjs/runtime/runtime'
  import FileList, { rowKey } from './FileList.svelte'
  import { api } from '../lib/api'
  import { discardFile, stageFile, unstageFile } from '../lib/actions'
  import { lineClass } from '../lib/diff'
  import { hasStagedChanges, nextSelection, worktreeSections, type SelectionKey } from '../lib/worktree'
  import type { FileStatus, WorktreeChangedEvent } from '../lib/types'
  import { busy, loadWorktreeState, worktreeState } from '../lib/stores'
  import { errorMessage } from '../lib/ui'
  import { onDestroy } from 'svelte'

  export let repoId: string

  // `selection` is the continuity anchor: which path (and, to break a tie,
  // which section) the user is looking at. It survives a stage/unstage that
  // moves the file to a different section — see nextSelection. `selected` is
  // just its rowKey(...) projection, for FileList's highlighting/onSelect.
  let selection: SelectionKey | null = null
  let text = ''
  let error = ''
  let request = 0

  // The pane follows $worktreeState (below), so the handler only reloads it.
  const off = EventsOn('worktree:changed', (payload: WorktreeChangedEvent) => {
    if (payload?.repoID !== repoId) return
    loadWorktreeState()
  })
  onDestroy(off)

  $: sections = $worktreeState ? worktreeSections($worktreeState) : []
  $: files = sections.flatMap((s) => s.files)
  // Which section (by object identity) each file came from. A partially
  // staged file (git status "MM") lists the SAME path under both Staged and
  // Unstaged as two distinct FileStatus objects — this map, keyed by object
  // identity rather than path, is what lets a row be told apart from its
  // same-path counterpart in the other section.
  $: fileSection = new Map(sections.flatMap((s) => s.files.map((f) => [f, s.title] as const)))
  $: selected = selection ? rowKey(selection.section, selection.path) : ''
  // Re-key the selection onto wherever its path now lives (staging or
  // unstaging moves a file to a different section, but it's still the file
  // the user had open) and re-read it — after a stage/unstage/discard, or a
  // focus that caught a change made in a terminal. This must NOT fall back
  // to the first file just because the path changed section.
  $: if ($worktreeState) {
    selection = nextSelection(selection, sections)
    refresh()
  }

  function isStaged(file: FileStatus): boolean {
    return fileSection.get(file) === 'Staged'
  }

  // rowKey(section, path) — the same identity scheme FileList uses — so a
  // row can be resolved back from the value onSelect hands us without
  // falling back to "the first file with this path", which for a partially
  // staged file always finds the Staged instance.
  function keyOf(file: FileStatus): string {
    return rowKey(fileSection.get(file) ?? '', file.path)
  }

  async function open(file: FileStatus) {
    const current = ++request
    selection = { section: fileSection.get(file) ?? '', path: file.path }
    text = ''
    error = ''
    try {
      const diff = await api.getWorktreeDiff(repoId, file.path, isStaged(file))
      if (current !== request) return
      text = diff
    } catch (e) {
      if (current === request) error = errorMessage(e)
    }
  }

  // Re-reads the open file in place — no blank flash — after the worktree
  // state changed. Uses `selection` directly (path AND its current section)
  // rather than looking the file up in `files`, since a path that only
  // changed section is still valid, and the diff must follow that new
  // section (unstaged vs staged) rather than a stale one.
  async function refresh() {
    if (!selection) return
    const current = ++request
    const path = selection.path
    const staged = selection.section === 'Staged'
    try {
      const diff = await api.getWorktreeDiff(repoId, path, staged)
      if (current !== request) return
      text = diff
      error = ''
    } catch (e) {
      if (current === request) error = errorMessage(e)
    }
  }

  function glyph(status: string): string {
    return status || '·'
  }

  function actionsFor(file: FileStatus) {
    const staged = isStaged(file)
    // The discard warning must reflect whether the PATH has staged content
    // anywhere, not which section this particular row came from: `git
    // restore --staged --worktree` throws both away together regardless of
    // which row (Staged or Unstaged) of a partially staged file triggered it.
    const alsoStaged = $worktreeState ? hasStagedChanges($worktreeState, file.path) : staged
    const discard = { label: 'Discard', run: () => discardFile(repoId, file, alsoStaged), danger: true, disabled: !!$busy, title: 'Throw this change away' }
    if (staged) {
      return [{ label: 'Unstage', run: () => unstageFile(repoId, file.path), disabled: !!$busy, title: 'Take out of the next commit' }, discard]
    }
    return [{ label: 'Stage', run: () => stageFile(repoId, file.path), disabled: !!$busy, title: 'Add to the next commit' }, discard]
  }
</script>

<div class="changes">
  <div class="body">
    <FileList
      {sections}
      {selected}
      onSelect={(key) => { const f = files.find((ff) => keyOf(ff) === key); if (f) open(f) }}
      actions={actionsFor}
      {glyph}
      emptyMessage="No changes."
    />
    <div class="content mono">
      {#if !files.length}
        <div class="empty">Nothing to show.</div>
      {:else if error}
        <div class="error">{error}</div>
      {:else}
        {#each text.split('\n') as line}
          <div class="line {lineClass(line)}">{line || ' '}</div>
        {/each}
      {/if}
    </div>
  </div>
</div>

<style>
  .changes { display: flex; flex-direction: column; height: 100%; }
  .body { display: grid; grid-template-columns: minmax(260px, 36%) 1fr; flex: 1; min-height: 0; }
  .content { overflow: auto; padding: 8px 0; user-select: text; }
  .line { padding: 0 12px; white-space: pre; line-height: 18px; }
  .add { background: var(--add-bg); }
  .del { background: var(--del-bg); }
  .hunk { color: var(--accent); }
  .meta { color: var(--faint); }
  .error { padding: 12px; color: var(--danger); white-space: pre-wrap; }
  .empty { padding: 12px; color: var(--faint); }
</style>
