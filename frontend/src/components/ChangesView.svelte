<script lang="ts">
  import { EventsOn } from '../../wailsjs/runtime/runtime'
  import FileList from './FileList.svelte'
  import { api } from '../lib/api'
  import { discardFile, stageFile, unstageFile } from '../lib/actions'
  import { lineClass } from '../lib/diff'
  import { worktreeSections } from '../lib/worktree'
  import type { FileStatus, WorktreeChangedEvent } from '../lib/types'
  import { busy, loadWorktreeState, worktreeState } from '../lib/stores'
  import { errorMessage } from '../lib/ui'
  import { onDestroy } from 'svelte'

  export let repoId: string

  let selected = ''
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
  // Which section (by object identity) each file came from, so an action can
  // tell a Staged row from an Unstaged row with the same status letter.
  $: fileSection = new Map(sections.flatMap((s) => s.files.map((f) => [f, s.title] as const)))
  // Re-read the open file whenever the worktree state reloads — after a
  // stage/unstage/discard, or a focus that caught a change made in a
  // terminal.
  $: if ($worktreeState) refresh()
  // Keep a selection valid as files are staged, unstaged or discarded
  // underneath it.
  $: if (files.length && !files.some((f) => f.path === selected)) open(files[0])
  $: if (!files.length) selected = ''

  function isStaged(file: FileStatus): boolean {
    return fileSection.get(file) === 'Staged'
  }

  async function open(file: FileStatus) {
    const current = ++request
    selected = file.path
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
  // state changed.
  async function refresh() {
    if (!selected) return
    const file = files.find((f) => f.path === selected)
    if (!file) return
    const current = ++request
    try {
      const diff = await api.getWorktreeDiff(repoId, file.path, isStaged(file))
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
    const discard = { label: 'Discard', run: () => discardFile(repoId, file, staged), danger: true, disabled: !!$busy, title: 'Throw this change away' }
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
      onSelect={(path) => open(files.find((f) => f.path === path)!)}
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
