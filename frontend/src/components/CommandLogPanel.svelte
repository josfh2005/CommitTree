<script lang="ts">
  import { onDestroy } from 'svelte'
  import Icon from './Icon.svelte'
  import { api } from '../lib/api'
  import { commandLine, emptyMessage, formatClock, formatDuration, mergeEntries, ORIGIN_LABEL, outcomeText, visibleEntries } from '../lib/cmdlog'
  import { commandsOpen, commandsShowReads, selectedRepo } from '../lib/stores'
  import type { CommandEntry, CommandOutput } from '../lib/types'
  import { copyText } from '../lib/ui'
  import { EventsOn } from '../../wailsjs/runtime/runtime'

  let entries: CommandEntry[] = []
  let error = ''
  let expanded: number | null = null
  let outputs: Record<number, CommandOutput> = {}
  let outputErrors: Record<number, string> = {}
  let loadedId = ''
  let loadedRepoKey = ''
  let list: HTMLElement

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
    error = ''
    if (!id) return
    try {
      const view = await api.commandLog(id)
      if (loadedId === id) {
        loadedRepoKey = view.repo
        entries = mergeEntries(entries, view.entries, loadedRepoKey)
      }
    } catch (e) {
      if (loadedId === id) error = String(e)
    }
  }

  const off = EventsOn('cmdlog:entry', (e: CommandEntry) => {
    if (loadedId && loadedRepoKey) entries = mergeEntries(entries, [e], loadedRepoKey)
  })
  onDestroy(off)

  async function toggle(e: CommandEntry) {
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
        entries = []
        outputs = {}
        outputErrors = {}
        expanded = null
      }
    } catch (e) {
      if (loadedId === id) error = String(e)
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
    <button class="btn" disabled={!entries.length} on:click={clear}>Clear</button>
    <button class="icon-btn" title="Hide commands" on:click={() => commandsOpen.set(false)}><Icon name="list" /></button>
  </header>
  {#if error}<div class="error ellipsis" title={error}>{error}</div>{/if}
  <!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
  <div class="body" role="list" bind:this={list} on:keydown={onKeydown}>
    {#each shown as e (e.id)}
      <div class="entry" role="listitem" class:failed={e.outcome !== 'ok'}>
        <div class="line">
          <button class="main" aria-expanded={expanded === e.id} title={commandLine(e.args)} on:click={() => toggle(e)}>
            <span class="mark" aria-label={e.outcome === 'ok' ? 'Succeeded' : outcomeText(e)}>{e.outcome === 'ok' ? '✓' : '✗'}</span>
            <span class="cmd ellipsis">{commandLine(e.args)}</span>
            <span class="badge {e.origin}">{ORIGIN_LABEL[e.origin]}</span>
            <span class="time">{formatClock(e.start)}</span>
            <span class="dur">{formatDuration(e.durationMs)}</span>
          </button>
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
