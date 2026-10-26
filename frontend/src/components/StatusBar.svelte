<script lang="ts">
  import { onDestroy, tick } from 'svelte'
  import { api } from '../lib/api'
  import { openCloneDialog } from '../lib/actions'
  import { cancelClone, cloneCancelling } from '../lib/clone'
  import { credentialRequest, waitingText } from '../lib/credential'
  import { commitStatuses, headStatusText, hostingInfo, hostingLimitText, hostingLimits, isHosted, requestHeadStatus, stateIcon } from '../lib/hosting'
  import { settingsTab } from '../lib/settingsTabs'
  import { announcement, cancelTask, elapsedText, openAction, outcomeIcon } from '../lib/statusBar'
  import { commandsOpen, refs, repos, selectRepo, selectedRepoId, settingsOpen, showCommitInLog } from '../lib/stores'
  import { cancelTarget, relativeTime, shownTasks, taskState, type Task, type TaskResult } from '../lib/tasks'
  import { toast } from '../lib/ui'
  import StatusIcon from './StatusIcon.svelte'

  let now = Date.now()
  const timer = setInterval(() => (now = Date.now()), 1000)
  onDestroy(() => clearInterval(timer))

  // Tasks whose Cancel was clicked (the clone's state is $cloneCancelling,
  // shared with its dialog).
  let cancelling: Record<string, boolean> = {}
  let listOpen = false
  let chip: HTMLButtonElement | undefined
  let listEl: HTMLUListElement | undefined

  $: tasks = $shownTasks
  $: shown = tasks[0] ?? null
  $: last = $taskState.last
  $: announced = announcement(shown, last)
  // "Cancelling…" goes with its task; the list closes when at most one is left.
  $: {
    const ids = new Set(tasks.map((t) => t.id))
    const kept = Object.fromEntries(Object.entries(cancelling).filter(([id]) => ids.has(id)))
    if (Object.keys(kept).length !== Object.keys(cancelling).length) cancelling = kept
  }
  $: if (listOpen && tasks.length <= 1) listOpen = false

  // The right side: the rate-limit notice of the selected repository's host,
  // else the status of its checked-out commit.
  $: info = $hostingInfo[$selectedRepoId]
  $: headHash = $refs?.headHash ?? ''
  // The request itself ends quietly for a repository that is not hosted.
  $: if ($selectedRepoId && headHash) requestHeadStatus($selectedRepoId, headHash)
  $: limit = info?.supported ? $hostingLimits[info.host] : undefined
  $: headState = isHosted(info) && headHash ? ($commitStatuses[$selectedRepoId]?.[headHash]?.state ?? 'none') : 'none'
  $: headText = headStatusText(info?.noun ?? 'Checks', headState)
  $: headIcon = stateIcon(headState)

  const canCancel = (t: Task) => cancelTarget($taskState, t) !== null
  const isCancelling = (t: Task, flags: Record<string, boolean>, cloneFlag: boolean) => (t.kind === 'clone' ? cloneFlag : !!flags[t.id])

  async function cancel(t: Task) {
    const target = cancelTarget($taskState, t)
    if (!target) return
    if (target.kind === 'clone') {
      await cancelClone(api.cancelClone)
      return
    }
    cancelling = { ...cancelling, [t.id]: true }
    const stays = await cancelTask(target, { cancelPull: api.cancelPull, cancelCommand: api.cancelCommand, toast })
    if (!stays) cancelling = { ...cancelling, [t.id]: false }
  }

  function open(t: Pick<Task | TaskResult, 'kind' | 'repoId'>, running: boolean) {
    const a = openAction(t, running, $repos)
    listOpen = false
    if (!a) return
    if (a.kind === 'clone') void openCloneDialog()
    else if (a.kind === 'providers') {
      settingsTab.set('providers')
      settingsOpen.set(true)
    } else {
      if (a.repoId !== $selectedRepoId) selectRepo(a.repoId)
      commandsOpen.set(true)
    }
  }

  async function toggleList() {
    listOpen = !listOpen
    if (listOpen) {
      await tick()
      listEl?.querySelector<HTMLElement>('[data-row]')?.focus()
    }
  }

  function closeList() {
    listOpen = false
    chip?.focus()
  }

  function onListKey(e: KeyboardEvent) {
    const rows = [...(listEl?.querySelectorAll<HTMLElement>('[data-row]') ?? [])]
    const i = rows.indexOf(document.activeElement as HTMLElement)
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      rows[Math.min(i + 1, rows.length - 1)]?.focus()
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      rows[Math.max(i - 1, 0)]?.focus()
    }
  }

  function onWindowKey(e: KeyboardEvent) {
    if (listOpen && e.key === 'Escape') {
      e.preventDefault()
      closeList()
    }
  }

  function onWindowDown(e: MouseEvent) {
    if (!listOpen) return
    const el = e.target as Node
    if (!listEl?.contains(el) && !chip?.contains(el)) listOpen = false
  }
</script>

<svelte:window on:mousedown={onWindowDown} on:keydown={onWindowKey} />

<footer class="statusbar">
  <!-- One persistent live region, outside the buttons: elapsed time and progress tick outside it. -->
  <span class="sr-only" role="status" aria-live="polite">{announced}</span>
  {#if shown}
    <button class="main" title={shown.label + (shown.repoName ? ` · ${shown.repoName}` : '')} on:click={() => open(shown, true)}>
      <span class="spinner" aria-hidden="true"></span>
      <span class="label">{shown.label}</span>
      {#if shown.repoName}<span class="muted">· {shown.repoName}</span>{/if}
      {#if waitingText(shown, $credentialRequest) || shown.progress}<span class="muted">· {waitingText(shown, $credentialRequest) || shown.progress}</span>{/if}
      {#if elapsedText(shown.start, now)}<span class="muted" aria-hidden="true">· {elapsedText(shown.start, now)}</span>{/if}
    </button>
    {#if canCancel(shown)}
      {@const busyCancelling = isCancelling(shown, cancelling, $cloneCancelling)}
      <button class="btn small" disabled={busyCancelling} on:click={() => cancel(shown)}>{busyCancelling ? 'Cancelling…' : 'Cancel'}</button>
    {/if}
  {:else if last}
    <button class="main result" class:error={last.outcome === 'failed'} title={last.message || last.text} on:click={() => open(last, false)}>
      <span class="label">{outcomeIcon(last.outcome)} {last.text}</span>
      {#if last.repoName}<span class="muted">· {last.repoName}</span>{/if}
      <span class="muted">· {relativeTime(now - last.end)}</span>
    </button>
  {/if}
  <span class="spacer"></span>
  {#if limit}
    <span class="limit muted" role="status">{hostingLimitText(limit)}</span>
  {:else if headText && headIcon}
    <button class="head" title="Show HEAD in the log" on:click={() => showCommitInLog(headHash)}>
      <StatusIcon state={headState} />
      <span class="label">{headText}</span>
    </button>
  {/if}
  {#if tasks.length > 1}
    <button bind:this={chip} class="chip" aria-haspopup="true" aria-expanded={listOpen} on:click={toggleList}>+{tasks.length - 1}</button>
  {/if}
  {#if listOpen}
    <!-- svelte-ignore a11y_no_noninteractive_element_interactions (arrow keys move between the rows) -->
    <ul class="list" bind:this={listEl} aria-label="Running tasks" on:keydown={onListKey}>
      {#each tasks as t (t.id)}
        <li>
          <button data-row class="row" on:click={() => open(t, true)}>
            <span class="spinner" aria-hidden="true"></span>
            <span class="label">{t.label}</span>
            {#if t.repoName}<span class="muted">· {t.repoName}</span>{/if}
            {#if waitingText(t, $credentialRequest) || t.progress}<span class="muted">· {waitingText(t, $credentialRequest) || t.progress}</span>{/if}
            {#if elapsedText(t.start, now)}<span class="muted">· {elapsedText(t.start, now)}</span>{/if}
          </button>
          {#if canCancel(t)}
            {@const rowCancelling = isCancelling(t, cancelling, $cloneCancelling)}
            <button class="btn small" disabled={rowCancelling} on:click={() => cancel(t)}>{rowCancelling ? 'Cancelling…' : 'Cancel'}</button>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}
</footer>

<style>
  .statusbar { position: relative; flex: none; height: 22px; display: flex; align-items: center; gap: 6px; padding: 0 8px; border-top: 1px solid var(--border); background: var(--sidebar); font-size: 12px; color: var(--text); min-width: 0; }
  .main { display: flex; align-items: center; gap: 4px; min-width: 0; overflow: hidden; }
  .label { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; min-width: 0; }
  .muted { color: var(--muted); white-space: nowrap; }
  .result .label { color: var(--muted); }
  .result.error .label { color: var(--danger); }
  .sr-only { position: absolute; width: 1px; height: 1px; margin: -1px; padding: 0; overflow: hidden; clip: rect(0 0 0 0); white-space: nowrap; border: 0; }
  .spacer { flex: 1; }
  .head { display: flex; align-items: center; gap: 4px; flex: none; padding: 0 4px; border-radius: 4px; }
  .head:hover { background: var(--hover); }
  .limit { flex: none; }
  .chip { font-size: 11px; padding: 0 6px; border-radius: 8px; border: 1px solid var(--border); background: var(--surface); }
  .chip:hover { background: var(--hover); }
  .list { position: absolute; right: 8px; bottom: 26px; z-index: 50; margin: 0; padding: 4px; list-style: none; min-width: 320px; max-width: min(560px, calc(100vw - 16px)); max-height: 50vh; overflow: auto; background: var(--surface); border: 1px solid var(--border); border-radius: 6px; box-shadow: var(--shadow); }
  .list li { display: flex; align-items: center; gap: 6px; }
  .row { flex: 1; display: flex; align-items: center; gap: 4px; min-width: 0; padding: 4px 6px; text-align: left; border-radius: 4px; }
  .row:hover, .row:focus-visible { background: var(--hover); }
  .spinner { flex: none; display: inline-block; width: 9px; height: 9px; border: 2px solid var(--border); border-top-color: var(--accent); border-radius: 50%; animation: spin 0.8s linear infinite; }
  .btn.small { flex: none; font-size: 11px; padding: 0 6px; height: 18px; border-radius: 5px; }
  @keyframes spin { to { transform: rotate(360deg); } }
  @media (prefers-reduced-motion: reduce) { .spinner { animation: none; } }
</style>
