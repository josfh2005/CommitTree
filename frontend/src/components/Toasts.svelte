<script lang="ts">
  import Icon from './Icon.svelte'
  import { copyText, dismissToast, toasts } from '../lib/ui'
</script>

<div class="toasts">
  {#each $toasts as t (t.id)}
    <div class="toast" class:error={t.kind === 'error'}>
      <pre class="message">{t.message}</pre>
      {#if t.kind === 'error'}
        <button class="icon-btn" title="Copy" on:click={() => copyText(t.message)}><Icon name="copy" size={14} /></button>
      {/if}
      <button class="icon-btn" title="Dismiss" on:click={() => dismissToast(t.id)}><Icon name="x" size={14} /></button>
    </div>
  {/each}
</div>

<style>
  .toasts { position: fixed; right: 16px; bottom: 16px; z-index: 60; display: flex; flex-direction: column; gap: 8px; }
  .toast {
    display: flex;
    align-items: flex-start;
    gap: 6px;
    max-width: 440px;
    padding: 10px 8px 10px 14px;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 10px;
    box-shadow: var(--shadow);
  }
  .error { border-left: 3px solid var(--danger); }
  .message { flex: 1; max-height: 200px; overflow: auto; user-select: text; }
</style>
