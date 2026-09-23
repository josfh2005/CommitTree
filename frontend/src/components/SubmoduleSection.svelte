<script lang="ts">
  import Icon from './Icon.svelte'
  import { initAllSubmodules, initSubmodule, openRepoFolder, openSubmodule, syncSubmodule, updateAllSubmodules, updateSubmodule } from '../lib/actions'
  import { api } from '../lib/api'
  import { revealLabel } from '../lib/platform'
  import { busy, expandedSubmoduleSections, logVersion, platform, repos, selectRepo, selectedRepoId, terminalOpen, toggleSubmodulesExpanded } from '../lib/stores'
  import { markers, rowLabel, splitPath, tooltip } from '../lib/submodules'
  import type { Submodule } from '../lib/types'
  import { errorMessage, openMenu } from '../lib/ui'

  export let parentId: string
  export let count: number

  let rows: Submodule[] = []
  let error = ''

  $: expanded = $expandedSubmoduleSections.includes(parentId)
  // Reloaded whenever the repo list or the log version changes — both
  // change after every write and on window focus (see refreshRepo /
  // startFocusRefresh), and whenever the section is (re)opened.
  $: if (expanded) loadRows(parentId, $repos, $logVersion)

  async function loadRows(id: string, _repos: unknown, _v: number) {
    try {
      rows = await api.getSubmodules(id)
      error = ''
    } catch (e) {
      error = errorMessage(e)
    }
  }

  // A submodule that is initialised has become a repository item (see
  // ListRepos) keyed by parentId + subPath; that item's id is what
  // selection, Show in Finder and the terminal act on.
  $: idByPath = new Map($repos.filter((r) => r.parentId === parentId).map((r) => [r.subPath, r.id]))
  const idOf = (s: Submodule) => idByPath.get(s.path)

  function openTerminalHere(id: string | undefined) {
    if (!id) return
    selectRepo(id)
    terminalOpen.set(true)
  }

  function rowMenu(event: MouseEvent, s: Submodule) {
    const label = rowLabel(s)
    const id = idOf(s)
    // Controller ruling: a "not configured" row (a .gitmodules entry with no
    // gitlink, or a gitlink with no .gitmodules entry) offers only Show in
    // Finder, and only when it has a working tree to show.
    if (label === 'not configured') {
      openMenu(event, [{ label: revealLabel($platform), action: () => id && openRepoFolder(id), disabled: !s.initialised }])
      return
    }
    openMenu(event, [
      ...(s.initialised ? [{ label: 'Open', action: () => openSubmodule(parentId, s) }] : []),
      ...(!s.initialised ? [{ label: 'Initialise', action: () => initSubmodule(parentId, s), disabled: !!$busy }] : []),
      ...(s.initialised && s.moved
        ? [{ label: 'Update to recorded commit', action: () => updateSubmodule(parentId, s), disabled: !!$busy }]
        : []),
      { label: 'Sync URL', action: () => syncSubmodule(parentId, s), disabled: !!$busy },
      ...(s.initialised ? [{ label: revealLabel($platform), action: () => id && openRepoFolder(id) }] : []),
      ...(s.initialised ? [{ label: 'Open terminal here', action: () => openTerminalHere(id) }] : []),
    ])
  }

  function headerMenu(event: MouseEvent) {
    openMenu(event, [
      {
        label: 'Initialise all',
        action: () => initAllSubmodules(parentId, rows),
        disabled: !!$busy || !rows.some((s) => s.configured && !s.initialised),
      },
      {
        label: 'Update all',
        action: () => updateAllSubmodules(parentId, rows),
        disabled: !!$busy || !rows.some((s) => s.initialised && s.moved),
      },
    ])
  }
</script>

<!-- Rendered as a sibling of RepoRefs (see RepoRow), not inside its .refs
     wrapper, so this repeats the section/row layout RepoRefs uses for
     Tags/Stash to line up the same way. -->
<div class="refs">
  <div class="section" on:contextmenu={headerMenu}>
    <span class="section-heading">
      <button class="section-title" on:click={() => toggleSubmodulesExpanded(parentId)}>Submodules</button>
      <span class="count">{count}</span>
    </span>
    <button class="icon-btn" title="Submodule actions" on:click={headerMenu}>
      <Icon name="more" size={14} />
    </button>
  </div>
  {#if expanded}
    {#if error}
      <div class="none">Could not read submodules: {error}</div>
    {:else}
      {#each rows as s (s.path)}
        {@const label = rowLabel(s)}
        {@const id = idOf(s)}
        {@const { dir, leaf } = splitPath(s.path)}
        <button
          class="row-item ref submodule"
          class:active={!!id && $selectedRepoId === id}
          class:dim={!!label}
          title={tooltip(s)}
          on:click={() => !label && openSubmodule(parentId, s)}
          on:contextmenu={(e) => rowMenu(e, s)}
        >
          <span class="mark"><Icon name="package" size={12} /></span>
          <span class="ellipsis"><span class="dir">{dir}</span>{leaf}</span>
          {#if label}
            <span class="row-label">{label}</span>
          {:else}
            {#each markers(s) as m (m.kind)}
              <span class="marker {m.kind}">{m.text}</span>
            {/each}
          {/if}
        </button>
      {:else}
        <div class="none">No submodules</div>
      {/each}
    {/if}
  {/if}
</div>

<style>
  .refs { padding: 0 0 12px 12px; }
  .section { display: flex; align-items: center; justify-content: space-between; height: 30px; padding: 6px 4px 0 var(--row-base-indent); }
  .section-heading { display: flex; align-items: center; gap: 6px; }
  .section .count { font-size: 11px; color: var(--faint); }
  .ref { height: 26px; padding-left: var(--row-base-indent); }
  .mark { width: 12px; flex: none; display: inline-grid; place-items: center; color: var(--muted); }
  .none { padding: 2px 10px 0 calc(var(--row-base-indent) + var(--row-indent-step)); color: var(--faint); font-size: 12px; }
  .dir { color: var(--faint); }
  .dim { color: var(--faint); }
  .dim .mark { color: var(--faint); }
  .row-label { margin-left: auto; font-size: 11px; color: var(--faint); flex: none; }
  .marker { margin-left: 6px; font-size: 11px; color: var(--muted); flex: none; }
  .marker.modified, .marker.conflict { color: var(--text); }
  .submodule .ellipsis { margin-right: auto; }
</style>
