<script lang="ts">
  import Icon from './Icon.svelte'
  import { checkoutBranch, deleteBranch, deleteTag, newBranch, newTag } from '../lib/actions'
  import { busy, filters, refs } from '../lib/stores'
  import type { Branch, Tag } from '../lib/types'
  import { openMenu } from '../lib/ui'

  export let repoId: string

  let showRemotes = true
  let showTags = true
  let openRemotes: Record<string, boolean> = {}

  const branchRef = (b: Branch) => (b.remote ? `refs/remotes/${b.remote}/${b.name}` : `refs/heads/${b.name}`)
  const branchLabel = (b: Branch) => (b.remote ? `${b.remote}/${b.name}` : b.name)

  function toggleFilter(ref: string) {
    filters.update((f) => ({ ...f, branch: f.branch === ref ? '' : ref }))
  }

  function checkout(b: Branch) {
    if (!b.current && !$busy) checkoutBranch(repoId, b)
  }

  function branchMenu(event: MouseEvent, b: Branch) {
    openMenu(event, [
      { label: 'Check out', action: () => checkoutBranch(repoId, b), disabled: b.current || !!$busy },
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
    {#each $refs.local as b (b.name)}
      <button
        class="row-item ref"
        class:active={$filters.branch === branchRef(b)}
        title={b.upstream ? `${b.name} → ${b.upstream}` : b.name}
        on:click={() => toggleFilter(branchRef(b))}
        on:dblclick={() => checkout(b)}
        on:contextmenu={(e) => branchMenu(e, b)}
      >
        <span class="mark">{#if b.current}<Icon name="check" size={12} />{/if}</span>
        <span class="ellipsis" class:current={b.current}>{b.name}</span>
      </button>
    {/each}

    {#if $refs.remotes.length}
      <div class="section">
        <button class="section-title" on:click={() => (showRemotes = !showRemotes)}>Remotes</button>
      </div>
      {#if showRemotes}
        {#each $refs.remotes as remote (remote.name)}
          <button class="row-item ref" on:click={() => (openRemotes = { ...openRemotes, [remote.name]: !openRemotes[remote.name] })}>
            <span class="mark"><Icon name={openRemotes[remote.name] ? 'chevron-down' : 'chevron-right'} size={12} /></span>
            <Icon name="cloud" size={14} />
            <span class="ellipsis">{remote.name}</span>
          </button>
          {#if openRemotes[remote.name]}
            {#each remote.branches as b (b.name)}
              <button
                class="row-item ref nested"
                class:active={$filters.branch === branchRef(b)}
                title={branchLabel(b)}
                on:click={() => toggleFilter(branchRef(b))}
                on:dblclick={() => checkout(b)}
                on:contextmenu={(e) => branchMenu(e, b)}
              >
                <span class="ellipsis">{b.name}</span>
              </button>
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
  .nested { padding-left: 42px; }
  .none { padding: 2px 10px 0 30px; color: var(--faint); font-size: 12px; }
</style>
