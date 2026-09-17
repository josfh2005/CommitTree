<script lang="ts">
  import { onDestroy } from 'svelte'
  import Icon from './Icon.svelte'
  import { api } from '../lib/api'
  import { filters, jumpTo } from '../lib/stores'

  export let repoId: string

  let text = $filters.text
  let paths = $filters.paths.join(', ')
  let authors: string[] = []
  let timer: ReturnType<typeof setTimeout> | undefined

  $: loadAuthors(repoId)

  async function loadAuthors(id: string) {
    try {
      authors = await api.getAuthors(id)
    } catch {
      authors = []
    }
  }

  function onText() {
    clearTimeout(timer)
    timer = setTimeout(async () => {
      const value = text.trim()
      if (/^[0-9a-f]{4,40}$/i.test(value)) {
        const hash = await api.resolveCommit(repoId, value).catch(() => '')
        if (hash) {
          filters.update((f) => ({ ...f, text: '' }))
          jumpTo.set(hash)
          return
        }
      }
      filters.update((f) => ({ ...f, text: value }))
    }, 300)
  }

  onDestroy(() => clearTimeout(timer))

  const update = (key: 'author' | 'since' | 'until') => (event: Event) =>
    filters.update((f) => ({ ...f, [key]: (event.target as HTMLInputElement | HTMLSelectElement).value }))

  function applyPaths() {
    filters.update((f) => ({ ...f, paths: paths.split(',').map((p) => p.trim()).filter(Boolean) }))
  }

  const shortRef = (ref: string) => ref.replace(/^refs\/(heads|remotes|tags)\//, '')
</script>

<div class="bar">
  <label class="search">
    <Icon name="search" size={14} />
    <input placeholder="Text or hash" bind:value={text} on:input={onText} />
  </label>
  {#if $filters.branch}
    <button class="chip" title="Show all branches" on:click={() => filters.update((f) => ({ ...f, branch: '' }))}>
      <span class="ellipsis">{shortRef($filters.branch)}</span>
      <Icon name="x" size={12} />
    </button>
  {:else}
    <span class="chip idle">All branches</span>
  {/if}
  <select value={$filters.author} on:change={update('author')} title="User">
    <option value="">All users</option>
    {#each authors as author}
      <option value={author}>{author}</option>
    {/each}
  </select>
  <input type="date" value={$filters.since} on:change={update('since')} title="Since" />
  <input type="date" value={$filters.until} on:change={update('until')} title="Until" />
  <input class="paths" placeholder="Paths, comma separated" bind:value={paths} on:change={applyPaths} />
</div>

<style>
  .bar { display: flex; align-items: center; gap: 8px; padding: 0 12px 10px; flex-wrap: wrap; }
  .search { display: flex; align-items: center; gap: 6px; padding-left: 8px; border: 1px solid var(--border); border-radius: 7px; color: var(--muted); background: var(--surface); }
  .search input { border: 0; padding-left: 0; width: 180px; }
  .chip { display: inline-flex; align-items: center; gap: 4px; max-width: 220px; height: 26px; padding: 0 8px; border-radius: 13px; background: var(--active); }
  .chip.idle { background: none; color: var(--muted); }
  select { height: 28px; max-width: 160px; }
  input[type='date'] { height: 28px; }
  .paths { flex: 1; min-width: 140px; }
</style>
