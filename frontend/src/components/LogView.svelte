<script lang="ts">
  import CommitDetails from './CommitDetails.svelte'
  import FilterBar from './FilterBar.svelte'
  import Icon from './Icon.svelte'
  import LogList from './LogList.svelte'
  import MergeView from './MergeView.svelte'
  import Splitter from './Splitter.svelte'
  import Toolbar from './Toolbar.svelte'
  import { chatOpen, detailsHeight, mergeState, selectedHash, selectedRepo } from '../lib/stores'
</script>

<div class="log-view">
  <header class="drag">
    <div class="title">
      {#if $selectedRepo}
        <span>{$selectedRepo.name}</span>
        <span class="path ellipsis">{$selectedRepo.path}</span>
      {/if}
    </div>
    {#if $selectedRepo && !$selectedRepo.missing}
      <Toolbar repoId={$selectedRepo.id} />
    {/if}
    {#if !$chatOpen}
      <button class="icon-btn" title="Show chat" on:click={() => chatOpen.set(true)}><Icon name="panel-right" /></button>
    {/if}
  </header>

  {#if $selectedRepo && !$selectedRepo.missing}
    {#key $selectedRepo.id}
      <FilterBar repoId={$selectedRepo.id} />
      <div class="list"><LogList repoId={$selectedRepo.id} /></div>
      {#if $mergeState?.merging || $selectedHash}
        <Splitter direction="horizontal" on:drag={(e) => detailsHeight.set(Math.min(720, Math.max(120, $detailsHeight - e.detail)))} />
        <div class="details" style="height: {$detailsHeight}px">
          {#if $mergeState?.merging}
            <MergeView repoId={$selectedRepo.id} />
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
  .path { font-weight: 400; font-size: 12px; color: var(--faint); }
  .list { flex: 1; min-height: 0; border-top: 1px solid var(--border); }
  .details { flex: none; min-height: 0; overflow: hidden; }
  .empty { flex: 1; display: grid; place-items: center; padding: 24px; color: var(--muted); text-align: center; }
</style>
