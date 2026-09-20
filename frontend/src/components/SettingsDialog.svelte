<script lang="ts">
  import { onDestroy } from 'svelte'
  import { EventsOn } from '../../wailsjs/runtime/runtime'
  import Icon from './Icon.svelte'
  import { api } from '../lib/api'
  import { formatBytes, percent } from '../lib/format'
  import { modelForProvider, modelHint, needsKey, processingNotice, PROVIDERS, settingsHaveModels, usesOllama } from '../lib/providers'
  import { settingsOpen } from '../lib/stores'
  import type { AISettings, AIStatus, ModelDone, ModelProgress, ProviderName, PromptInfo } from '../lib/types'
  import { errorMessage, toast } from '../lib/ui'

  const RECOMMENDED = 'qwen2.5:7b'

  let settings: AISettings | null = null
  let status: AIStatus | null = null
  let prompts: PromptInfo[] = []
  let pull: ModelProgress | null = null
  let otherModel = ''
  let saving = false
  let keyInput: Record<string, string> = { openai: '', anthropic: '' }
  let chatModels: string[] = []
  let taskModels: string[] = []
  // modelErrors[provider] is set when api.listModels(provider) itself fails
  // (e.g. a stored key is invalid or expired) — distinct from modelHint,
  // which only reflects the backend's aiStatus snapshot (no key stored).
  let modelErrors: Record<string, string> = {}

  const offProgress = EventsOn('model:progress', (p: ModelProgress) => (pull = p))
  const offDone = EventsOn('model:done', async (p: ModelDone) => {
    pull = null
    if (p.error && !p.canceled) toast(p.error, 'error')
    else if (!p.error) toast(`Downloaded ${p.name}`)
    await refresh()
  })
  onDestroy(() => {
    offProgress()
    offDone()
  })

  $: if ($settingsOpen) load()
  $: remote = !!settings && !/^https?:\/\/(localhost|127\.0\.0\.1)(:\d+)?\/?$/.test(settings.ollamaURL)
  $: ollamaSelected = !!settings && usesOllama(settings.chatProvider, settings.taskProvider)
  $: models = status?.ollama.models ?? []
  $: chatHint = (settings && status ? modelHint(settings.chatProvider, status) : '') || (settings ? modelErrors[settings.chatProvider] : '') || ''
  $: taskHint = (settings && status ? modelHint(settings.taskProvider, status) : '') || (settings ? modelErrors[settings.taskProvider] : '') || ''

  async function load() {
    try {
      settings = await api.getAISettings()
      prompts = await api.listPrompts()
    } catch (e) {
      toast(errorMessage(e), 'error')
    }
    await refresh()
    await syncModels()
  }

  async function refresh() {
    try {
      status = await api.aiStatus()
    } catch {
      status = null
    }
  }

  // modelsFor loads a provider's models. Ollama's list comes from the
  // already-fetched status; a hosted provider is asked directly, and a
  // failure (e.g. an invalid or expired key) is kept in modelErrors instead
  // of being swallowed, so the dialog can explain the empty dropdown.
  async function modelsFor(provider: ProviderName): Promise<string[]> {
    if (provider === 'ollama') return (status?.ollama.models ?? []).map((m) => m.name)
    if (!status || modelHint(provider, status)) {
      // The backend already explains the empty list (no key, no key store);
      // drop any stale listModels() failure so it doesn't linger on screen.
      if (modelErrors[provider]) {
        const { [provider]: _dropped, ...rest } = modelErrors
        modelErrors = rest
      }
      return []
    }
    try {
      const list = await api.listModels(provider)
      if (modelErrors[provider]) {
        delete modelErrors[provider]
        modelErrors = modelErrors
      }
      return list
    } catch (e) {
      modelErrors = { ...modelErrors, [provider]: errorMessage(e) }
      return []
    }
  }

  // syncModels reloads both providers' model lists and, for each, replaces
  // the selected model with one that is actually on the new list — keeping
  // the current choice when it is still valid, otherwise the list's first
  // model, or "" when the list is empty. It does not save; callers save
  // afterwards once the settings object holds a valid model.
  async function syncModels() {
    if (!settings) return
    chatModels = await modelsFor(settings.chatProvider)
    settings.chatModel = modelForProvider(settings.chatModel, chatModels)
    taskModels = settings.taskProvider === settings.chatProvider ? chatModels : await modelsFor(settings.taskProvider)
    settings.taskModel = modelForProvider(settings.taskModel, taskModels)
  }

  async function save() {
    if (!settings) return
    saving = true
    try {
      await api.saveAISettings(settings)
      await refresh()
    } catch (e) {
      toast(errorMessage(e), 'error')
    } finally {
      saving = false
    }
  }

  // onProviderChange loads the newly chosen provider's models and picks a
  // valid one BEFORE saving, so a provider switch never persists the old
  // provider's model (e.g. an Ollama model name saved under chatProvider:
  // "openai") for the backend to fail on later.
  async function onProviderChange() {
    await syncModels()
    await save()
  }

  async function saveKey(provider: string) {
    try {
      await api.setProviderKey(provider, keyInput[provider])
      await refresh()
      await syncModels()
      // syncModels() can clear a model to "" (its provider's list no longer
      // has the current value); saving that fails validation even though
      // the key operation itself succeeded, so skip it until a model is
      // picked.
      if (settings && settingsHaveModels(settings)) await save()
    } catch (e) {
      toast(errorMessage(e), 'error')
    } finally {
      keyInput[provider] = ''
    }
  }

  async function removeKey(provider: string) {
    try {
      await api.deleteProviderKey(provider)
      await refresh()
      await syncModels()
      if (settings && settingsHaveModels(settings)) await save()
    } catch (e) {
      toast(errorMessage(e), 'error')
    }
  }

  async function startPull(name: string) {
    const model = name.trim()
    if (!model) return
    try {
      await api.pullModel(model)
      pull = { name: model, status: 'starting', completed: 0, total: 0 }
    } catch (e) {
      toast(errorMessage(e), 'error')
    }
  }

  async function reset(name: string) {
    try {
      await api.resetPrompt(name)
      prompts = await api.listPrompts()
      toast(`Restored ${name}.md`)
    } catch (e) {
      toast(errorMessage(e), 'error')
    }
  }

  async function openFolder() {
    try {
      await api.openPromptsFolder()
      prompts = await api.listPrompts()
    } catch (e) {
      toast(errorMessage(e), 'error')
    }
  }

  const close = () => settingsOpen.set(false)
</script>

<svelte:window on:keydown={(e) => $settingsOpen && e.key === 'Escape' && close()} on:focus={() => $settingsOpen && refresh()} />

{#if $settingsOpen && settings}
  <div class="backdrop" on:click|self={close} role="presentation">
    <div class="dialog" role="dialog" aria-label="Settings">
      <header>
        <h3>Settings</h3>
        <button class="icon-btn" title="Close" on:click={close}><Icon name="x" /></button>
      </header>

      <section>
        <h4>Ollama</h4>
        <div class="status">
          {#if status?.ollama.running}
            <span class="dot ok"></span> Running · {models.length} {models.length === 1 ? 'model' : 'models'}
          {:else}
            <span class="dot bad"></span> Not responding — open Ollama or install it from ollama.com
          {/if}
        </div>
        <label>
          <span>URL</span>
          <div class="row">
            <input bind:value={settings.ollamaURL} />
            <button class="btn" disabled={saving} on:click={save}>Test</button>
          </div>
        </label>
        {#if remote}<p class="warn">Diffs will be sent over the network to this host.</p>{/if}
        {#if ollamaSelected}
          <label>
            <span>Chat model</span>
            <select bind:value={settings.chatModel} on:change={save}>
              {#each models as m}
                <option value={m.name}>{m.name} · {formatBytes(m.size)}</option>
              {/each}
              {#if !models.some((m) => m.name === settings?.chatModel)}
                <option value={settings.chatModel}>{settings.chatModel} (not installed)</option>
              {/if}
            </select>
          </label>
          {#if pull}
            <div class="pull">
              <div class="bar"><div style="width: {percent(pull.completed, pull.total)}%"></div></div>
              <span class="hint">
                {pull.name}: {pull.status}{#if pull.total} · {formatBytes(pull.completed)} / {formatBytes(pull.total)}{/if}
              </span>
              <button class="btn" on:click={() => api.cancelPull()}>Cancel</button>
            </div>
          {:else if status?.ollama.running}
            {#if !status.ollama.chatModelInstalled}
              <button class="btn primary" on:click={() => startPull(settings?.chatModel ?? RECOMMENDED)}>
                Download {settings.chatModel}{settings.chatModel === RECOMMENDED ? ' (~4.7 GB)' : ''}
              </button>
            {/if}
            <div class="row">
              <input placeholder="Other model, e.g. llama3.1:8b" bind:value={otherModel} />
              <button class="btn" disabled={!otherModel.trim()} on:click={() => startPull(otherModel)}>Download</button>
            </div>
          {/if}
        {/if}
      </section>

      <section>
        <h4>Chat &amp; agent</h4>
        <label>
          <span>Provider</span>
          <select bind:value={settings.chatProvider} on:change={onProviderChange}>
            {#each PROVIDERS as p}
              <option value={p.value}>{p.label}</option>
            {/each}
          </select>
        </label>
        <label>
          <span>Model</span>
          {#if chatHint}
            <span class="hint">{chatHint}</span>
          {:else}
            <select bind:value={settings.chatModel} on:change={save}>
              {#each chatModels as m}
                <option value={m}>{m}</option>
              {/each}
              {#if !chatModels.includes(settings.chatModel)}
                <option value={settings.chatModel}>{settings.chatModel}</option>
              {/if}
            </select>
          {/if}
        </label>
      </section>

      <section>
        <h4>Explain commit</h4>
        <label>
          <span>Provider</span>
          <select bind:value={settings.taskProvider} on:change={onProviderChange}>
            {#each PROVIDERS as p}
              <option value={p.value}>{p.label}</option>
            {/each}
          </select>
        </label>
        <label>
          <span>Model</span>
          {#if taskHint}
            <span class="hint">{taskHint}</span>
          {:else}
            <select bind:value={settings.taskModel} on:change={save}>
              {#each taskModels as m}
                <option value={m}>{m}</option>
              {/each}
              {#if !taskModels.includes(settings.taskModel)}
                <option value={settings.taskModel}>{settings.taskModel}</option>
              {/if}
            </select>
          {/if}
        </label>
      </section>

      <section>
        <h4>API keys</h4>
        {#each PROVIDERS.filter((p) => needsKey(p.value)) as p}
          {@const st = status?.providers.find((s) => s.provider === p.value)}
          <label class="row">
            <span>{p.label} API key</span>
            {#if st?.hasKey}
              <span class="hint">{st.keyHint}</span>
              <button class="btn" on:click={() => removeKey(p.value)}>Remove</button>
            {:else}
              <input type="password" placeholder="sk-…" bind:value={keyInput[p.value]} />
              <button class="btn primary" disabled={!keyInput[p.value]} on:click={() => saveKey(p.value)}>Save</button>
            {/if}
          </label>
        {/each}
        {#if status?.keyStore}<p class="warn">{status.keyStore}</p>{/if}
      </section>

      <section>
        <h4>Prompts</h4>
        {#each prompts as p}
          <div class="prompt">
            <span class="mono">{p.name}.md</span>
            {#if p.customized}
              <span class="badge">customized</span>
              <button class="btn" on:click={() => reset(p.name)}>Restore default</button>
            {/if}
          </div>
        {/each}
        <button class="btn" on:click={openFolder}>Open prompts folder</button>
      </section>

      <footer>{processingNotice(settings.chatProvider, settings.taskProvider, remote)}</footer>
    </div>
  </div>
{/if}

<style>
  .backdrop { position: fixed; inset: 0; z-index: 40; display: grid; place-items: center; background: rgba(0, 0, 0, 0.25); }
  .dialog { width: 520px; max-height: 86vh; overflow-y: auto; padding: 16px 20px; background: var(--surface); border: 1px solid var(--border); border-radius: 12px; box-shadow: var(--shadow); }
  header { display: flex; align-items: center; justify-content: space-between; }
  h3 { margin: 0; font-size: 15px; font-weight: 600; }
  h4 { margin: 0 0 8px; font-size: 12px; font-weight: 500; color: var(--muted); }
  section { display: flex; flex-direction: column; gap: 8px; padding: 14px 0; border-bottom: 1px solid var(--border); }
  label { display: flex; flex-direction: column; gap: 4px; font-size: 12px; color: var(--muted); }
  label.row { flex-direction: row; align-items: center; }
  .row { display: flex; gap: 6px; }
  .row input { flex: 1; }
  .status { display: flex; align-items: center; gap: 6px; }
  .dot { width: 8px; height: 8px; border-radius: 50%; }
  .dot.ok { background: var(--ok); }
  .dot.bad { background: var(--danger); }
  .warn { margin: 0; font-size: 12px; color: var(--danger); }
  .hint { font-size: 12px; color: var(--muted); }
  .pull { display: flex; align-items: center; gap: 8px; }
  .bar { flex: 1; height: 6px; border-radius: 3px; background: var(--hover); overflow: hidden; }
  .bar div { height: 100%; background: var(--accent); }
  .prompt { display: flex; align-items: center; gap: 8px; }
  .badge { font-size: 11px; padding: 0 6px; border-radius: 4px; background: var(--hover); color: var(--muted); }
  footer { padding-top: 12px; font-size: 12px; color: var(--faint); }
</style>
