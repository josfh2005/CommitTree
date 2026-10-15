<script lang="ts">
  import { onDestroy, tick } from 'svelte'
  import { api } from '../lib/api'
  import { applyPick, closedList, createCompleter, handleKey, markStale, openList, splitPath } from '../lib/pathcomplete'

  /** The folder as typed (bindable); the backend trims it and expands a leading ~. */
  export let value: string
  /** Opens the system folder picker at the given folder; "" when cancelled. */
  export let pickFolder: (start: string) => Promise<string>
  /** Prefix for the listbox and option ids (unique per dialog). */
  export let idPrefix: string
  /** Called after Choose… put a folder in the field. */
  export let onPicked: (path: string) => void = () => {}
  /** Called after the user changed the text by typing. */
  export let onEdit: () => void = () => {}
  export let autofocus = false

  // Folder suggestions: looked up while typing (after a short pause), only the latest answer counts.
  // Programmatic changes (a remembered folder, Choose…) don't open them.
  let list = closedList
  const completer = createCompleter(api.listDirs, (items) => (list = openList(items)), 150)
  const optionID = (i: number) => `${idPrefix}-opt-${i}`
  $: if (list.selected >= 0) tick().then(() => document.getElementById(optionID(list.selected))?.scrollIntoView({ block: 'nearest' }))
  onDestroy(() => completer.cancel())

  function pick(item: string) {
    value = applyPick(item)
    list = closedList
    onEdit()
    // The next level, straight away.
    completer.now(value)
  }

  function onKey(e: KeyboardEvent) {
    // keyCode 229: the key that confirms an IME composition (Safari reports no isComposing then).
    if (e.isComposing || e.keyCode === 229) return
    const r = handleKey(list, e.key, e.shiftKey, value, e.ctrlKey || e.altKey || e.metaKey)
    list = r.state
    if (!r.handled) return
    e.preventDefault()
    if (e.key === 'Escape') {
      // Esc closes the suggestions only, not the dialog, and no lookup may reopen them.
      e.stopPropagation()
      closeList()
    }
    if (r.pick !== null) pick(r.pick)
  }

  export function closeList() {
    completer.cancel()
    list = closedList
  }

  async function choose() {
    closeList()
    try {
      const picked = await pickFolder(value)
      if (picked) {
        value = picked
        onPicked(picked)
      }
    } catch {
      // The picker failed or was dismissed: keep the current folder.
    }
  }
</script>

<span class="parent">
  <span class="field">
    <!-- svelte-ignore a11y_autofocus -->
    <input {autofocus} spellcheck="false" autocomplete="off" placeholder="/path/to/folder or ~/folder" bind:value
      role="combobox" aria-label="Parent folder" aria-autocomplete="list" aria-expanded={list.open} aria-controls={list.open ? `${idPrefix}-list` : undefined}
      aria-activedescendant={list.selected >= 0 ? optionID(list.selected) : undefined}
      on:input={(e) => { list = markStale(list); completer.request(e.currentTarget.value); onEdit() }}
      on:keydown={onKey} on:blur={closeList} />
    {#if list.open}
      <!-- mousedown is kept off the input's blur, scrollbar included. -->
      <ul id="{idPrefix}-list" class="suggest" role="listbox" aria-label="Folders" on:mousedown|preventDefault>
        {#each list.items as item, i (item)}
          {@const parts = splitPath(item)}
          <li id={optionID(i)} role="option" aria-selected={i === list.selected} class:selected={i === list.selected}
            title={item} on:mousedown={(e) => e.button === 0 && pick(item)}>
            <span class="dir">{parts.dir}</span><span class="name">{parts.name}</span>
          </li>
        {/each}
      </ul>
    {/if}
  </span>
  <button type="button" class="btn" on:click={choose}>Choose…</button>
</span>

<style>
  .parent { display: flex; gap: 8px; }
  .field { position: relative; flex: 1; min-width: 0; display: flex; }
  .field input { flex: 1; min-width: 0; font-size: 13px; }
  .suggest {
    position: absolute; top: calc(100% + 4px); left: 0; right: 0; z-index: 5;
    margin: 0; padding: 4px; list-style: none; max-height: 200px; overflow-y: auto;
    background: var(--surface); border: 1px solid var(--border); border-radius: 8px; box-shadow: var(--shadow);
  }
  .suggest li {
    padding: 4px 8px; border-radius: 6px; cursor: pointer; font-family: var(--mono); font-size: 12px;
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  }
  .suggest li:hover { background: var(--hover); }
  .suggest li.selected { background: var(--active); }
  .suggest .dir { color: var(--muted); }
  .suggest .name { color: var(--text); font-weight: 600; }
</style>
