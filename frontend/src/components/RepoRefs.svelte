<script lang="ts">
  import BranchRow from './BranchRow.svelte'
  import Icon from './Icon.svelte'
  import { checkoutBranch, deleteBranch, deleteTag, mergeBranch, newBranch, newTag } from '../lib/actions'
  import { groupBranches, leafName, type BranchGroup } from '../lib/branches'
  import { busy, filters, mergeState, refs } from '../lib/stores'
  import type { Branch, Tag } from '../lib/types'
  import { openMenu } from '../lib/ui'

  export let repoId: string

  let showRemotes = true
  let showTags = true
  let openRemotes: Record<string, boolean> = {}
  let openGroups: Record<string, boolean> = {}

  const branchRef = (b: Branch) => (b.remote ? `refs/remotes/${b.remote}/${b.name}` : `refs/heads/${b.name}`)
  const branchLabel = (b: Branch) => (b.remote ? `${b.remote}/${b.name}` : b.name)

  $: local = groupBranches($refs?.local ?? [])

  // A group opens on demand, and on its own when it holds the current branch
  // or the branch the log is filtered by.
  function isOpen(key: string, group: BranchGroup, filtered: string): boolean {
    const remembered = openGroups[key]
    if (remembered !== undefined) return remembered
    return group.hasCurrent || group.branches.some((b) => branchRef(b) === filtered)
  }

  const toggleGroup = (key: string) => (openGroups = { ...openGroups, [key]: !isOpen(key, groupsByKey[key], $filters.branch) })

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

  const selectBranch = (b: Branch) => toggleFilter(branchRef(b))

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

  function tagMenu(event: MouseEvent, t: Tag) {
    openMenu(event, [
      { label: 'New branch from here…', action: () => newBranch(repoId, `refs/tags/${t.name}`, t.name) },
      { label: 'Delete…', action: () => deleteTag(repoId, t.name), danger: true },
    ])
  }
</script>

{#if $refs}
  <div class="refs">
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
        onSelect={selectBranch}
        onCheckout={checkout}
        onMenu={branchMenu}
      />
    {/each}
    {#each local.groups as group (group.name)}
      {@const key = `local:${group.name}`}
      {@const open = isOpen(key, group, $filters.branch)}
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
            onSelect={selectBranch}
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
                onSelect={selectBranch}
                onCheckout={checkout}
                onMenu={branchMenu}
              />
            {/each}
            {#each grouped.groups as group (group.name)}
              {@const key = `${remote.name}:${group.name}`}
              {@const open = isOpen(key, group, $filters.branch)}
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
                    onSelect={selectBranch}
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
  </div>
{/if}

<style>
  .refs { padding: 0 0 12px 12px; }
  .section { display: flex; align-items: center; justify-content: space-between; height: 30px; padding: 6px 4px 0 10px; }
  .ref { height: 26px; }
  .mark { width: 12px; flex: none; display: inline-grid; place-items: center; color: var(--muted); }
  .current { font-weight: 500; }
  .detached { color: var(--muted); }
  .nested { padding-left: 26px; }
  .group .count { margin-left: auto; font-size: 11px; color: var(--faint); }
  .none { padding: 2px 10px 0 30px; color: var(--faint); font-size: 12px; }
</style>
