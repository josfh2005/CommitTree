<script lang="ts">
  import Icon from './Icon.svelte'
  import { api } from '../lib/api'
  import { chatModelOptions, needsKey, providerShortLabel, PROVIDERS } from '../lib/providers'
  import { aiSettings, loadAISettings, settingsOpen } from '../lib/stores'
  import type { AIStatus, ProviderName } from '../lib/types'
  import { errorMessage, toast } from '../lib/ui'

  export let status: AIStatus | null
  export let disabled = false
  // Called once a new chat provider/model is saved, so the panel can check
  // whether it is ready to answer.
  export let onChange: () => void = () => {}

  let open = false
  let saving = false
  // Hosted providers' model lists, fetched when the menu first opens and
  // kept while the panel lives; a provider whose list fails is left out.
  let hostedModels: Partial<Record<ProviderName, string[]>> = {}

  $: provider = $aiSettings?.chatProvider ?? 'ollama'
  $: model = $aiSettings?.chatModel ?? ''
  $: options = status ? chatModelOptions(status, hostedModels) : []

  async function toggle() {
    open = !open
    if (!open || !status) return
    for (const { value } of PROVIDERS) {
      if (!needsKey(value) || hostedModels[value] || !status.providers.find((p) => p.provider === value)?.hasKey) continue
      api
        .listModels(value)
        .then((list) => (hostedModels = { ...hostedModels, [value]: list }))
        .catch(() => {})
    }
  }

  async function choose(p: ProviderName, m: string) {
    open = false
    if (p === provider && m === model) return
    saving = true
    try {
      const settings = await api.getAISettings()
      await api.saveAISettings({ ...settings, chatProvider: p, chatModel: m })
      await loadAISettings()
      onChange()
    } catch (e) {
      toast(errorMessage(e), 'error')
    } finally {
      saving = false
    }
  }

  function openSettings() {
    open = false
    settingsOpen.set(true)
  }
</script>

<svelte:window on:click={() => (open = false)} on:keydown={(e) => open && e.key === 'Escape' && (open = false)} />

<div class="picker">
  <button
    class="current"
    title="{providerShortLabel(provider)} · {model} — change the chat model"
    disabled={disabled || saving || !$aiSettings}
    on:click|stopPropagation={toggle}
  >
    <span class="ellipsis">{model || 'Choose a model'}</span>
    <Icon name="chevron-down" size={12} />
  </button>
  {#if open}
    <div class="menu" role="menu">
      {#each options as group}
        <div class="group">{group.label}</div>
        {#each group.models as m}
          <button class="item" role="menuitemradio" aria-checked={group.provider === provider && m === model} on:click={() => choose(group.provider, m)}>
            <span class="check">{#if group.provider === provider && m === model}<Icon name="check" size={12} />{/if}</span>
            <span class="ellipsis">{m}</span>
          </button>
        {/each}
      {:else}
        <div class="empty">No models available.</div>
      {/each}
      <div class="sep"></div>
      <button class="item" on:click={openSettings}><span class="check"></span>Settings…</button>
    </div>
  {/if}
</div>

<style>
  .picker { position: relative; min-width: 0; }
  .current { display: flex; align-items: center; gap: 4px; max-width: 220px; height: 24px; padding: 0 6px; border-radius: 6px; color: var(--muted); font-size: 12px; }
  .current:hover:not(:disabled) { background: var(--hover); color: var(--text); }
  .menu { position: absolute; bottom: calc(100% + 6px); left: 0; z-index: 50; min-width: 220px; max-height: 320px; overflow-y: auto; padding: 4px; background: var(--surface); border: 1px solid var(--border); border-radius: 10px; box-shadow: var(--shadow); }
  .group { padding: 6px 10px 2px; font-size: 11px; color: var(--faint); }
  .item { display: flex; align-items: center; gap: 6px; width: 100%; height: 28px; padding: 0 10px 0 6px; border-radius: 6px; text-align: left; }
  .item:hover { background: var(--hover); }
  .check { display: inline-flex; width: 14px; flex: none; }
  .empty { padding: 6px 10px; color: var(--muted); font-size: 12px; }
  .sep { height: 1px; margin: 4px 0; background: var(--border); }
</style>
