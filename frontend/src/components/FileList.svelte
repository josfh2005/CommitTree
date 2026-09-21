<script context="module" lang="ts">
  /** rowKey disambiguates a row that shares its path with another section's
   *  row — a partially staged file (git status "MM") lists the same path
   *  under both Staged and Unstaged. Selecting or opening a row must resolve
   *  to exactly the row that was clicked, not "whichever section's array
   *  happens to contain that path". Exported so MergeView and ChangesView
   *  can derive the same key from a section title + path when interpreting
   *  the value onSelect hands back. */
  export function rowKey(section: string, path: string): string {
    return `${section}:${path}`
  }
</script>

<script lang="ts">
  import Icon from './Icon.svelte'

  interface ListFile {
    path: string
    status: string
    oldPath?: string
  }

  interface FileAction {
    label: string
    run: () => void
    danger?: boolean
    disabled?: boolean
    title?: string
  }

  export let sections: { title: string; files: ListFile[] }[]
  /** A rowKey(section title, path) value — see the module-level rowKey. */
  export let selected: string
  export let onSelect: (key: string) => void
  export let actions: (file: ListFile) => FileAction[] = () => []
  export let onMenu: (event: MouseEvent, file: ListFile) => void = () => {}
  export let glyph: (status: string) => string = (s) => s
  export let emptyMessage = 'Nothing left to resolve.'
</script>

<div class="files">
  {#each sections as section (section.title)}
    <div class="section">{section.title}</div>
    {#each section.files as f (f.path)}
      {@const key = rowKey(section.title, f.path)}
      <div class="row" class:active={selected === key}>
        <button class="row-item file" class:active={selected === key} on:click={() => onSelect(key)} on:contextmenu|preventDefault={(e) => onMenu(e, f)}>
          <span class="status s-{f.status}">
            {#if f.status === 'staged'}<Icon name="check" size={12} />{:else}{glyph(f.status)}{/if}
          </span>
          <span class="ellipsis">{f.path}</span>
        </button>
        {#each actions(f) as action (action.label)}
          <button class="act" class:danger={action.danger} disabled={action.disabled} title={action.title} on:click={action.run}>{action.label}</button>
        {/each}
      </div>
    {/each}
  {:else}
    <div class="none">{emptyMessage}</div>
  {/each}
</div>

<style>
  .files { overflow-y: auto; padding: 6px 8px; border-right: 1px solid var(--border); }
  .section { padding: 8px 10px 2px; font-size: 11px; font-weight: 600; color: var(--faint); }
  .section:first-child { padding-top: 2px; }
  .row { position: relative; }
  .file { height: 24px; font-size: 12px; }
  .row .file { padding-right: 64px; }
  .act { position: absolute; right: 4px; top: 2px; height: 20px; padding: 0 8px; font-size: 11px; border-radius: 6px; border: 1px solid var(--border); background: var(--surface); color: var(--text); visibility: hidden; }
  .row:hover .act, .row.active .act, .act:focus-visible { visibility: visible; }
  .act:hover:not(:disabled) { background: var(--hover); }
  .act.danger { color: var(--danger); border-color: var(--danger); }
  .status { width: 14px; flex: none; font-family: var(--mono); font-weight: 600; color: var(--muted); }
  .s-staged { color: var(--ok); }
  .s-manual { color: var(--danger); }
  .none { padding: 4px 10px; color: var(--faint); font-size: 12px; }
</style>
