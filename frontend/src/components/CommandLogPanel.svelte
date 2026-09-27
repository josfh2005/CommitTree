<script lang="ts">
  import { onDestroy } from 'svelte'
  import Icon from './Icon.svelte'
  import { api } from '../lib/api'
  import { commandLine, elapsedMs, emptyMessage, formatClock, formatDuration, formatElapsed, mergeEntries, mergeLoad, NOT_RUNNING_MESSAGE, ORIGIN_LABEL, outcomeText, visibleEntries } from '../lib/cmdlog'
  import { commandsOpen, commandsShowReads, selectedRepo } from '../lib/stores'
  import type { CommandEntry, CommandOutput } from '../lib/types'
  import { copyText, errorMessage } from '../lib/ui'
  import { EventsOn } from '../../wailsjs/runtime/runtime'

  let entries: CommandEntry[] = []
  let error = ''
  let expanded: number | null = null
  let outputs: Record<number, CommandOutput> = {}
  let outputErrors: Record<number, string> = {}
  let loadedId = ''
  let loadedRepoKey = ''
  // Events that arrive while a load is in flight (loadedRepoKey not yet
  // known, so mergeEntries can't filter by repo) are buffered here and
  // applied on top of the snapshot once it resolves — see mergeLoad.
  let pending: CommandEntry[] = []
  let list: HTMLElement
  let cancelling: Record<number, boolean> = {}
  // now ticks once a second while a command is running, for its elapsed time.
  let now = Date.now()
  let timer: ReturnType<typeof setInterval> | undefined
  $: anyRunning = entries.some((e) => e.outcome === 'running')
  $: if (anyRunning && !timer) {
    now = Date.now()
    timer = setInterval(() => (now = Date.now()), 1000)
  } else if (!anyRunning && timer) {
    clearInterval(timer)
    timer = undefined
  }
  onDestroy(() => clearInterval(timer))

  $: repoId = $selectedRepo && !$selectedRepo.missing ? $selectedRepo.id : ''
  $: if (repoId !== loadedId) load(repoId)
  $: shown = visibleEntries(entries, $commandsShowReads)
  $: empty = emptyMessage(entries, $commandsShowReads)

  async function load(id: string) {
    loadedId = id
    loadedRepoKey = ''
    entries = []
    outputs = {}
    outputErrors = {}
    expanded = null
    cancelling = {}
    error = ''
    pending = []
    if (!id) return
    try {
      const view = await api.commandLog(id)
      if (loadedId === id) {
        loadedRepoKey = view.repo
        entries = mergeLoad(view.entries, pending, loadedRepoKey)
        pending = []
      }
    } catch (e) {
      if (loadedId === id) error = String(e)
    }
  }

  const off = EventsOn('cmdlog:entry', (e: CommandEntry) => {
    if (!loadedId) return
    // The snapshot hasn't resolved yet: its repo key isn't known, so this
    // can't be merged in (and dropping it could leave a running row stuck
    // forever once the snapshot arrives). Buffer it and apply it on top of
    // the snapshot in load() once loadedRepoKey is known.
    if (!loadedRepoKey) {
      pending = [...pending, e]
      return
    }
    entries = mergeEntries(entries, [e], loadedRepoKey)
  })
  onDestroy(off)

  async function toggle(e: CommandEntry) {
    if (e.outcome === 'running') return
    expanded = expanded === e.id ? null : e.id
    if (expanded !== e.id || e.outputDropped || outputs[e.id] || outputErrors[e.id]) return
    try {
      outputs = { ...outputs, [e.id]: await api.commandLogOutput(loadedId, e.id) }
    } catch (err) {
      outputErrors = { ...outputErrors, [e.id]: String(err) }
    }
  }

  async function clear() {
    const id = loadedId
    try {
      await api.clearCommandLog(id)
      if (loadedId === id) {
        // The backend keeps running commands: their end is still to come.
        entries = entries.filter((e) => e.outcome === 'running')
        outputs = {}
        outputErrors = {}
        expanded = null
      }
    } catch (e) {
      if (loadedId === id) error = String(e)
    }
  }

  async function cancel(e: CommandEntry) {
    const id = loadedId
    cancelling = { ...cancelling, [e.id]: true }
    try {
      await api.cancelCommand(id, e.id)
    } catch (err) {
      if (loadedId !== id) return
      cancelling = Object.fromEntries(Object.entries(cancelling).filter(([k]) => Number(k) !== e.id))
      const message = errorMessage(err)
      // It ended on its own while the click was on its way: the backend
      // reports that as ErrNotRunning, not a real failure, so it never
      // reaches the panel's error line.
      if (message !== NOT_RUNNING_MESSAGE) error = message
    }
  }

  // ↑/↓ move between rows; Enter/Space on a row is the button's own click.
  function onKeydown(ev: KeyboardEvent) {
    if (ev.key !== 'ArrowDown' && ev.key !== 'ArrowUp') return
    const rows = Array.from(list.querySelectorAll<HTMLButtonElement>('button.main'))
    const at = rows.indexOf(document.activeElement as HTMLButtonElement)
    const next = rows[ev.key === 'ArrowDown' ? at + 1 : Math.max(0, at - 1)]
    if (next) {
      ev.preventDefault()
      next.focus()
    }
  }
</script>

<div class="panel">
  <header>
    <span class="title">Commands</span>
    <label class="reads"><input type="checkbox" bind:checked={$commandsShowReads} /> Show reads</label>
    <span class="spacer"></span>
    <button class="btn" disabled={!entries.some((e) => e.outcome !== 'running')} on:click={clear}>Clear</button>
    <button class="icon-btn" title="Hide commands" on:click={() => commandsOpen.set(false)}><Icon name="list" /></button>
  </header>
  {#if error}<div class="error ellipsis" title={error}>{error}</div>{/if}
  <!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
  <div class="body" role="list" bind:this={list} on:keydown={onKeydown}>
    {#each shown as e (e.id)}
      <div class="entry" role="listitem" class:failed={e.outcome === 'failed' || e.outcome === 'timeout'} class:cancelled={e.outcome === 'cancelled'}>
        <div class="line">
          <button class="main" aria-expanded={e.outcome === 'running' ? undefined : expanded === e.id} title={commandLine(e.args)} on:click={() => toggle(e)}>
            {#if e.outcome === 'running'}
              <span class="mark"><span class="spinner" role="img" aria-label="Running"></span></span>
            {:else}
              <span class="mark" aria-label={e.outcome === 'ok' ? 'Succeeded' : outcomeText(e)}>{e.outcome === 'ok' ? '✓' : e.outcome === 'cancelled' ? '⊘' : '✗'}</span>
            {/if}
            <span class="cmd ellipsis">{commandLine(e.args)}</span>
            <span class="badge {e.origin}">{ORIGIN_LABEL[e.origin]}</span>
            <span class="time">{formatClock(e.start)}</span>
            <span class="dur">{e.outcome === 'running' ? formatElapsed(elapsedMs(e, now)) : formatDuration(e.durationMs)}</span>
          </button>
          {#if e.outcome === 'running'}
            <button class="btn cancel" disabled={cancelling[e.id]} aria-label={`Cancel ${commandLine(e.args)}`} on:click={() => cancel(e)}>Cancel</button>
          {/if}
          <button class="icon-btn" title="Copy command" on:click={() => copyText(commandLine(e.args))}><Icon name="copy" size={14} /></button>
        </div>
        {#if expanded === e.id}
          <div class="detail">
            <div class="meta">{outcomeText(e)}</div>
            {#if e.outputDropped}
              <div class="muted">Output no longer kept</div>
            {:else if outputErrors[e.id]}
              <div class="error">{outputErrors[e.id]}</div>
            {:else if outputs[e.id]}
              {#if outputs[e.id].stdout}<pre>{outputs[e.id].stdout}</pre>{/if}
              {#if outputs[e.id].stderr}<pre class="stderr">{outputs[e.id].stderr}</pre>{/if}
              {#if !outputs[e.id].stdout && !outputs[e.id].stderr}<div class="muted">No output</div>{/if}
            {/if}
            {#if e.outputTruncated}<div class="muted">Output truncated</div>{/if}
          </div>
        {/if}
      </div>
    {/each}
    {#if empty === 'none'}
      <div class="empty">No git commands yet</div>
    {:else if empty === 'onlyReads'}
      <div class="empty">Only reads so far — <button class="link" on:click={() => commandsShowReads.set(true)}>Show reads</button></div>
    {/if}
  </div>
</div>

<style>
  .panel { display: flex; flex-direction: column; height: 100%; min-height: 0; background: var(--surface); }
  header { display: flex; align-items: center; gap: 8px; padding: 4px 8px; border-bottom: 1px solid var(--border); flex: none; font-size: 12px; }
  .title { font-weight: 600; }
  .reads { display: flex; align-items: center; gap: 4px; color: var(--muted); }
  .spacer { flex: 1; }
  .error { padding: 4px 8px; font-size: 12px; color: var(--danger); }
  .body { flex: 1; min-height: 0; overflow-y: auto; font-size: 12px; }
  .line { display: flex; align-items: center; }
  .main { flex: 1; min-width: 0; display: flex; align-items: center; gap: 8px; padding: 3px 8px; background: none; border: 0; color: inherit; font: inherit; text-align: left; cursor: pointer; }
  .main:hover, .main:focus-visible { background: var(--hover); }
  .mark { flex: none; width: 1em; color: var(--ok); }
  .failed .mark { color: var(--danger); }
  .cancelled .mark { color: var(--muted); }
  .spinner { display: inline-block; width: 9px; height: 9px; border: 2px solid var(--border); border-top-color: var(--accent); border-radius: 50%; animation: spin 0.8s linear infinite; }
  @keyframes spin { to { transform: rotate(360deg); } }
  @media (prefers-reduced-motion: reduce) { .spinner { animation: none; } }
  .cancel { flex: none; padding: 1px 8px; font-size: 11px; }
  .cmd { flex: 1; min-width: 0; font-family: var(--mono); }
  .badge { flex: none; padding: 0 6px; border: 1px solid var(--border); border-radius: 8px; font-size: 11px; color: var(--muted); }
  .badge.ai { color: var(--accent); border-color: var(--accent); }
  .time, .dur { flex: none; color: var(--muted); font-variant-numeric: tabular-nums; }
  .dur { width: 5em; text-align: right; }
  .detail { padding: 4px 8px 8px 28px; display: flex; flex-direction: column; gap: 4px; }
  .meta, .muted { color: var(--muted); }
  pre { margin: 0; max-height: 200px; overflow: auto; padding: 6px; background: var(--bg); border: 1px solid var(--border); border-radius: 6px; font-family: var(--mono); font-size: 11px; white-space: pre-wrap; word-break: break-word; }
  pre.stderr { color: var(--danger); }
  .empty { padding: 16px; text-align: center; color: var(--muted); }
  .link { background: none; border: 0; padding: 0; color: var(--accent); font: inherit; cursor: pointer; text-decoration: underline; }
</style>
