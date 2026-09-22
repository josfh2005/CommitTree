<script lang="ts">
  import BranchRow from './BranchRow.svelte'
  import Icon from './Icon.svelte'
  import { checkoutBranch, deleteBranch, deleteTag, mergeBranch, newBranch, newTag, stashApply, stashDrop, stashPop } from '../lib/actions'
  import { groupBranches, leafName, type BranchGroup } from '../lib/branches'
  import { busy, filters, mainView, mergeState, refs, selectRepo, selectStash, selectedRepoId, selectedStash, stashEntries, worktreeState } from '../lib/stores'
  import type { Branch, StashEntry, Tag } from '../lib/types'
  import { changedCount } from '../lib/worktree'
  import { openMenu } from '../lib/ui'

  export let repoId: string

  let showRemotes = true
  let showTags = true
  let showStash = true
  let openRemotes: Record<string, boolean> = {}
  let openGroups: Record<string, boolean> = {}

  const branchRef = (b: Branch) => (b.remote ? `refs/remotes/${b.remote}/${b.name}` : `refs/heads/${b.name}`)
  const branchLabel = (b: Branch) => (b.remote ? `${b.remote}/${b.name}` : b.name)

  $: local = groupBranches($refs?.local ?? [])

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
      ($refs?.remotes ?? []).flatMap((remote) =>
        groupBranches(remote.branches).groups.map((g) => [`${remote.name}:${g.name}`, g]),
      ),
    ),
  } as Record<string, BranchGroup>

  function toggleFilter(ref: string) {
    filters.update((f) => ({ ...f, branch: f.branch === ref ? '' : ref }))
  }

  function checkout(b: Branch) {
    if (!b.current && !$busy) checkoutBranch(repoId, b)
  }

  function branchMenu(event: MouseEvent, b: Branch) {
    openMenu(event, [
      { label: 'Check out', action: () => checkoutBranch(repoId, b), disabled: b.current || !!$busy },
      {
        label: `Merge ${branchLabel(b)} into ${$refs?.head ?? ''}`,
        action: () => mergeBranch(repoId, b, $refs?.head ?? ''),
        disabled: b.current || !!$busy || !!$refs?.detached || !!$mergeState?.merging,
      },
      { label: 'New branch from here…', action: () => newBranch(repoId, branchLabel(b), branchLabel(b)) },
      { label: 'New tag here…', action: () => newTag(repoId, branchLabel(b), branchLabel(b)) },
      { label: b.remote ? 'Delete on remote…' : 'Delete…', action: () => deleteBranch(repoId, b), danger: true, disabled: b.current },
    ])
  }

  // Clicking Changes under an expanded-but-not-selected repository must act
  // on THAT repository, not silently read/route the currently selected
  // one's state — selectRepo() first, same as clicking the repo row itself
  // would. Once this repo is selected, the row is a toggle: clicking it
  // again while the Changes view is already showing returns to the log
  // (which is the only way back once the log pane is unmounted). A merge in
  // progress always wins — the log pane is where the merge view lives.
  function openChanges() {
    if (repoId !== $selectedRepoId) {
      selectRepo(repoId)
      mainView.set('changes')
      return
    }
    if ($mergeState?.merging) {
      mainView.set('log')
      return
    }
    mainView.set($mainView === 'changes' ? 'log' : 'changes')
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
  // one on screen selects it first, the same cross-repo handling openChanges
  // above uses. Double click applies it — Apply keeps the entry, so this
  // needs no confirmation, matching the context menu's own Apply.
  function openStash(entry: StashEntry) {
    if (repoId !== $selectedRepoId) selectRepo(repoId)
    selectStash(entry)
  }
</script>

{#if $refs}
  <div class="refs">
    <button
      class="row-item ref changes"
      class:active={$mainView === 'changes' && !$mergeState?.merging}
      on:click={openChanges}
    >
      <span class="ellipsis">Changes</span>
      {#if changedCount($worktreeState)}<span class="count">{changedCount($worktreeState)}</span>{/if}
    </button>

    <div class="section">
      <span class="section-title">Branches</span>
      <button class="icon-btn" title="New branch from HEAD" on:click={() => newBranch(repoId, 'HEAD', 'HEAD')}>
        <Icon name="plus" size={14} />
      </button>
    </div>
    {#if $refs.detached}
      <div class="row-item ref detached">
        <span class="mark"><Icon name="check" size={12} /></span>
        <span class="mono">HEAD ({$refs.headHash.slice(0, 8)})</span>
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

    {#if $refs.remotes.length}
      <div class="section">
        <button class="section-title" on:click={() => (showRemotes = !showRemotes)}>Remotes</button>
      </div>
      {#if showRemotes}
        {#each $refs.remotes as remote (remote.name)}
          {@const grouped = groupBranches(remote.branches)}
          <button class="row-item ref" on:click={() => (openRemotes = { ...openRemotes, [remote.name]: !openRemotes[remote.name] })}>
            <span class="mark"><Icon name={openRemotes[remote.name] ? 'chevron-down' : 'chevron-right'} size={12} /></span>
            <Icon name="cloud" size={14} />
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
              <button class="row-item ref group nested" on:click={() => toggleGroup(key)}>
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
      <button class="section-title" on:click={() => (showTags = !showTags)}>Tags</button>
      <button class="icon-btn" title="New tag at HEAD" on:click={() => newTag(repoId, 'HEAD', 'HEAD')}>
        <Icon name="plus" size={14} />
      </button>
    </div>
    {#if showTags}
      {#each $refs.tags as t (t.name)}
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
      <button class="section-title" on:click={() => (showStash = !showStash)}>Stash</button>
      <span class="count">{$stashEntries.length}</span>
    </div>
    {#if showStash}
      {#each $stashEntries as entry (entry.index)}
        <button
          class="row-item ref"
          class:active={repoId === $selectedRepoId && $mainView === 'stash' && $selectedStash?.hash === entry.hash}
          on:click={() => openStash(entry)}
          on:dblclick={() => !$busy && stashApply(repoId, entry.index)}
          on:contextmenu={(e) => stashMenu(e, entry)}
        >
          <span class="mark"><Icon name="download" size={12} /></span>
          <span class="ellipsis">{entry.message}</span>
        </button>
      {:else}
        <div class="none">No stashed changes</div>
      {/each}
    {/if}
  </div>
{/if}

<style>
  .refs { padding: 0 0 12px 12px; }
  .section { display: flex; align-items: center; justify-content: space-between; height: 30px; padding: 6px 4px 0 10px; }
  .ref { height: 26px; }
  .changes .count { margin-left: auto; font-size: 11px; padding: 0 6px; border-radius: 4px; background: var(--hover); color: var(--muted); }
  .section .count { font-size: 11px; color: var(--faint); }
  .mark { width: 12px; flex: none; display: inline-grid; place-items: center; color: var(--muted); }
  .current { font-weight: 500; }
  .detached { color: var(--muted); }
  .nested { padding-left: 26px; }
  .group .count { margin-left: auto; font-size: 11px; color: var(--faint); }
  .none { padding: 2px 10px 0 30px; color: var(--faint); font-size: 12px; }
</style>
