<script lang="ts">
  import { onMount } from 'svelte'
  import ChangesView from './components/ChangesView.svelte'
  import ChatPanel from './components/ChatPanel.svelte'
  import ContextMenu from './components/ContextMenu.svelte'
  import DialogHost from './components/DialogHost.svelte'
  import LogView from './components/LogView.svelte'
  import SettingsDialog from './components/SettingsDialog.svelte'
  import Sidebar from './components/Sidebar.svelte'
  import Splitter from './components/Splitter.svelte'
  import Toasts from './components/Toasts.svelte'
  import { startFocusRefresh } from './lib/actions'
  import { conflictOwnsScreen } from './lib/remote'
  import { chatOpen, chatWidth, loadAISettings, loadRefs, loadRepos, mainView, mergeState, selectedHash, selectedRepo, sidebarWidth, stashConflictDismissed } from './lib/stores'

  // A merge in progress always wins: the Changes view has nothing to show
  // that the merge view (reached through the log pane) doesn't already cover,
  // and only the merge view can commit or abort a merge. A dismissed stash
  // conflict is the one case where the Changes view may show a repository
  // with unmerged entries, since nothing else — no abort, no continue — can
  // finish it; Done just hands the screen back with the entries still there.
  $: showChanges = $mainView === 'changes' && !conflictOwnsScreen($mergeState, $stashConflictDismissed) && !!$selectedRepo && !$selectedRepo.missing
  // Selecting a commit in the log means the user wants to look at history,
  // not the working tree — switch the main pane back.
  $: if ($selectedHash) mainView.set('log')

  const clamp = (v: number, min: number, max: number) => Math.min(max, Math.max(min, v))

  onMount(() => {
    loadRepos().then(loadRefs)
    loadAISettings()
    return startFocusRefresh()
  })
</script>

<div class="app">
  <aside style="width: {$sidebarWidth}px"><Sidebar /></aside>
  <Splitter on:drag={(e) => sidebarWidth.set(clamp($sidebarWidth + e.detail, 200, 480))} />
  <main>
    {#if showChanges && $selectedRepo}
      <ChangesView repoId={$selectedRepo.id} />
    {:else}
      <LogView />
    {/if}
  </main>
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
