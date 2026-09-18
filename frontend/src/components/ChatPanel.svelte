<script lang="ts">
  import { onDestroy, tick } from 'svelte'
  import { EventsOn } from '../../wailsjs/runtime/runtime'
  import Icon from './Icon.svelte'
  import { api } from '../lib/api'
  import { applyEvent, CHAT_EVENTS, emptyChat, errorText, fromMessages, shouldReloadChat, startRun, toolLabel, type ChatState } from '../lib/chat'
  import { renderMarkdown } from '../lib/markdown'
  import { chatOpen, jumpTo, selectedRepo, settingsOpen } from '../lib/stores'
  import type { AIStatus } from '../lib/types'
  import { errorMessage, toast } from '../lib/ui'

  let state: ChatState = emptyChat('')
  let status: AIStatus | null = null
  let input = ''
  let list: HTMLDivElement
  let loadError = ''

  const offs = CHAT_EVENTS.map((name) =>
    EventsOn(name, (payload) => {
      const next = applyEvent(state, name, payload)
      if (next !== state) {
        state = next
        scrollDown()
      }
    }),
  )
  onDestroy(() => offs.forEach((off) => off()))

  $: load($selectedRepo?.id ?? '')
  $: running = state.runID !== null
  $: ready = !!status?.ollama.running && !!status?.ollama.chatModelInstalled

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
    } catch (e) {
      loadError = errorMessage(e)
    }
  }

  async function refreshStatus() {
    try {
      status = await api.aiStatus()
    } catch {
      status = null
    }
  }

  async function send() {
    const text = input.trim()
    if (!text || running || !state.repoID || !ready) return
    const runID = crypto.randomUUID()
    input = ''
    state = startRun(state, text, runID)
    scrollDown()
    try {
      await api.sendChat(state.repoID, text, runID)
    } catch (e) {
      state = applyEvent(state, 'chat:error', { repoID: state.repoID, runID, message: errorMessage(e), code: 'other' })
    }
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
    {:else if status && !status.ollama.running}
      <div class="notice">
        <strong>Ollama is not running</strong>
        <p>Open Ollama or install it from ollama.com.</p>
        <div class="actions">
          <button class="btn" on:click={refreshStatus}>Retry</button>
          <button class="btn" on:click={() => settingsOpen.set(true)}>Settings</button>
        </div>
      </div>
    {:else if status && !status.ollama.chatModelInstalled}
      <div class="notice">
        <strong>Model {status.ollama.chatModel} is not installed</strong>
        <p>Download it from Settings to start chatting.</p>
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

    {#each state.items as item, i}
      {#if item.role === 'user'}
        <div class="msg user">{item.text}</div>
      {:else}
        <div class="msg assistant">
          {#each item.tools as tool}
            <div class="tool" title={JSON.stringify(tool.args ?? {})}>
              <Icon name="search" size={12} />
              <span class="ellipsis">{toolLabel(tool)}</span>
              {#if tool.summary}<span class="summary ellipsis">· {tool.summary}</span>{/if}
            </div>
          {/each}
          {#each item.notices ?? [] as notice}<div class="note">{notice}</div>{/each}
          {#if item.text}
            <div class="md">{@html renderMarkdown(item.text)}</div>
          {:else if running && i === state.items.length - 1 && !item.error}
            <div class="typing">Thinking…</div>
          {/if}
          {#if item.stopped}<div class="note">Stopped</div>{/if}
          {#if item.error}<div class="error">{errorText(item.error)}</div>{/if}
        </div>
      {/if}
    {/each}
  </div>

  <div class="composer">
    <textarea
      rows="2"
      placeholder={ready ? 'Ask about this repo…' : 'Set up Ollama to chat'}
      bind:value={input}
      on:keydown={onKey}
      disabled={!$selectedRepo || !ready}
    ></textarea>
    {#if running}
      <button class="icon-btn" title="Stop" on:click={stop}><Icon name="stop" /></button>
    {:else}
      <button class="icon-btn" title="Send" disabled={!input.trim() || !ready} on:click={send}><Icon name="send" /></button>
    {/if}
  </div>
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
  .msg { max-width: 100%; user-select: text; line-height: 1.5; }
  .user { align-self: flex-end; max-width: 85%; padding: 8px 12px; border-radius: 12px; background: var(--active); white-space: pre-wrap; }
  .tool { display: flex; align-items: center; gap: 6px; max-width: 100%; margin-bottom: 4px; padding: 2px 8px; border-radius: 6px; background: var(--hover); color: var(--muted); font-size: 12px; }
  .summary { color: var(--faint); }
  .md :global(p) { margin: 0 0 6px; }
  .md :global(ul) { margin: 4px 0 6px; padding-left: 18px; }
  .md :global(code) { font-family: var(--mono); font-size: 12px; padding: 0 4px; border-radius: 4px; background: var(--hover); }
  .md :global(pre) { margin: 6px 0; padding: 8px; border-radius: 8px; background: var(--hover); overflow-x: auto; }
  .md :global(pre code) { padding: 0; background: none; }
  .md :global(a) { color: var(--accent); cursor: pointer; }
  .typing, .note { font-size: 12px; color: var(--faint); }
  .error { font-size: 12px; color: var(--danger); }
  .composer { display: flex; align-items: flex-end; gap: 6px; margin: 12px; padding: 8px; background: var(--surface); border: 1px solid var(--border); border-radius: 12px; }
  textarea { flex: 1; resize: none; border: 0; padding: 2px 4px; background: transparent; }
</style>
