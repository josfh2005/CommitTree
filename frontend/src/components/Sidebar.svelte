<script lang="ts">
  import Icon from './Icon.svelte'
  import RepoRow from './RepoRow.svelte'
  import { addRepo } from '../lib/actions'
  import { groupRepos } from '../lib/repoGroups'
  import { busy, collapsedRepoGroups, repos, settingsOpen, toggleRepoGroupCollapsed } from '../lib/stores'
</script>

<div class="sidebar">
  <div class="titlebar drag"></div>

  <button class="row-item add" on:click={addRepo}><Icon name="plus" /> Add repo</button>

  <div class="section-title heading">Repos</div>
  <div class="list">
    {#if $repos.length}
      {@const grouped = groupRepos($repos)}
      {#each grouped.loose as repo (repo.id)}
        <RepoRow {repo} />
      {/each}
      {#each grouped.groups as group (group.name)}
        {@const collapsed = $collapsedRepoGroups.includes(group.name)}
        <button class="row-item group-header" on:click={() => toggleRepoGroupCollapsed(group.name)}>
          <span class="mark"><Icon name={collapsed ? 'chevron-right' : 'chevron-down'} size={12} /></span>
          <span class="ellipsis">{group.name}</span>
          <span class="count">{group.repos.length}</span>
        </button>
        {#if !collapsed}
          {#each group.repos as repo (repo.id)}
            <RepoRow {repo} depth={1} />
          {/each}
        {/if}
      {/each}
    {:else}
      <p class="empty">Add a git repository to get started.</p>
    {/if}
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
  .group-header { gap: 6px; }
  .mark { width: 12px; flex: none; display: inline-grid; place-items: center; color: var(--muted); }
  .group-header .count { margin-left: auto; font-size: 11px; color: var(--faint); }
  .empty { margin: 0; padding: 6px 10px; color: var(--muted); }
  .footer { flex: none; border-top: 1px solid var(--border); padding-top: 6px; }
  .note { padding: 4px 10px; font-size: 12px; color: var(--muted); }
</style>
