<script lang="ts">
  import CommandLogPanel from './CommandLogPanel.svelte'
  import Splitter from './Splitter.svelte'
  import TerminalPanel from './TerminalPanel.svelte'
  import { busy, commandsOpen, dockSplit, terminalOpen } from '../lib/stores'

  const MIN = 240
  const clamp = (v: number, min: number, max: number) => Math.min(max, Math.max(min, v))
  let width = 0
</script>

<!-- TerminalPanel stays mounted for the app's lifetime even while hidden,
     so its xterm instances and scrollback survive; only its layout is
     toggled with CSS. The Commands panel is mounted only while open: it
     reloads its log from the backend. -->
<div class="wrap">
  {#if $busy}<div class="busy ellipsis" title="Operation running: {$busy}">Operation running: {$busy}</div>{/if}
  <div class="dock" bind:clientWidth={width}>
    <div class="terminal" class:hidden={!$terminalOpen} style={$commandsOpen ? `flex: none; width: ${clamp($dockSplit, MIN, Math.max(MIN, width - MIN))}px` : 'flex: 1'}>
      <TerminalPanel />
    </div>
    {#if $terminalOpen && $commandsOpen}
      <Splitter on:drag={(e) => dockSplit.set(clamp($dockSplit + e.detail, MIN, Math.max(MIN, width - MIN)))} />
    {/if}
    {#if $commandsOpen}<div class="commands"><CommandLogPanel /></div>{/if}
  </div>
</div>

<style>
  .wrap { display: flex; flex-direction: column; height: 100%; min-height: 0; }
  .busy { flex: none; padding: 2px 8px; font-size: 11px; color: var(--muted); background: var(--surface); border-bottom: 1px solid var(--border); }
  .dock { flex: 1; display: flex; min-height: 0; }
  .terminal { min-width: 0; min-height: 0; }
  .terminal.hidden { display: none; }
  .commands { flex: 1; min-width: 0; min-height: 0; }
</style>
