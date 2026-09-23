<script lang="ts">
  import ChangesView from './ChangesView.svelte'
  import CommitDetails from './CommitDetails.svelte'
  import FilterBar from './FilterBar.svelte'
  import Icon from './Icon.svelte'
  import LogList from './LogList.svelte'
  import MergeView from './MergeView.svelte'
  import Splitter from './Splitter.svelte'
  import Toolbar from './Toolbar.svelte'
  import { conflictOwnsScreen } from '../lib/remote'
  import { terminalShortcutLabel } from '../lib/terminal'
  import { uncommittedCount } from '../lib/uncommitted'
  import { chatOpen, detailsHeight, mergeState, platform, repos, selectRepo, selectedHash, selectedRepo, stashConflictDismissed, terminalOpen, uncommittedSelected, worktreeState } from '../lib/stores'

  // The row can be selected a moment before the tree turns out clean (LogList
  // then moves the selection to HEAD); never open an empty Changes pane.
  $: showUncommitted = $uncommittedSelected && uncommittedCount($worktreeState) > 0

  // A submodule has no row of its own — its selection is shown as a
  // highlighted row under its parent's Submodules section — so the header
  // shows a breadcrumb back to the parent instead of the submodule's own name.
  $: parent = $selectedRepo?.submodule ? $repos.find((r) => r.id === $selectedRepo?.parentId) : undefined
</script>

<div class="log-view">
  <header class="drag">
    <div class="title">
      {#if $selectedRepo && parent}
        <button class="crumb" on:click={() => selectRepo(parent.id)}>{parent.name}</button>
        <span class="sep">›</span>
        <span class="path ellipsis">{$selectedRepo.subPath}</span>
      {:else if $selectedRepo}
        <span>{$selectedRepo.name}</span>
        <span class="path ellipsis">{$selectedRepo.path}</span>
      {/if}
    </div>
    {#if $selectedRepo && !$selectedRepo.missing}
      <Toolbar repoId={$selectedRepo.id} />
    {/if}
    {#if !$terminalOpen}
      <button class="btn terminal-btn" title={'Show terminal (' + terminalShortcutLabel($platform) + ' or Ctrl+`)'} disabled={!$selectedRepo || $selectedRepo.missing} on:click={() => terminalOpen.set(true)}><Icon name="terminal" /><span>Terminal</span></button>
    {/if}
    {#if !$chatOpen}
      <button class="icon-btn" title="Show chat" on:click={() => chatOpen.set(true)}><Icon name="panel-right" /></button>
    {/if}
  </header>

  {#if $selectedRepo && !$selectedRepo.missing}
    {#key $selectedRepo.id}
      <FilterBar repoId={$selectedRepo.id} />
      <div class="list"><LogList repoId={$selectedRepo.id} /></div>
      {#if conflictOwnsScreen($mergeState, $stashConflictDismissed) || $selectedHash || showUncommitted}
        <Splitter direction="horizontal" on:drag={(e) => detailsHeight.set(Math.min(720, Math.max(120, $detailsHeight - e.detail)))} />
        <div class="details" style="height: {$detailsHeight}px">
          {#if conflictOwnsScreen($mergeState, $stashConflictDismissed)}
            <MergeView repoId={$selectedRepo.id} />
          {:else if showUncommitted}
            <ChangesView repoId={$selectedRepo.id} />
          {:else if $selectedHash}
            <CommitDetails repoId={$selectedRepo.id} hash={$selectedHash} />
          {/if}
        </div>
      {/if}
    {/key}
  {:else if $selectedRepo}
    <div class="empty">This repository was moved or deleted. Right-click it in the sidebar and choose Locate…</div>
  {:else}
    <div class="empty">Select or add a repository.</div>
  {/if}
</div>

<style>
  .log-view { display: flex; flex-direction: column; height: 100%; }
  header { display: flex; align-items: center; gap: 8px; height: 44px; padding: 0 10px 0 14px; flex: none; }
  .title { flex: 1; min-width: 0; display: flex; align-items: baseline; gap: 8px; font-weight: 500; }
  .terminal-btn { display: inline-flex; align-items: center; gap: 6px; }
  .path { font-weight: 400; font-size: 12px; color: var(--faint); }
  .crumb { background: none; border: none; padding: 0; font: inherit; font-weight: 500; color: var(--text); cursor: pointer; }
  .crumb:hover { text-decoration: underline; }
  .sep { color: var(--faint); }
  .list { flex: 1; min-height: 0; border-top: 1px solid var(--border); }
  .details { flex: none; min-height: 0; overflow: hidden; }
  .empty { flex: 1; display: grid; place-items: center; padding: 24px; color: var(--muted); text-align: center; }
</style>
