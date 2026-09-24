<script lang="ts">
  import Icon from './Icon.svelte'
  import SubmoduleDiffView from './SubmoduleDiff.svelte'
  import { api } from '../lib/api'
  import { jumpTo, repos, selectRepo } from '../lib/stores'
  import { parseSubmoduleDiff, submoduleRepoId } from '../lib/submodules'
  import type { Details, FileChange } from '../lib/types'
  import { copyText, errorMessage } from '../lib/ui'

  export let repoId: string
  export let hash: string

  const MAX_LINES = 5000

  let details: Details | null = null
  let error = ''
  let file: FileChange | null = null
  let diff = ''
  let request = 0



  $: load(repoId, hash)
  $: lines = diff.split('\n')
  $: sub = file?.submodule ? parseSubmoduleDiff(diff) : null
  $: subRepoId = file?.submodule ? submoduleRepoId($repos, repoId, file.path) : undefined

  async function load(id: string, h: string) {
    const current = ++request
    details = null
    error = ''
    file = null
    diff = ''
    try {
      const d = await api.getDetails(id, h)
      if (current !== request) return
      details = d
      if (d.files.length) openFile(d.files[0])
    } catch (e) {
      if (current === request) error = errorMessage(e)
    }
  }

  async function openFile(f: FileChange) {
    if (!details) return
    const current = request
    file = f
    diff = ''
    const paths = f.oldPath ? [f.oldPath, f.path] : [f.path]
    try {
      const text = await api.getDiff(repoId, details.parents[0] ?? '', details.hash, paths)
      if (current === request && file === f) diff = text
    } catch (e) {
      if (current === request && file === f) diff = errorMessage(e)
    }
  }

  function lineClass(line: string): string {
    if (line.startsWith('+++') || line.startsWith('---') || line.startsWith('diff ') || line.startsWith('index ')) return 'meta'
    if (line.startsWith('@@')) return 'hunk'
    if (line.startsWith('+')) return 'add'
    if (line.startsWith('-')) return 'del'
    return ''
  }

</script>

<div class="details">
  {#if error}
    <div class="error">{error}</div>
  {:else if details}
    <div class="info">
      <div class="subject">{details.subject}</div>
      {#if details.body}<pre class="body">{details.body}</pre>{/if}
      <div class="meta">
        <span>{details.author} &lt;{details.email}&gt;</span>
        <span>{new Date(details.date).toLocaleString()}</span>
        <span class="links">
          <button class="mono link" title="Copy full hash" on:click={() => copyText(details?.hash ?? '')}>{details.short}</button>
          {#each details.parents as parent}
            <button class="mono link" title="Go to parent" on:click={() => jumpTo.set(parent)}>↑ {parent.slice(0, 7)}</button>
          {/each}
        </span>
      </div>
      <div class="files">
        {#each details.files as f (f.path)}
          <button
            class="row-item file"
            class:active={file === f}
            title={f.oldPath ? `${f.oldPath} → ${f.path}` : f.path}
            on:click={() => openFile(f)}
          >
            {#if f.submodule}<span class="sub-mark"><Icon name="package" size={12} /></span>{/if}
            <span class="status s-{f.status}">{f.status}</span>
            <span class="ellipsis">{f.path}</span>
          </button>
        {:else}
          <div class="none">No file changes</div>
        {/each}
      </div>
    </div>
    <div class="diff mono">
      {#if sub}
        <SubmoduleDiffView diff={sub} onOpen={subRepoId ? () => selectRepo(subRepoId) : null} />
      {:else}
        {#each lines.slice(0, MAX_LINES) as line}
          <div class="line {lineClass(line)}">{line || ' '}</div>
        {/each}
        {#if lines.length > MAX_LINES}
          <div class="line meta">… diff truncated after {MAX_LINES} lines</div>
        {/if}
      {/if}
    </div>
  {/if}
</div>

<style>
  .details { display: grid; grid-template-columns: minmax(260px, 36%) 1fr; height: 100%; }
  .info { overflow-y: auto; padding: 12px 8px 12px 14px; border-right: 1px solid var(--border); user-select: text; }
  .subject { font-weight: 600; margin-bottom: 6px; }
  .body { color: var(--muted); margin-bottom: 8px; }
  .meta { display: flex; flex-direction: column; gap: 2px; font-size: 12px; color: var(--muted); margin-bottom: 10px; }
  .links { display: flex; flex-wrap: wrap; gap: 8px; }
  .link { color: var(--accent); }
  .link:hover { text-decoration: underline; }
  .file { height: 24px; font-size: 12px; }
  .status { width: 14px; flex: none; font-family: var(--mono); font-weight: 600; color: var(--muted); }
  .sub-mark { width: 12px; flex: none; display: inline-grid; place-items: center; color: var(--muted); }
  .s-A { color: #4f9d4f; }
  .s-D { color: var(--danger); }
  .s-R, .s-C { color: #3f7fbf; }
  .none { padding: 4px 10px; color: var(--faint); font-size: 12px; }
  .diff { overflow: auto; padding: 8px 0; user-select: text; }
  .line { padding: 0 12px; white-space: pre; line-height: 18px; }
  .add { background: var(--add-bg); }
  .del { background: var(--del-bg); }
  .hunk { color: #3f7fbf; }
  .meta { color: var(--faint); }
  .error { padding: 12px; color: var(--danger); white-space: pre-wrap; }
  .explanation :global(p) { margin: 0 0 6px; }
  .explanation :global(code) { font-family: var(--mono); font-size: 12px; background: var(--hover); padding: 0 4px; border-radius: 4px; }
</style>
