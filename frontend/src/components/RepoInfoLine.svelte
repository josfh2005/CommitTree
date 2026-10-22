<script lang="ts">
  import { api } from '../lib/api'
  import { collectGarbage } from '../lib/actions'
  import { garbageText, infoLine, objectsText, sizeText, statusText } from '../lib/repoInfo'
  import { busy, logVersion } from '../lib/stores'
  import type { RepoInfo } from '../lib/types'

  /** The line after the repository's path in the log header, and its
   *  popover (docs/spec/01-repositories-and-sidebar.md, "Repository info"). */
  export let repoId: string

  let info: RepoInfo | null = null
  let open = false
  let files: number | null = null
  let filesError = ''
  let button: HTMLButtonElement
  let popover: HTMLDivElement

  // Read on selection and after every operation that refreshes the repository
  // (logVersion); a late answer for another repository is dropped.
  $: load(repoId, $logVersion)
  async function load(id: string, _version: number) {
    try {
      const got = await api.repoInfo(id)
      if (id === repoId) info = got
    } catch {
      if (id === repoId) info = null
    }
  }

  // The files size walks the whole tree, so it is read only when the popover opens.
  async function toggle() {
    open = !open
    if (!open) return
    const id = repoId
    files = null
    filesError = ''
    try {
      const size = await api.worktreeSize(id)
      if (id === repoId) files = size
    } catch (e) {
      if (id === repoId) filesError = 'unavailable'
    }
  }

  function onWindowDown(e: MouseEvent) {
    if (open && !popover?.contains(e.target as Node) && !button?.contains(e.target as Node)) open = false
  }
  function onWindowKey(e: KeyboardEvent) {
    if (open && e.key === 'Escape') {
      e.preventDefault()
      open = false
    }
  }
  $: if (repoId) open = false
</script>

<svelte:window on:mousedown={onWindowDown} on:keydown={onWindowKey} />

{#if info}
  <span class="wrap">
    <button bind:this={button} class="line" aria-haspopup="true" aria-expanded={open} on:click={toggle}>
      · {infoLine(info)}{#if info.gcReason}<span class="gc"> · GC recommended</span>{/if}
    </button>
    {#if open}
      <div class="popover" bind:this={popover} role="dialog" aria-label="Repository">
        <h5>Repository</h5>
        <dl>
          <dt>Size on disk (.git)</dt><dd>{sizeText(info.size)}</dd>
          <dt>Files (working tree)</dt><dd>{files !== null ? sizeText(files) : filesError || '…'}</dd>
          <dt>Objects</dt><dd>{objectsText(info)}</dd>
          <dt>Garbage</dt><dd>{garbageText(info)}</dd>
          <dt>Status</dt><dd class:warn={!!info.gcReason}>{statusText(info)}</dd>
        </dl>
        <div class="actions">
          <button class="btn" class:primary={!!info.gcReason} disabled={!!$busy} on:click={() => { open = false; collectGarbage(repoId) }}>Run garbage collection</button>
        </div>
      </div>
    {/if}
  </span>
{/if}

<style>
  .wrap { position: relative; flex: none; }
  .line { background: none; border: none; padding: 0 0 0 4px; font: inherit; font-size: 12px; color: var(--faint); cursor: pointer; white-space: nowrap; }
  .line:hover { color: var(--muted); text-decoration: underline; }
  .gc, .warn { color: var(--warning); }
  .popover { --wails-draggable: no-drag; position: absolute; top: calc(100% + 6px); left: 0; z-index: 60; min-width: 300px; padding: 10px 12px; background: var(--surface); border: 1px solid var(--border); border-radius: 6px; box-shadow: var(--shadow); font-weight: 400; color: var(--text); cursor: default; }
  h5 { margin: 0 0 8px; font-size: 12px; font-weight: 600; }
  dl { display: grid; grid-template-columns: auto 1fr; gap: 4px 12px; margin: 0; font-size: 12px; }
  dt { color: var(--muted); }
  dd { margin: 0; white-space: nowrap; }
  .actions { display: flex; justify-content: flex-end; margin-top: 10px; }
</style>
