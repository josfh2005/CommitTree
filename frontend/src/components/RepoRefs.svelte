<script lang="ts">
  import BranchRow from './BranchRow.svelte'
  import Icon from './Icon.svelte'
  import { api } from '../lib/api'
  import { checkoutBranch, deleteBranch, deleteTag, mergeBranch, newBranch, newTag, rebaseOnto, stashApply, stashDrop, stashPop } from '../lib/actions'
  import { groupBranches, leafName, type BranchGroup } from '../lib/branches'
  import { rebaseBlocker } from '../lib/rebase'
  import { busy, expandedStashSections, repos, expandedTagSections, filters, loadSideRefs, mainView, mergeState, refs, selectRepo, selectStash, selectedRepoId, selectedStash, sideRefs, stashEntries, toggleStashExpanded, toggleTagsExpanded } from '../lib/stores'
  import { refsView } from '../lib/repoRefs'
  import type { Branch, StashEntry, Tag } from '../lib/types'
  import { openMenu, openMenuAsync } from '../lib/ui'

  export let repoId: string
  /** Set when repoId is a submodule opened under its parent (see RepoRow):
   *  its path relative to the top repository, shown as a breadcrumb row so
   *  the sections below are clearly the submodule's, not the parent's. */
  export let submodulePath = ''

  let showRemotes = true
  // This repository's own refs and stash: the live stores when it is the
  // selected one, its sideRefs entry otherwise (never the selected one's).
  $: view = refsView(repoId, $selectedRepoId, { refs: $refs, stash: $stashEntries }, $sideRefs)
  $: repoRefs = view.refs
  $: stash = view.stash
  // A repository expanded but not selected loads its own refs, and reloads
  // them whenever the repository list is re-read (after an action, a fetch or
  // a window focus) or it stops being the selected one.
  $: if (repoId !== $selectedRepoId) reloadSide($repos)
  function reloadSide(_list: unknown) {
    loadSideRefs(repoId)
  }
  // git keeps one stash per repository, shared by all of its worktrees.
  $: isWorktree = !!$repos.find((r) => r.id === repoId)?.worktree
  $: parentId = $repos.find((r) => r.id === repoId)?.parentId ?? ''
  let openRemotes: Record<string, boolean> = {}
  let openGroups: Record<string, boolean> = {}

  const branchRef = (b: Branch) => (b.remote ? `refs/remotes/${b.remote}/${b.name}` : `refs/heads/${b.name}`)
  const branchLabel = (b: Branch) => (b.remote ? `${b.remote}/${b.name}` : b.name)

  $: local = groupBranches(repoRefs?.local ?? [])

  // A group opens on demand, and on its own when it holds the current branch
  // or the branch the log is filtered by. `open` is taken as a parameter
  // (rather than read from the closed-over `openGroups`) so the template
  // expression that calls this visibly depends on it — otherwise Svelte's
  // compiled dirty-check, which only tracks identifiers referenced directly
  // in the template, never re-evaluates the {@const} after toggleGroup
  // assigns a new openGroups.
  function isOpen(open: Record<string, boolean>, key: string, group: BranchGroup, filtered: string): boolean {
    const remembered = open[key]
    if (remembered !== undefined) return remembered
    return group.hasCurrent || group.branches.some((b) => branchRef(b) === filtered)
  }

  const toggleGroup = (key: string) =>
    (openGroups = { ...openGroups, [key]: !isOpen(openGroups, key, groupsByKey[key], $filters.branch) })

  // Every rendered group, so toggleGroup can read the one it flips.
  $: groupsByKey = {
    ...Object.fromEntries(local.groups.map((g) => [`local:${g.name}`, g])),
    ...Object.fromEntries(
      (repoRefs?.remotes ?? []).flatMap((remote) =>
        groupBranches(remote.branches).groups.map((g) => [`${remote.name}:${g.name}`, g]),
      ),
    ),
  } as Record<string, BranchGroup>

  function toggleFilter(ref: string) {
    filters.update((f) => ({ ...f, branch: f.branch === ref ? '' : ref }))
  }

  function checkout(b: Branch) {
    if (!b.current && !b.worktree && !$busy) checkoutBranch(repoId, b)
  }

  function branchMenu(event: MouseEvent, b: Branch) {
    const head = repoRefs?.head ?? ''
    openMenuAsync(event, async () => {
      const contained = b.current ? false : await api.isAncestorOfHead(repoId, branchLabel(b)).catch(() => false)
      const rebaseWhy = rebaseBlocker(
        { isHead: b.current, contained, detached: !!repoRefs?.detached, busy: !!$busy, kind: $mergeState?.merging ? $mergeState.kind : '' },
        head,
        branchLabel(b),
      )
      return [
        { label: 'Check out', action: () => checkoutBranch(repoId, b), disabled: b.current || !!b.worktree || !!$busy },
        {
          label: `Merge ${branchLabel(b)} into ${head}`,
          action: () => mergeBranch(repoId, b, head),
          disabled: b.current || !!$busy || !!repoRefs?.detached || !!$mergeState?.merging,
        },
        {
          label: `Rebase ${head} onto ${branchLabel(b)}`,
          action: () => rebaseOnto(repoId, branchLabel(b), branchLabel(b), head),
          disabled: rebaseWhy !== null,
          title: rebaseWhy ?? undefined,
        },
        { label: 'New branch from here…', action: () => newBranch(repoId, branchLabel(b), branchLabel(b)) },
        { label: 'New tag here…', action: () => newTag(repoId, branchLabel(b), branchLabel(b)) },
        { label: b.remote ? 'Delete on remote…' : 'Delete…', action: () => deleteBranch(repoId, b), danger: true, disabled: b.current || !!b.worktree },
      ]
    })
  }

  function tagMenu(event: MouseEvent, t: Tag) {
    openMenu(event, [
      { label: 'New branch from here…', action: () => newBranch(repoId, `refs/tags/${t.name}`, t.name) },
      { label: 'Delete…', action: () => deleteTag(repoId, t.name), danger: true },
    ])
  }

  function stashMenu(event: MouseEvent, entry: { index: number }) {
    openMenu(event, [
      { label: 'Apply', action: () => stashApply(repoId, entry.index), disabled: !!$busy },
      { label: 'Pop', action: () => stashPop(repoId, entry.index), disabled: !!$busy },
      { label: 'Drop', action: () => stashDrop(repoId, entry.index), danger: true, disabled: !!$busy },
    ])
  }

  // A single click previews the stash; a row for a repository other than the
  // one on screen selects it first. Double click applies it — Apply keeps the entry, so this
  // needs no confirmation, matching the context menu's own Apply.
  function openStash(entry: StashEntry) {
    if (repoId !== $selectedRepoId) selectRepo(repoId)
    selectStash(entry)
  }
</script>

{#if repoRefs}
  <div class="refs">
    {#if submodulePath}
      <button class="row-item ref breadcrumb" title="Back to the repository" on:click={() => selectRepo(parentId)}>
        <span class="ellipsis">↳ {submodulePath}</span>
      </button>
    {/if}
    <div class="section">
      <span class="section-title">Branches</span>
      <button class="icon-btn" title="New branch from HEAD" on:click={() => newBranch(repoId, 'HEAD', 'HEAD')}>
        <Icon name="plus" size={14} />
      </button>
    </div>
    {#if repoRefs.detached}
      <div class="row-item ref detached">
        <span class="mark"><Icon name="check" size={12} /></span>
        <span class="mono">HEAD ({repoRefs.headHash.slice(0, 8)})</span>
      </div>
    {/if}
    {#each local.loose as b (b.name)}
      <BranchRow
        branch={b}
        text={b.name}
        active={$filters.branch === branchRef(b)}
        title={b.upstream ? `${b.name} → ${b.upstream}` : b.name}
        onCheckout={checkout}
        onMenu={branchMenu}
      />
    {/each}
    {#each local.groups as group (group.name)}
      {@const key = `local:${group.name}`}
      {@const open = isOpen(openGroups, key, group, $filters.branch)}
      <button class="row-item ref group" on:click={() => toggleGroup(key)}>
        <span class="mark"><Icon name={open ? 'chevron-down' : 'chevron-right'} size={12} /></span>
        <span class="ellipsis" class:current={group.hasCurrent}>{group.name}</span>
        <span class="count">{group.branches.length}</span>
      </button>
      {#if open}
        {#each group.branches as b (b.name)}
          <BranchRow
            branch={b}
            text={leafName(b.name, group.name)}
            depth={1}
            active={$filters.branch === branchRef(b)}
            title={b.upstream ? `${b.name} → ${b.upstream}` : b.name}
            onCheckout={checkout}
            onMenu={branchMenu}
          />
        {/each}
      {/if}
    {/each}

    {#if repoRefs.remotes.length}
      <div class="section">
        <button class="section-title" on:click={() => (showRemotes = !showRemotes)}>Remotes</button>
      </div>
      {#if showRemotes}
        {#each repoRefs.remotes as remote (remote.name)}
          {@const grouped = groupBranches(remote.branches)}
          <button class="row-item ref" on:click={() => (openRemotes = { ...openRemotes, [remote.name]: !openRemotes[remote.name] })}>
            <span class="mark"><Icon name={openRemotes[remote.name] ? 'chevron-down' : 'chevron-right'} size={12} /></span>
            <span class="ellipsis">{remote.name}</span>
          </button>
          {#if openRemotes[remote.name]}
            {#each grouped.loose as b (b.name)}
              <BranchRow
                branch={b}
                text={b.name}
                depth={1}
                active={$filters.branch === branchRef(b)}
                title={branchLabel(b)}
                onCheckout={checkout}
                onMenu={branchMenu}
              />
            {/each}
            {#each grouped.groups as group (group.name)}
              {@const key = `${remote.name}:${group.name}`}
              {@const open = isOpen(openGroups, key, group, $filters.branch)}
              <button
                class="row-item ref group"
                style="padding-left: calc(var(--row-base-indent) + var(--row-indent-step))"
                on:click={() => toggleGroup(key)}
              >
                <span class="mark"><Icon name={open ? 'chevron-down' : 'chevron-right'} size={12} /></span>
                <span class="ellipsis">{group.name}</span>
                <span class="count">{group.branches.length}</span>
              </button>
              {#if open}
                {#each group.branches as b (b.name)}
                  <BranchRow
                    branch={b}
                    text={leafName(b.name, group.name)}
                    depth={2}
                    active={$filters.branch === branchRef(b)}
                    title={branchLabel(b)}
                    onCheckout={checkout}
                    onMenu={branchMenu}
                  />
                {/each}
              {/if}
            {/each}
          {/if}
        {/each}
      {/if}
    {/if}

    <div class="section">
      <span class="section-heading">
        <button class="section-title" on:click={() => toggleTagsExpanded(repoId)}>Tags</button>
        <span class="count">{repoRefs.tags.length}</span>
      </span>
      <button class="icon-btn" title="New tag at HEAD" on:click={() => newTag(repoId, 'HEAD', 'HEAD')}>
        <Icon name="plus" size={14} />
      </button>
    </div>
    {#if $expandedTagSections.includes(repoId)}
      {#each repoRefs.tags as t (t.name)}
        <button
          class="row-item ref"
          class:active={$filters.branch === `refs/tags/${t.name}`}
          on:click={() => toggleFilter(`refs/tags/${t.name}`)}
          on:contextmenu={(e) => tagMenu(e, t)}
        >
          <span class="mark"><Icon name="tag" size={12} /></span>
          <span class="ellipsis">{t.name}</span>
        </button>
      {:else}
        <div class="none">No tags</div>
      {/each}
    {/if}

    <div class="section">
      <span class="section-heading">
        <button
          class="section-title"
          title={isWorktree ? 'Shared with the main repository and its other worktrees' : undefined}
          on:click={() => toggleStashExpanded(repoId)}>Stash</button>
        <span class="count">{stash.length}</span>
      </span>
    </div>
    {#if $expandedStashSections.includes(repoId)}
      {#each stash as entry (entry.index)}
        <button
          class="row-item ref"
          class:active={repoId === $selectedRepoId && $mainView === 'stash' && $selectedStash?.hash === entry.hash}
          on:click={() => openStash(entry)}
          on:dblclick={() => !$busy && stashApply(repoId, entry.index)}
          on:contextmenu={(e) => stashMenu(e, entry)}
        >
          <span class="mark"></span>
          <span class="ellipsis">{entry.message}</span>
        </button>
      {:else}
        <div class="none">No stashed changes</div>
      {/each}
    {/if}
  </div>
{/if}

<style>
  /* Bottom spacing before whatever follows (the next repo row, or this
     repository's Submodules section — see RepoRow, which wraps both in a
     shared padding-bottom so it isn't doubled when both render) lives on
     the wrapper, not here. */
  /* Section headings sit at the base indent (under the repository name);
     their rows sit one step in - see the --row-* custom properties in
     theme.css. */
  .section { display: flex; align-items: center; justify-content: space-between; height: 30px; padding: 6px 4px 0 var(--row-base-indent); }
  .section-heading { display: flex; align-items: center; gap: 6px; }
  /* Opt this file's own rows into the shared refs indentation - see
     --row-base-indent in theme.css - without moving unrelated .row-item
     consumers elsewhere in the app that share the base class but not this
     block's anchor (BranchRow overrides this inline for depth > 0). */
  .ref { height: 26px; padding-left: var(--row-base-indent); }
  .section .count { font-size: 11px; color: var(--faint); }
  .mark { width: 12px; flex: none; display: inline-grid; place-items: center; color: var(--muted); }
  .current { font-weight: 500; }
  .detached { color: var(--muted); }
  .breadcrumb { color: var(--muted); }
  .group .count { margin-left: auto; font-size: 11px; color: var(--faint); }
  /* Empty-state lines stand in for the rows they describe (one step in
     from the heading above them), so they align with that row TEXT - no
     marker gutter of their own. */
  .none { padding: 2px 10px 0 calc(var(--row-base-indent) + var(--row-indent-step)); color: var(--faint); font-size: 12px; }
</style>
