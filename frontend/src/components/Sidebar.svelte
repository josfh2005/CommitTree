<script lang="ts">
  import Icon from './Icon.svelte'
  import RepoRow from './RepoRow.svelte'
  import { addRepo, dropRepoOnGroup, renameGroup } from '../lib/actions'
  import { groupRepos } from '../lib/repoGroups'
  import { REPO_DRAG_MIME } from '../lib/repoDrop'
  import { busy, collapsedRepoGroups, repos, settingsOpen, toggleRepoGroupCollapsed } from '../lib/stores'
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

  function handleDragOver(event: DragEvent, target: string) {
    if (!isRepoDrag(event)) return
    event.preventDefault()
    if (event.dataTransfer) event.dataTransfer.dropEffect = 'move'
    dragOverTarget = target
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
    const id = event.dataTransfer?.getData(REPO_DRAG_MIME)
    dragOverTarget = null
    if (!id) return
    event.preventDefault()
    dropRepoOnGroup(id, target)
  }

  function handleWindowDragStart(event: DragEvent) {
    if (isRepoDrag(event)) dragActive = true
  }

  function handleWindowDragEnd() {
    dragActive = false
    dragOverTarget = null
  }

  function groupMenu(event: MouseEvent, name: string) {
    openMenu(event, [{ label: 'Rename group…', action: () => renameGroup(name) }])
  }
</script>

<svelte:window on:dragstart={handleWindowDragStart} on:dragend={handleWindowDragEnd} />

<div class="sidebar">
  <div class="titlebar drag"></div>

  <button class="row-item add" on:click={addRepo}><Icon name="plus" /> Add repo</button>

  <div class="section-title heading">Repos</div>
  <div class="list">
    {#if $repos.length}
      {@const grouped = groupRepos($repos)}
      <div
        class="loose-section"
        role="group"
        class:drag-over={dragOverTarget === ''}
        on:dragover={(e) => handleDragOver(e, '')}
        on:dragleave={(e) => handleDragLeave(e, '')}
        on:drop={(e) => handleDrop(e, '')}
      >
        {#each grouped.loose as node (node.repo.id)}
          <RepoRow repo={node.repo} />
          {#each node.children as child (child.id)}
            <RepoRow repo={child} depth={1} child />
          {/each}
        {/each}
        {#if grouped.loose.length === 0 && dragActive}
          <p class="loose-placeholder">Drop here to remove from its group</p>
        {/if}
      </div>
      {#each grouped.groups as group (group.name)}
        {@const collapsed = $collapsedRepoGroups.includes(group.name)}
        <div
          class="group-section"
          role="group"
          class:drag-over={dragOverTarget === group.name}
          on:dragover={(e) => handleDragOver(e, group.name)}
          on:dragleave={(e) => handleDragLeave(e, group.name)}
          on:drop={(e) => handleDrop(e, group.name)}
        >
          <button
            class="row-item group-header"
            on:click={() => toggleRepoGroupCollapsed(group.name)}
            on:contextmenu={(e) => groupMenu(e, group.name)}
          >
            <span class="mark"><Icon name={collapsed ? 'chevron-right' : 'chevron-down'} size={12} /></span>
            <span class="ellipsis">{group.name}</span>
            <span class="count">{group.repos.length}</span>
          </button>
          {#if !collapsed}
            {#each group.repos as node (node.repo.id)}
              <RepoRow repo={node.repo} depth={1} />
              {#each node.children as child (child.id)}
                <RepoRow repo={child} depth={2} child />
              {/each}
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
    <button class="row-item" on:click={() => settingsOpen.set(true)}><Icon name="settings" /> Settings</button>
  </div>
</div>

<style>
  .sidebar { display: flex; flex-direction: column; height: 100%; padding: 0 8px 8px; }
  .titlebar { height: 40px; flex: none; }
  .add { font-weight: 500; margin-bottom: 16px; }
  .heading { padding: 0 10px 6px; }
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
  .note { padding: 4px 10px; font-size: 12px; color: var(--muted); }
</style>
