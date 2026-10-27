<script lang="ts">
  import { onMount } from 'svelte'
  import Icon from './Icon.svelte'
  import { get } from 'svelte/store'
  import { api } from '../lib/api'
  import { dialog } from '../lib/ui'
  import { settingsOpen } from '../lib/stores'
  import { escClosesCard, progressText, updateCardOpen, updateInfo, updatePhase } from '../lib/update'
  import { installUpdate } from '../lib/updateInstall'
  import { BrowserOpenURL } from '../../wailsjs/runtime/runtime'

  // Sits above the toasts (same corner): its bottom follows the toasts' height.
  let lift = 0
  onMount(() => {
    const el = document.querySelector('.toasts')
    if (!el || typeof ResizeObserver === 'undefined') return
    const ro = new ResizeObserver(() => (lift = el.children.length > 0 ? el.getBoundingClientRect().height + 8 : 0))
    ro.observe(el)
    return () => ro.disconnect()
  })

  $: info = $updateInfo
  $: phase = $updatePhase
  $: busy = phase.phase !== 'idle' && phase.phase !== 'error'
  $: status =
    phase.phase === 'download' ? progressText(phase.done, phase.total)
    : phase.phase === 'install' ? 'Installing…'
    : phase.phase === 'ready' ? 'Waiting for your answer…'
    : ''

  const openPage = () => info && BrowserOpenURL(info.pageURL)
  const close = () => updateCardOpen.set(false)
  const cancel = () => void api.cancelUpdate().catch(() => {})
  // Esc belongs to a dialog or Settings when one is open. The state is read in
  // the capture phase, before their own handlers close them.
  let escTaken = false
  const beforeKey = () => (escTaken = get(dialog) !== null || get(settingsOpen))
  const onKey = (e: KeyboardEvent) => {
    if ($updateCardOpen && escClosesCard(e, escTaken)) close()
  }
</script>

<svelte:window on:keydown|capture={beforeKey} on:keydown={onKey} />

{#if $updateCardOpen && info?.newer}
  <section class="card" style:bottom="{38 + lift}px" aria-label="Update available">
    <div class="body">
      <div class="title">CommitTree {info.latest} is available</div>
      <div class="muted">You have {info.current}</div>
      {#if status}<div class="status" role="status">{status}</div>{/if}
      {#if phase.phase === 'error'}<div class="error" role="alert">{phase.error}</div>{/if}
      {#if !info.canInstall && info.reason}<div class="muted reason">{info.reason}</div>{/if}
      <div class="actions">
        {#if info.canInstall}
          <button class="btn primary" disabled={busy} on:click={installUpdate}>Update and restart</button>
        {:else}
          <button class="btn primary" on:click={openPage}>Download</button>
        {/if}
        {#if phase.phase === 'download'}
          <button class="btn" on:click={cancel}>Cancel</button>
        {:else if info.canInstall && phase.phase === 'error'}
          <button class="btn" on:click={openPage}>Open release page</button>
        {:else if info.canInstall && !busy}
          <button class="btn" on:click={openPage}>What's new</button>
        {/if}
      </div>
    </div>
    <button class="icon-btn" title="Close" aria-label="Close" on:click={close}><Icon name="x" size={14} /></button>
  </section>
{/if}

<style>
  .card {
    position: fixed;
    right: 16px;
    z-index: 61;
    display: flex;
    align-items: flex-start;
    gap: 6px;
    max-width: 340px;
    padding: 10px 8px 10px 14px;
    background: var(--surface);
    border: 1px solid var(--border);
    border-left: 4px solid var(--accent);
    border-radius: 10px;
    box-shadow: var(--shadow);
  }
  .body { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 4px; }
  .title { font-weight: 600; }
  .muted { color: var(--muted); font-size: 12px; }
  .reason { overflow-wrap: anywhere; }
  .error { color: var(--danger); font-size: 12px; overflow-wrap: anywhere; -webkit-user-select: text; user-select: text; }
  .actions { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 6px; }
</style>
