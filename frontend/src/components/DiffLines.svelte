<script lang="ts">
  import { applyHunkSelection } from '../lib/actions'
  import { clickSelect, diffRows, emptySelection, escapeClears, keyMods, pickSummary, rowKey, selectable, selectionKey, toPicks, type LineSelection } from '../lib/hunks'
  import { busy, settingsOpen } from '../lib/stores'
  import { dialog, menu } from '../lib/ui'
  import type { HunkAction, HunkPick, WorktreeDiff } from '../lib/types'

  export let repoId: string
  export let path: string
  export let staged: boolean
  export let diff: WorktreeDiff

  let sel: LineSelection = emptySelection()
  let root: HTMLElement

  $: parsed = diffRows(diff.text, diff.truncated)
  $: actionable = diff.patchable ? parsed.actionable : new Set<number>()
  // Another file, section or diff content drops the selection; a reload that
  // brings back the same diff (the key is a string, compared by value) keeps it.
  $: key = selectionKey(path, staged, diff)
  $: key, (sel = emptySelection())
  $: actions = (staged ? [['unstage', 'Unstage']] : [['stage', 'Stage'], ['discard', 'Discard']]) as [HunkAction, string][]

  function click(event: MouseEvent, index: number) {
    // A drag that selected text to copy is not a line click.
    if (!event.shiftKey && window.getSelection()?.toString()) return
    sel = clickSelect(parsed.rows, actionable, sel, index, { shift: event.shiftKey, toggle: event.metaKey || event.ctrlKey })
  }

  // Without this, Shift+click would extend the native text selection instead.
  function mousedown(event: MouseEvent, index: number) {
    if (event.shiftKey && selectable(parsed.rows[index], actionable)) event.preventDefault()
  }

  function act(action: HunkAction, picks: HunkPick[]) {
    sel = emptySelection()
    applyHunkSelection(repoId, path, staged, diff.hash, picks, action, pickSummary(picks))
  }

  // Runs in the capture phase, before a menu or dialog's own Esc handler
  // closes it, so an Esc that closes one of those leaves the selection alone.
  function keydown(event: KeyboardEvent) {
    if (!sel.keys.size) return
    const target = event.target as Node | null
    const inPane = target === document.body || (!!target && root?.contains(target))
    if (escapeClears(event.key, { overlayOpen: !!$menu || !!$dialog || $settingsOpen, inPane })) sel = emptySelection()
  }

  // Enter or Space on a focused change line selects it like a click.
  function lineKey(event: KeyboardEvent, index: number) {
    const mods = keyMods(event)
    if (!mods) return
    event.preventDefault()
    sel = clickSelect(parsed.rows, actionable, sel, index, mods)
  }
</script>

<svelte:window on:keydown|capture={keydown} />

<div bind:this={root}>
{#if sel.keys.size}
  <div class="selbar">
    <span class="count">{sel.keys.size === 1 ? '1 line' : `${sel.keys.size} lines`} selected</span>
    {#each actions as [action, label]}
      <button class:danger={action === 'discard'} disabled={!!$busy} on:click={() => act(action, toPicks(sel.keys))}>{label} lines</button>
    {/each}
    <button class="clear" title="Clear selection (Esc)" aria-label="Clear selection" on:click={() => (sel = emptySelection())}>×</button>
  </div>
{/if}
{#each parsed.rows as row, i}
  {#if row.kind === 'hunk'}
    <div class="line hunk">
      <span class="ellipsis">{row.text}</span>
      {#if actionable.has(row.hunk)}
        <span class="hunk-actions">
          {#each actions as [action, label]}
            <button class:danger={action === 'discard'} disabled={!!$busy} on:click={() => act(action, [{ hunk: row.hunk, lines: [] }])}>{label} hunk</button>
          {/each}
        </span>
      {/if}
    </div>
  {:else}
    <!-- svelte-ignore a11y_no_static_element_interactions a11y_no_noninteractive_tabindex (role and tabindex are set together, on selectable lines only) -->
    <div
      class="line {row.kind}"
      class:selectable={selectable(row, actionable)}
      class:selected={sel.keys.has(rowKey(row))}
      role={selectable(row, actionable) ? 'checkbox' : undefined}
      aria-checked={selectable(row, actionable) ? sel.keys.has(rowKey(row)) : undefined}
      tabindex={selectable(row, actionable) ? 0 : undefined}
      on:mousedown={(e) => mousedown(e, i)}
      on:click={(e) => click(e, i)}
      on:keydown={(e) => lineKey(e, i)}
    >{row.text || ' '}</div>
  {/if}
{/each}
</div>

<style>
  .line { padding: 0 12px; white-space: pre; line-height: 18px; border-left: 3px solid transparent; }
  .add { background: var(--add-bg); }
  .del { background: var(--del-bg); }
  .meta, .note { color: var(--faint); }
  .hunk { color: var(--accent); display: flex; align-items: center; justify-content: space-between; gap: 8px; min-height: 22px; }
  .selectable { cursor: pointer; }
  .selectable:focus-visible { outline: 1px solid var(--accent); outline-offset: -1px; }
  .selected { border-left-color: var(--accent); filter: saturate(1.6) brightness(0.95); }
  .hunk-actions { display: flex; gap: 4px; flex: none; }
  .selbar {
    position: sticky; top: 0; z-index: 1; display: flex; align-items: center; gap: 6px;
    padding: 4px 12px; background: var(--surface); border-bottom: 1px solid var(--border);
  }
  .count { color: var(--faint); margin-right: auto; }
  button {
    font: inherit; font-size: 11px; line-height: 16px; padding: 1px 6px; cursor: pointer;
    color: var(--text); background: var(--bg); border: 1px solid var(--border); border-radius: 4px;
  }
  button:disabled { opacity: 0.5; cursor: default; }
  button.danger { color: var(--danger); }
  .clear { border: 0; background: none; font-size: 14px; }
</style>
