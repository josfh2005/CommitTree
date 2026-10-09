<script lang="ts">
  import { tick } from 'svelte'
  import { api } from '../lib/api'
  import {
    cancelClone, cloneCancelling, cloneDialogOpen, cloneForm, cloneView, editName, editURL, joinPath, nameError, startClone,
  } from '../lib/clone'
  import { applyPick, closedList, createCompleter, handleKey, markStale, openList, splitPath } from '../lib/pathcomplete'
  import { cloneParent } from '../lib/stores'
  import { dialog } from '../lib/ui'

  let parent = ''
  let opened = false
  // Every opening reads the remembered parent afresh (the last clone's, else the home folder).
  // Only the opening does: the field may be emptied while typing a new path.
  $: if ($cloneDialogOpen !== opened) {
    opened = $cloneDialogOpen
    if (opened) initParent()
    else {
      parent = ''
      completer.cancel()
      list = closedList
    }
  }
  async function initParent() {
    try {
      const initial = $cloneParent || (await api.defaultCloneParent())
      // Typing before the default arrived wins.
      if (parent === '') parent = initial
    } catch {
      // No default: the user picks a folder with Choose….
    }
  }

  $: view = $cloneView
  $: form = $cloneForm
  $: nameErr = form.name === '' ? '' : nameError(form.name)
  // The parent may be typed: the backend trims it and expands a leading ~.
  $: dir = parent.trim()
  $: canClone = form.url.trim() !== '' && form.name.trim() !== '' && nameErr === '' && dir !== ''

  // Folder suggestions under the parent field: looked up while typing (after a short pause),
  // only the latest answer counts. Programmatic changes (the remembered parent, Choose…) don't open it.
  let list = closedList
  const completer = createCompleter(api.listDirs, (items) => (list = openList(items)), 150)
  const optionID = (i: number) => `clone-parent-opt-${i}`
  $: if (list.selected >= 0) tick().then(() => document.getElementById(optionID(list.selected))?.scrollIntoView({ block: 'nearest' }))

  function pick(item: string) {
    parent = applyPick(item)
    list = closedList
    // The next level, straight away.
    completer.now(parent)
  }

  function onParentKey(e: KeyboardEvent) {
    // keyCode 229: the key that confirms an IME composition (Safari reports no isComposing then).
    if (e.isComposing || e.keyCode === 229) return
    const r = handleKey(list, e.key, e.shiftKey, parent, e.ctrlKey || e.altKey || e.metaKey)
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

  function closeList() {
    completer.cancel()
    list = closedList
  }

  async function choose() {
    closeList()
    try {
      const picked = await api.pickCloneParent(parent)
      if (picked) parent = picked
    } catch {
      // The picker failed or was dismissed: keep the current folder.
    }
  }

  async function submit() {
    // One clone at a time: a running one is only ever shown, never restarted.
    if (view.kind !== 'form' || !canClone) return
    closeList()
    // Only a parent the backend accepted is remembered.
    if (await startClone(dir, api.cloneRepo, api.cloneStatus)) cloneParent.set(dir)
  }

  // Closing never stops a running clone: that is "Continue in background".
  const close = () => cloneDialogOpen.set(false)
  // Escape closes a confirm opened on top of this dialog first.
  const onKey = (e: KeyboardEvent) => { if ($cloneDialogOpen && e.key === 'Escape' && !$dialog && !e.defaultPrevented) close() }
</script>

<svelte:window on:keydown={onKey} />

{#if $cloneDialogOpen}
  <div class="backdrop" on:click|self={close} role="presentation">
    <div class="dialog" role="dialog" aria-label="Clone a repository">
      <h3>Clone a repository</h3>
      {#if view.kind === 'form'}
        <form on:submit|preventDefault={submit}>
          <label>URL
            <!-- svelte-ignore a11y_autofocus -->
            <input autofocus spellcheck="false" placeholder="https://github.com/org/repo.git"
              value={form.url} on:input={(e) => cloneForm.set(editURL(form, e.currentTarget.value))} />
          </label>
          <label>Parent folder
            <span class="parent">
              <span class="field">
                <input spellcheck="false" autocomplete="off" placeholder="/path/to/folder or ~/folder" bind:value={parent}
                  role="combobox" aria-label="Parent folder" aria-autocomplete="list" aria-expanded={list.open} aria-controls={list.open ? 'clone-parent-list' : undefined}
                  aria-activedescendant={list.selected >= 0 ? optionID(list.selected) : undefined}
                  on:input={(e) => { list = markStale(list); completer.request(e.currentTarget.value) }}
                  on:keydown={onParentKey} on:blur={closeList} />
                {#if list.open}
                  <!-- mousedown is kept off the input's blur, scrollbar included. -->
                  <ul id="clone-parent-list" class="suggest" role="listbox" aria-label="Folders" on:mousedown|preventDefault>
                    {#each list.items as item, i (item)}
                      {@const parts = splitPath(item)}
                      <li id={optionID(i)} role="option" aria-selected={i === list.selected} class:selected={i === list.selected}
                        title={item} on:mousedown={() => pick(item)}>
                        <span class="dir">{parts.dir}</span><span class="name">{parts.name}</span>
                      </li>
                    {/each}
                  </ul>
                {/if}
              </span>
              <button type="button" class="btn" on:click={choose}>Choose…</button>
            </span>
          </label>
          <label>Folder name
            <input spellcheck="false" value={form.name}
              on:input={(e) => cloneForm.set(editName(form, e.currentTarget.value))} />
          </label>
          {#if nameErr}<p class="problem">{nameErr}</p>{/if}
          {#if dir && form.name.trim()}<p class="preview ellipsis" title={joinPath(dir, form.name.trim())}>{joinPath(dir, form.name.trim())}</p>{/if}
          {#if view.error}<p class="problem" role="alert">{view.error}</p>{/if}
          <div class="buttons">
            <button type="button" class="btn" on:click={close}>Cancel</button>
            <button type="submit" class="btn primary" disabled={!canClone}>Clone</button>
          </div>
        </form>
      {:else}
        {@const p = view.progress}
        <p class="url ellipsis" title={view.url}>{view.url}</p>
        <p class="preview ellipsis" title={view.dest}>→ {view.dest}</p>
        <p class="phase">{p ? p.phase : 'Starting…'}</p>
        {#if p && p.percent >= 0}
          <progress max="100" value={p.percent} aria-label={p.phase}></progress>
        {:else}
          <progress aria-label="Cloning"></progress>
        {/if}
        {#if p?.detail}<p class="preview ellipsis" title={p.detail}>{p.detail}</p>{/if}
        <div class="buttons">
          <button type="button" class="btn" disabled={$cloneCancelling} on:click={() => cancelClone(api.cancelClone)}>
            {$cloneCancelling ? 'Cancelling…' : 'Cancel'}
          </button>
          <button type="button" class="btn primary" on:click={close}>Continue in background</button>
        </div>
      {/if}
    </div>
  </div>
{/if}

<style>
  .backdrop { position: fixed; inset: 0; z-index: 40; display: grid; place-items: center; background: rgba(0, 0, 0, 0.25); }
  .dialog {
    width: min(460px, 92vw);
    padding: 18px 20px 16px;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 12px;
    box-shadow: var(--shadow);
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  form { display: flex; flex-direction: column; gap: 12px; }
  h3 { margin: 0; font-size: 14px; font-weight: 600; }
  p { margin: 0; line-height: 1.5; }
  label { display: flex; flex-direction: column; gap: 5px; color: var(--muted); font-size: 12px; }
  label input { font-size: 13px; }
  .parent { display: flex; gap: 8px; }
  .field { position: relative; flex: 1; min-width: 0; display: flex; }
  .field input { flex: 1; min-width: 0; }
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
  .preview { color: var(--muted); font-size: 12px; font-family: var(--mono); }
  .url { font-family: var(--mono); font-size: 12px; }
  .phase { color: var(--text); }
  progress { width: 100%; }
  .problem { color: var(--danger); font-size: 12px; overflow-wrap: anywhere; white-space: pre-line; }
  .buttons { display: flex; justify-content: flex-end; gap: 8px; margin-top: 4px; }
</style>
