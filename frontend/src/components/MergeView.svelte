<script lang="ts">
  import { EventsOn } from '../../wailsjs/runtime/runtime'
  import Icon from './Icon.svelte'
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
  function manualMenu(event: MouseEvent, file: MergeFile) {
    if (file.status !== 'manual') return
    const into = $mergeState?.into ?? ''
    const from = $mergeState?.from ?? ''
    openMenu(event, [
      { label: `Take ours (${into})`, action: () => takeMergeSide(repoId, file.path, 'ours', into), disabled: !!$busy },
      { label: `Take theirs (${from})`, action: () => takeMergeSide(repoId, file.path, 'theirs', from), disabled: !!$busy },
    ])
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
    <div class="files">
      {#each sections as section (section.title)}
        <div class="section">{section.title}</div>
        {#each section.files as f (f.path)}
          <div class="row" class:active={selected === f.path}>
            <button class="row-item file" class:active={selected === f.path} on:click={() => open(f)} on:contextmenu|preventDefault={(e) => manualMenu(e, f)}>
              <span class="status s-{f.status}">
                {#if f.status === 'staged'}<Icon name="check" size={12} />{:else if f.status === 'manual'}!{:else if f.status === 'unstaged'}M{:else}·{/if}
              </span>
              <span class="ellipsis">{f.path}</span>
            </button>
            {#if f.status === 'unstaged'}
              <button class="act" disabled={!!$busy} title="Add to the merge commit" on:click={() => stageMergeFile(repoId, f.path)}>Stage</button>
            {:else if f.status === 'staged'}
              <button class="act" disabled={!!$busy} title="Take out of the merge commit, keeping the content" on:click={() => unstageMergeFile(repoId, f.path)}>Unstage</button>
            {/if}
          </div>
        {/each}
      {:else}
        <div class="none">Nothing left to resolve.</div>
      {/each}
    </div>
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
  .files { overflow-y: auto; padding: 6px 8px; border-right: 1px solid var(--border); }
  .section { padding: 8px 10px 2px; font-size: 11px; font-weight: 600; color: var(--faint); }
  .section:first-child { padding-top: 2px; }
  .row { position: relative; }
  .file { height: 24px; font-size: 12px; }
  .row .file { padding-right: 64px; }
  .act { position: absolute; right: 4px; top: 2px; height: 20px; padding: 0 8px; font-size: 11px; border-radius: 6px; border: 1px solid var(--border); background: var(--surface); color: var(--text); visibility: hidden; }
  .row:hover .act, .row.active .act, .act:focus-visible { visibility: visible; }
  .act:hover:not(:disabled) { background: var(--hover); }
  .status { width: 14px; flex: none; font-family: var(--mono); font-weight: 600; color: var(--muted); }
  .s-staged { color: var(--ok); }
  .s-manual { color: var(--danger); }
  .none { padding: 4px 10px; color: var(--faint); font-size: 12px; }
  .content { overflow: auto; padding: 8px 0; user-select: text; }
  .line { padding: 0 12px; white-space: pre; line-height: 18px; }
  .marker { background: var(--hover); color: var(--muted); font-weight: 600; }
  .add { background: var(--add-bg); }
  .del { background: var(--del-bg); }
  .hunk { color: var(--accent); }
  .meta { color: var(--faint); }
  .error { padding: 12px; color: var(--danger); white-space: pre-wrap; }
</style>
