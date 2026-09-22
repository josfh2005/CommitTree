<script lang="ts">
  import FileList, { rowKey } from './FileList.svelte'
  import { api } from '../lib/api'
  import { lineClass } from '../lib/diff'
  import { stashFileTitle, stashSections } from '../lib/stash'
  import type { StashEntry, StashFile } from '../lib/types'
  import { errorMessage } from '../lib/ui'

  export let repoId: string
  export let entry: StashEntry

  let files: StashFile[] = []
  let selectedPath = ''
  let text = ''
  let error = ''
  let request = 0

  $: sections = stashSections(files)
  $: selectedFile = files.find((f) => f.path === selectedPath) ?? null
  $: selected = selectedFile ? rowKey(stashFileTitle(selectedFile), selectedFile.path) : ''

  // A different stash selected in the sidebar reloads the whole file list —
  // there is no continuity to preserve across two unrelated stashes, unlike
  // ChangesView's selection surviving a stage/unstage.
  $: loadFiles(repoId, entry.index)

  async function loadFiles(id: string, index: number) {
    const current = ++request
    files = []
    selectedPath = ''
    text = ''
    error = ''
    try {
      const result = await api.getStashFiles(id, index)
      if (current !== request) return
      files = result
      if (files.length) await open(files[0])
    } catch (e) {
      if (current === request) error = errorMessage(e)
    }
  }

  async function open(file: StashFile) {
    const current = ++request
    selectedPath = file.path
    text = ''
    error = ''
    try {
      const diff = await api.getStashFileDiff(repoId, entry.index, file.path)
      if (current !== request) return
      text = diff
    } catch (e) {
      if (current === request) error = errorMessage(e)
    }
  }

  function glyph(status: string): string {
    return status || '·'
  }
</script>

<div class="stash">
  <div class="header">
    <div class="message ellipsis" title={entry.message}>{entry.message}</div>
    <div class="branch ellipsis">from {entry.branch}</div>
  </div>
  <div class="body">
    <FileList
      {sections}
      {selected}
      onSelect={(key) => { const f = files.find((ff) => rowKey(stashFileTitle(ff), ff.path) === key); if (f) open(f) }}
      {glyph}
      emptyMessage="Empty stash."
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
  .stash { display: flex; flex-direction: column; height: 100%; }
  .header { flex: none; padding: 10px 12px; border-bottom: 1px solid var(--border); }
  .message { font-size: 13px; font-weight: 500; }
  .branch { font-size: 11px; color: var(--faint); }
  .ellipsis { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
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
