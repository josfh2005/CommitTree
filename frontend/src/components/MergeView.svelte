<script lang="ts">
  import { EventsOn } from '../../wailsjs/runtime/runtime'
  import Icon from './Icon.svelte'
  import FileList from './FileList.svelte'
  import { api } from '../lib/api'
  import { abortMerge, commitMerge, resolveConflicts, stageMergeFile, takeMergeSide, unstageMergeFile } from '../lib/actions'
  import { mergeSections, type MergeFile } from '../lib/merge'
  import { busy, loadMergeState, mergeState } from '../lib/stores'
  import { errorMessage, openMenu } from '../lib/ui'
  import { onDestroy } from 'svelte'

  export let repoId: string

  let selected = ''
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
  $: pending = ($mergeState?.conflicts.length ?? 0) + ($mergeState?.manual.length ?? 0)
  // Re-read the open file whenever the merge state reloads — after an agent
  // edit, an action, or a focus that caught a change made in a terminal.
  $: if ($mergeState) refresh()
  // Keep a selection valid as the agent resolves files underneath it.
  $: if (files.length && !files.some((f) => f.path === selected)) open(files[0])

  async function open(file: MergeFile) {
    const current = ++request
    selected = file.path
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
  // changed.
  async function refresh() {
    if (!selected) return
    const current = ++request
    const path = selected
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

  function lineClass(line: string): string {
    if (!resolved) {
      if (line.startsWith('<<<<<<<') || line.startsWith('>>>>>>>') || line.startsWith('|||||||') || line.startsWith('=======')) return 'marker'
      return ''
    }
    if (line.startsWith('+++') || line.startsWith('---') || line.startsWith('diff ') || line.startsWith('index ')) return 'meta'
    if (line.startsWith('@@')) return 'hunk'
    if (line.startsWith('+')) return 'add'
    if (line.startsWith('-')) return 'del'
    return ''
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
    <FileList {sections} {selected} onSelect={(path) => open(files.find((f) => f.path === path)!)} actions={actionsFor} onMenu={manualMenu} {glyph} />
    <div class="content mono">
      {#if error}
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
