<script lang="ts">
  import Icon from './Icon.svelte'
  import RepoRefs from './RepoRefs.svelte'
  import { addRepo, fetchRepo, pullRepo, relocateRepo, removeRepo } from '../lib/actions'
  import { busy, expandedRepos, repos, selectRepo, selectedRepoId, settingsOpen, toggleRepoExpanded } from '../lib/stores'
  import type { Repo } from '../lib/types'
  import { openMenu } from '../lib/ui'

  function repoMenu(event: MouseEvent, repo: Repo) {
    openMenu(event, [
      ...(repo.missing ? [{ label: 'Locate…', action: () => relocateRepo(repo.id) }] : []),
      { label: 'Fetch', action: () => fetchRepo(repo.id), disabled: repo.missing || !!$busy },
      { label: 'Pull', action: () => pullRepo(repo.id), disabled: repo.missing || !!$busy },
      { label: 'Remove from list…', action: () => removeRepo(repo), danger: true },
    ])
  }
</script>

<div class="sidebar">
  <div class="titlebar drag"></div>

  <button class="row-item add" on:click={addRepo}><Icon name="plus" /> Add repo</button>

  <div class="section-title heading">Repos</div>
  <div class="list">
    {#each $repos as repo (repo.id)}
      {@const active = repo.id === $selectedRepoId}
      {@const expanded = $expandedRepos.includes(repo.id) && !repo.missing}
      <div class="repo row-item" class:active class:missing={repo.missing} on:contextmenu={(e) => repoMenu(e, repo)}>
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
            <span class="branch ellipsis">{repo.branch}</span>
          {/if}
        </button>
        {#if !repo.missing}
          <span class="hover-actions">
            <button class="icon-btn" title="Fetch" disabled={!!$busy} on:click={() => fetchRepo(repo.id)}><Icon name="refresh" size={14} /></button>
            <button class="icon-btn" title="Pull" disabled={!!$busy} on:click={() => pullRepo(repo.id)}><Icon name="download" size={14} /></button>
          </span>
        {/if}
      </div>
      {#if expanded}
        <RepoRefs repoId={repo.id} />
      {/if}
    {:else}
      <p class="empty">Add a git repository to get started.</p>
    {/each}
  </div>

  <div class="footer">
    {#if $busy}<div class="note">{$busy}</div>{/if}
    <button class="row-item" on:click={() => settingsOpen.set(true)}><Icon name="settings" /> Settings</button>
  </div>
</div>

<style>
  .sidebar { display: flex; flex-direction: column; height: 100%; padding: 0 8px 8px; }
  .titlebar { height: 40px; flex: none; }
  .add { font-weight: 500; margin-bottom: 16px; }
  .heading { padding: 0 10px 6px; }
  .list { flex: 1; overflow-y: auto; min-height: 0; }
  .repo { padding: 0 4px 0 4px; gap: 4px; }
  .fold { width: 20px; height: 20px; flex: none; }
  .fold:disabled { opacity: 0; }
  .select { flex: 1; min-width: 0; height: 100%; display: flex; align-items: center; gap: 8px; color: var(--muted); }
  .name { color: var(--text); flex: none; max-width: 60%; }
  .name.selected { font-weight: 600; }
  .branch { margin-left: auto; font-size: 12px; color: var(--muted); }
  .missing .name { color: var(--faint); }
  .badge { margin-left: auto; font-size: 11px; padding: 0 6px; border-radius: 4px; background: var(--hover); color: var(--muted); }
  .hover-actions { display: none; }
  .repo:hover .hover-actions { display: flex; }
  .repo:hover .branch { display: none; }
  .empty { margin: 0; padding: 6px 10px; color: var(--muted); }
  .footer { flex: none; border-top: 1px solid var(--border); padding-top: 6px; }
  .note { padding: 4px 10px; font-size: 12px; color: var(--muted); }
</style>
