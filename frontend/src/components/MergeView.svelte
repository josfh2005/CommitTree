<script lang="ts">
  import { EventsOn } from '../../wailsjs/runtime/runtime'
  import Icon from './Icon.svelte'
  import FileList, { rowKey } from './FileList.svelte'
  import { api } from '../lib/api'
  import { abortMerge, commitMerge, dismissStashConflict, resolveConflicts, resolveMergeRegion, restartConflictFile, skipStep, stageMergeFile, stashDrop, takeMergeSide, unstageMergeFile } from '../lib/actions'
  import { conflictActions, conflictHeader, keptEdit, layoutLines, mergeSections, regionSides, takeLabels, withLineEndings, type MergeFile } from '../lib/merge'
  import { lineClass } from '../lib/diff'
  import { nextSelection, type SelectionKey } from '../lib/worktree'
  import { busy, chatRunRepo, loadMergeState, mergeState, owedStashDrop, pendingFinish } from '../lib/stores'
  import type { Region } from '../lib/types'
  import { finishingLine } from '../lib/flow'
  import { errorMessage, openMenu, toast } from '../lib/ui'
  import { onDestroy } from 'svelte'
  import { isApplyKey } from '../lib/shortcuts'

  export let repoId: string

  // `selection` is the continuity anchor: which path (and, to break a tie,
  // which section) the user is looking at. It survives a resolve/stage that
  // moves the file to a different section — see nextSelection. `selected` is
  // just its rowKey(...) projection, for FileList's highlighting/onSelect.
  let selection: SelectionKey | null = null
  let text = ''
  let resolved = false
  let error = ''
  let request = 0

  let regions: Region[] = []
  let restartable = false
  // What a hovered Take button would keep, to dim the rest of its region.
  let hover: { id: string; keep: 'ours' | 'theirs' } | null = null
  // The region being edited by hand, and its text box's content.
  // sample is the region's original text, whose line endings the edit keeps.
  let editing: { id: string; value: string; sample: string } | null = null

  $: lines = text.split('\n')
  $: layout = layoutLines(lines, regions)
  $: locked = !!$busy || $chatRunRepo === repoId
  $: sides = takeLabels($mergeState)
  $: current = files.find((f) => selection && f.path === selection.path)

  function dimmed(i: number): boolean {
    const l = layout[i]
    if (!hover || l.region?.id !== hover.id) return false
    return l.part !== hover.keep
  }

  function take(regionId: string, choice: 'ours' | 'theirs' | 'both') {
    if (!selection) return
    hover = null
    resolveMergeRegion(repoId, selection.path, regionId, choice, '', refresh)
  }

  function startEdit(regionId: string) {
    const s = regionSides(lines, layout, regionId)
    editing = { id: regionId, value: s.ours + s.theirs, sample: s.ours + s.theirs }
  }

  function applyEdit() {
    if (!selection || !editing) return
    const { id, value, sample } = editing
    editing = null
    resolveMergeRegion(repoId, selection.path, id, 'text', withLineEndings(value, sample), refresh)
  }

  function editKeys(e: KeyboardEvent) {
    if (isApplyKey(e)) { e.preventDefault(); applyEdit() }
    if (e.key === 'Escape') { e.preventDefault(); editing = null }
  }

  // The merge commit's message while it is being edited; null when the
  // editor is closed. Commit merge opens it with git's prepared message.
  let commitMsg: string | null = null

  async function openMessage() {
    try {
      commitMsg = await api.getMergeMessage(repoId)
    } catch (e) {
      toast(errorMessage(e), 'error')
    }
  }

  async function commitWithMessage() {
    if (commitMsg === null || !commitMsg.trim() || locked || pending > 0) return
    if (await commitMerge(repoId, commitMsg)) commitMsg = null
  }

  function messageKeys(e: KeyboardEvent) {
    if (isApplyKey(e)) { e.preventDefault(); commitWithMessage() }
    if (e.key === 'Escape') { e.preventDefault(); commitMsg = null }
  }

  // The editor belongs to the merge it was opened for.
  $: if ($mergeState?.kind !== 'merge') commitMsg = null

  // The pane follows $mergeState (below), so the handler only reloads it.
  const off = EventsOn('merge:changed', (payload: { repoID: string }) => {
    if (payload?.repoID !== repoId) return
    loadMergeState()
  })
  onDestroy(off)

  $: sections = $mergeState ? mergeSections($mergeState) : []
  $: files = sections.flatMap((s) => s.files)
  // Which section (by object identity) each file came from, so a row's key
  // can be reconstructed from the file alone.
  $: fileSection = new Map(sections.flatMap((s) => s.files.map((f) => [f, s.title] as const)))
  $: pending = ($mergeState?.conflicts.length ?? 0) + ($mergeState?.manual.length ?? 0)
  $: head = $mergeState ? conflictHeader($mergeState) : null
  $: flowLine = finishingLine($pendingFinish, repoId)
  $: acts = $mergeState ? conflictActions($mergeState) : { abort: null, confirm: null, ai: false, done: false, skip: false }
  $: selected = selection ? rowKey(selection.section, selection.path) : ''
  // Re-key the selection onto wherever its path now lives (a resolve/stage
  // moves a file Conflicts → Unstaged → Staged, but it's still the file the
  // user had open) and re-read it — after an agent edit, an action, or a
  // focus that caught a change made in a terminal. This must NOT fall back
  // to the first file just because the path changed section.
  $: if ($mergeState) {
    selection = nextSelection(selection, sections)
    refresh()
  }

  function keyOf(file: MergeFile): string {
    return rowKey(fileSection.get(file) ?? '', file.path)
  }

  async function open(file: MergeFile) {
    const current = ++request
    selection = { section: fileSection.get(file) ?? '', path: file.path }
    text = ''
    regions = []
    restartable = false
    error = ''
    try {
      const f = await api.getConflictFile(repoId, file.path)
      if (current !== request) return
      text = f.text
      resolved = f.resolved
      regions = f.regions ?? []
      restartable = f.restartable
      editing = null
    } catch (e) {
      if (current === request) error = errorMessage(e)
    }
  }

  // Re-reads the open file in place — no blank flash — after the merge state
  // changed. Uses `selection.path` directly rather than looking the file up
  // in `files`, since a path that only changed section is still valid.
  async function refresh() {
    if (!selection) return
    const current = ++request
    const path = selection.path
    try {
      const f = await api.getConflictFile(repoId, path)
      if (current !== request) return
      text = f.text
      resolved = f.resolved
      regions = f.regions ?? []
      restartable = f.restartable
      const kept = keptEdit(editing, regions)
      if (editing && !kept) toast('The region you were editing was resolved meanwhile; your edit was dropped.', 'error')
      editing = kept
      error = ''
    } catch (e) {
      if (current === request) error = errorMessage(e)
    }
  }

  // A Manual file is settled by taking one side whole, after a confirmation:
  // it overwrites the worktree copy, which Unstage does not restore.
  function manualMenu(event: MouseEvent, file: { path: string; status: string }) {
    if (file.status !== 'manual') return
    const { ours, theirs } = takeLabels($mergeState)
    openMenu(event, [
      { label: `Take ${ours}`, action: () => takeMergeSide(repoId, file.path, 'ours', ours), disabled: !!$busy },
      { label: `Take ${theirs}`, action: () => takeMergeSide(repoId, file.path, 'theirs', theirs), disabled: !!$busy },
    ])
  }

  function glyph(status: string): string {
    if (status === 'manual') return '!'
    if (status === 'unstaged') return 'M'
    return '·'
  }

  function actionsFor(file: { path: string; status: string }) {
    // A plain text conflict is staged the same way an unstaged file is —
    // merge.Stage refuses one that still has conflict markers left in it,
    // and that error surfaces as a toast, so no extra guard is needed here.
    if (file.status === 'conflict' || file.status === 'unstaged') {
      return [{ label: 'Stage', run: () => stageMergeFile(repoId, file.path), disabled: !!$busy, title: 'Add to the merge commit' }]
    }
    if (file.status === 'staged') {
      return [{ label: 'Unstage', run: () => unstageMergeFile(repoId, file.path), disabled: !!$busy, title: 'Take out of the merge commit, keeping the content' }]
    }
    return []
  }
</script>

<div class="merge">
  <header>
    {#if head}
      <span class="title">
        {head.lead}
        {#if head.from}<strong>{head.from}</strong>{/if}
        {#if head.connector}{head.connector} <strong>{head.into}</strong>{/if}
      </span>
      {#if head.detail}<span class="count">{head.detail}</span>{/if}
    {/if}
    <span class="count">{pending} left</span>
    <span class="spacer"></span>
    {#if acts.ai}
      <button class="btn" disabled={locked || pending === 0} title={$chatRunRepo === repoId ? "The AI is resolving; stop it from the chat" : undefined} on:click={() => resolveConflicts(repoId)}>
        <Icon name="sparkle" size={14} /><span>Resolve with AI</span>
      </button>
    {/if}
    {#if acts.abort}
      <button class="btn" disabled={!!$busy} title={$chatRunRepo === repoId ? "Stops the AI resolver, then aborts" : undefined} on:click={() => abortMerge(repoId)}>{acts.abort}</button>
    {/if}
    {#if acts.skip}
      <button class="btn" disabled={!!$busy} on:click={() => skipStep(repoId)}>Skip this commit</button>
    {/if}
    {#if acts.confirm}
      <button class="btn primary" disabled={locked || pending > 0} title={$chatRunRepo === repoId ? "The AI is resolving; wait for it or stop it from the chat" : undefined} on:click={() => ($mergeState?.kind === 'merge' ? openMessage() : commitMerge(repoId))}>{acts.confirm}</button>
    {/if}
    {#if acts.done}
      <!-- A stash conflict has no git-level abort or continue. Drop stash is
           the only way to get rid of the entry a conflicted Pop deliberately
           kept; Done leaves the files exactly as they are and gives the
           screen back (the toolbar's "Resolve conflicts" brings it back). -->
      {#if $owedStashDrop >= 0}
        <button class="btn" disabled={!!$busy} on:click={() => stashDrop(repoId, $owedStashDrop)}>Drop stash</button>
      {/if}
      <button class="btn primary" disabled={!!$busy} on:click={dismissStashConflict}>Done</button>
    {/if}
  </header>
  {#if flowLine}<div class="flow-note">{flowLine}</div>{/if}
  {#if commitMsg !== null}
    <div class="commit-msg">
      <!-- svelte-ignore a11y_autofocus -->
      <textarea rows="4" aria-label="Merge commit message" autofocus bind:value={commitMsg} on:keydown={messageKeys}></textarea>
      <div class="commit-msg-actions">
        <button class="btn" on:click={() => (commitMsg = null)}>Cancel</button>
        <button class="btn primary" title="⌘↵ / Ctrl+Enter" disabled={locked || pending > 0 || !commitMsg.trim()} on:click={commitWithMessage}>Commit</button>
      </div>
    </div>
  {/if}

  <div class="body">
    <FileList {sections} {selected} onSelect={(key) => { const f = files.find((ff) => keyOf(ff) === key); if (f) open(f) }} actions={actionsFor} onMenu={manualMenu} {glyph} />
    <div class="content mono">
      {#if error}
        <div class="error">{error}</div>
      {:else}
        {#if selection && (restartable || current?.status === 'manual')}
          <div class="file-actions">
            {#if current?.status === 'manual'}
              <button class="btn" disabled={locked} on:click={() => selection && takeMergeSide(repoId, selection.path, 'ours', sides.ours)}>Take {sides.ours}</button>
              <button class="btn" disabled={locked} on:click={() => selection && takeMergeSide(repoId, selection.path, 'theirs', sides.theirs)}>Take {sides.theirs}</button>
            {/if}
            <span class="spacer"></span>
            {#if restartable}
              <button class="btn" disabled={locked} title="Put the file back as the merge left it, with its conflict markers" on:click={() => selection && restartConflictFile(repoId, selection.path)}>Restart file</button>
            {/if}
          </div>
        {/if}
        {#each lines as line, i}
          {@const l = layout[i]}
          {#if l.starts && l.region}
            {@const id = l.region.id}
            {#if editing?.id === id}
              <div class="region-edit">
                <textarea class="mono" rows={Math.max(3, editing.value.split('\n').length)} bind:value={editing.value} on:keydown={editKeys}></textarea>
                <div class="region-bar">
                  <button class="btn primary" on:click={applyEdit}>Apply</button>
                  <button class="btn" on:click={() => (editing = null)}>Cancel</button>
                </div>
              </div>
            {:else}
              <div class="region-bar">
                <button class="btn" disabled={locked} on:mouseenter={() => (hover = { id, keep: 'ours' })} on:mouseleave={() => (hover = null)} on:click={() => take(id, 'ours')}>Take {sides.ours}</button>
                <button class="btn" disabled={locked} on:mouseenter={() => (hover = { id, keep: 'theirs' })} on:mouseleave={() => (hover = null)} on:click={() => take(id, 'theirs')}>Take {sides.theirs}</button>
                <button class="btn" disabled={locked} on:click={() => take(id, 'both')}>Both</button>
                <button class="btn" disabled={locked} on:click={() => startEdit(id)}>Edit…</button>
              </div>
            {/if}
          {/if}
          {#if !(editing && l.region?.id === editing.id)}
            <div class="line {lineClass(line, resolved)}" class:dim={dimmed(i)}>{line || ' '}</div>
          {/if}
        {/each}
      {/if}
    </div>
  </div>
</div>

<style>
  .merge { display: flex; flex-direction: column; height: 100%; }
  .flow-note { padding: 4px 10px; font-size: 12px; color: var(--muted); border-bottom: 1px solid var(--border); }
  .commit-msg { display: flex; flex-direction: column; gap: 6px; padding: 8px 10px; border-bottom: 1px solid var(--border); flex: none; }
  .commit-msg textarea { box-sizing: border-box; width: 100%; resize: vertical; font-family: var(--mono); font-size: 12px; }
  .commit-msg-actions { display: flex; justify-content: flex-end; gap: 6px; }
  header { display: flex; align-items: center; gap: 8px; padding: 6px 10px; border-bottom: 1px solid var(--border); flex: none; }
  .title { font-size: 13px; }
  .count { font-size: 12px; color: var(--muted); }
  .spacer { flex: 1; }
  .body { display: grid; grid-template-columns: minmax(260px, 36%) 1fr; flex: 1; min-height: 0; }
  .content { overflow: auto; padding: 8px 0; -webkit-user-select: text; user-select: text; }
  .line { padding: 0 12px; white-space: pre; line-height: 18px; }
  .marker { background: var(--hover); color: var(--muted); font-weight: 600; }
  .add { background: var(--add-bg); }
  .del { background: var(--del-bg); }
  .hunk { color: var(--accent); }
  .meta { color: var(--faint); }
  .error { padding: 12px; color: var(--danger); white-space: pre-wrap; }
  .file-actions { display: flex; gap: 6px; padding: 0 12px 8px; }
  .file-actions .spacer { flex: 1; }
  .region-bar { display: flex; gap: 4px; padding: 3px 12px; }
  /* Compact, in the UI font (the pane itself is monospace), on a light
     grey so they read as controls, not as file content. */
  .region-bar .btn { font: 11px/16px var(--font); padding: 1px 8px; background: var(--hover); border-color: transparent; border-radius: 5px; }
  .region-bar .btn:hover:not(:disabled) { background: var(--active); }
  .region-bar .btn.primary { background: var(--text); color: var(--bg); }
  .region-bar .btn.primary:hover:not(:disabled) { background: var(--text); opacity: 0.85; }
  .region-edit { padding: 4px 12px; }
  .region-edit textarea { width: 100%; box-sizing: border-box; font-size: 12px; line-height: 18px; padding: 6px 8px; border: 1px solid var(--border); border-radius: 6px; background: var(--bg); color: var(--text); resize: vertical; }
  .line.dim { opacity: 0.35; }
</style>
