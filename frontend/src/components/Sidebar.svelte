<script lang="ts">
  import Icon from './Icon.svelte'
  import RepoRow from './RepoRow.svelte'
  import { addRepo, dropRepoOnGroup, openCloneDialog, placeRepo, placeRepoGroup, renameGroup, setRepoSortOrder } from '../lib/actions'
  import { groupRepos } from '../lib/repoGroups'
  import { GROUP_DRAG_MIME, REPO_DRAG_MIME } from '../lib/repoDrop'
  import { busy, collapsedRepoGroups, repoSortOrder, repos, settingsOpen, toggleRepoGroupCollapsed } from '../lib/stores'
  import { openMenu } from '../lib/ui'

  // Which drop target (a group name, or '' for the loose area) the drag is
  // currently over, for the highlight. A drop target spans a whole section
  // (header + body for a group) so the highlight and the drop itself are
  // handled once per section, not once per row inside it.
  let dragOverTarget: string | null = null

  // Whether a repo drag is in progress anywhere in the sidebar, independent
  // of which section the pointer is currently over. Needed because the
  // loose area can be empty (every repo is in a group) and, with no rows
  // and no min-height, would otherwise never get a dragover to react to —
  // this flag lets the empty loose area show a placeholder for the whole
  // duration of the drag, not just once the pointer happens to be over it.
  // dragstart/dragend bubble, so a window-level listener sees them even
  // though the drag originates inside a RepoRow.
  let dragActive = false

  function isRepoDrag(event: DragEvent) {
    return !!event.dataTransfer?.types.includes(REPO_DRAG_MIME)
  }

  function isGroupDrag(event: DragEvent) {
    return !!event.dataTransfer?.types.includes(GROUP_DRAG_MIME)
  }

  $: manual = $repoSortOrder === 'manual'

  // Manual order only: the repository row the pointer is over, which a drop
  // lands just above (null: the section's empty space, i.e. its end). A row's
  // dragover runs before its section's, so the row leaves its id in
  // `overRow` and the section's handler takes it — or null when the pointer
  // is over no row (the group header, the gap after the last row).
  let dropBefore: string | null = null
  let overRow: string | null = null
  function handleRowDragOver(event: DragEvent, id: string) {
    if (manual && isRepoDrag(event)) overRow = id
  }

  function handleDragOver(event: DragEvent, target: string) {
    if (!isRepoDrag(event)) return
    event.preventDefault()
    if (event.dataTransfer) event.dataTransfer.dropEffect = 'move'
    dragOverTarget = target
    dropBefore = overRow
    overRow = null
  }

  // Manual order only: dragging a group header places the group above the
  // one the pointer is over, or below it when over its lower half (before
  // the next group, or at the end after the last one).
  let groupBefore: string | null | undefined = undefined
  function handleGroupDragOver(event: DragEvent, name: string, next: string | null) {
    if (!manual || !isGroupDrag(event)) return
    event.preventDefault()
    if (event.dataTransfer) event.dataTransfer.dropEffect = 'move'
    const rect = (event.currentTarget as HTMLElement).getBoundingClientRect()
    groupBefore = event.clientY > rect.top + rect.height / 2 ? next : name
  }

  function handleGroupDrop(event: DragEvent) {
    const name = event.dataTransfer?.getData(GROUP_DRAG_MIME)
    const before = groupBefore
    groupBefore = undefined
    if (!name || before === undefined) return
    event.preventDefault()
    placeRepoGroup(name, before)
  }

  function handleGroupDragStart(event: DragEvent, name: string) {
    if (!manual) return
    event.dataTransfer?.setData(GROUP_DRAG_MIME, name)
    if (event.dataTransfer) event.dataTransfer.effectAllowed = 'move'
  }

  function sortMenu(event: MouseEvent) {
    const mark = (on: boolean) => (on ? '✓ ' : '\u2003')
    openMenu(event, [
      { label: `${mark(!manual)}By name`, action: () => setRepoSortOrder('name') },
      { label: `${mark(manual)}Manual (drag to arrange)`, action: () => setRepoSortOrder('manual') },
    ])
  }

  // dragleave fires as the pointer crosses any child element inside the
  // section, not just when it leaves the section itself, so only clear the
  // highlight once the pointer has actually left the section's bounds.
  function handleDragLeave(event: DragEvent, target: string) {
    if (dragOverTarget !== target) return
    const related = event.relatedTarget as Node | null
    const section = event.currentTarget as Node
    if (related && section.contains(related)) return
    dragOverTarget = null
  }

  function handleDrop(event: DragEvent, target: string) {
    if (isGroupDrag(event)) return handleGroupDrop(event)
    const id = event.dataTransfer?.getData(REPO_DRAG_MIME)
    const before = dropBefore
    dragOverTarget = null
    dropBefore = null
    if (!id) return
    event.preventDefault()
    if (manual) {
      if (before !== id) placeRepo(id, { group: target, beforeId: before })
    } else {
      dropRepoOnGroup(id, target)
    }
  }

  function handleWindowDragStart(event: DragEvent) {
    if (isRepoDrag(event)) dragActive = true
  }

  function handleWindowDragEnd() {
    dragActive = false
    dragOverTarget = null
    dropBefore = null
    groupBefore = undefined
  }

  function groupMenu(event: MouseEvent, name: string) {
    openMenu(event, [{ label: 'Rename group…', action: () => renameGroup(name) }])
  }
</script>

<svelte:window on:dragstart={handleWindowDragStart} on:dragend={handleWindowDragEnd} />

<div class="sidebar">
  <div class="titlebar drag"></div>

  <button
    class="row-item add"
    on:click={(e) => openMenu(e, [
      { label: 'Open folder…', action: addRepo },
      { label: 'Clone…', action: openCloneDialog },
    ])}
  ><Icon name="plus" /> Add repo</button>

  <div class="heading">
    <span class="section-title">Repos</span>
    <button class="icon-btn sort" title="Order: {manual ? 'manual (drag to arrange)' : 'by name'}" aria-label="Repository order" on:click={sortMenu}>
      <Icon name="list" size={12} />
    </button>
  </div>
  <div class="list">
    {#if $repos.length}
      {@const grouped = groupRepos($repos, $repoSortOrder)}
      <div
        class="loose-section"
        role="group"
        class:drag-over={dragOverTarget === ''}
        on:dragover={(e) => handleDragOver(e, '')}
        on:dragleave={(e) => handleDragLeave(e, '')}
        on:drop={(e) => handleDrop(e, '')}
      >
        {#each grouped.loose as node (node.repo.id)}
          <div class="node" class:drop-before={manual && dragOverTarget === '' && dropBefore === node.repo.id} role="presentation" on:dragover={(e) => handleRowDragOver(e, node.repo.id)}>
            <RepoRow repo={node.repo} />
            {#each node.children as child (child.id)}
              <RepoRow repo={child} depth={1} child />
            {/each}
          </div>
        {/each}
        {#if grouped.loose.length === 0 && dragActive}
          <p class="loose-placeholder">Drop here to remove from its group</p>
        {/if}
      </div>
      {#each grouped.groups as group, gi (group.name)}
        {@const collapsed = $collapsedRepoGroups.includes(group.name)}
        {@const next = grouped.groups[gi + 1]?.name ?? null}
        <div
          class="group-section"
          role="group"
          class:drag-over={dragOverTarget === group.name}
          class:group-drop-above={groupBefore === group.name}
          class:group-drop-below={groupBefore === null && next === null}
          on:dragover={(e) => {
            handleGroupDragOver(e, group.name, next)
            handleDragOver(e, group.name)
          }}
          on:dragleave={(e) => handleDragLeave(e, group.name)}
          on:drop={(e) => handleDrop(e, group.name)}
        >
          <button
            class="row-item group-header"
            draggable={manual ? 'true' : 'false'}
            on:dragstart={(e) => handleGroupDragStart(e, group.name)}
            on:click={() => toggleRepoGroupCollapsed(group.name)}
            on:contextmenu={(e) => groupMenu(e, group.name)}
          >
            <span class="mark"><Icon name={collapsed ? 'chevron-right' : 'chevron-down'} size={12} /></span>
            <span class="ellipsis">{group.name}</span>
            <span class="count">{group.repos.length}</span>
          </button>
          {#if !collapsed}
            {#each group.repos as node (node.repo.id)}
              <div class="node" class:drop-before={manual && dragOverTarget === group.name && dropBefore === node.repo.id} role="presentation" on:dragover={(e) => handleRowDragOver(e, node.repo.id)}>
                <RepoRow repo={node.repo} depth={1} />
                {#each node.children as child (child.id)}
                  <RepoRow repo={child} depth={2} child />
                {/each}
              </div>
            {/each}
          {/if}
        </div>
      {/each}
    {:else}
      <p class="empty">Add a git repository to get started.</p>
    {/if}
  </div>

  <div class="footer">
    {#if $busy}<div class="note">{$busy}</div>{/if}
    <button class="row-item" on:click={() => settingsOpen.set(true)}><Icon name="settings" /> Settings<span class="version">v{__APP_VERSION__}</span></button>
  </div>
</div>

<style>
  .sidebar { display: flex; flex-direction: column; height: 100%; padding: 0 8px 8px; }
  .titlebar { height: 40px; flex: none; }
  .add { font-weight: 500; margin-bottom: 16px; }
  .heading { display: flex; align-items: center; justify-content: space-between; padding: 0 4px 6px 10px; }
  .sort { color: var(--muted); }
  /* Manual order: where a dragged repository or group will land. */
  .node.drop-before { box-shadow: inset 0 2px 0 var(--accent); }
  .group-drop-above { box-shadow: inset 0 2px 0 var(--accent); }
  .group-drop-below { box-shadow: inset 0 -2px 0 var(--accent); }
  .list { flex: 1; overflow-y: auto; min-height: 0; }
  .loose-section, .group-section { border-radius: var(--radius); }
  /* A real drop surface even with zero rows, so a repo can always be
     dragged back out of a group — an empty, heightless div can never
     receive a dragover. */
  .loose-section { min-height: 28px; }
  .loose-section.drag-over, .group-section.drag-over { background: var(--selection); }
  .loose-placeholder { margin: 0; padding: 6px 10px; font-size: 12px; color: var(--faint); pointer-events: none; }
  .group-header { gap: 6px; }
  .mark { width: 12px; flex: none; display: inline-grid; place-items: center; color: var(--muted); }
  .group-header .count { margin-left: auto; font-size: 11px; color: var(--faint); }
  .empty { margin: 0; padding: 6px 10px; color: var(--muted); }
  .footer { flex: none; border-top: 1px solid var(--border); padding-top: 6px; }
  .version { margin-left: auto; font-size: 11px; color: var(--muted); }
  .note { padding: 4px 10px; font-size: 12px; color: var(--muted); }
</style>
