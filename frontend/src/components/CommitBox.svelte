<script lang="ts">
  import { onDestroy } from 'svelte'
  import { EventsOn } from '../../wailsjs/runtime/runtime'
  import Icon from './Icon.svelte'
  import { api } from '../lib/api'
  import { amendWarning, canCommit, shouldAutoGenerate } from '../lib/worktree'
  import type { CommitDeltaEvent, CommitDoneEvent, CommitInfo, WorktreeChangedEvent } from '../lib/types'
  import { aiSettings, busy, loadWorktreeState } from '../lib/stores'
  import { confirmDialog, errorMessage, toast } from '../lib/ui'

  export let repoId: string

  let info: CommitInfo | null = null
  let message = ''
  let touched = false
  let amend = false
  let preAmendMessage = ''
  let runID: string | null = null
  let request = 0

  const offDelta = EventsOn('commit:delta', (payload: CommitDeltaEvent) => {
    if (payload.repoID !== repoId || payload.runID !== runID) return
    message += payload.text
  })
  const offDone = EventsOn('commit:done', (payload: CommitDoneEvent) => {
    if (payload.repoID !== repoId || payload.runID !== runID) return
    if (payload.error) toast(payload.error, 'error')
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
  })

  $: load(repoId)
  // Whenever the staged set changes (info.stagedCount moves), decide whether
  // to write the message for the user. Keyed off stagedCount rather than the
  // whole object so this doesn't refire on every unrelated preview reload.
  $: maybeAutoGenerate(info?.stagedCount ?? 0)

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

  function maybeAutoGenerate(_stagedCount: number) {
    if (!info || !$aiSettings) return
    if (shouldAutoGenerate($aiSettings.commitMessage, $aiSettings.taskProvider, message, touched)) generate()
  }

  function onInput() {
    touched = message.trim() !== ''
  }

  async function generate() {
    if (runID || !repoId) return
    const id = crypto.randomUUID()
    runID = id
    message = ''
    try {
      await api.generateCommitMessage(repoId, id)
    } catch (e) {
      if (runID === id) runID = null
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
  }

  function toggleAmend() {
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
    busy.set('Committing…')
    try {
      await api.commitChanges(repoId, message, amend)
      message = ''
      touched = false
      amend = false
      await loadWorktreeState()
      await refresh()
    } catch (e) {
      toast(errorMessage(e), 'error')
    } finally {
      busy.set('')
    }
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
      <input type="checkbox" checked={amend} disabled={!info?.canAmend} on:change={toggleAmend} />
      Amend
    </label>
    <span class="spacer"></span>
    {#if runID}
      <button class="btn" on:click={stopGenerating}><Icon name="stop" /> Stop</button>
    {:else}
      <button class="btn" disabled={!repoId} on:click={generate}><Icon name="sparkle" /> Write with AI</button>
    {/if}
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
