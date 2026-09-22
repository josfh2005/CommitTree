<script lang="ts">
  import Icon from './Icon.svelte'
  import RepoRefs from './RepoRefs.svelte'
  import { fetchRemote, moveRepoToGroup, pull, push, relocateRepo, removeRepo } from '../lib/actions'
  import { busy, expandedRepos, mergeState, selectRepo, selectedRepoId, toggleRepoExpanded } from '../lib/stores'
  import type { Repo } from '../lib/types'
  import { openMenu } from '../lib/ui'

  export let repo: Repo
  /** Indentation level — 0 for a loose repo, 1 for one inside a group. */
  export let depth = 0

  $: active = repo.id === $selectedRepoId
  $: expanded = $expandedRepos.includes(repo.id) && !repo.missing

  function repoMenu(event: MouseEvent) {
    openMenu(event, [
      ...(repo.missing ? [{ label: 'Locate…', action: () => relocateRepo(repo.id) }] : []),
      { label: 'Fetch', action: () => fetchRemote(repo.id), disabled: repo.missing || !!$busy },
      { label: 'Pull', action: () => pull(repo.id), disabled: repo.missing || !!$busy || !!$mergeState?.merging },
      { label: 'Push', action: () => push(repo.id), disabled: repo.missing || !!$busy || !!$mergeState?.merging },
      { label: 'Move to group…', action: () => moveRepoToGroup(repo) },
      { label: 'Remove from list…', action: () => removeRepo(repo), danger: true },
    ])
  }
</script>

<div
  class="repo row-item"
  class:active
  class:missing={repo.missing}
  style="padding-left: {4 + depth * 16}px"
  on:contextmenu={repoMenu}
>
  <button
    class="fold icon-btn"
    title={expanded ? 'Collapse' : 'Expand'}
    disabled={repo.missing}
    on:click={() => toggleRepoExpanded(repo.id)}
  >
    <Icon name={expanded ? 'chevron-down' : 'chevron-right'} size={12} />
  </button>
  <button class="select" on:click={() => selectRepo(repo.id)}>
    <span class="name ellipsis" class:selected={active}>{repo.name}</span>
    {#if repo.missing}
      <span class="badge">missing</span>
    {:else}
      {#if active && $mergeState?.merging}<span class="badge">merging</span>{/if}
      <span class="branch ellipsis">{repo.branch}</span>
    {/if}
  </button>
</div>
{#if expanded}
  <RepoRefs repoId={repo.id} />
{/if}

<style>
  .repo { padding: 0 4px 0 4px; gap: 4px; }
  .fold { width: 20px; height: 20px; flex: none; }
  .fold:disabled { opacity: 0; }
  .select { flex: 1; min-width: 0; height: 100%; display: flex; align-items: center; gap: 8px; color: var(--muted); }
  .name { color: var(--text); flex: none; max-width: 60%; }
  .name.selected { font-weight: 600; }
  .branch { margin-left: auto; font-size: 12px; color: var(--muted); }
  .missing .name { color: var(--faint); }
  .badge { margin-left: auto; font-size: 11px; padding: 0 6px; border-radius: 4px; background: var(--hover); color: var(--muted); }
</style>
