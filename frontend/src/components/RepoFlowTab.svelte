<script lang="ts">
  import { api } from '../lib/api'
  import { flowSettingsError, flowSettingsPanel } from '../lib/flowSettings'
  import { busy, expandedRepos, loadSideRefs, refreshRepo, selectedRepoId } from '../lib/stores'
  import { toast } from '../lib/ui'
  import { get } from 'svelte/store'

  export let repoID: string

  const panel = flowSettingsPanel({
    get: (id) => api.getFlowSettings(id),
    save: (id, cfg) => api.saveFlowSettings(id, cfg),
    // The same Go path as "Initialize git-flow" in the toolbar's Flow button.
    init: (id, cfg) => api.initFlow(id, cfg),
    afterWrite: async (id) => {
      // The official-branch rule is read with the refs: reload the ones on screen.
      if (id === get(selectedRepoId)) await refreshRepo()
      else if (get(expandedRepos).includes(id)) await loadSideRefs(id)
    },
  })
  const { state, form, dirty } = panel

  $: panel.open(repoID)
  $: ({ settings, loadError, error } = $state)
  $: setUp = !!settings?.initialized
  $: formError = settings ? flowSettingsError($form) : ''

  async function save() {
    if (!settings || !!$busy || (setUp && !$dirty)) return
    // Before the await: saving reloads the settings and flips setUp.
    const wasSetUp = setUp
    busy.set(wasSetUp ? 'Saving git-flow…' : 'Setting up git-flow…')
    try {
      if ((await panel.save()) && !wasSetUp) toast('git-flow set up')
    } finally {
      busy.set('')
    }
  }
  const onKey = (e: KeyboardEvent) => { if (e.key === 'Enter') save() }
</script>

{#if loadError}<p class="warn">{loadError}</p>{/if}
{#if settings}
  {#if !setUp}
    <p class="hint">Git-flow isn't set up in this repository. Until it is, these defaults decide the main branches for "Push main branches". "Set up git-flow" writes them and creates the development branch if it is missing.</p>
  {:else if settings.problem}
    <p class="warn">{settings.problem}.</p>
  {/if}
  <div class="fields">
    <label>Production branch<input bind:value={$form.master} on:keydown={onKey} /></label>
    <label>Development branch<input bind:value={$form.develop} on:keydown={onKey} /></label>
    <label>Feature prefix<input bind:value={$form.prefixes.feature} on:keydown={onKey} /></label>
    <label>Release prefix<input bind:value={$form.prefixes.release} on:keydown={onKey} /></label>
    <label>Hotfix prefix<input bind:value={$form.prefixes.hotfix} on:keydown={onKey} /></label>
    <label>Warmfix prefix<input bind:value={$form.prefixes.warmfix} on:keydown={onKey} /></label>
  </div>
  <p class="hint">These names decide the main branches pushed by "Push main branches".</p>
  {#if formError}<p class="warn">{formError}</p>{/if}
  {#if error && error !== formError}<p class="warn">{error}</p>{/if}
  <div>
    <button class="btn primary" disabled={!!$busy || formError !== '' || (setUp && !$dirty)} on:click={save}>{setUp ? 'Save' : 'Set up git-flow'}</button>
  </div>
{/if}

<style>
  .fields { display: grid; grid-template-columns: 1fr 1fr; gap: 10px 12px; margin: 8px 0; }
  label { display: flex; flex-direction: column; gap: 4px; font-size: 12px; color: var(--muted); }
  input { min-width: 0; font-family: var(--mono); font-size: 12px; }
  .warn { margin: 8px 0; font-size: 12px; color: var(--danger); }
  .hint { margin: 8px 0; font-size: 12px; color: var(--muted); }
</style>
