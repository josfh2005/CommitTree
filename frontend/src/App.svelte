<script lang="ts">
  import { onMount } from 'svelte'
  import BlameView from './components/BlameView.svelte'
  import BottomDock from './components/BottomDock.svelte'
  import ChangesView from './components/ChangesView.svelte'
  import ChatPanel from './components/ChatPanel.svelte'
  import ContextMenu from './components/ContextMenu.svelte'
  import DialogHost from './components/DialogHost.svelte'
  import LogView from './components/LogView.svelte'
  import SettingsDialog from './components/SettingsDialog.svelte'
  import RepoSettingsDialog from './components/RepoSettingsDialog.svelte'
  import Sidebar from './components/Sidebar.svelte'
  import Splitter from './components/Splitter.svelte'
  import StashView from './components/StashView.svelte'
  import Toasts from './components/Toasts.svelte'
  import { startFocusRefresh } from './lib/actions'
  import { nextChatRunRepo } from './lib/chat'
  import { startNotifications } from './lib/notify'
  import { startAutoFetch } from './lib/autoFetch'
  import { isCommandsToggle } from './lib/cmdlog'
  import { isSettingsShortcut } from './lib/shortcuts'
  import { isTerminalToggle } from './lib/terminal'
  import { conflictOwnsScreen } from './lib/remote'
  import { blameTarget, chatOpen, chatRunRepo, chatWidth, closeBlame, commandsOpen, dockHeight, loadAISettings, loadRefs, loadRepos, loadWorktreeState, mainView, mergeState, platform, refreshRepo, selectedHash, selectedRepo, selectedRepoId, selectedStash, settingsOpen, sidebarWidth, stashConflictDismissed, stashEntries, terminalOpen, uncommittedSelected } from './lib/stores'
  import type { RepoChangedEvent, WorktreeChangedEvent } from './lib/types'
  import { Environment, EventsOn } from '../wailsjs/runtime/runtime'

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
  $: showBlame = $mainView === 'blame' && !conflictWins && !!$selectedRepo && !$selectedRepo.missing && !!$blameTarget
  // Selecting a commit — or the log's uncommitted row — means the user
  // wants the log pane, so switch the main pane back.
  $: if ($selectedHash || $uncommittedSelected) mainView.set('log')
  // A repository going missing while its Blame view is open falls back to
  // the log (showBlame above turns false) but leaves blameTarget/blameStack
  // set, so they would reappear if the repository comes back — clear them
  // the same way selectRepo does for a repository switch.
  $: if ($mainView === 'blame' && $selectedRepo?.missing) closeBlame()

  const clamp = (v: number, min: number, max: number) => Math.min(max, Math.max(min, v))

  let mainHeight = 0
  $: dockOpen = $terminalOpen || $commandsOpen

  function toggleTerminal(e: KeyboardEvent) {
    const inTerminal = e.target instanceof Element && !!e.target.closest('.xterm')
    if (isTerminalToggle(e, $platform, inTerminal)) {
      e.preventDefault()
      // Opening needs a selected, present repository — the toolbar and its
      // Terminal toggle are not shown otherwise — so the shortcut can't open
      // an empty panel. Closing is always allowed.
      terminalOpen.update((v) => (v ? false : !!$selectedRepo && !$selectedRepo.missing))
    }
  }

  function toggleCommands(e: KeyboardEvent) {
    const inTerminal = e.target instanceof Element && !!e.target.closest('.xterm')
    if (isCommandsToggle(e, $platform, inTerminal)) {
      e.preventDefault()
      // Same rule as the terminal: opening needs a selected, present repository.
      commandsOpen.update((v) => (v ? false : !!$selectedRepo && !$selectedRepo.missing))
    }
  }

  onMount(() => {
    loadRepos().then(loadRefs)
    const offSelected = selectedRepoId.subscribe(() => loadAISettings())
    // The payload is ignored on purpose: a worktree and its main repository share
    // their AI settings, so the event may name a repo other than the selected one.
    const offRepoAI = EventsOn('repo-ai:changed', () => loadAISettings())
    Environment().then((env) => platform.set(env.platform)).catch(() => {})
    const stopFocus = startFocusRefresh()
    const stopNotifications = startNotifications()
    const stopAutoFetch = startAutoFetch()
    // Every view of the working tree follows $worktreeState — the Changes
    // view and the log's "Uncommitted changes" row alike — so one app-wide
    // listener keeps them fresh whether or not a Changes pane is mounted.
    const offWorktree = EventsOn('worktree:changed', (payload: WorktreeChangedEvent) => {
      if (payload?.repoID === $selectedRepo?.id) loadWorktreeState()
    })
    // A write the AI chat ran (commit, push, branch, …) after the user
    // approved it: refresh the repo the same way any other external change
    // would, whether or not that repo is currently selected.
    const offRepoChanged = EventsOn('repo:changed', (payload: RepoChangedEvent) => {
      if (payload?.repoID === $selectedRepo?.id) refreshRepo()
    })
    // macOS: the native menu's Settings… item (⌘,), see internal/app/menu.go.
    const offSettings = EventsOn('menu:settings', () => settingsOpen.set(true))
    // Which repository an AI run holds, for the Merge view's buttons —
    // tracked here, not in ChatPanel, which is unmounted when closed.
    const offRuns = ['chat:start', 'chat:done', 'chat:error'].map((name) =>
      EventsOn(name, (payload: { repoID?: string }) => chatRunRepo.update((cur) => nextChatRunRepo(cur, name, payload))),
    )
    return () => {
      stopFocus()
      stopNotifications()
      stopAutoFetch()
      offWorktree()
      offRepoChanged()
      offSettings()
      offSelected()
      offRepoAI()
      offRuns.forEach((off) => off())
    }
  })

  function onKeydown(e: KeyboardEvent) {
    toggleTerminal(e)
    toggleCommands(e)
    if (isSettingsShortcut(e, $platform)) {
      e.preventDefault()
      settingsOpen.set(true)
    }
  }
</script>

<svelte:window on:keydown={onKeydown} />

<div class="app">
  <aside style="width: {$sidebarWidth}px"><Sidebar /></aside>
  <Splitter on:drag={(e) => sidebarWidth.set(clamp($sidebarWidth + e.detail, 200, 480))} />
  <main bind:clientHeight={mainHeight}>
    <div class="view">
      {#if showChanges && $selectedRepo}
        <ChangesView repoId={$selectedRepo.id} />
      {:else if showStash && $selectedRepo && selectedStashEntry}
        <StashView repoId={$selectedRepo.id} entry={selectedStashEntry} />
      {:else if showBlame && $selectedRepo}
        <BlameView repoId={$selectedRepo.id} />
      {:else}
        <LogView />
      {/if}
    </div>
    {#if dockOpen}
      <Splitter direction="horizontal" on:drag={(e) => dockHeight.set(clamp($dockHeight - e.detail, 120, Math.max(120, mainHeight - 200)))} />
    {/if}
    <!-- The dock stays mounted while closed so the terminal's shells and
         scrollback survive; only its layout is toggled with CSS. -->
    <div class="dock" class:hidden={!dockOpen} style="height: {clamp($dockHeight, 120, Math.max(120, mainHeight - 200))}px"><BottomDock /></div>
  </main>
  {#if $chatOpen}
    <Splitter on:drag={(e) => chatWidth.set(clamp($chatWidth - e.detail, 260, 560))} />
    <section class="side" style="width: {$chatWidth}px"><ChatPanel /></section>
  {/if}
</div>

<ContextMenu />
<!-- Before DialogHost: a confirm opened from it must stack on top. -->
<RepoSettingsDialog />
<DialogHost />
<Toasts />
<SettingsDialog />

<style>
  .app { display: flex; height: 100%; }
  aside { flex: none; min-width: 0; background: var(--sidebar); }
  main { flex: 1; min-width: 0; display: flex; flex-direction: column; background: var(--surface); }
  .view { flex: 1; min-height: 0; }
  .dock { flex: none; min-height: 0; border-top: 1px solid var(--border); }
  .dock.hidden { display: none; }
  .side { flex: none; min-width: 0; display: flex; flex-direction: column; background: var(--bg); }
</style>
