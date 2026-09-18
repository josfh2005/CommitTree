<script lang="ts">
  import { EventsOn } from '../../wailsjs/runtime/runtime'
  import Icon from './Icon.svelte'
  import { api } from '../lib/api'
  import { abortMerge, commitMerge, resolveConflicts } from '../lib/actions'
  import { mergeFiles, type MergeFile } from '../lib/merge'
  import { busy, loadMergeState, mergeStarted, mergeState } from '../lib/stores'
  import { errorMessage } from '../lib/ui'
  import { onDestroy } from 'svelte'

  export let repoId: string

  let selected = ''
  let text = ''
  let resolved = false
  let error = ''
  let request = 0

  const off = EventsOn('merge:changed', () => loadMergeState())
  onDestroy(off)

  $: files = mergeFiles($mergeState ?? { merging: false, from: '', into: '', conflicts: [], manual: [] }, $mergeStarted)
  $: pending = ($mergeState?.conflicts.length ?? 0) + ($mergeState?.manual.length ?? 0)
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
      {#each files as f (f.path)}
        <button class="row-item file" class:active={selected === f.path} on:click={() => open(f)}>
          <span class="status s-{f.status}">
            {#if f.status === 'resolved'}<Icon name="check" size={12} />{:else if f.status === 'manual'}!{:else}·{/if}
          </span>
          <span class="ellipsis">{f.path}</span>
        </button>
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
  .file { height: 24px; font-size: 12px; }
  .status { width: 14px; flex: none; font-family: var(--mono); font-weight: 600; color: var(--muted); }
  .s-resolved { color: var(--ok); }
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
