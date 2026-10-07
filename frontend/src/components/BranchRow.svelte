<script lang="ts">
  import Icon from './Icon.svelte'
  import { trackTitle } from '../lib/push'
  import type { Branch } from '../lib/types'

  export let branch: Branch
  export let text: string
  export let active = false
  // Tint the checked-out branch: only in the selected repository, so an
  // expanded repository that is not selected never looks active.
  export let tint = false
  export let depth = 0
  export let title = ''
  export let onSelect: ((b: Branch) => void) | undefined = undefined
  export let onCheckout: (b: Branch) => void
  export let onMenu: (event: MouseEvent, b: Branch) => void

  $: elsewhere = branch.worktree
    ? `Checked out in ${branch.worktree.split(/[\\/]/).pop()}` + (branch.worktreeGone ? ' (directory gone — run git worktree prune)' : '')
    : ''
  $: track = trackTitle(branch)
</script>

<button
  class="row-item ref"
  class:active
  class:checked-out={branch.current}
  class:tint
  style="padding-left: calc(var(--row-base-indent) + {depth} * var(--row-indent-step))"
  title={elsewhere || title || text}
  on:click={() => onSelect?.(branch)}
  on:dblclick={() => onCheckout(branch)}
  on:contextmenu={(e) => onMenu(e, branch)}
>
  <span class="mark">{#if branch.current}<Icon name="check" size={12} />{/if}</span>
  <span class="ellipsis" class:current={branch.current}>{text}</span>
  {#if track}
    <span class="track" title={track}>
      {#if branch.ahead}<span class="ahead">↑{branch.ahead}</span>{/if}
      {#if branch.behind}<span class="behind">↓{branch.behind}</span>{/if}
    </span>
  {/if}
  {#if elsewhere}<span class="wt">worktree</span>{/if}
  {#if active}<span class="filtered" title="The log is filtered to this branch — choose All branches in the filter bar to clear it"><Icon name="filter" size={11} /></span>{/if}
</button>

<style>
  .ref { height: 26px; }
  .mark { width: 12px; flex: none; display: inline-grid; place-items: center; color: var(--accent); }
  .current { font-weight: 600; }
  /* The branch checked out in the selected repository: a soft accent tint so
     it stands out among its siblings; a row selected as the log filter keeps
     its own background. Other repositories' current branch keeps only the
     check and the bold name. */
  .checked-out.tint:not(.active) { background: color-mix(in srgb, var(--accent) 12%, transparent); }
  .checked-out.tint:not(.active):hover { background: color-mix(in srgb, var(--accent) 18%, transparent); }
  /* Ahead/behind its upstream, as of the last fetch: small coloured numbers,
     no pill, so a list of branches stays calm. */
  .track { margin-left: auto; flex: none; display: inline-flex; gap: 4px; font-size: 11px; font-variant-numeric: tabular-nums; }
  .ahead { color: var(--ahead); }
  .behind { color: var(--behind); }
  .track + .wt, .track + .filtered { margin-left: 6px; }
  .filtered { margin-left: auto; flex: none; display: inline-grid; place-items: center; color: var(--muted); }
  .wt + .filtered { margin-left: 6px; }
  .wt { margin-left: auto; flex: none; font-size: 11px; padding: 0 6px; border-radius: 4px; background: var(--hover); color: var(--muted); }
</style>
