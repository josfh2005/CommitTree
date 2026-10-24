<script lang="ts">
  import StatusIcon from './StatusIcon.svelte'
  import { formatDuration, isHttps, stateText } from '../lib/hosting'
  import type { CommitStatus } from '../lib/types'
  import { BrowserOpenURL } from '../../wailsjs/runtime/runtime'

  /** The pipelines or checks of one commit. */
  export let status: CommitStatus | null
  /** "Pipelines" or "Checks". */
  export let noun: string
  export let loading: boolean
  export let error: string
  export let onRetry: () => void
</script>

<div class="checks">
  <div class="title">{noun}</div>
  {#if loading}
    <div class="note">Loading…</div>
  {:else if error}
    <div class="note err">{error} <button class="link" on:click={onRetry}>Retry</button></div>
  {:else}
    {#each status?.checks ?? [] as c (c.key)}
      <div class="check">
        <StatusIcon state={c.state} />
        <span class="name ellipsis" title={c.name}>{c.name}</span>
        <span class="state">{stateText(c.state)}</span>
        {#if c.durationSec > 0}<span class="duration">{formatDuration(c.durationSec)}</span>{/if}
        {#if isHttps(c.url)}<button class="link" on:click={() => BrowserOpenURL(c.url)}>Open</button>{/if}
      </div>
    {/each}
  {/if}
</div>

<style>
  .checks { margin-bottom: 10px; font-size: 12px; }
  .title { font-weight: 600; margin-bottom: 4px; }
  .check { display: flex; align-items: center; gap: 6px; min-height: 22px; }
  .name { flex: 1; min-width: 0; }
  .state, .duration { color: var(--muted); flex: none; }
  .note { color: var(--muted); }
  .err { color: var(--danger); }
  .link { color: var(--accent); flex: none; }
  .link:hover { text-decoration: underline; }
</style>
