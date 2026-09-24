<script lang="ts">
  import Icon from './Icon.svelte'
  import RepoRefs from './RepoRefs.svelte'
  import SubmoduleSection from './SubmoduleSection.svelte'
  import { fetchRemote, moveRepoToGroup, openRepoFolder, pull, push, relocateRepo, removeRepo } from '../lib/actions'
  import { REPO_DRAG_MIME } from '../lib/repoDrop'
  import { revealLabel } from '../lib/platform'
  import { busy, expandedRepos, mergeState, platform, selectRepo, selectedRepo, selectedRepoId, toggleRepoExpanded } from '../lib/stores'
  import type { Repo } from '../lib/types'
  import { openMenu } from '../lib/ui'

  export let repo: Repo
  /** Indentation level — 0 for a loose repo, 1 for one inside a group. */
  export let depth = 0
  /** Shown under its main repository: a linked worktree. */
  export let child = false

  // Dragging state is purely visual and local: dragend always fires, even
  // when the drag is cancelled (dropped outside the window, Escape), so
  // there is no path that leaves a row stuck looking like it's dragging.
  let dragging = false

  function handleDragStart(event: DragEvent) {
    event.dataTransfer?.setData(REPO_DRAG_MIME, repo.id)
    if (event.dataTransfer) event.dataTransfer.effectAllowed = 'move'
    dragging = true
  }

  function handleDragEnd() {
    dragging = false
  }

  $: active = repo.id === $selectedRepoId
  $: expanded = $expandedRepos.includes(repo.id) && !repo.missing
  // The submodule currently open under this repository, if any — its own
  // sections (Changes/Branches/…) show in place of this repo's, under the
  // same row, per decision 4: a submodule has no row of its own.
  $: openSub = $selectedRepo?.submodule && $selectedRepo.parentId === repo.id ? $selectedRepo : null

  function repoMenu(event: MouseEvent) {
    // A detected worktree is not a list entry: it follows its main
    // repository, so nothing that edits the list applies to it.
    if (repo.worktree) {
      openMenu(event, [
        { label: 'Fetch', action: () => fetchRemote(repo.id), disabled: !!$busy },
        { label: 'Pull', action: () => pull(repo.id), disabled: !!$busy || !!$mergeState?.merging },
        { label: 'Push', action: () => push(repo.id), disabled: !!$busy || !!$mergeState?.merging },
        { label: revealLabel($platform), action: () => openRepoFolder(repo.id) },
      ])
      return
    }
    openMenu(event, [
      ...(repo.missing ? [{ label: 'Locate…', action: () => relocateRepo(repo.id) }] : []),
      { label: 'Fetch', action: () => fetchRemote(repo.id), disabled: repo.missing || !!$busy },
      { label: 'Pull', action: () => pull(repo.id), disabled: repo.missing || !!$busy || !!$mergeState?.merging },
      { label: 'Push', action: () => push(repo.id), disabled: repo.missing || !!$busy || !!$mergeState?.merging },
      { label: revealLabel($platform), action: () => openRepoFolder(repo.id), disabled: repo.missing },
      ...(child ? [] : [{ label: 'Move to group…', action: () => moveRepoToGroup(repo) }]),
      { label: 'Remove from list…', action: () => removeRepo(repo), danger: true },
    ])
  }
</script>

<div
  class="repo row-item"
  class:active
  class:missing={repo.missing}
  class:dragging
  style="padding-left: calc(var(--row-base-indent) - var(--repo-row-inset) + {depth} * var(--row-indent-step))"
  role="group"
  aria-label={repo.name}
  draggable={child ? 'false' : 'true'}
  on:contextmenu={repoMenu}
  on:dragstart={handleDragStart}
  on:dragend={handleDragEnd}
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
    {#if child}<span class="child-mark" title="Worktree">↳</span>{/if}
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
  <RepoRefs repoId={openSub?.id ?? repo.id} submodulePath={openSub?.subPath ?? ''} />
  {#if !repo.submodule && repo.submoduleCount}
    <SubmoduleSection parentId={repo.id} count={repo.submoduleCount} />
  {/if}
{/if}

<style>
  .repo { padding: 0 4px 0 4px; gap: 4px; }
  .repo.dragging { opacity: 0.5; }
  .fold { width: 20px; height: 20px; flex: none; }
  .fold:disabled { opacity: 0; }
  .select { flex: 1; min-width: 0; height: 100%; display: flex; align-items: center; gap: 8px; color: var(--muted); }
  .name { color: var(--text); flex: none; max-width: 60%; }
  .child-mark { flex: none; color: var(--faint); margin-right: -4px; }
  .name.selected { font-weight: 600; }
  .branch { margin-left: auto; font-size: 12px; color: var(--muted); }
  .missing .name { color: var(--faint); }
  .badge { margin-left: auto; font-size: 11px; padding: 0 6px; border-radius: 4px; background: var(--hover); color: var(--muted); }
</style>
