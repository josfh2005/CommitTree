<script lang="ts">
  import Icon from './Icon.svelte'
  import { fetchRemote, pull, push } from '../lib/actions'
  import { canSync } from '../lib/remote'
  import { busy, mergeState, remoteInfo, stashConflictDismissed } from '../lib/stores'

  export let repoId: string

  $: syncable = canSync($mergeState, $busy)
</script>

<div class="toolbar">
  <button class="icon-btn" title="Fetch" disabled={!!$busy} on:click={() => fetchRemote(repoId)}>
    <Icon name="refresh" size={14} />
  </button>
  <button class="icon-btn" title="Pull" disabled={!syncable} on:click={() => pull(repoId)}>
    <Icon name="download" size={14} />
    {#if $remoteInfo?.behind}<span class="badge">{$remoteInfo.behind}</span>{/if}
  </button>
  <button class="icon-btn" title="Push" disabled={!syncable} on:click={() => push(repoId)}>
    <Icon name="upload" size={14} />
    {#if $remoteInfo?.ahead}<span class="badge">{$remoteInfo.ahead}</span>{/if}
  </button>
  <!-- The only way back into a stash conflict the user dismissed with
       "Done": without it the conflict view would be unreachable until the
       files happen to resolve. -->
  {#if $mergeState?.kind === 'stash' && $stashConflictDismissed}
    <button class="btn" on:click={() => stashConflictDismissed.set(false)}>Resolve conflicts</button>
  {/if}
</div>

<style>
  .toolbar { display: flex; align-items: center; gap: 2px; flex: none; }
  /* .icon-btn itself is global (theme.css:88) — only the badge anchor is
     local, so the shared hover/disabled styling keeps applying. */
  .toolbar :global(.icon-btn) { position: relative; }
  .badge {
    position: absolute; top: -3px; right: -3px; font-size: 9px; line-height: 1;
    padding: 1px 3px; border-radius: 6px; background: var(--accent); color: white;
  }
</style>
