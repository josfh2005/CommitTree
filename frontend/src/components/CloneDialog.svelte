<script lang="ts">
  import { api } from '../lib/api'
  import {
    cancelClone, cloneCancelling, cloneDialogOpen, cloneForm, cloneView, editName, editURL, joinPath, nameError, startClone,
  } from '../lib/clone'
  import { cloneParent } from '../lib/stores'
  import { dialog } from '../lib/ui'

  let parent = ''
  // Every opening reads the remembered parent afresh (the last clone's, else the home folder).
  $: if ($cloneDialogOpen) { if (parent === '') initParent() } else parent = ''
  async function initParent() {
    try {
      parent = $cloneParent || (await api.defaultCloneParent())
    } catch {
      // No default: the user picks a folder with Choose….
    }
  }

  $: view = $cloneView
  $: form = $cloneForm
  $: nameErr = form.name === '' ? '' : nameError(form.name)
  $: canClone = form.url.trim() !== '' && form.name.trim() !== '' && nameErr === '' && parent !== ''

  async function choose() {
    try {
      const picked = await api.pickCloneParent(parent)
      if (picked) parent = picked
    } catch {
      // The picker failed or was dismissed: keep the current folder.
    }
  }

  function submit() {
    // One clone at a time: a running one is only ever shown, never restarted.
    if (view.kind !== 'form' || !canClone) return
    cloneParent.set(parent)
    startClone(parent, api.cloneRepo)
  }

  // Closing never stops a running clone: that is "Continue in background".
  const close = () => cloneDialogOpen.set(false)
  // Escape closes a confirm opened on top of this dialog first.
  const onKey = (e: KeyboardEvent) => { if ($cloneDialogOpen && e.key === 'Escape' && !$dialog) close() }
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
              <input readonly value={parent} title={parent} />
              <button type="button" class="btn" on:click={choose}>Choose…</button>
            </span>
          </label>
          <label>Folder name
            <input spellcheck="false" value={form.name}
              on:input={(e) => cloneForm.set(editName(form, e.currentTarget.value))} />
          </label>
          {#if nameErr}<p class="problem">{nameErr}</p>{/if}
          {#if parent && form.name.trim()}<p class="preview ellipsis" title={joinPath(parent, form.name.trim())}>{joinPath(parent, form.name.trim())}</p>{/if}
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
  .parent input { flex: 1; min-width: 0; }
  .preview { color: var(--muted); font-size: 12px; font-family: var(--mono); }
  .url { font-family: var(--mono); font-size: 12px; }
  .phase { color: var(--text); }
  progress { width: 100%; }
  .problem { color: var(--danger); font-size: 12px; overflow-wrap: anywhere; white-space: pre-line; }
  .buttons { display: flex; justify-content: flex-end; gap: 8px; margin-top: 4px; }
</style>
