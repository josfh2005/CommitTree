<script lang="ts">
  import { dialog } from '../lib/ui'

  let value = ''
  let second = ''
  let checked = false

  $: if ($dialog?.kind === 'prompt') {
    value = $dialog.value ?? ''
    second = ''
    checked = $dialog.checked ?? false
  }

  function finish(ok: boolean) {
    const current = $dialog
    if (!current) return
    dialog.set(null)
    if (current.kind === 'confirm') current.resolve(ok)
    else current.resolve(ok ? { value, second, checked } : null)
  }

  function focus(node: HTMLElement, enabled = true) {
    if (enabled) setTimeout(() => node.focus())
  }
</script>

<svelte:window on:keydown={(e) => $dialog && e.key === 'Escape' && finish(false)} />

{#if $dialog}
  <div class="backdrop" on:click|self={() => finish(false)} role="presentation">
    <form class="dialog" on:submit|preventDefault={() => finish(true)}>
      <h3>{$dialog.title}</h3>
      {#if $dialog.kind === 'confirm'}
        <p>{$dialog.message}</p>
      {:else}
        <label>
          <span>{$dialog.label}</span>
          <input bind:value use:focus />
        </label>
        {#if $dialog.secondLabel}
          <label>
            <span>{$dialog.secondLabel}</span>
            <input bind:value={second} />
          </label>
        {/if}
        {#if $dialog.checkboxLabel}
          <label class="check"><input type="checkbox" bind:checked /> {$dialog.checkboxLabel}</label>
        {/if}
      {/if}
      <div class="buttons">
        <button type="button" class="btn" on:click={() => finish(false)}>Cancel</button>
        <button
          type="submit"
          class="btn {$dialog.kind === 'confirm' && $dialog.danger ? 'danger' : 'primary'}"
          use:focus={$dialog.kind === 'confirm'}
        >
          {$dialog.kind === 'confirm' ? $dialog.confirmLabel : $dialog.submitLabel ?? 'OK'}
        </button>
      </div>
    </form>
  </div>
{/if}

<style>
  .backdrop { position: fixed; inset: 0; z-index: 40; display: grid; place-items: center; background: rgba(0, 0, 0, 0.25); }
  .dialog {
    width: 420px;
    padding: 18px 20px 16px;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 12px;
    box-shadow: var(--shadow);
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  h3 { margin: 0; font-size: 14px; font-weight: 600; }
  p { margin: 0; color: var(--muted); line-height: 1.5; }
  label { display: flex; flex-direction: column; gap: 5px; color: var(--muted); font-size: 12px; }
  label input:not([type='checkbox']) { font-size: 13px; }
  .check { flex-direction: row; align-items: center; gap: 6px; color: var(--text); font-size: 13px; }
  .buttons { display: flex; justify-content: flex-end; gap: 8px; margin-top: 4px; }
</style>
