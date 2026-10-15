<script lang="ts">
  import { api } from '../lib/api'
  import { addReposFromFolder } from '../lib/actions'
  import {
    addLabel, addReposDialogOpen, chosenGroup, createLatest, folderName, fromScan, groupOptions, NEW_GROUP, selectAll, selectNone,
    skippedLine, ticked, toggle, type ScanView,
  } from '../lib/addRepos'
  import { repos, reposParent } from '../lib/stores'
  import { dialog, errorMessage } from '../lib/ui'
  import FolderField from './FolderField.svelte'

  let parent = ''
  let opened = false
  let field: FolderField | undefined
  let view: ScanView | null = null
  let scanning = false
  let scanError = ''
  let groupChoice = ''
  let newGroup = ''
  let adding = false
  let addError = ''
  const scans = createLatest()

  // Every opening starts afresh at the remembered folder (else home); nothing is scanned yet.
  $: if ($addReposDialogOpen !== opened) {
    opened = $addReposDialogOpen
    if (opened) init()
  }
  async function init() {
    scans.invalidate()
    parent = ''
    view = null
    scanning = false
    scanError = ''
    groupChoice = ''
    newGroup = ''
    adding = false
    addError = ''
    try {
      const initial = $reposParent || (await api.defaultReposParent())
      // Typing before the default arrived wins.
      if (parent === '') parent = initial
    } catch {
      // No default: the user types or picks a folder.
    }
  }

  $: dir = parent.trim()
  $: groups = groupOptions($repos)
  $: paths = ticked(view)
  $: group = chosenGroup(groupChoice, newGroup)
  $: canAdd = !adding && paths.length > 0 && group !== null

  // The list described the folder that was scanned; once the field changes it no longer applies.
  function edited() {
    scans.invalidate()
    view = null
    scanning = false
    scanError = ''
    addError = ''
  }

  async function scan() {
    // Read at call time: right after Choose…, the reactive `dir` is still the previous folder.
    const target = parent.trim()
    if (target === '' || scanning) return
    field?.closeList()
    const n = scans.next()
    scanning = true
    view = null
    scanError = ''
    addError = ''
    try {
      const result = await api.scanReposIn(target)
      if (!scans.isCurrent(n)) return
      view = fromScan(result)
      // Only a folder that scanned is remembered.
      reposParent.set(target)
    } catch (e) {
      if (scans.isCurrent(n)) scanError = errorMessage(e)
    } finally {
      if (scans.isCurrent(n)) scanning = false
    }
  }

  function onGroupChange() {
    if (groupChoice === NEW_GROUP && newGroup === '') newGroup = folderName(view?.parent ?? dir)
  }

  async function add() {
    if (!canAdd || group === null) return
    adding = true
    addError = ''
    const err = await addReposFromFolder(paths, group)
    adding = false
    if (err) addError = err
    else addReposDialogOpen.set(false)
  }

  // Closing is refused while adding: the add can't be cancelled, and a late answer would reach a later opening.
  const close = () => { if (!adding) addReposDialogOpen.set(false) }
  // Escape closes a confirm opened on top of this dialog first.
  const onKey = (e: KeyboardEvent) => { if ($addReposDialogOpen && e.key === 'Escape' && !$dialog && !e.defaultPrevented) close() }
</script>

<svelte:window on:keydown={onKey} />

{#if $addReposDialogOpen}
  <div class="backdrop" on:click|self={close} role="presentation">
    <div class="dialog" role="dialog" aria-label="Add repositories from a folder">
      <h3>Add repositories from a folder</h3>
      <!-- Enter in the field (no suggestion highlighted) submits this form: it scans. -->
      <form on:submit|preventDefault={scan}>
        <label>Parent folder
          <span class="scanrow">
            <FolderField bind:this={field} bind:value={parent} autofocus pickFolder={api.pickReposParent} idPrefix="repos-parent"
              onEdit={edited} onPicked={(p) => { parent = p; edited(); scan() }} />
            <button type="submit" class="btn" disabled={dir === '' || scanning}>{scanning ? 'Scanning…' : 'Scan'}</button>
          </span>
        </label>
      </form>

      {#if scanError}
        <p class="problem" role="alert">{scanError}</p>
      {:else if view}
        {#if view.rows.length === 0}
          <p class="muted">No git repositories in this folder.</p>
        {:else}
          <div class="listhead">
            <button type="button" class="link" on:click={() => view && (view = selectAll(view))}>Select all</button>
            <button type="button" class="link" on:click={() => view && (view = selectNone(view))}>None</button>
          </div>
          <ul class="rows" aria-label="Repositories">
            {#each view.rows as row (row.path)}
              <li title={row.path}>
                <label class="row" class:disabled={row.added}>
                  <input type="checkbox" checked={row.ticked} disabled={row.added}
                    on:change={() => view && (view = toggle(view, row.path))} />
                  <span class="name ellipsis">{row.name}</span>
                  {#if row.added}<span class="tag">Already added</span>{/if}
                </label>
              </li>
            {/each}
          </ul>
        {/if}
        {#if view.skipped}<p class="muted">{skippedLine(view.skipped)}</p>{/if}
      {/if}

      <label>Group
        <select bind:value={groupChoice} on:change={onGroupChange}>
          <option value="">No group</option>
          {#each groups as g (g)}<option value={g}>{g}</option>{/each}
          <option value={NEW_GROUP}>New group…</option>
        </select>
      </label>
      {#if groupChoice === NEW_GROUP}
        <label>Group name
          <input spellcheck="false" bind:value={newGroup} />
        </label>
      {/if}

      {#if addError}<p class="problem" role="alert">{addError}</p>{/if}
      <div class="buttons">
        <button type="button" class="btn" disabled={adding} on:click={close}>Cancel</button>
        <button type="button" class="btn primary" disabled={!canAdd} on:click={add}>
          {adding ? 'Adding…' : addLabel(paths.length)}
        </button>
      </div>
    </div>
  </div>
{/if}

<style>
  .backdrop { position: fixed; inset: 0; z-index: 40; display: grid; place-items: center; background: rgba(0, 0, 0, 0.25); }
  .dialog {
    width: min(480px, 92vw);
    max-height: 86vh;
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
  p { margin: 0; line-height: 1.5; }
  label { display: flex; flex-direction: column; gap: 5px; color: var(--muted); font-size: 12px; }
  label input:not([type='checkbox']), select { font-size: 13px; }
  .scanrow { display: flex; gap: 8px; }
  .scanrow :global(.parent) { flex: 1; min-width: 0; }
  .listhead { display: flex; gap: 12px; font-size: 12px; }
  .link { background: none; border: 0; padding: 0; color: var(--accent); cursor: pointer; font-size: 12px; }
  .rows {
    margin: 0; padding: 4px; list-style: none; overflow-y: auto; max-height: 260px; min-height: 0;
    border: 1px solid var(--border); border-radius: 8px;
  }
  .row { flex-direction: row; align-items: center; gap: 8px; padding: 4px 6px; border-radius: 6px; color: var(--text); font-size: 13px; cursor: pointer; }
  .row:hover { background: var(--hover); }
  .row.disabled { color: var(--muted); cursor: default; }
  .name { flex: 1; min-width: 0; }
  .tag { font-size: 11px; color: var(--muted); }
  .muted { color: var(--muted); font-size: 12px; }
  .problem { color: var(--danger); font-size: 12px; overflow-wrap: anywhere; white-space: pre-line; }
  .buttons { display: flex; justify-content: flex-end; gap: 8px; margin-top: 4px; }
</style>
