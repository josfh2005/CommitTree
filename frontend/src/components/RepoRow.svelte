<script lang="ts">
  import Icon from './Icon.svelte'
  import RepoRefs from './RepoRefs.svelte'
  import SubmoduleSection from './SubmoduleSection.svelte'
  import { api } from '../lib/api'
  import { fetchRemote, moveRepoToGroup, openRepoFolder, pull, push, relocateRepo, removeRepo, removeWorktree } from '../lib/actions'
  import { REPO_DRAG_MIME } from '../lib/repoDrop'
  import { revealLabel } from '../lib/platform'
  import { busy, expandedRepos, mergeState, platform, selectRepo, selectedRepo, selectedRepoId, toggleRepoExpanded, worktreeState } from '../lib/stores'
  import type { Repo } from '../lib/types'
  import { openMenu, openMenuAsync, type MenuItem } from '../lib/ui'
  import { changedCount } from '../lib/worktree'

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
  // The selected repository's uncommitted changes, as a count after its
  // branch — the working tree is only read for the selected repository.
  $: changes = active ? changedCount($worktreeState) : 0
  // The submodule currently open under this repository, if any — its own
  // sections (Branches/Remotes/…) show in place of this repo's, under the
  // same row, per decision 4: a submodule has no row of its own.
  $: openSub = $selectedRepo?.submodule && $selectedRepo.parentId === repo.id ? $selectedRepo : null

  function repoMenu(event: MouseEvent) {
    // A detected worktree is not a list entry: it follows its main
    // repository, so nothing that edits the list applies to it — except
    // removing the worktree itself, whose disabled/locked state needs a
    // backend read first, hence openMenuAsync rather than the plain items
    // below.
    if (repo.worktree) {
      openMenuAsync(event, async () => {
        let removeItem: MenuItem
        try {
          const info = await api.getWorktreeRemovalInfo(repo.id)
          removeItem = info.locked
            ? {
                label: 'Remove worktree…',
                action: () => {},
                danger: true,
                disabled: true,
                title: 'This worktree is locked (git worktree lock)',
              }
            : { label: 'Remove worktree…', action: () => removeWorktree(repo), danger: true, disabled: !!$busy }
        } catch {
          // The removal-info read failed (e.g. the worktree just vanished);
          // removeWorktree makes its own read and reports its own error, so
          // the item is left enabled rather than silently dropped.
          removeItem = { label: 'Remove worktree…', action: () => removeWorktree(repo), danger: true, disabled: !!$busy }
        }
        return [
          { label: 'Fetch', action: () => fetchRemote(repo.id), disabled: !!$busy },
          { label: 'Pull', action: () => pull(repo.id), disabled: !!$busy || !!$mergeState?.merging },
          { label: 'Push', action: () => push(repo.id), disabled: !!$busy || !!$mergeState?.merging },
          { label: revealLabel($platform), action: () => openRepoFolder(repo.id) },
          removeItem,
        ]
      })
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
      {#if changes}<span class="changes" title="{changes} changed {changes === 1 ? 'file' : 'files'}">{changes}</span>{/if}
    {/if}
  </button>
</div>
{#if expanded}
  <div class="body" style="--row-base-indent: calc(var(--row-root-indent) + {depth} * var(--row-indent-step))">
    <RepoRefs repoId={openSub?.id ?? repo.id} submodulePath={openSub?.subPath ?? ''} />
    {#if !repo.submodule && repo.submoduleCount}
      <SubmoduleSection parentId={repo.id} count={repo.submoduleCount} />
    {/if}
  </div>
{/if}

<style>
  /* Bottom spacing before the next repo row, shared by RepoRefs and
     SubmoduleSection so it isn't doubled when both render (see their own
     .refs, which carry no bottom padding of their own). */
  .body { padding-bottom: 12px; }
  .repo { padding: 0 4px 0 4px; gap: 4px; }
  .repo.dragging { opacity: 0.5; }
  /* The selected repository: the selection background plus an accent bar at
     its left edge, so it is found at a glance among expanded repositories. */
  .repo.active { background: var(--selection); box-shadow: inset 3px 0 0 var(--accent); }
  .repo.active .branch { color: var(--accent); font-weight: 500; }
  .fold { width: 20px; height: 20px; flex: none; }
  .fold:disabled { opacity: 0; }
  .select { flex: 1; min-width: 0; height: 100%; display: flex; align-items: center; gap: 8px; color: var(--muted); }
  .name { color: var(--text); flex: none; max-width: 60%; }
  .child-mark { flex: none; color: var(--faint); margin-right: -4px; }
  .name.selected { font-weight: 600; }
  .branch { margin-left: auto; font-size: 12px; color: var(--muted); }
  .missing .name { color: var(--faint); }
  .changes { flex: none; font-size: 11px; padding: 0 6px; border-radius: 4px; background: var(--hover); color: var(--muted); }
  .badge { margin-left: auto; font-size: 11px; padding: 0 6px; border-radius: 4px; background: var(--hover); color: var(--muted); }
</style>
