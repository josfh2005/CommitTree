<script lang="ts">
  import { onDestroy } from 'svelte'
  import { EventsOn } from '../../wailsjs/runtime/runtime'
  import Icon from './Icon.svelte'
  import { api } from '../lib/api'
  import { formatBytes, percent } from '../lib/format'
  import { settingsOpen } from '../lib/stores'
  import type { AISettings, AIStatus, ModelDone, ModelProgress, PromptInfo } from '../lib/types'
  import { errorMessage, toast } from '../lib/ui'

  const RECOMMENDED = 'qwen2.5:7b'
  const appleReasons: Record<string, string> = {
    deviceNotEligible: 'This Mac does not support Apple Intelligence.',
    appleIntelligenceNotEnabled: 'Turn on Apple Intelligence in System Settings.',
    modelNotReady: 'The Apple Intelligence model is still downloading.',
    helperNotFound: 'Helper not found. Build the app with "make build".',
    helperFailed: 'The Apple Intelligence helper failed to start.',
    unknown: 'Apple Intelligence is unavailable.',
  }

  let settings: AISettings | null = null
  let status: AIStatus | null = null
  let prompts: PromptInfo[] = []
  let pull: ModelProgress | null = null
  let otherModel = ''
  let saving = false

  const offProgress = EventsOn('model:progress', (p: ModelProgress) => (pull = p))
  const offDone = EventsOn('model:done', async (p: ModelDone) => {
    pull = null
    if (p.error && !p.error.includes('context canceled')) toast(p.error, 'error')
    else if (!p.error) toast(`Downloaded ${p.name}`)
    await refresh()
  })
  onDestroy(() => {
    offProgress()
    offDone()
  })

  $: if ($settingsOpen) load()
  $: remote = !!settings && !/^https?:\/\/(localhost|127\.0\.0\.1)(:\d+)?\/?$/.test(settings.ollamaURL)
  $: models = status?.ollama.models ?? []
  $: appleText = status?.apple.available ? 'Available on this Mac' : appleReasons[status?.apple.reason ?? 'unknown'] ?? appleReasons.unknown

  async function load() {
    try {
      settings = await api.getAISettings()
      prompts = await api.listPrompts()
    } catch (e) {
      toast(errorMessage(e), 'error')
    }
    await refresh()
  }

  async function refresh() {
    try {
      status = await api.aiStatus()
    } catch {
      status = null
    }
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
      </section>

      <section>
        <h4>Explain commit</h4>
        <label class="radio">
          <input type="radio" bind:group={settings.taskProvider} value="apple" on:change={save} />
          <span>Apple Intelligence <span class="hint">· {appleText}</span></span>
        </label>
        <label class="radio">
          <input type="radio" bind:group={settings.taskProvider} value="ollama" on:change={save} />
          <span>Ollama</span>
        </label>
        {#if settings.taskProvider === 'ollama'}
          <select bind:value={settings.taskModel} on:change={save}>
            {#each models as m}
              <option value={m.name}>{m.name}</option>
            {/each}
            {#if !models.some((m) => m.name === settings?.taskModel)}
              <option value={settings.taskModel}>{settings.taskModel} (not installed)</option>
            {/if}
          </select>
        {/if}
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

      <footer>Everything is processed on this Mac.</footer>
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
  label.radio { flex-direction: row; align-items: center; gap: 8px; font-size: 13px; color: var(--text); }
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
