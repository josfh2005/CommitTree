<script lang="ts">
  import Icon from './Icon.svelte'
  import { stateIcon } from '../lib/hosting'
  import type { HostState } from '../lib/types'

  export let state: HostState
  export let size = 12

  $: icon = stateIcon(state)
</script>

{#if icon}
  <span class="status {icon.tone}" style="width: {size}px; height: {size}px"><Icon name={icon.name} {size} /></span>
{/if}

<style>
  .status { display: inline-grid; place-items: center; flex: none; }
  .ok { color: var(--ok); }
  .bad { color: var(--danger); }
  .busy { color: var(--warning); }
  .busy :global(svg) { animation: spin 1.2s linear infinite; }
  .muted { color: var(--muted); }
  @keyframes spin { to { transform: rotate(360deg); } }
</style>
