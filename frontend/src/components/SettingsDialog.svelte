<script lang="ts">
  import { onDestroy } from 'svelte'
  import { EventsOn } from '../../wailsjs/runtime/runtime'
  import Icon from './Icon.svelte'
  import { api } from '../lib/api'
  import { formatBytes, percent } from '../lib/format'
  import { modelForProvider, modelHint, processingNotice, providerShortLabel, PROVIDERS, settingsHaveModels } from '../lib/providers'
  import { SETTINGS_TABS, isSettingsTab, migrateRememberedTab, tabUsesAI } from '../lib/settingsTabs'
  import { statusText } from '../lib/notifyRules'
  import { AUTO_FETCH_CHOICES, autoFetchMinutes, highContrast, loadAISettings, loadGitSettings, notifyAi, notifyDone, notifyEnabled, notifyProblem, notifyRemote, persisted, settingsOpen, themePref } from '../lib/stores'
  import type { ThemePref } from '../lib/theme'

  const THEMES: { value: ThemePref; label: string }[] = [
    { value: 'auto', label: 'Auto' },
    { value: 'light', label: 'Light' },
    { value: 'dark', label: 'Dark' },
  ]
  import type { AISettings, AIStatus, GitSettings, ModelDone, ModelProgress, ProviderName, PromptInfo } from '../lib/types'
  import { errorMessage, toast } from '../lib/ui'

  const RECOMMENDED = 'qwen2.5:7b'

  // The tab the dialog opens on: the last one used.
  try {
    migrateRememberedTab(localStorage, 'settingsTab')
  } catch {
    // Storage unavailable: the tab falls back to General.
  }
  const tab = persisted('settingsTab', 'general', isSettingsTab)
  // The hosted providers on the Providers tab, each with its API key.
  const HOSTED: ProviderName[] = ['anthropic', 'openai']

  // Read again on every open: the user may have changed it in the system settings.
  let notifyStatus = ''
  $: if ($settingsOpen) api.notificationStatus().then((s) => (notifyStatus = s)).catch(() => (notifyStatus = ''))

  let settings: AISettings | null = null
  // Why the AI settings could not be loaded; the AI tabs show it, General
  // still works.
  let aiError = ''
  let git: GitSettings = { pullStrategy: 'auto' }
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
  $: models = status?.ollama.models ?? []
  // What an installed Ollama model is used for, shown next to it on Providers.
  $: modelUses = (name: string) =>
    settings
      ? [
          ...(settings.chatProvider === 'ollama' && sameModel(name, settings.chatModel) ? ['chat'] : []),
          ...(settings.taskProvider === 'ollama' && sameModel(name, settings.taskModel) ? ['explain commit'] : []),
        ]
      : []
  // Ollama reports "qwen2.5:7b" but a model may be chosen as "llama3" for "llama3:latest".
  const sameModel = (installed: string, chosen: string) => installed === chosen || installed === `${chosen}:latest`
  $: chatHint = (settings && status ? modelHint(settings.chatProvider, status) : '') || (settings ? modelErrors[settings.chatProvider] : '') || ''
  $: taskHint = (settings && status ? modelHint(settings.taskProvider, status) : '') || (settings ? modelErrors[settings.taskProvider] : '') || ''

  // Git and AI settings load separately, so a failure on the AI side (AI
  // disabled, an unreadable settings file) leaves the General tab usable.
  async function load() {
    try {
      git = await api.getGitSettings()
    } catch (e) {
      toast(errorMessage(e), 'error')
    }
    try {
      settings = await api.getAISettings()
      prompts = await api.listPrompts()
      aiError = ''
    } catch (e) {
      aiError = errorMessage(e)
    }
    await refresh()
    await syncModels()
  }

  async function saveGit() {
    try {
      await api.saveGitSettings(git)
      await loadGitSettings() // keeps the store the rest of the app reads in step
    } catch (e) {
      toast(errorMessage(e), 'error')
    }
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
      await loadAISettings()
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

{#if $settingsOpen}
  <div class="backdrop" on:click|self={close} role="presentation">
    <div class="dialog" role="dialog" aria-label="Settings">
      <nav class="tabs" aria-label="Settings sections">
        <h3>Settings</h3>
        {#each SETTINGS_TABS as t}
          <button class="tab" class:active={$tab === t.id} aria-current={$tab === t.id ? 'page' : undefined} on:click={() => tab.set(t.id)}>{t.label}</button>
        {/each}
      </nav>

      <div class="pane">
        <header>
          <h3>{SETTINGS_TABS.find((t) => t.id === $tab)?.label}</h3>
          <button class="icon-btn" title="Close" on:click={close}><Icon name="x" /></button>
        </header>

        <div class="content">
          {#if $tab === 'general'}
            <section>
              <h4>Appearance</h4>
              <div class="row theme">
                <span>Theme</span>
                <div class="segmented" role="radiogroup" aria-label="Theme">
                  {#each THEMES as t}
                    <button type="button" role="radio" aria-checked={$themePref === t.value} class:on={$themePref === t.value} title={t.value === 'auto' ? 'Follow the system setting' : ''} on:click={() => themePref.set(t.value)}>{t.label}</button>
                  {/each}
                </div>
              </div>
              <label class="row check">
                <input type="checkbox" bind:checked={$highContrast} />
                <span>High contrast</span>
              </label>
            </section>

            <section>
              <h4>Fetch</h4>
              <label>
                <span>Fetch in the background</span>
                <select bind:value={$autoFetchMinutes}>
                  {#each AUTO_FETCH_CHOICES as m}
                    <option value={m}>{m === 0 ? 'Off' : `Every ${m} min`}</option>
                  {/each}
                </select>
              </label>
            </section>

            <section>
              <h4>Notifications</h4>
              <label class="row check">
                <input type="checkbox" bind:checked={$notifyEnabled} />
                <span>Show notifications</span>
              </label>
              <label class="row check sub">
                <input type="checkbox" bind:checked={$notifyDone} disabled={!$notifyEnabled} />
                <span>Finished operations (10 s or longer)</span>
              </label>
              <label class="row check sub">
                <input type="checkbox" bind:checked={$notifyAi} disabled={!$notifyEnabled} />
                <span>The AI needs you</span>
              </label>
              <label class="row check sub">
                <input type="checkbox" bind:checked={$notifyProblem} disabled={!$notifyEnabled} />
                <span>Problems — failures and conflicts</span>
              </label>
              <label class="row check sub">
                <input type="checkbox" bind:checked={$notifyRemote} disabled={!$notifyEnabled || $autoFetchMinutes === 0} />
                <span>New commits on the remote</span>
              </label>
              {#if $autoFetchMinutes === 0}<p class="hint remote-off">Turn on Fetch in the background first</p>{/if}
              {#if statusText(notifyStatus)}<p class="hint notify-status">{statusText(notifyStatus)}</p>{/if}
            </section>

            <section>
              <h4>Git</h4>
              <label>
                <span>Pull strategy</span>
                <select bind:value={git.pullStrategy} on:change={saveGit}>
                  <option value="auto">Auto — follow this repository's git config</option>
                  <option value="merge">Always merge</option>
                  <option value="rebase">Always rebase</option>
                </select>
              </label>
            </section>
          {:else if tabUsesAI($tab) && !settings}
            <p class={aiError ? 'warn' : 'hint'}>{aiError || 'Loading…'}</p>
          {:else if settings && $tab === 'providers'}
            {#each HOSTED as provider}
              {@const st = status?.providers.find((s) => s.provider === provider)}
              <section>
                <h4>{providerShortLabel(provider)}</h4>
                <label class="row">
                  <span class="key-label">API key</span>
                  {#if st?.hasKey}
                    <span class="hint">{st.keyHint}</span>
                    <button class="btn" on:click={() => removeKey(provider)}>Remove</button>
                  {:else}
                    <input type="password" placeholder="sk-…" bind:value={keyInput[provider]} />
                    <button class="btn primary" disabled={!keyInput[provider]} on:click={() => saveKey(provider)}>Save</button>
                  {/if}
                </label>
              </section>
            {/each}
            {#if status?.keyStore}<p class="warn">{status.keyStore}</p>{/if}

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
              {#if models.length}
                <ul class="installed" aria-label="Installed models">
                  {#each models as m}
                    <li>
                      <span class="mono">{m.name}</span>
                      <span class="hint">{formatBytes(m.size)}</span>
                      {#each modelUses(m.name) as use}<span class="badge">{use}</span>{/each}
                    </li>
                  {/each}
                </ul>
              {/if}
              {#if pull}
                <div class="pull">
                  <div class="bar"><div style="width: {percent(pull.completed, pull.total)}%"></div></div>
                  <span class="hint">
                    {pull.name}: {pull.status}{#if pull.total} · {formatBytes(pull.completed)} / {formatBytes(pull.total)}{/if}
                  </span>
                  <button class="btn" on:click={() => api.cancelPull()}>Cancel</button>
                </div>
              {:else if status?.ollama.running}
                {#if settings.chatProvider === 'ollama' && !status.ollama.chatModelInstalled}
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

            <p class="notice">{processingNotice(settings.chatProvider, settings.taskProvider, remote)}</p>
          {:else if settings && $tab === 'models'}
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
              <label>
                <span>Commit message</span>
                <select bind:value={settings.commitMessage} on:change={save}>
                  <option value="auto-local">Automatic for local models (default)</option>
                  <option value="auto">Always automatic</option>
                  <option value="manual">Only when I ask</option>
                </select>
              </label>
              <label>
                <span>Suggested replies</span>
                <select bind:value={settings.suggestReplies} on:change={save}>
                  <option value="auto-local">Automatic for local models (default)</option>
                  <option value="auto">Always</option>
                  <option value="off">Off</option>
                </select>
              </label>
            </section>

            <p class="notice">{processingNotice(settings.chatProvider, settings.taskProvider, remote)}</p>
          {:else if settings && $tab === 'prompts'}
            <section>
              {#each prompts as p}
                <div class="prompt">
                  <span class="mono">{p.name}.md</span>
                  {#if p.customized}
                    <span class="badge">customized</span>
                    <button class="btn" on:click={() => reset(p.name)}>Restore default</button>
                  {/if}
                </div>
              {/each}
              <div><button class="btn" on:click={openFolder}>Open prompts folder</button></div>
              <p class="hint">Repositories can add their own instructions in Repository settings → AI.</p>
            </section>
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
    height: min(520px, 86vh);
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
  h4 { margin: 0 0 8px; font-size: 12px; font-weight: 500; color: var(--muted); }
  section { display: flex; flex-direction: column; gap: 8px; padding: 14px 0; border-bottom: 1px solid var(--border); }
  section:last-of-type { border-bottom: 0; }
  label { display: flex; flex-direction: column; gap: 4px; font-size: 12px; color: var(--muted); }
  label.row { flex-direction: row; align-items: center; }
  label.check { gap: 6px; color: var(--text); font-size: 13px; }
  .key-label { flex: none; width: 130px; }
  .row { display: flex; gap: 6px; }
  .theme { align-items: center; justify-content: space-between; color: var(--text); font-size: 13px; }
  .segmented { display: inline-flex; padding: 2px; gap: 2px; border: 1px solid var(--border); border-radius: 8px; background: var(--bg); }
  .segmented button { padding: 3px 12px; border-radius: 6px; color: var(--muted); }
  .segmented button:hover:not(.on) { background: var(--hover); color: var(--text); }
  .segmented button.on { background: var(--surface); color: var(--text); box-shadow: 0 0 0 1px var(--border); }
  .row input { flex: 1; }
  .row input[type='checkbox'] { flex: none; }
  .status { display: flex; align-items: center; gap: 6px; }
  .installed { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 4px; }
  .installed li { display: flex; align-items: center; gap: 8px; }
  .dot { width: 8px; height: 8px; border-radius: 50%; }
  .dot.ok { background: var(--ok); }
  .dot.bad { background: var(--danger); }
  .warn { margin: 0; font-size: 12px; color: var(--danger); }
  .hint { font-size: 12px; color: var(--muted); }
  p.hint, p.warn { padding: 14px 0; }
.check.sub { padding-left: 22px; }
p.hint.notify-status { padding: 4px 0 0; }
  p.hint.remote-off { padding: 0 0 0 44px; }
  .pull { display: flex; align-items: center; gap: 8px; }
  .bar { flex: 1; height: 6px; border-radius: 3px; background: var(--hover); overflow: hidden; }
  .bar div { height: 100%; background: var(--accent); }
  .prompt { display: flex; align-items: center; gap: 8px; }
  .badge { font-size: 11px; padding: 0 6px; border-radius: 4px; background: var(--hover); color: var(--muted); }
  .notice { margin: 0; padding-top: 12px; font-size: 12px; color: var(--faint); }
</style>
