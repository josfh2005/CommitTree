<script lang="ts">
  import { tick } from 'svelte'
  import { filterPick } from '../lib/pick'
  import { dialog, formMessage, initialFormValues } from '../lib/ui'

  // Loosely typed: bind:checked and bind:value share this record.
  let values: Record<string, any> = {}
  let value = ''
  let second = ''
  let checked = false
  let choice = ''
  let query = ''
  let index = 0

  $: if ($dialog?.kind === 'prompt') {
    value = $dialog.value ?? ''
    second = ''
    checked = $dialog.checked ?? false
  }
  $: if ($dialog?.kind === 'confirm') checked = $dialog.checked ?? false
  $: if ($dialog?.kind === 'choice') choice = $dialog.value
  $: if ($dialog?.kind === 'form') values = initialFormValues($dialog.fields)
  $: if ($dialog?.kind === 'pick') {
    query = ''
    index = 0
  }
  $: shown = $dialog?.kind === 'pick' ? filterPick($dialog.items, query) : []
  // Keep the selected item visible while arrowing past the fold.
  $: if ($dialog?.kind === 'pick' && index >= 0) tick().then(() => document.querySelector('.pick-item.selected')?.scrollIntoView({ block: 'nearest' }))
  $: danger = $dialog?.kind === 'confirm' ? !!$dialog.danger : $dialog?.kind === 'choice' ? !!$dialog.danger?.(choice) : false
  $: submitLabel =
    $dialog?.kind === 'confirm' ? $dialog.confirmLabel : $dialog?.kind === 'choice' ? $dialog.confirmLabel(choice)
      : $dialog?.kind === 'pick' ? $dialog.submitLabel
      : $dialog?.kind === 'form' ? $dialog.submitLabel
      : $dialog?.kind === 'results' ? 'OK'
      : ($dialog?.submitLabel ?? 'OK')

  function finish(ok: boolean) {
    const current = $dialog
    if (!current) return
    // Read the pick before closing: `shown` is recomputed from $dialog.
    const picked = ok ? (shown[index]?.key ?? null) : null
    dialog.set(null)
    if (current.kind === 'confirm') current.resolve({ ok, checked })
    else if (current.kind === 'choice') current.resolve(ok ? choice : null)
    else if (current.kind === 'pick') current.resolve(picked)
    else if (current.kind === 'form') current.resolve(ok ? values : null)
    else if (current.kind === 'results') current.resolve()
    else current.resolve(ok ? { value, second, checked } : null)
  }

  function pickKey(e: KeyboardEvent) {
    if (e.key === 'ArrowDown') index = Math.min(index + 1, shown.length - 1)
    else if (e.key === 'ArrowUp') index = Math.max(index - 1, 0)
    else return
    e.preventDefault()
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
        {#if $dialog.checkboxLabel}
          <label class="check"><input type="checkbox" bind:checked /> {$dialog.checkboxLabel}</label>
        {/if}
      {:else if $dialog.kind === 'choice'}
        <label>
          <span>{$dialog.label}</span>
          <select bind:value={choice} use:focus>
            {#each $dialog.options as option}<option value={option.value}>{option.label}</option>{/each}
          </select>
        </label>
        <p>{$dialog.message(choice)}</p>
      {:else if $dialog.kind === 'pick'}
        <input class="search" placeholder={$dialog.placeholder} bind:value={query} on:input={() => (index = 0)} on:keydown={pickKey} use:focus />
        <div class="pick-list" role="listbox">
          {#each shown as item, i (item.key)}
            {#if item.group && item.group !== shown[i - 1]?.group}<div class="pick-group">{item.group}</div>{/if}
            <button type="button" class="pick-item" class:selected={i === index} role="option" aria-selected={i === index} tabindex="-1" on:mousedown|preventDefault on:click={() => (index = i)} on:dblclick={() => finish(true)}>{item.label}</button>
          {:else}
            <p class="pick-empty">{$dialog.empty}</p>
          {/each}
        </div>
      {:else if $dialog.kind === 'form'}
        {#if formMessage($dialog, values)}<p>{formMessage($dialog, values)}</p>{/if}
        {#each $dialog.fields as field, i (field.key)}
          {#if field.kind === 'checkbox'}
            <label class="check"><input type="checkbox" bind:checked={values[field.key]} /> {field.label}</label>
          {:else if field.kind === 'select'}
            <label>
              <span>{field.label}</span>
              <select bind:value={values[field.key]} use:focus={i === 0}>
                {#each field.options as option}<option value={option.value}>{option.label}</option>{/each}
              </select>
            </label>
          {:else}
            <label>
              <span>{field.label}</span>
              <span class="prefixed">
                {#if field.prefix}<span class="prefix">{field.prefix}</span>{/if}
                <input bind:value={values[field.key]} use:focus={i === 0} />
              </span>
            </label>
          {/if}
        {/each}
      {:else if $dialog.kind === 'results'}
        {#if $dialog.message}<p>{$dialog.message}</p>{/if}
        <ul class="results">
          {#each $dialog.rows as row}
            <li class={row.tone}><span class="mark">{row.mark}</span><span class="label">{row.label}</span><span class="detail">{row.detail}</span></li>
          {/each}
        </ul>
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
        {#if $dialog.kind !== 'results'}<button type="button" class="btn" on:click={() => finish(false)}>Cancel</button>{/if}
        <button
          type="submit"
          class="btn {danger ? 'danger' : 'primary'}"
          use:focus={$dialog.kind === 'confirm' || $dialog.kind === 'results'}
          disabled={$dialog.kind === 'pick' && shown.length === 0}
        >
          {submitLabel}
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
  p { margin: 0; color: var(--muted); line-height: 1.5; white-space: pre-line; }
  label { display: flex; flex-direction: column; gap: 5px; color: var(--muted); font-size: 12px; }
  label input:not([type='checkbox']), label select { font-size: 13px; }
  .check { flex-direction: row; align-items: center; gap: 6px; color: var(--text); font-size: 13px; }
  .search { font-size: 13px; }
  .prefixed { display: flex; align-items: center; gap: 4px; }
  .prefixed input { flex: 1; min-width: 0; }
  .prefix { color: var(--faint); font-size: 13px; }
  .pick-list { max-height: 260px; overflow-y: auto; display: flex; flex-direction: column; border: 1px solid var(--border); border-radius: 8px; padding: 4px; }
  .pick-group { padding: 6px 8px 2px; font-size: 11px; color: var(--faint); }
  .pick-item { text-align: left; padding: 5px 8px; border-radius: 6px; }
  .pick-item:hover { background: var(--hover); }
  .pick-item.selected { background: var(--active); }
  .pick-empty { padding: 8px; font-size: 12px; }
  .results { list-style: none; margin: 0; padding: 0; max-height: 50vh; overflow: auto; display: grid; gap: 8px; }
  .results li { display: grid; grid-template-columns: 16px 1fr; column-gap: 8px; }
  .results .detail { grid-column: 2; font-size: 12px; color: var(--muted); white-space: pre-wrap; overflow-wrap: anywhere; }
  .results .error .mark, .results .error .detail { color: var(--danger); }
  .results .ok .mark { color: var(--accent); }
  .results .muted .mark { color: var(--muted); }
  .buttons { display: flex; justify-content: flex-end; gap: 8px; margin-top: 4px; }
</style>
