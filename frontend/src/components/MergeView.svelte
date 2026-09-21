<script lang="ts">
  import { EventsOn } from '../../wailsjs/runtime/runtime'
  import Icon from './Icon.svelte'
  import FileList, { rowKey } from './FileList.svelte'
  import { api } from '../lib/api'
  import { abortMerge, commitMerge, resolveConflicts, stageMergeFile, takeMergeSide, unstageMergeFile } from '../lib/actions'
  import { mergeSections, type MergeFile } from '../lib/merge'
  import { lineClass } from '../lib/diff'
  import { nextSelection, type SelectionKey } from '../lib/worktree'
  import { busy, loadMergeState, mergeState } from '../lib/stores'
  import { errorMessage, openMenu } from '../lib/ui'
  import { onDestroy } from 'svelte'

  export let repoId: string

  // `selection` is the continuity anchor: which path (and, to break a tie,
  // which section) the user is looking at. It survives a resolve/stage that
  // moves the file to a different section — see nextSelection. `selected` is
  // just its rowKey(...) projection, for FileList's highlighting/onSelect.
  let selection: SelectionKey | null = null
  let text = ''
  let resolved = false
  let error = ''
  let request = 0

  // The pane follows $mergeState (below), so the handler only reloads it.
  const off = EventsOn('merge:changed', (payload: { repoID: string }) => {
    if (payload?.repoID !== repoId) return
    loadMergeState()
  })
  onDestroy(off)

  $: sections = $mergeState ? mergeSections($mergeState) : []
  $: files = sections.flatMap((s) => s.files)
  // Which section (by object identity) each file came from, so a row's key
  // can be reconstructed from the file alone.
  $: fileSection = new Map(sections.flatMap((s) => s.files.map((f) => [f, s.title] as const)))
  $: pending = ($mergeState?.conflicts.length ?? 0) + ($mergeState?.manual.length ?? 0)
  $: selected = selection ? rowKey(selection.section, selection.path) : ''
  // Re-key the selection onto wherever its path now lives (a resolve/stage
  // moves a file Conflicts → Unstaged → Staged, but it's still the file the
  // user had open) and re-read it — after an agent edit, an action, or a
  // focus that caught a change made in a terminal. This must NOT fall back
  // to the first file just because the path changed section.
  $: if ($mergeState) {
    selection = nextSelection(selection, sections)
    refresh()
  }

  function keyOf(file: MergeFile): string {
    return rowKey(fileSection.get(file) ?? '', file.path)
  }

  async function open(file: MergeFile) {
    const current = ++request
    selection = { section: fileSection.get(file) ?? '', path: file.path }
    text = ''
    error = ''
    try {
      const f = await api.getConflictFile(repoId, file.path)
      if (current !== request) return
      text = f.text
      resolved = f.resolved
    } catch (e) {
      if (current === request) error = errorMessage(e)
    }
  }

  // Re-reads the open file in place — no blank flash — after the merge state
  // changed. Uses `selection.path` directly rather than looking the file up
  // in `files`, since a path that only changed section is still valid.
  async function refresh() {
    if (!selection) return
    const current = ++request
    const path = selection.path
    try {
      const f = await api.getConflictFile(repoId, path)
      if (current !== request) return
      text = f.text
      resolved = f.resolved
      error = ''
    } catch (e) {
      if (current === request) error = errorMessage(e)
    }
  }

  // A Manual file is settled by taking one side whole, after a confirmation:
  // it overwrites the worktree copy, which Unstage does not restore.
  function manualMenu(event: MouseEvent, file: { path: string; status: string }) {
    if (file.status !== 'manual') return
    const into = $mergeState?.into ?? ''
    const from = $mergeState?.from ?? ''
    openMenu(event, [
      { label: `Take ours (${into})`, action: () => takeMergeSide(repoId, file.path, 'ours', into), disabled: !!$busy },
      { label: `Take theirs (${from})`, action: () => takeMergeSide(repoId, file.path, 'theirs', from), disabled: !!$busy },
    ])
  }

  function glyph(status: string): string {
    if (status === 'manual') return '!'
    if (status === 'unstaged') return 'M'
    return '·'
  }

  function actionsFor(file: { path: string; status: string }) {
    if (file.status === 'unstaged') {
      return [{ label: 'Stage', run: () => stageMergeFile(repoId, file.path), disabled: !!$busy, title: 'Add to the merge commit' }]
    }
    if (file.status === 'staged') {
      return [{ label: 'Unstage', run: () => unstageMergeFile(repoId, file.path), disabled: !!$busy, title: 'Take out of the merge commit, keeping the content' }]
    }
    return []
  }
</script>

<div class="merge">
  <header>
    <span class="title">Merging <strong>{$mergeState?.from}</strong> into <strong>{$mergeState?.into}</strong></span>
    <span class="count">{pending} left</span>
    <span class="spacer"></span>
    <button class="btn" disabled={!!$busy || pending === 0} on:click={() => resolveConflicts(repoId)}>
      <Icon name="sparkle" size={14} /> Resolve with AI
    </button>
    <button class="btn" disabled={!!$busy} on:click={() => abortMerge(repoId)}>Abort merge</button>
    <button class="btn primary" disabled={!!$busy || pending > 0} on:click={() => commitMerge(repoId)}>Commit merge</button>
  </header>

  <div class="body">
    <FileList {sections} {selected} onSelect={(key) => { const f = files.find((ff) => keyOf(ff) === key); if (f) open(f) }} actions={actionsFor} onMenu={manualMenu} {glyph} />
    <div class="content mono">
      {#if error}
        <div class="error">{error}</div>
      {:else}
        {#each text.split('\n') as line}
          <div class="line {lineClass(line, resolved)}">{line || ' '}</div>
        {/each}
      {/if}
    </div>
  </div>
</div>

<style>
  .merge { display: flex; flex-direction: column; height: 100%; }
  header { display: flex; align-items: center; gap: 8px; padding: 6px 10px; border-bottom: 1px solid var(--border); flex: none; }
  .title { font-size: 13px; }
  .count { font-size: 12px; color: var(--muted); }
  .spacer { flex: 1; }
  .body { display: grid; grid-template-columns: minmax(260px, 36%) 1fr; flex: 1; min-height: 0; }
  .content { overflow: auto; padding: 8px 0; user-select: text; }
  .line { padding: 0 12px; white-space: pre; line-height: 18px; }
  .marker { background: var(--hover); color: var(--muted); font-weight: 600; }
  .add { background: var(--add-bg); }
  .del { background: var(--del-bg); }
  .hunk { color: var(--accent); }
  .meta { color: var(--faint); }
  .error { padding: 12px; color: var(--danger); white-space: pre-wrap; }
</style>
