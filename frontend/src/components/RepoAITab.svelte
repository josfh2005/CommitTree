<script lang="ts">
  import { onDestroy, onMount } from 'svelte'
  import { EventsOn } from '../../wailsjs/runtime/runtime'
  import { api } from '../lib/api'
  import {
    ACTIONS, COMMIT_MODES, REPLY_MODES, actionLabel, chooseProvider, globalLabel, globalModeLabel,
    mergeDrafts, providerModels, stateText, withOverride,
  } from '../lib/repoAI'
  import { PROVIDERS, needsKey } from '../lib/providers'
  import type { AIStatus, ProviderName, RepoAIInfo, RepoAIOverride } from '../lib/types'
  import { errorMessage } from '../lib/ui'

  export let repoID: string

  type Role = 'chat' | 'task'
  const ROLES: { role: Role; title: string }[] = [
    { role: 'chat', title: 'Chat & agent' },
    { role: 'task', title: 'Tasks' },
  ]

  let info: RepoAIInfo | null = null
  let status: AIStatus | null = null
  let listed: Partial<Record<ProviderName, string[]>> = {}
  let listErrors: Partial<Record<ProviderName, string>> = {}
  let error = ''
  let adding = ''
  let added: string[] = []              // action boxes opened but still empty
  let open: Record<string, boolean> = {} // foldable instruction files
  let drafts: Record<string, string> = {} // text of the instruction boxes, by action ('all' for every action)
  let focused = ''                       // box being typed in; a reload leaves its text alone
  let loadedID = ''
  let seq = 0
  let off: (() => void) | undefined

  $: load(repoID)
  $: o = info?.overrides ?? {}
  $: g = info?.global
  $: aiOff = !!info?.aiOff
  $: shownActions = ACTIONS.filter((a) => o.instructions?.[a] !== undefined || added.includes(a))

  const provider = (role: Role): ProviderName | '' => (role === 'chat' ? o.chatProvider : o.taskProvider) ?? ''
  const model = (role: Role) => (role === 'chat' ? o.chatModel : o.taskModel) ?? ''
  const globalProvider = (role: Role) => (role === 'chat' ? g?.chatProvider : g?.taskProvider)
  const globalModel = (role: Role) => (role === 'chat' ? g?.chatModel : g?.taskModel) ?? ''
  $: modelsOf = (p: ProviderName) => providerModels(p, status, listed, listErrors)

  async function load(id: string) {
    const mine = ++seq
    if (id !== loadedID) {
      loadedID = id
      info = null; added = []; open = {}; drafts = {}; focused = ''; adding = ''
    }
    error = ''
    try {
      const next = await api.getRepoAISettings(id)
      if (mine !== seq) return
      info = next
      drafts = mergeDrafts(next.overrides.instructions, drafts, focused)
      if (next.error) error = next.error
      status = await api.aiStatus().catch(() => null)
      if (mine !== seq) return
      for (const { value } of PROVIDERS) {
        if (!needsKey(value) || !status || providerModels(value, status, {}).hint) continue
        api.listModels(value)
          .then((l) => { listed = { ...listed, [value]: l }; const { [value]: _gone, ...rest } = listErrors; listErrors = rest })
          .catch((e) => { listErrors = { ...listErrors, [value]: errorMessage(e) } })
      }
    } catch (e) {
      if (mine === seq) error = errorMessage(e)
    }
  }

  async function save(patch: Partial<RepoAIOverride>) {
    if (!info) return
    const next = withOverride(o, patch)
    try {
      await api.saveRepoAISettings(repoID, next)
      error = ''
      await load(repoID)
    } catch (e) {
      error = errorMessage(e)
      info = { ...info, overrides: next } // keep what was entered
    }
  }

  function setProvider(role: Role, el: HTMLSelectElement) {
    const p = el.value as ProviderName | ''
    const { models, hint } = p ? modelsOf(p) : { models: [], hint: '' }
    const patch = chooseProvider(role, p, models)
    if (!patch) {
      // Nothing to pair the provider with; show why and put the select back.
      error = hint || `No models are available for ${PROVIDERS.find((x) => x.value === p)?.label ?? p}`
      el.value = provider(role)
      return
    }
    save(patch)
  }
  const setModel = (role: Role, m: string) => save(role === 'chat' ? { chatModel: m } : { taskModel: m })

  function commitInstruction(key: string) {
    focused = ''
    const text = drafts[key] ?? ''
    if (text === (o.instructions?.[key] ?? '')) return
    save({ instructions: { ...(o.instructions ?? {}), [key]: text } })
  }
  function addAction() {
    if (!adding) return
    if (!added.includes(adding)) added = [...added, adding]
    adding = ''
  }
  function removeAction(a: string) {
    added = added.filter((x) => x !== a)
    const { [a]: _gone, ...rest } = drafts
    drafts = rest
    const kept = { ...(o.instructions ?? {}) }
    delete kept[a]
    save({ instructions: kept })
  }

  // A refused Approve/Ignore (the files changed since they were read) leaves
  // the tab showing the new content, so both reload either way.
  async function decide(run: (id: string, hash: string) => Promise<void>) {
    if (!info) return
    try { await run(repoID, info.repoInstructions.hash); error = '' } catch (e) { error = errorMessage(e) }
    const shown = error
    await load(repoID)
    if (shown) error = shown
  }
  const approve = () => decide(api.approveRepoInstructions)
  const ignore = () => decide(api.ignoreRepoInstructions)

  // The payload is ignored on purpose: a worktree and its main repository share
  // their AI settings, so the event may name another repository's id.
  onMount(() => { off = EventsOn('repo-ai:changed', () => load(repoID)) })
  onDestroy(() => off?.())
</script>

{#if error}<p class="warn">{error}</p>{/if}
{#if info}
  <label class="check">
    <input type="checkbox" checked={!aiOff} disabled={!!info.error} on:change={(e) => save({ aiOff: !e.currentTarget.checked })} />
    <span>Use AI in this repository</span>
  </label>

  <div class="body" class:dim={aiOff}>
    <section>
      <h4>Models</h4>
      {#each ROLES as { role, title }}
        {@const p = provider(role)}
        {@const list = p ? modelsOf(p) : { models: [], hint: '' }}
        <div class="pair">
          <span class="name">{title}</span>
          <select aria-label="{title} provider" disabled={aiOff} value={p} on:change={(e) => setProvider(role, e.currentTarget)}>
            <option value="">{g ? globalLabel(globalProvider(role) ?? 'ollama', globalModel(role)) : 'Global'}</option>
            {#each PROVIDERS as pr}<option value={pr.value}>{pr.label}</option>{/each}
          </select>
          {#if p}
            <select aria-label="{title} model" disabled={aiOff} value={model(role)} on:change={(e) => setModel(role, e.currentTarget.value)}>
              {#each list.models as m}<option value={m}>{m}</option>{/each}
              {#if !list.models.includes(model(role))}<option value={model(role)}>{model(role)}</option>{/if}
            </select>
          {/if}
        </div>
        {#if p && list.hint}<p class="hint indent">{list.hint}</p>{/if}
      {/each}
    </section>

    <section>
      <h4>Automation</h4>
      <label class="pair"><span class="name">Commit message</span>
        <select disabled={aiOff} value={o.commitMessage ?? ''} on:change={(e) => save({ commitMessage: e.currentTarget.value })}>
          <option value="">{globalModeLabel(COMMIT_MODES, g?.commitMessage)}</option>
          {#each COMMIT_MODES as m}<option value={m.value}>{m.label}</option>{/each}
        </select>
      </label>
      <label class="pair"><span class="name">Suggested replies</span>
        <select disabled={aiOff} value={o.suggestReplies ?? ''} on:change={(e) => save({ suggestReplies: e.currentTarget.value })}>
          <option value="">{globalModeLabel(REPLY_MODES, g?.suggestReplies)}</option>
          {#each REPLY_MODES as m}<option value={m.value}>{m.label}</option>{/each}
        </select>
      </label>
    </section>

    <section>
      <h4>Your instructions</h4>
      <p class="hint">Private: they stay on this computer.</p>
      <label class="block"><span>For every action</span>
        <textarea rows="3" disabled={aiOff} value={drafts.all ?? ''}
          on:input={(e) => (drafts = { ...drafts, all: e.currentTarget.value })}
          on:focus={() => (focused = 'all')} on:blur={() => commitInstruction('all')}></textarea>
      </label>
      {#each shownActions as a (a)}
        <div class="block">
          <div class="head"><span>{actionLabel(a)}</span><button class="link" disabled={aiOff} on:click={() => removeAction(a)}>Remove</button></div>
          <textarea rows="3" aria-label="{actionLabel(a)} instructions" disabled={aiOff} value={drafts[a] ?? ''}
            on:input={(e) => (drafts = { ...drafts, [a]: e.currentTarget.value })}
            on:focus={() => (focused = a)} on:blur={() => commitInstruction(a)}></textarea>
        </div>
      {/each}
      {#if ACTIONS.some((a) => !shownActions.includes(a))}
        <div class="pair">
          <select aria-label="Add instructions for an action" disabled={aiOff} bind:value={adding} on:change={addAction}>
            <option value="">Add instructions for…</option>
            {#each ACTIONS.filter((a) => !shownActions.includes(a)) as a}<option value={a}>{actionLabel(a)}</option>{/each}
          </select>
        </div>
      {/if}
    </section>

    <section>
      <h4>Repository instructions</h4>
      {#if info.repoInstructions.error}<p class="warn">{info.repoInstructions.error}</p>{/if}
      {#if info.repoInstructions.files.length === 0}
        <p class="hint">This repository has no shared instructions. Add <code>.committree/instructions.md</code> to share them with your team.</p>
      {:else}
        <div class="state">
          <strong>{stateText(info.state)}</strong>
          {#if info.state === 'pending' || info.state === 'changed'}
            <button class="btn primary" disabled={aiOff} on:click={approve}>Approve</button>
            <button class="btn" disabled={aiOff} on:click={ignore}>Ignore</button>
          {:else if info.state === 'ignored'}
            <button class="btn" disabled={aiOff} on:click={approve}>Approve</button>
          {/if}
        </div>
        <ul class="files">
          {#each info.repoInstructions.files as f (f.name)}
            <li>
              <button class="link" aria-expanded={!!open[f.name]} on:click={() => (open = { ...open, [f.name]: !open[f.name] })}>{open[f.name] ? '▾' : '▸'} .committree/{f.name}</button>
              {#if f.ignored}<span class="hint"> — ignored: {f.ignored}</span>{/if}
              {#if open[f.name] && f.text}<pre>{f.text}</pre>{/if}
            </li>
          {/each}
        </ul>
      {/if}
    </section>
  </div>
{/if}

<style>
  .body.dim { opacity: 0.5; }
  section { padding: 12px 0; border-bottom: 1px solid var(--border); }
  section:last-child { border-bottom: 0; }
  h4 { margin: 0 0 8px; font-size: 12px; font-weight: 500; color: var(--muted); }
  .check { display: flex; align-items: center; gap: 6px; padding: 8px 0 0; font-size: 13px; }
  .pair { display: flex; gap: 8px; align-items: center; margin: 6px 0; font-size: 12px; }
  .pair .name { flex: none; width: 130px; color: var(--muted); }
  .pair select { min-width: 0; max-width: 260px; }
  .block { display: flex; flex-direction: column; gap: 4px; margin: 6px 0; font-size: 12px; color: var(--muted); }
  .block textarea { width: 100%; box-sizing: border-box; resize: vertical; }
  .head { display: flex; align-items: center; justify-content: space-between; }
  .state { display: flex; gap: 8px; align-items: center; margin: 6px 0; font-size: 13px; }
  .files { list-style: none; padding: 0; margin: 0; font-size: 12px; }
  .files li { margin: 4px 0; }
  .files pre { margin-top: 4px; background: var(--bg); border: 1px solid var(--border); padding: 8px; border-radius: 6px; max-height: 200px; overflow: auto; font-family: var(--mono); }
  .link { color: var(--accent); cursor: pointer; }
  .link:disabled { color: var(--faint); cursor: default; }
  .warn { margin: 8px 0; font-size: 12px; color: var(--danger); }
  .hint { margin: 4px 0; font-size: 12px; color: var(--muted); }
  .hint.indent { padding-left: 138px; }
</style>
