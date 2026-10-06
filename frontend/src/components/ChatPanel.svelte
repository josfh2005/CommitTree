<script lang="ts">
  import { onDestroy, tick } from 'svelte'
  import { EventsOn } from '../../wailsjs/runtime/runtime'
  import Icon from './Icon.svelte'
  import ModelPicker from './ModelPicker.svelte'
  import { api } from '../lib/api'
  import { answeredBy, appliedText, applyEvent, conversationTokens, tokensText, tokensTitle, choiceState, decisionCard, parts, CHAT_EVENTS, isWriteTool, confirmResultText, emptyChat, errorText, fromMessages, shouldReloadChat, startRun, toolLabel, withConfirmDecision, withPendingConfirm, pendingCards, type ChatState } from '../lib/chat'
  import { relativeDate } from '../lib/format'
  import { renderMarkdown } from '../lib/markdown'
  import { chatBlocker } from '../lib/providers'
  import { aiOff, aiSettings, chatOpen, chatPreparing, jumpTo, repoAI, repoSettings, selectedRepo, settingsOpen } from '../lib/stores'
  import { showsInstructionsStrip } from '../lib/repoAI'
  import type { AIStatus } from '../lib/types'
  import { copyText, errorMessage, toast } from '../lib/ui'
  import DecisionTray from './DecisionTray.svelte'

  let state: ChatState = emptyChat('')
  let status: AIStatus | null = null
  let input = ''
  let list: HTMLDivElement
  let loadError = ''
  // A click on a pending card's line in the conversation; DecisionTray shows it.
  let focus: { id: string } | null = null

  const offs = CHAT_EVENTS.map((name) =>
    EventsOn(name, (payload) => {
      const next = applyEvent(state, name, payload)
      if (next !== state) {
        // Follow the answer only when already at the bottom, so reading
        // earlier calls while it runs isn't yanked away by each new one.
        const follow = atBottom()
        state = next
        if (follow) scrollDown()
      }
    }),
  )
  onDestroy(() => offs.forEach((off) => off()))

  // Ticks the answers' "2m ago" along.
  let now = new Date()
  const clock = setInterval(() => (now = new Date()), 60_000)
  onDestroy(() => clearInterval(clock))

  $: load($selectedRepo?.id ?? '')
  // An explanation takes the chat while its context is still being read,
  // before its chat:start; show it as running then too, so Stop works and
  // a message typed meanwhile is not refused as busy.
  $: preparing = state.runID === null && !!state.repoID && $chatPreparing.includes(state.repoID)
  $: running = state.runID !== null || preparing
  $: tokens = conversationTokens(state)
  $: pending = pendingCards(state)
  // Settings may have changed the chat provider, a key or the installed
  // models; read the status again once it closes.
  $: if (!$settingsOpen) refreshStatus()
  // Before the settings load, assume the default provider, Ollama.
  $: blocker = status ? chatBlocker($aiSettings?.chatProvider ?? 'ollama', status, $aiSettings?.chatModel) : null
  $: ready = !!status && !blocker

  async function load(repoID: string) {
    refreshStatus()
    if (!shouldReloadChat(state, repoID)) return
    state = emptyChat(repoID)
    loadError = ''
    if (!repoID) return
    try {
      const messages = await api.getChat(repoID)
      if (state.repoID === repoID && state.runID === null) {
        state = fromMessages(repoID, messages)
        scrollDown()
      }
      const confirm = await api.getChatConfirm(repoID)
      if (confirm && state.repoID === repoID) {
        state = withPendingConfirm(state, confirm)
        scrollDown()
      }
    } catch (e) {
      loadError = errorMessage(e)
    }
  }

  async function decide(id: string, approve: boolean) {
    if (!state.repoID) return
    state = withConfirmDecision(state, id, approve ? 'approved' : 'rejecting')
    try {
      await api.confirmChatAction(state.repoID, id, approve)
    } catch (e) {
      // Only a failed confirmChatAction re-enables the buttons: on success
      // the card stays disabled, showing "Running…"/"Rejecting…", until
      // chat:tool_result resolves it — ConfirmChatAction returning is not
      // the same as the write finishing.
      state = withConfirmDecision(state, id, 'pending')
      toast(errorMessage(e), 'error')
    }
  }

  async function refreshStatus() {
    try {
      status = await api.aiStatus()
    } catch {
      status = null
    }
  }

  function send() {
    const text = input.trim()
    if (text && sendText(text)) input = ''
  }

  // sendText starts an answer to text — typed or a suggested reply; false
  // when the chat can't take it.
  function sendText(text: string): boolean {
    if (!text || running || !state.repoID || !ready) return false
    const runID = crypto.randomUUID()
    const repoID = state.repoID
    state = startRun(state, text, runID)
    scrollDown()
    api.sendChat(repoID, text, runID).catch((e) => {
      state = applyEvent(state, 'chat:error', { repoID, runID, message: errorMessage(e), code: 'other' })
    })
    return true
  }

  function stop() {
    if (state.repoID) api.stopChat(state.repoID).catch(() => {})
  }

  async function clear() {
    if (!state.repoID || running) return
    try {
      await api.clearChat(state.repoID)
      state = emptyChat(state.repoID)
    } catch (e) {
      toast(errorMessage(e), 'error')
    }
  }

  function onKey(e: KeyboardEvent) {
    if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
      e.preventDefault()
      send()
    }
  }

  function onClick(e: MouseEvent) {
    const link = (e.target as HTMLElement).closest('a[data-hash]') as HTMLElement | null
    if (!link) return
    e.preventDefault()
    jumpTo.set(link.dataset.hash ?? '')
  }

  function atBottom(): boolean {
    return !list || list.scrollHeight - list.scrollTop - list.clientHeight < 48
  }

  async function scrollDown() {
    await tick()
    if (list) list.scrollTop = list.scrollHeight
  }
</script>

<svelte:window on:focus={refreshStatus} />

<div class="chat">
  <header class="drag">
    <span class="title">Chat</span>
    <span class="spacer"></span>
    <button class="icon-btn" title="New chat" disabled={running || state.items.length === 0} on:click={clear}><Icon name="plus" /></button>
    <button class="icon-btn" title="Hide chat" on:click={() => chatOpen.set(false)}><Icon name="panel-right" /></button>
  </header>

  <div class="messages" bind:this={list} on:click={onClick} role="presentation">
    {#if !$selectedRepo}
      <div class="empty"><Icon name="sparkle" size={22} /><p>Select a repository to chat about it.</p></div>
    {:else if $aiOff}
      <div class="notice">
        <strong>AI is off for this repository</strong>
        {#if $repoAI?.error}<p>{$repoAI.error}</p>{/if}
        <div class="actions"><button class="btn" on:click={() => repoSettings.set({ repoID: $selectedRepo.id, tab: 'ai' })}>Repository settings</button></div>
      </div>
    {:else if blocker?.kind === 'ollama_down'}
      <div class="notice">
        <strong>Ollama is not running</strong>
        <p>Open Ollama or install it from ollama.com.</p>
        <div class="actions">
          <button class="btn" on:click={refreshStatus}>Retry</button>
          <button class="btn" on:click={() => settingsOpen.set(true)}>Settings</button>
        </div>
      </div>
    {:else if blocker?.kind === 'model_missing'}
      <div class="notice">
        <strong>Model {blocker.model} is not installed</strong>
        <p>Download it from Settings to start chatting.</p>
        <div class="actions"><button class="btn primary" on:click={() => settingsOpen.set(true)}>Open Settings</button></div>
      </div>
    {:else if blocker?.kind === 'no_key'}
      <div class="notice">
        <strong>The chat provider has no API key</strong>
        <p>{blocker.message}</p>
        <div class="actions"><button class="btn primary" on:click={() => settingsOpen.set(true)}>Open Settings</button></div>
      </div>
    {:else if loadError}
      <div class="notice"><strong>Couldn't load the conversation</strong><p>{loadError}</p></div>
    {:else if state.items.length === 0}
      <div class="empty">
        <Icon name="sparkle" size={22} />
        <p>Ask anything about {$selectedRepo.name}.</p>
        <p class="hint">For example: "What changed this week on develop?"</p>
      </div>
    {/if}

    {#if !$aiOff}
    {#each state.items as item, i}
      {#if item.role === 'user'}
        <div class="msg user">{item.text}</div>
      {:else}
        <div class="msg assistant">
          {#each parts(item) as part}
            {#if part.kind === 'text'}
              <div class="md">{@html renderMarkdown(part.text)}</div>
            {:else if part.kind === 'notice'}
              <div class="notice">{part.text}</div>
            {:else}
              {@const tool = part.tool}
              {@const card = decisionCard(tool)}
              {@const cardState = card && tool.id ? choiceState(tool) : null}
              {#if card && tool.id && cardState}
                {@const id = tool.id}
                {#if cardState === 'pending'}
                  <button class="decision-line" title="Show in pending decisions" on:click={() => (focus = { id })}>
                    ◆ Decision pending: {card.path} · region {card.region} — {card.question}
                  </button>
                {:else}
                  <div class="confirm decision">
                    <div class="confirm-title">{card.path} · region {card.region}</div>
                    <div class="decision-question">{card.question}</div>
                    <div class="confirm-result">{cardState === 'chosen' ? tool.summary : 'Settled another way'}</div>
                  </div>
                {/if}
              {:else if tool.confirm}
                {@const confirm = tool.confirm}
                <div class="confirm">
                  <div class="confirm-title">{confirm.title}</div>
                  {#each confirm.details as line}<div class="confirm-detail">{line}</div>{/each}
                  {#if confirm.state === 'pending'}
                    <div class="confirm-actions">
                      <button class="btn" on:click={() => decide(confirm.id, false)}>Reject</button>
                      <button class="btn primary" on:click={() => decide(confirm.id, true)}>Approve</button>
                    </div>
                  {:else if confirm.state === 'approved'}
                    <div class="confirm-result">Running…</div>
                  {:else if confirm.state === 'rejecting'}
                    <div class="confirm-result">Rejecting…</div>
                  {:else if confirm.state === 'failed'}
                    <div class="confirm-result failed">{confirmResultText(tool)}</div>
                  {:else}
                    <div class="confirm-result">{confirmResultText(tool)}</div>
                  {/if}
                </div>
              {:else}
                <div class="tool" title={JSON.stringify(tool.args ?? {})}>
                  <Icon name={isWriteTool(tool.name) ? 'pencil' : 'search'} size={12} />
                  <span class="ellipsis">{toolLabel(tool)}</span>
                  {#if tool.summary}<span class="summary ellipsis">· {tool.summary}</span>{/if}
                </div>
                {@const applied = appliedText(tool)}
                {#if applied !== null}
                  <details class="applied" open>
                    <summary>Written to the file</summary>
                    <pre>{applied}</pre>
                  </details>
                {/if}
              {/if}
            {/if}
          {/each}
          {#if running && i === state.items.length - 1 && !item.error && parts(item).at(-1)?.kind !== 'text'}
            <div class="typing">Thinking…</div>
          {/if}
          {#if item.stopped}<div class="note">Stopped</div>{/if}
          {#if item.error}<div class="error">{errorText(item.error)}</div>{/if}
          {#if item.text && !(running && i === state.items.length - 1)}
            <div class="answer-actions">
              <button class="icon-btn" title="Copy" on:click={() => copyText(item.text)}><Icon name="copy" size={13} /></button>
              {#if item.at}<span title={new Date(item.at).toLocaleString()}>{relativeDate(item.at, now)}</span>{/if}
              {#if answeredBy(item)}<span class="ellipsis">{answeredBy(item)}</span>{/if}
              {#if item.usage}<span class="tokens" title={tokensTitle(item.usage)}>{tokensText(item.usage)}</span>{/if}
            </div>
          {/if}
        </div>
      {/if}
    {/each}
    {#if preparing}<div class="typing">Preparing the explanation…</div>{/if}
    {/if}
  </div>

  {#if !$aiOff}
  <div class="composer">
    <DecisionTray repoID={state.repoID} cards={pending} {running} {focus} />
    {#if !running && state.suggestions?.length}
      <div class="suggestions">
        {#each state.suggestions as reply}
          <button class="suggestion" title="Send “{reply}”" disabled={!ready} on:click={() => sendText(reply)}>{reply}</button>
        {/each}
      </div>
    {/if}
    {#if showsInstructionsStrip($repoAI) && $selectedRepo}
      <div class="strip">
        This repository has instructions for the AI.
        <button class="link" on:click={() => repoSettings.set({ repoID: $selectedRepo.id, tab: 'ai' })}>Review</button>
      </div>
    {/if}
    <textarea
      rows="2"
      placeholder={ready ? 'Ask about this repo…' : 'Set up the AI provider to chat'}
      bind:value={input}
      on:keydown={onKey}
      disabled={!$selectedRepo || !ready}
    ></textarea>
    <div class="composer-bar">
      <ModelPicker {status} disabled={running} onChange={refreshStatus} />
      {#if tokens}
        <span class="tokens" title={`This conversation: ${tokensTitle(tokens.total)}${tokens.missing ? '\nSome answers have no count' : ''}`}>{tokensText(tokens.total)}</span>
      {/if}
      <span class="spacer"></span>
      {#if running}
        <button class="icon-btn" title="Stop" on:click={stop}><Icon name="stop" /></button>
      {:else}
        <button class="icon-btn" title="Send" disabled={!input.trim() || !ready} on:click={send}><Icon name="send" /></button>
      {/if}
    </div>
  </div>
  {/if}
</div>

<style>
  .chat { display: flex; flex-direction: column; height: 100%; }
  header { display: flex; align-items: center; gap: 2px; height: 44px; padding: 0 10px 0 16px; flex: none; }
  .title { font-weight: 500; }
  .spacer { flex: 1; }
  .messages { flex: 1; min-height: 0; overflow-y: auto; display: flex; flex-direction: column; gap: 14px; padding: 8px 16px; }
  .empty { flex: 1; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 4px; text-align: center; color: var(--muted); }
  .empty p { margin: 0; }
  .hint { font-size: 12px; color: var(--faint); }
  .notice { padding: 12px; border: 1px solid var(--border); border-radius: 10px; background: var(--surface); }
  .notice p { margin: 4px 0 8px; color: var(--muted); }
  .actions { display: flex; gap: 6px; }
  .msg { max-width: 100%; -webkit-user-select: text; user-select: text; line-height: 1.5; }
  .user { align-self: flex-end; max-width: 85%; padding: 8px 12px; border-radius: 12px; background: var(--active); white-space: pre-wrap; }
  .applied { margin: -2px 0 6px; font-size: 12px; color: var(--muted); }
  .applied summary { cursor: pointer; padding: 0 8px; }
  .applied pre { margin: 4px 0 0; padding: 6px 8px; max-height: 240px; overflow: auto; border: 1px solid var(--border); border-radius: 6px; background: var(--bg); color: var(--text); font-size: 11px; white-space: pre; }
  .tool { display: flex; align-items: center; gap: 6px; max-width: 100%; margin-bottom: 4px; padding: 2px 8px; border-radius: 6px; background: var(--hover); color: var(--muted); font-size: 12px; }
  .summary { color: var(--faint); }
  .confirm { margin-bottom: 6px; padding: 10px; border: 1px solid var(--border); border-radius: 10px; background: var(--surface); }
  .confirm-title { font-weight: 500; }
  .confirm-detail { margin-top: 4px; font-family: var(--mono); font-size: 12px; color: var(--muted); }
  .confirm-actions { display: flex; justify-content: flex-end; gap: 6px; margin-top: 8px; }
  .confirm-result { margin-top: 6px; font-size: 12px; color: var(--muted); }
  .confirm-result.failed { color: var(--danger); }
  .decision-question { margin: 4px 0 2px; }
  .decision-line { display: block; width: 100%; margin-bottom: 6px; padding: 6px 10px; border: 1px dashed var(--border); border-radius: 8px; background: var(--surface); color: var(--text); font-size: 12px; text-align: left; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .decision-line:hover { background: var(--hover); }
  .md :global(p) { margin: 0 0 6px; }
  .md :global(ul) { margin: 4px 0 6px; padding-left: 18px; }
  .md :global(code) { font-family: var(--mono); font-size: 12px; padding: 0 4px; border-radius: 4px; background: var(--hover); }
  .md :global(pre) { margin: 6px 0; padding: 8px; border-radius: 8px; background: var(--hover); overflow-x: auto; }
  .md :global(pre code) { padding: 0; background: none; }
  .md :global(a) { color: var(--accent); cursor: pointer; }
  .typing, .note { font-size: 12px; color: var(--faint); }
  /* The app's own word, not the model's: after the text, and marked apart. */
  .notice { margin-top: 6px; padding: 4px 8px; border-left: 2px solid var(--accent); font-size: 12px; color: var(--muted); }
  .error { font-size: 12px; color: var(--danger); }
  .answer-actions { display: flex; align-items: center; gap: 8px; min-width: 0; margin-top: 2px; font-size: 11px; color: var(--faint); }
  .answer-actions .icon-btn { width: 22px; height: 22px; margin-left: -4px; }
  .composer { display: flex; flex-direction: column; gap: 4px; margin: 12px; padding: 8px; background: var(--surface); border: 1px solid var(--border); border-radius: 12px; }
  .strip { padding: 6px; border-radius: 6px; background: var(--hover); font-size: 12px; color: var(--muted); }
  .link { padding: 0; color: var(--accent); font-size: inherit; }
  .link:hover { text-decoration: underline; }
  .suggestions { display: flex; flex-wrap: wrap; gap: 6px; padding: 0 0 4px; }
  .suggestion { max-width: 100%; padding: 3px 10px; border: 1px solid var(--border); border-radius: 999px; background: var(--bg); font-size: 12px; color: var(--text); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .suggestion:hover:not(:disabled) { background: var(--hover); }
  .composer-bar { display: flex; align-items: center; gap: 6px; min-width: 0; }
  .tokens { flex: none; font-size: 11px; color: var(--faint); white-space: nowrap; font-variant-numeric: tabular-nums; }
  textarea { resize: none; border: 0; padding: 2px 4px; background: transparent; }
</style>
