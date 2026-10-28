<script lang="ts">
  import { onMount } from 'svelte'
  import { get } from 'svelte/store'
  import AddReposDialog from './components/AddReposDialog.svelte'
  import BlameView from './components/BlameView.svelte'
  import BottomDock from './components/BottomDock.svelte'
  import ChangesView from './components/ChangesView.svelte'
  import ChatPanel from './components/ChatPanel.svelte'
  import CloneDialog from './components/CloneDialog.svelte'
  import ContextMenu from './components/ContextMenu.svelte'
  import CredentialDialog from './components/CredentialDialog.svelte'
  import DialogHost from './components/DialogHost.svelte'
  import LogView from './components/LogView.svelte'
  import PullRequestsView from './components/PullRequestsView.svelte'
  import PushDialog from './components/PushDialog.svelte'
  import SettingsDialog from './components/SettingsDialog.svelte'
  import RepoSettingsDialog from './components/RepoSettingsDialog.svelte'
  import Sidebar from './components/Sidebar.svelte'
  import Splitter from './components/Splitter.svelte'
  import StashView from './components/StashView.svelte'
  import StatusBar from './components/StatusBar.svelte'
  import Toasts from './components/Toasts.svelte'
import UpdateCard from './components/UpdateCard.svelte'
  import { startFocusRefresh } from './lib/actions'
  import { nextChatRunRepo } from './lib/chat'
  import { notifyBody, onClosed, onRequest, type CredentialRequest } from './lib/credential'
  import { notify, notifyPipelineEvent, startNotifications, windowFocused } from './lib/notify'
  import { startAutoFetch } from './lib/autoFetch'
  import { applyLimit, applyWatchEvent, hostingInfo, refreshStatuses, startHostingPolling } from './lib/hosting'
  import { startCloneEvents } from './lib/clone'
  import { isCommandsToggle } from './lib/cmdlog'
  import { isSettingsShortcut } from './lib/shortcuts'
  import { isTerminalToggle } from './lib/terminal'
  import { conflictOwnsScreen } from './lib/remote'
  import { blameTarget, chatOpen, chatRunRepo, chatWidth, closeBlame, commandsOpen, dockHeight, loadAISettings, loadGitSettings, loadRefs, loadRepos, loadWorktreeState, mainView, mergeState, platform, refreshRepo, selectRepo, selectedHash, selectedRepo, selectedRepoId, selectedStash, settingsOpen, sidebarWidth, stashConflictDismissed, stashEntries, terminalOpen, uncommittedSelected } from './lib/stores'
  import { loadJira } from './lib/tickets'
  import { noteCommand } from './lib/tasks'
  import { toast } from './lib/ui'
  import { setUpdateProgress } from './lib/update'
  import { updateChecker } from './lib/updateInstall'
  import type { HostingLimit, RepoChangedEvent, UpdateProgress, WatchEvent, WorktreeChangedEvent } from './lib/types'
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
  $: showPulls = $mainView === 'pulls' && !conflictWins && !!$selectedRepo && !$selectedRepo.missing && !!$hostingInfo[$selectedRepo.id]?.supported
  // The Pull requests view hidden because it cannot be shown (the repository
  // is unsupported, missing or in a merge) falls back to the log for good, or
  // it would come back later with the sidebar row still highlighted.
  $: if ($mainView === 'pulls' && !showPulls) mainView.set('log')
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
    loadGitSettings()
    loadJira()
    const offSelected = selectedRepoId.subscribe(() => loadAISettings())
    // The payload is ignored on purpose: a worktree and its main repository share
    // their AI settings, so the event may name a repo other than the selected one.
    const offRepoAI = EventsOn('repo-ai:changed', () => loadAISettings())
    Environment().then((env) => platform.set(env.platform)).catch(() => {})
    const stopFocus = startFocusRefresh()
    const stopNotifications = startNotifications()
    const stopAutoFetch = startAutoFetch()
    const stopHostingPolling = startHostingPolling()
    // A host's request limit started or ended, and a watched push changed
    // state: both feed lib/hosting.ts, which the log and the status bar read.
    const offLimit = EventsOn('hosting:limit', (payload: HostingLimit) => applyLimit(payload))
    const offHostingStatus = EventsOn('hosting:status', (payload: WatchEvent) => {
      applyWatchEvent(payload)
      // A final state is said once, and the checks behind it are asked again.
      if (payload.final) {
        notifyPipelineEvent(payload)
        refreshStatuses(payload.repoID)
      }
    })
    // Closing Settings: an account may have been connected or removed.
    const offSettingsClosed = settingsOpen.subscribe((open) => {
      if (!open) refreshStatuses(get(selectedRepoId))
    })
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
    // Follows a clone whether or not its dialog is open.
    const offClone = startCloneEvents({
      on: EventsOn,
      added: async (repo) => {
        await loadRepos()
        selectRepo(repo.id)
      },
      toast,
    })
    // Running git writes of every repository, for the status bar — the
    // Commands panel keeps its own listener.
    const offCommands = EventsOn('cmdlog:entry', noteCommand)
    // A git command needs a credential: the dialog shows it (see CredentialDialog);
    // unfocused, a notification brings the user back. A re-send of the same
    // request (only "waiting" changed) raises no second one. The body has the
    // host and context only, never a username or anything typed.
    let notifiedId = ''
    const offCredReq = EventsOn('credential:request', (r: CredentialRequest) => {
      onRequest(r)
      if (r.id === notifiedId) return
      notifiedId = r.id
      if (!get(windowFocused)) void notify({ category: 'problem', repoID: r.repoId, target: 'repo', body: notifyBody(r) })
    })
    const offCredClosed = EventsOn('credential:closed', (p: { id: string }) => onClosed(p.id))
    const offCredNotice = EventsOn('credential:notice', (p: { text: string }) => toast(p.text, 'info'))
    // Looks for a newer release at start and when the window gets focus (at
    // most every 6 h); the card and the status-bar pill follow what it finds.
    void updateChecker.run('start')
    const onFocus = () => void updateChecker.run('focus')
    window.addEventListener('focus', onFocus)
    const offUpdateProgress = EventsOn('update:progress', (p: UpdateProgress) => setUpdateProgress(p))
    return () => {
      window.removeEventListener('focus', onFocus)
      offUpdateProgress()
      offClone()
      offCommands()
      offCredReq()
      offCredClosed()
      offCredNotice()
      stopFocus()
      stopNotifications()
      stopAutoFetch()
      stopHostingPolling()
      offLimit()
      offHostingStatus()
      offSettingsClosed()
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

<div class="shell">
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
        {:else if showPulls && $selectedRepo}
          <PullRequestsView repoId={$selectedRepo.id} />
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
  <StatusBar />
</div>

<ContextMenu />
<!-- Before DialogHost: a confirm opened from it must stack on top. -->
<RepoSettingsDialog />
<CloneDialog />
<PushDialog />
<AddReposDialog />
<DialogHost />
<CredentialDialog />
<Toasts />
<UpdateCard />
<SettingsDialog />

<style>
  .shell { display: flex; flex-direction: column; height: 100%; }
  .app { display: flex; flex: 1; min-height: 0; }
  aside { flex: none; min-width: 0; background: var(--sidebar); }
  main { flex: 1; min-width: 0; display: flex; flex-direction: column; background: var(--surface); }
  .view { flex: 1; min-height: 0; }
  .dock { flex: none; min-height: 0; border-top: 1px solid var(--border); }
  .dock.hidden { display: none; }
  .side { flex: none; min-width: 0; display: flex; flex-direction: column; background: var(--bg); }
</style>
