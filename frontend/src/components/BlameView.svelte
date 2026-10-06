<script lang="ts">
  import { api } from '../lib/api'
  import { blocksIn, lineBlocks, previousBlocker, rangeAt, revLabel, selectLine, singleCommit, type LineRange } from '../lib/blame'
  import { relativeDate } from '../lib/format'
  import { aiOff, blameBack, blameIgnoreWhitespace, blamePrevious, blameTarget, chatOpen, explainIntoChat, showCommitInLog, worktreeState } from '../lib/stores'
  import type { Blame, BlameBlock } from '../lib/types'
  import { copyText, errorMessage, openMenu, toast } from '../lib/ui'

  export let repoId: string

  let blame: Blame | null = null
  let error = ''
  let loading = false
  let request = 0
  let selection: LineRange | null = null
  let anchor: number | null = null
  // Tracks the path/rev this component last loaded (or is loading), so a
  // change to either can clear the stale `blame` synchronously — otherwise
  // old rows stay under the new header until the new blame resolves. Not
  // reset by an ignore-whitespace toggle, which reuses the same path/rev.
  let loadedKey = ''
  // The worktreeState value last seen, so a reactive statement that reads
  // both target and worktreeState (to reload a working-tree blame on an
  // edit) does not also refire on every target or ignore-whitespace change
  // it happens to reference — see the two `$:` blocks below. It starts at
  // the current value so opening the view does not count as a change.
  let lastWorktreeState: unknown = $worktreeState

  $: target = $blameTarget
  $: if (target) {
    const key = target.path + '\u0000' + target.rev
    if (key !== loadedKey) {
      loadedKey = key
      blame = null
      selection = null
      anchor = null
    }
    load(repoId, target.path, target.rev, $blameIgnoreWhitespace)
  }
  // A working-tree blame follows edits made while it is open. Guarded on
  // worktreeState actually changing (not just being read) so opening the
  // view or toggling ignore-whitespace — which also touch this statement's
  // dependencies — do not start a second, redundant blame alongside the
  // one above.
  $: {
    const ws = $worktreeState
    if (ws !== lastWorktreeState) {
      lastWorktreeState = ws
      if (target?.rev === '') load(repoId, target.path, '', $blameIgnoreWhitespace)
    }
  }
  $: owners = blame ? lineBlocks(blame) : []

  async function load(id: string, path: string, rev: string, ws: boolean) {
    const current = ++request
    loading = true
    error = ''
    try {
      const b = await api.getBlame(id, rev, path, ws)
      if (current !== request) return
      const contentChanged = blame?.lines.length !== b.lines.length || blame?.lines.join('\n') !== b.lines.join('\n')
      if (blame?.path !== b.path || blame?.rev !== b.rev || contentChanged) { selection = null; anchor = null }
      blame = b
    } catch (e) {
      if (current === request) { blame = null; error = errorMessage(e) }
    } finally {
      if (current === request) loading = false
    }
  }

  function retry() {
    if (target) load(repoId, target.path, target.rev, $blameIgnoreWhitespace)
  }

  function clickLine(event: MouseEvent, line: number) {
    ;({ selection, anchor } = selectLine(selection, anchor, line, event.shiftKey))
  }

  function inSelection(line: number): boolean {
    return !!selection && line >= selection.start && line <= selection.end
  }

  async function explain(range: LineRange) {
    // blame.path/blame.rev, not target: a row's line range only matches the
    // blame that produced it, and target can already point at a different
    // revision while that blame is still loading (see loadedKey above).
    if (!blame) return
    chatOpen.set(true)
    try {
      const { rev, path } = blame
      await explainIntoChat(repoId, () => api.explainLinesInChat(repoId, rev, path, range.start, range.end, '', crypto.randomUUID()))
    } catch (e) {
      toast(errorMessage(e), 'error')
    }
  }

  function menu(event: MouseEvent, line: number) {
    if (!blame) return
    const range = rangeAt(blame, selection, line)
    const blocks = blocksIn(blame, range)
    const commit = singleCommit(blocks)
    const prevWhy = previousBlocker(blocks)
    openMenu(event, [
      ...($aiOff ? [] : [{ label: '✨ Explain these lines in chat', action: () => explain(range) }]),
      {
        label: 'Blame previous revision',
        action: () => commit?.previous && blamePrevious(commit.prevPath || commit.filename, commit.previous),
        disabled: prevWhy !== null,
        title: prevWhy ?? undefined,
      },
      { label: 'Show commit', action: () => commit && showCommitInLog(commit.hash), disabled: !commit },
      { label: 'Copy hash', action: () => commit && copyText(commit.hash), disabled: !commit },
    ])
  }

  function gutter(b: BlameBlock): string {
    if (b.uncommitted) return 'Not committed yet'
    return `${b.boundary ? '^' : ''}${b.short} · ${b.author} · ${relativeDate(b.date)}`
  }
</script>

<div class="blame">
  <div class="header">
    <button class="back" on:click={blameBack}>← Back</button>
    <span class="path ellipsis mono" title={target?.path}>{target?.path}</span>
    <span class="rev mono">{revLabel(target?.rev ?? '')}</span>
    <label class="ws"><input type="checkbox" bind:checked={$blameIgnoreWhitespace} /> Ignore whitespace</label>
  </div>
  {#if blame?.truncated}<div class="notice">Showing the first 20 000 lines</div>{/if}
  <div class="body mono">
    {#if error}
      <div class="error">{error} <button on:click={retry}>Retry</button></div>
    {:else if !blame && loading}
      <div class="empty">Loading blame…</div>
    {:else if blame}
      {#each blame.lines as text, i}
        {@const line = blame.startLine + i}
        {@const bi = owners[i]}
        {@const b = blame.blocks[bi]}
        <div class="row" class:band={bi % 2 === 1} class:selected={inSelection(line)} on:contextmenu|preventDefault={(e) => menu(e, line)}>
          <div class="gutter" class:faint={b.uncommitted} title={b.uncommitted ? '' : b.summary}>
            {#if line === b.start || i === 0}
              {#if b.uncommitted}
                {gutter(b)}
              {:else}
                <button class="hash" on:click={() => showCommitInLog(b.hash)}>{gutter(b)}</button>
              {/if}
            {/if}
          </div>
          <button class="num" on:click={(e) => clickLine(e, line)}>{line}</button>
          <div class="text">{text || ' '}</div>
        </div>
      {/each}
    {/if}
  </div>
</div>

<style>
  .blame { display: flex; flex-direction: column; height: 100%; }
  .header { display: flex; align-items: center; gap: 10px; padding: 8px 12px; border-bottom: 1px solid var(--border); font-size: 12px; }
  .back { color: var(--accent); }
  .path { min-width: 0; flex: 1; }
  .rev, .ws { color: var(--muted); flex: none; }
  .notice { padding: 4px 12px; font-size: 12px; color: var(--muted); border-bottom: 1px solid var(--border); }
  .body { flex: 1; overflow: auto; padding: 4px 0; -webkit-user-select: text; user-select: text; }
  .row { display: grid; grid-template-columns: 260px 48px 1fr; line-height: 18px; content-visibility: auto; contain-intrinsic-size: auto 18px; }
  .row.band { background: color-mix(in srgb, var(--hover) 50%, transparent); }
  .row.selected { background: var(--add-bg); }
  .gutter { padding: 0 8px 0 12px; overflow: hidden; white-space: nowrap; text-overflow: ellipsis; color: var(--muted); font-size: 12px; }
  .gutter.faint { color: var(--faint); }
  .hash { color: inherit; max-width: 100%; overflow: hidden; text-overflow: ellipsis; text-align: left; }
  .hash:hover { color: var(--accent); text-decoration: underline; }
  .num { text-align: right; padding-right: 8px; color: var(--faint); }
  .text { white-space: pre; padding-right: 12px; }
  .empty { padding: 12px; color: var(--faint); }
  .error { padding: 12px; color: var(--danger); white-space: pre-wrap; }
</style>
