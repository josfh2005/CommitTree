<script lang="ts">
  import { onMount } from 'svelte'
  import ChatPanel from './components/ChatPanel.svelte'
  import ContextMenu from './components/ContextMenu.svelte'
  import DialogHost from './components/DialogHost.svelte'
  import LogView from './components/LogView.svelte'
  import SettingsDialog from './components/SettingsDialog.svelte'
  import Sidebar from './components/Sidebar.svelte'
  import Splitter from './components/Splitter.svelte'
  import Toasts from './components/Toasts.svelte'
  import { startFocusRefresh } from './lib/actions'
  import { chatOpen, chatWidth, loadRefs, loadRepos, sidebarWidth } from './lib/stores'

  const clamp = (v: number, min: number, max: number) => Math.min(max, Math.max(min, v))

  onMount(() => {
    loadRepos().then(loadRefs)
    return startFocusRefresh()
  })
</script>

<div class="app">
  <aside style="width: {$sidebarWidth}px"><Sidebar /></aside>
  <Splitter on:drag={(e) => sidebarWidth.set(clamp($sidebarWidth + e.detail, 200, 480))} />
  <main><LogView /></main>
  {#if $chatOpen}
    <Splitter on:drag={(e) => chatWidth.set(clamp($chatWidth - e.detail, 260, 560))} />
    <section class="chat" style="width: {$chatWidth}px"><ChatPanel /></section>
  {/if}
</div>

<ContextMenu />
<DialogHost />
<Toasts />
<SettingsDialog />

<style>
  .app { display: flex; height: 100%; }
  aside { flex: none; min-width: 0; background: var(--sidebar); }
  main { flex: 1; min-width: 0; background: var(--surface); }
  .chat { flex: none; min-width: 0; background: var(--bg); }
</style>
