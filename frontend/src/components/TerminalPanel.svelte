<script lang="ts">
  import { onDestroy } from 'svelte'
  import { EventsOn } from '../../wailsjs/runtime/runtime'
  import Icon from './Icon.svelte'
  import TerminalView from './TerminalView.svelte'
  import { api } from '../lib/api'
  import { checkExternalChanges } from '../lib/actions'
  import { busy, loadRepos, selectedRepo, selectedRepoId, terminalOpen } from '../lib/stores'
  import { addTab, markExited, removeTab, setActive, settledAction, tabTitle, tabsFor, terminalState } from '../lib/terminal'
  import { errorMessage, toast } from '../lib/ui'
  import { get } from 'svelte/store'

  const writers = new Map<string, (data: string) => void>()
  // Output that arrives before a tab's view has mounted.
  const early = new Map<string, string>()
  let shell = 'sh'
  api.terminalShell().then((s) => (shell = s)).catch(() => {})

  function register(tab: string, write: ((data: string) => void) | null) {
    if (!write) return void writers.delete(tab)
    writers.set(tab, write)
    const pending = early.get(tab)
    if (pending) {
      early.delete(tab)
      write(pending)
    }
  }

  const offs = [
    EventsOn('terminal:data', (p: { tab: string; data: string }) => {
      const w = writers.get(p.tab)
      if (w) w(p.data)
      else early.set(p.tab, (early.get(p.tab) ?? '') + p.data)
    }),
    EventsOn('terminal:exit', (p: { tab: string; code: number }) => terminalState.update((s) => markExited(s, p.tab, p.code))),
    EventsOn('terminal:settled', (p: { tab: string; repo: string }) => {
      if (settledAction(get(selectedRepoId), p.repo) === 'check') checkExternalChanges()
      else loadRepos()
    }),
  ]
  onDestroy(() => offs.forEach((off) => off()))

  async function open(repoId: string) {
    try {
      // The view refits to the real size as soon as it mounts.
      const id = await api.terminalOpen(repoId, 80, 24)
      terminalState.update((s) => addTab(s, repoId, id, shell))
    } catch (e) {
      toast(errorMessage(e), 'error')
    }
  }

  async function close(id: string) {
    terminalState.update((s) => removeTab(s, id))
    early.delete(id)
    await api.terminalClose(id).catch(() => {})
  }

  $: repoId = $selectedRepo && !$selectedRepo.missing ? $selectedRepo.id : ''
  $: tabs = repoId ? tabsFor($terminalState, repoId) : []
  $: active = repoId ? $terminalState.active[repoId] : undefined
  // Opening the panel on a repository with no tabs opens one. Tracked per
  // repository so closing the last tab leaves "Open shell" instead of
  // immediately spawning another. The panel stays mounted even while
  // hidden (so background shells and scrollback survive), so this must
  // also require $terminalOpen — otherwise switching repositories while
  // the terminal is closed would silently spawn a shell for each one.
  const autoOpened = new Set<string>()
  $: if ($terminalOpen && repoId && tabs.length === 0 && !autoOpened.has(repoId)) {
    autoOpened.add(repoId)
    open(repoId)
  }
</script>

<div class="panel">
  <header>
    <div class="tabs">
      {#each tabs as t (t.id)}
        <div class="tab" class:active={t.id === active} class:exited={t.exitCode !== null}>
          <button class="name" on:click={() => terminalState.update((s) => setActive(s, t.repoId, t.id))}>{tabTitle(t)}</button>
          <button class="x" title="Close" on:click={() => close(t.id)}>×</button>
        </div>
      {/each}
      {#if repoId}
        <button class="icon-btn" title="New shell" on:click={() => open(repoId)}>+</button>
      {/if}
    </div>
    {#if $busy}<span class="busy ellipsis">Operation running: {$busy}</span>{/if}
    <button class="icon-btn" title="Hide terminal" on:click={() => terminalOpen.set(false)}><Icon name="terminal" /></button>
  </header>
  <div class="body">
    <!-- Every tab of every repository stays mounted so its scrollback
         survives switching repositories; only the active one is shown.
         Also gated on $terminalOpen: TerminalPanel itself stays mounted
         while the panel is hidden with CSS, so nothing here should be
         "visible" until it is actually shown again. -->
    {#each $terminalState.tabs as t (t.id)}
      <TerminalView tab={t.id} visible={$terminalOpen && t.repoId === repoId && t.id === active} {register} />
    {/each}
    {#if repoId && tabs.length === 0}
      <div class="empty"><button class="btn" on:click={() => open(repoId)}>Open shell</button></div>
    {/if}
  </div>
</div>

<style>
  .panel { display: flex; flex-direction: column; height: 100%; min-height: 0; background: var(--surface); }
  header { display: flex; align-items: center; gap: 8px; padding: 4px 8px; border-bottom: 1px solid var(--border); flex: none; }
  .tabs { display: flex; align-items: center; gap: 2px; flex: 1; min-width: 0; overflow-x: auto; }
  .tab { display: flex; align-items: center; border-radius: 6px; font-size: 12px; white-space: nowrap; }
  .tab.active { background: var(--active); }
  .tab.exited .name { color: var(--muted); }
  .tab button { background: none; border: 0; padding: 3px 6px; color: inherit; font: inherit; cursor: pointer; }
  .tab .x { padding-left: 0; color: var(--muted); }
  .busy { font-size: 11px; color: var(--muted); max-width: 40%; }
  .body { position: relative; flex: 1; min-height: 0; }
  .empty { position: absolute; inset: 0; display: flex; align-items: center; justify-content: center; }
</style>
