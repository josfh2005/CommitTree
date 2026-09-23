<script lang="ts">
  import Icon from './Icon.svelte'
  import type { Branch } from '../lib/types'

  export let branch: Branch
  export let text: string
  export let active = false
  export let depth = 0
  export let title = ''
  export let onSelect: ((b: Branch) => void) | undefined = undefined
  export let onCheckout: (b: Branch) => void
  export let onMenu: (event: MouseEvent, b: Branch) => void

  $: elsewhere = branch.worktree
    ? `Checked out in ${branch.worktree.split(/[\\/]/).pop()}` + (branch.worktreeGone ? ' (directory gone — run git worktree prune)' : '')
    : ''
</script>

<button
  class="row-item ref"
  class:active
  style="padding-left: calc(var(--row-base-indent) + {depth} * var(--row-indent-step))"
  title={elsewhere || title || text}
  on:click={() => onSelect?.(branch)}
  on:dblclick={() => onCheckout(branch)}
  on:contextmenu={(e) => onMenu(e, branch)}
>
  <span class="mark">{#if branch.current}<Icon name="check" size={12} />{/if}</span>
  <span class="ellipsis" class:current={branch.current}>{text}</span>
  {#if elsewhere}<span class="wt">worktree</span>{/if}
</button>

<style>
  .ref { height: 26px; }
  .mark { width: 12px; flex: none; display: inline-grid; place-items: center; color: var(--accent); }
  .current { font-weight: 500; }
  .wt { margin-left: auto; flex: none; font-size: 11px; padding: 0 6px; border-radius: 4px; background: var(--hover); color: var(--muted); }
</style>
