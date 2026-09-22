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
  import StashView from './components/StashView.svelte'
  import TerminalPanel from './components/TerminalPanel.svelte'
  import Toasts from './components/Toasts.svelte'
  import { startFocusRefresh } from './lib/actions'
  import { conflictOwnsScreen } from './lib/remote'
  import { chatOpen, chatWidth, loadAISettings, loadRefs, loadRepos, mainView, mergeState, selectedHash, selectedRepo, selectedStash, sidebarWidth, stashConflictDismissed, stashEntries, terminalHeight, terminalOpen } from './lib/stores'

  // A merge in progress always wins: neither the Changes view nor a stash
  // preview has anything to show that the merge view (reached through the
  // log pane) doesn't already cover, and only the merge view can commit or
  // abort a merge. A dismissed stash conflict is the one case where they may
  // show a repository with unmerged entries, since nothing else — no abort,
  // no continue — can finish it; Done just hands the screen back with the
  // entries still there.
  $: conflictWins = conflictOwnsScreen($mergeState, $stashConflictDismissed)
  $: showChanges = $mainView === 'changes' && !conflictWins && !!$selectedRepo && !$selectedRepo.missing
  // selectedStash is revalidated against the list elsewhere (see
  // validSelectedStash in lib/stash.ts), but a slower revalidation landing
  // after it was already cleared, or vice versa, must not show a stash
  // that isn't actually in the current list — hence looking the entry up
  // here too, by hash rather than the index alone, which can end up naming
  // a different stash once entries below it shift.
  $: selectedStashEntry = $stashEntries.find((e) => e.hash === $selectedStash?.hash) ?? null
  $: showStash = $mainView === 'stash' && !conflictWins && !!$selectedRepo && !$selectedRepo.missing && !!selectedStashEntry
  // Selecting a commit in the log means the user wants to look at history,
  // not the working tree — switch the main pane back.
  $: if ($selectedHash) mainView.set('log')

  const clamp = (v: number, min: number, max: number) => Math.min(max, Math.max(min, v))

  let sideHeight = 0

  // e.code, not e.key: on layouts where ` is a dead key (Spanish, among
  // others) e.key is "Dead", but the physical key is still Backquote.
  function toggleTerminal(e: KeyboardEvent) {
    if (e.ctrlKey && !e.metaKey && !e.altKey && e.code === 'Backquote') {
      e.preventDefault()
      terminalOpen.update((v) => !v)
    }
  }

  onMount(() => {
    loadRepos().then(loadRefs)
    loadAISettings()
    return startFocusRefresh()
  })
</script>

<svelte:window on:keydown={toggleTerminal} />

<div class="app">
  <aside style="width: {$sidebarWidth}px"><Sidebar /></aside>
  <Splitter on:drag={(e) => sidebarWidth.set(clamp($sidebarWidth + e.detail, 200, 480))} />
  <main>
    {#if showChanges && $selectedRepo}
      <ChangesView repoId={$selectedRepo.id} />
    {:else if showStash && $selectedRepo && selectedStashEntry}
      <StashView repoId={$selectedRepo.id} entry={selectedStashEntry} />
    {:else}
      <LogView />
    {/if}
  </main>
  {#if $chatOpen || $terminalOpen}
    <Splitter on:drag={(e) => chatWidth.set(clamp($chatWidth - e.detail, 260, 560))} />
  {/if}
  <!-- TerminalPanel stays mounted for the app's lifetime (see below) even
       while this column is closed, so its xterm instances and scrollback
       survive hiding it; only its layout is toggled with CSS. -->
  <section class="side" class:hidden={!($chatOpen || $terminalOpen)} style="width: {$chatWidth}px" bind:clientHeight={sideHeight}>
    {#if $chatOpen}<div class="chat"><ChatPanel /></div>{/if}
    {#if $chatOpen && $terminalOpen}
      <Splitter direction="horizontal" on:drag={(e) => terminalHeight.set(clamp($terminalHeight - e.detail, 120, Math.max(120, sideHeight - 120)))} />
    {/if}
    <div class="terminal" class:hidden={!$terminalOpen} style={$chatOpen ? `height: ${$terminalHeight}px` : 'flex: 1'}><TerminalPanel /></div>
  </section>
</div>

<ContextMenu />
<DialogHost />
<Toasts />
<SettingsDialog />

<style>
  .app { display: flex; height: 100%; }
  aside { flex: none; min-width: 0; background: var(--sidebar); }
  main { flex: 1; min-width: 0; background: var(--surface); }
  .side { flex: none; min-width: 0; display: flex; flex-direction: column; background: var(--bg); }
  .side.hidden { display: none; }
  .chat { flex: 1; min-height: 0; }
  .terminal { flex: none; min-height: 0; }
  .terminal.hidden { display: none; }
</style>
