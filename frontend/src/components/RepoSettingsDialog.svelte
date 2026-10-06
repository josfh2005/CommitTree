<script lang="ts">
  import Icon from './Icon.svelte'
  import RepoAITab from './RepoAITab.svelte'
  import { api } from '../lib/api'
  import { defaultRemoteName, remoteFormError } from '../lib/remoteForm'
  import { busy, refreshRepo, repoSettings, repos, selectedRepoId } from '../lib/stores'
  import { remotesPanel } from '../lib/remotesPanel'
  import type { RemoteConfig } from '../lib/types'
  import { confirmDialog, dialog } from '../lib/ui'

  const panel = remotesPanel({
    list: (id) => api.listRemotes(id),
    test: (id, name) => api.testRemote(id, name),
    afterWrite: async (id) => {
      if (id === $selectedRepoId) await refreshRepo()
    },
  })
  const state = panel.state
  let editing = ''               // remote whose URL is being edited
  let editURL = ''
  let adding = false
  let newName = ''
  let newURL = ''

  $: repoID = $repoSettings?.repoID ?? ''
  $: tab = $repoSettings?.tab ?? 'remotes'
  // Switching tabs rewrites the store but is not a new opening: it must not
  // reset the Remotes pane (an edit or add form in progress).
  let switching = false
  const setTab = (t: 'remotes' | 'ai') => {
    switching = true
    repoSettings.update((s) => (s ? { ...s, tab: t } : s))
  }
  $: repo = $repos.find((r) => r.id === repoID)
  // The repository left the list (removed, or a worktree that vanished).
  $: if ($repoSettings && !repo) close()
  // Every opening starts fresh, even for the same repository.
  $: if ($repoSettings) opened($repoSettings.repoID)
  $: ({ remotes, loadError, error, tests } = $state)
  $: names = remotes.map((r) => r.name)
  $: formError = remoteFormError(newName, newURL, names)

  // Read inside a function so `switching` is not a dependency of the statement above.
  function opened(id: string) {
    if (switching) switching = false
    else reset(id)
  }

  function reset(id: string) {
    editing = ''; adding = false
    panel.open(id)
  }

  async function write(label: string, fn: () => Promise<void>): Promise<boolean> {
    busy.set(label)
    try {
      return await panel.write(fn)
    } finally {
      busy.set('')
    }
  }

  const test = (name: string) => panel.test(name)

  function startEdit(r: RemoteConfig) { editing = r.name; editURL = r.fetchURL; panel.clearTest(r.name) }
  async function saveEdit() {
    const name = editing
    if (await write('Saving remote…', () => api.setRemoteURL(repoID, name, editURL.trim()))) editing = ''
  }
  async function remove(name: string) {
    const ok = await confirmDialog({
      title: `Remove remote ${name}?`,
      message: 'Its remote branches leave the log, and branches that track it lose their upstream.',
      confirmLabel: 'Remove',
      danger: true,
    })
    if (ok) await write('Removing remote…', () => api.removeRemote(repoID, name))
  }
  function startAdd() { adding = true; newName = defaultRemoteName(names); newURL = '' }
  async function add() {
    if (formError !== '') return
    if (await write('Adding remote…', () => api.addRemote(repoID, newName.trim(), newURL.trim()))) adding = false
  }
  function close() { repoSettings.set(null) }
  // Escape closes the confirm dialog on top first; an Escape inside the URL
  // or add fields cancels that edit instead (handled on the inputs).
  function onKey(e: KeyboardEvent) { if ($repoSettings && e.key === 'Escape' && !$dialog && !e.defaultPrevented) close() }
</script>

<svelte:window on:keydown={onKey} />

{#if $repoSettings && repo}
  <div class="backdrop" on:click|self={close} role="presentation">
    <div class="dialog" role="dialog" aria-label="{repo.name} settings">
      <nav class="tabs" aria-label="Repository settings sections">
        <h3 class="ellipsis" title={repo.name}>{repo.name} settings</h3>
        <button class="tab" class:active={tab === 'remotes'} aria-current={tab === 'remotes' ? 'page' : undefined} on:click={() => setTab('remotes')}>Remotes</button>
        <button class="tab" class:active={tab === 'ai'} aria-current={tab === 'ai' ? 'page' : undefined} on:click={() => setTab('ai')}>AI</button>
      </nav>
      <div class="pane">
        <header>
          <h3>{tab === 'ai' ? 'AI' : 'Remotes'}</h3>
          <button class="icon-btn" title="Close" on:click={close}><Icon name="x" /></button>
        </header>
        <div class="content">
          {#if tab === 'ai'}
            <RepoAITab {repoID} />
          {:else}
          {#if loadError}<p class="warn">{loadError}</p>{/if}
          {#if error}<p class="warn">{error}</p>{/if}
          {#if remotes.length === 0 && !loadError}<p class="hint">No remotes yet.</p>{/if}
          <ul class="remotes">
            {#each remotes as r (r.name)}
              {@const t = tests[r.name]}
              <li>
                <div class="line">
                  <strong>{r.name}</strong>
                  {#if editing === r.name}
                    <!-- svelte-ignore a11y_autofocus -->
                    <input class="url-input" bind:value={editURL} autofocus
                      on:keydown={(e) => { if (e.key === 'Enter') saveEdit(); if (e.key === 'Escape') { e.preventDefault(); editing = '' } }} />
                    <button class="btn primary" disabled={!!$busy || !editURL.trim() || editURL.trim() === r.fetchURL} on:click={saveEdit}>Save</button>
                    <button class="btn" on:click={() => (editing = '')}>Cancel</button>
                  {:else}
                    <span class="url ellipsis" title={r.fetchURL}>{r.fetchURL}</span>
                    <button class="btn" disabled={t === 'running'} on:click={() => test(r.name)}>{t === 'running' ? 'Testing…' : 'Test'}</button>
                    <button class="btn" disabled={!!$busy} on:click={() => startEdit(r)}>Edit</button>
                    <button class="btn danger-text" disabled={!!$busy} on:click={() => remove(r.name)}>Remove…</button>
                  {/if}
                </div>
                {#if t && t !== 'running'}<p class="result" class:ok={t.ok}>{t.message}</p>{/if}
              </li>
            {/each}
          </ul>
          {#if adding}
            <div class="add">
              <label>Name<input bind:value={newName} on:keydown={(e) => { if (e.key === 'Escape') { e.preventDefault(); adding = false } }} /></label>
              <label class="grow">URL<input bind:value={newURL}
                on:keydown={(e) => { if (e.key === 'Enter') add(); if (e.key === 'Escape') { e.preventDefault(); adding = false } }} /></label>
              <button class="btn primary" disabled={formError !== '' || !!$busy} on:click={add}>Add</button>
              <button class="btn" on:click={() => (adding = false)}>Cancel</button>
            </div>
            {#if formError !== '' && formError !== '-'}<p class="warn">{formError}</p>{/if}
          {:else}
            <div><button class="btn" disabled={!!loadError} on:click={startAdd}>Add remote</button></div>
          {/if}
          {/if}
        </div>
      </div>
    </div>
  </div>
{/if}

<style>
  .backdrop { position: fixed; inset: 0; z-index: 40; display: grid; place-items: center; background: rgba(0, 0, 0, 0.25); }
  .dialog {
    display: flex;
    width: min(720px, 92vw);
    height: min(440px, 86vh);
    overflow: hidden;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 12px;
    box-shadow: var(--shadow);
  }
  .tabs { flex: none; width: 168px; display: flex; flex-direction: column; gap: 2px; padding: 16px 10px; background: var(--sidebar); border-right: 1px solid var(--border); }
  .tabs h3 { padding: 0 8px 10px; }
  .tab { height: 28px; padding: 0 10px; border-radius: 7px; text-align: left; }
  .tab:hover { background: var(--hover); }
  .tab.active { background: var(--active); font-weight: 500; }
  .pane { flex: 1; min-width: 0; display: flex; flex-direction: column; }
  header { flex: none; display: flex; align-items: center; justify-content: space-between; padding: 14px 16px 6px 20px; }
  .content { flex: 1; min-height: 0; overflow-y: auto; padding: 0 20px 16px; }
  h3 { margin: 0; font-size: 15px; font-weight: 600; }
  .remotes { list-style: none; margin: 8px 0 12px; padding: 0; display: flex; flex-direction: column; gap: 6px; }
  .remotes li { padding: 8px 10px; border: 1px solid var(--border); border-radius: 8px; }
  .line { display: flex; align-items: center; gap: 8px; min-width: 0; }
  .line strong { flex: none; }
  .url { flex: 1; min-width: 0; color: var(--muted); font-family: var(--mono); font-size: 12px; }
  .url-input { flex: 1; min-width: 0; font-family: var(--mono); font-size: 12px; }
  .result { margin: 6px 0 0; font-size: 12px; color: var(--danger); }
  .result.ok { color: var(--ok); }
  .add { display: flex; align-items: flex-end; gap: 8px; }
  .add label { display: flex; flex-direction: column; gap: 4px; font-size: 12px; color: var(--muted); }
  .add .grow { flex: 1; }
  .warn { margin: 8px 0; font-size: 12px; color: var(--danger); }
  .hint { margin: 8px 0; font-size: 12px; color: var(--muted); }
  .danger-text { color: var(--danger); }
</style>
