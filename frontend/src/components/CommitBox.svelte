<script lang="ts">
  import { onDestroy } from 'svelte'
  import { EventsOn } from '../../wailsjs/runtime/runtime'
  import Icon from './Icon.svelte'
  import { api } from '../lib/api'
  import { commitChanges, stashChanges } from '../lib/actions'
  import { amendWarning, canCommit, shouldAutoGenerate } from '../lib/worktree'
  import type { CommitDeltaEvent, CommitDoneEvent, CommitInfo, WorktreeChangedEvent } from '../lib/types'
  import { aiSettings, busy, worktreeState } from '../lib/stores'
  import { confirmDialog, errorMessage, toast } from '../lib/ui'

  export let repoId: string

  let info: CommitInfo | null = null
  let message = ''
  let touched = false
  let amend = false
  let preAmendMessage = ''
  let runID: string | null = null
  let request = 0
  // Safety net for the no-backend-cancellation limitation: if commit:done
  // never arrives (a crashed provider call, a dropped event) this clears the
  // run after a while so the box doesn't stay disabled with Stop as the only
  // escape. It is cleared on any normal completion, error, or manual Stop.
  let stallTimer: ReturnType<typeof setTimeout> | null = null

  const offDelta = EventsOn('commit:delta', (payload: CommitDeltaEvent) => {
    if (payload.repoID !== repoId || payload.runID !== runID) return
    message += payload.text
  })
  const offDone = EventsOn('commit:done', (payload: CommitDoneEvent) => {
    if (payload.repoID !== repoId || payload.runID !== runID) return
    clearStallTimer()
    if (payload.error) {
      toast(payload.error, 'error')
    } else if (message.trim() !== '') {
      // A completed, non-empty generation is content worth keeping: mark it
      // touched so unticking Amend afterwards doesn't silently discard it
      // (see toggleAmend's restore-on-untick, which only replaces an
      // untouched box).
      touched = true
    }
    runID = null
  })
  const offChanged = EventsOn('worktree:changed', (payload: WorktreeChangedEvent) => {
    if (payload?.repoID !== repoId) return
    refresh()
  })
  onDestroy(() => {
    offDelta()
    offDone()
    offChanged()
    clearStallTimer()
  })

  $: load(repoId)
  // A separate reactive value so the auto-generate check below depends only
  // on the staged COUNT, not on the whole `info` object — Svelte tracks
  // whatever variables a reactive statement's expression reads, and `info`
  // appears in `info?.stagedCount` just as much as it would in `info` alone.
  // Folding that lookup directly into maybeAutoGenerate(...)'s argument (the
  // previous version) meant the statement depended on `info` itself and
  // re-ran on every preview reload — including a worktree:changed that never
  // touched the index. Naming the derived count here, and depending only on
  // that name below, is what limits re-firing to when the count changes.
  $: stagedCount = info?.stagedCount ?? 0
  $: maybeAutoGenerate(stagedCount)
  $: hasChanges = !!$worktreeState && ($worktreeState.staged.length > 0 || $worktreeState.unstaged.length > 0 || $worktreeState.untracked.length > 0)

  async function load(id: string) {
    message = ''
    touched = false
    amend = false
    runID = null
    if (!id) {
      info = null
      return
    }
    await refresh()
  }

  async function refresh() {
    const current = ++request
    try {
      const next = await api.getCommitPreview(repoId)
      if (current !== request) return
      info = next
    } catch (e) {
      if (current === request) toast(errorMessage(e), 'error')
    }
  }

  function maybeAutoGenerate(count: number) {
    // Nothing staged: Go's GenerateCommitMessage rejects this with
    // ErrNothingStaged, and the shipped defaults (taskProvider "ollama",
    // commitMessage "auto-local") would otherwise fire on every clean open
    // and after every commit — see shouldAutoGenerate's own guard too.
    if (!info || count <= 0 || !$aiSettings) return
    if (shouldAutoGenerate($aiSettings.commitMessage, $aiSettings.taskProvider, message, touched, count)) generate()
  }

  function onInput() {
    touched = message.trim() !== ''
  }

  const STALL_MS = 60_000

  function clearStallTimer() {
    if (stallTimer) {
      clearTimeout(stallTimer)
      stallTimer = null
    }
  }

  async function generate() {
    if (runID || !repoId) return
    const id = crypto.randomUUID()
    // Captured before clearing the box: the backend call can reject
    // immediately (no provider configured, nothing staged), and whatever the
    // user had typed must come back rather than be lost to an empty box.
    const previous = message
    runID = id
    message = ''
    clearStallTimer()
    stallTimer = setTimeout(() => {
      if (runID !== id) return
      runID = null
      toast('The AI did not finish writing a message — try again.', 'error')
    }, STALL_MS)
    try {
      await api.generateCommitMessage(repoId, id)
    } catch (e) {
      if (runID === id) {
        runID = null
        clearStallTimer()
        message = previous
      }
      toast(errorMessage(e), 'error')
    }
  }

  // Stop only stops this box from CONSUMING the stream — the events above
  // check runID against this, so once it's cleared any further commit:delta
  // for that run is ignored and the button re-enables. There is no backend
  // cancellation for GenerateCommitMessage (unlike chat): the provider call
  // itself keeps running on the Go side until it finishes; this box simply
  // stops listening to it.
  function stopGenerating() {
    runID = null
    clearStallTimer()
  }

  function toggleAmend() {
    // Amend is disabled in the template while a run is live; this guard is
    // defensive only. Clearing runID here instead (to "allow" toggling
    // mid-stream) would not stop the Go-side call — with no backend
    // cancellation, the live delta handler would keep appending onto
    // whatever this function put in the box, mixing the old message with an
    // AI continuation. Disabling is the only correct option.
    if (runID) return
    amend = !amend
    if (amend) {
      if (!touched) {
        preAmendMessage = message
        message = info?.lastMessage ?? ''
        touched = false
      }
    } else {
      if (!touched) message = preAmendMessage
    }
  }

  async function commit() {
    if (!info || !canCommit(info, message, amend) || $busy) return
    if (amend) {
      const warning = amendWarning(info)
      if (warning) {
        const ok = await confirmDialog({ title: 'Amend commit', message: warning, confirmLabel: 'Amend anyway', danger: true })
        if (!ok) return
      }
    }
    const ok = await commitChanges(repoId, message, amend)
    if (ok) {
      message = ''
      touched = false
      amend = false
    }
    await refresh()
  }
</script>

<div class="commit-box">
  <textarea
    rows="3"
    placeholder="Commit message"
    bind:value={message}
    on:input={onInput}
    disabled={!!runID}
  ></textarea>
  <div class="row">
    <label class="amend">
      <input type="checkbox" checked={amend} disabled={!info?.canAmend || !!runID} on:change={toggleAmend} />
      Amend
    </label>
    <span class="spacer"></span>
    {#if runID}
      <button class="btn" on:click={stopGenerating}><Icon name="stop" /> Stop</button>
    {:else}
      <button class="btn" disabled={!repoId} on:click={generate}><Icon name="sparkle" /> Write with AI</button>
    {/if}
    <button class="btn" disabled={!repoId || !!$busy || !hasChanges} on:click={() => stashChanges(repoId)}>Stash…</button>
    <button class="btn primary" disabled={!canCommit(info, message, amend) || !!$busy} on:click={commit}>
      {amend ? 'Amend' : 'Commit'}
    </button>
  </div>
</div>

<style>
  .commit-box { display: flex; flex-direction: column; gap: 8px; padding: 10px 12px; border-top: 1px solid var(--border); background: var(--surface); flex: none; }
  textarea { resize: vertical; padding: 8px; border: 1px solid var(--border); border-radius: 8px; background: var(--bg); font: inherit; }
  .row { display: flex; align-items: center; gap: 8px; }
  .amend { display: flex; align-items: center; gap: 4px; font-size: 12px; color: var(--muted); }
  .spacer { flex: 1; }
</style>
