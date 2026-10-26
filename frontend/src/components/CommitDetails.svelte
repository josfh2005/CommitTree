<script lang="ts">
  import ChecksList from './ChecksList.svelte'
  import DiffText from './DiffText.svelte'
  import Icon from './Icon.svelte'
  import SubmoduleDiffView from './SubmoduleDiff.svelte'
  import Tickets from './Tickets.svelte'
  import { api } from '../lib/api'
  import { blameBlocker } from '../lib/blame'
  import { isHosted, loadHostingInfo, settleChecks } from '../lib/hosting'
  import { jumpTo, openBlame, repos, selectRepo } from '../lib/stores'
  import { parseSubmoduleDiff, submoduleRepoId } from '../lib/submodules'
  import type { CommitStatus, Details, FileChange } from '../lib/types'
  import { copyText, errorMessage, openMenu } from '../lib/ui'

  export let repoId: string
  export let hash: string

  let details: Details | null = null
  let error = ''
  let file: FileChange | null = null
  let diff = ''
  let request = 0

  // The commit's pipelines or checks, asked of the host once the repository
  // is known to be on one with a connected account.
  let checks: CommitStatus | null = null
  let checksNoun = ''
  let checksLoading = false
  let checksError = ''
  let checksRequest = 0
  // Nothing to show for a commit with no pipelines or checks.
  $: showChecks = checksLoading || !!checksError || (!!checks && (checks.state !== 'none' || !!checks.checks?.length))

  $: load(repoId, hash)
  $: loadChecks(repoId, hash)
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

  async function loadChecks(id: string, h: string) {
    const current = ++checksRequest
    checks = null
    checksError = ''
    checksLoading = false
    try {
      // Selecting commits one after another (arrow keys) asks for the last one only.
      if (!(await settleChecks(() => current === checksRequest))) return
      const info = await loadHostingInfo(id)
      if (current !== checksRequest || !isHosted(info)) return
      checksNoun = info.noun
      checksLoading = true
      const answer = await api.commitChecks(id, h)
      if (current === checksRequest) checks = answer
    } catch (e) {
      if (current === checksRequest) checksError = errorMessage(e)
    } finally {
      if (current === checksRequest) checksLoading = false
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

  function fileMenu(event: MouseEvent, f: FileChange) {
    const hash = details?.hash
    if (!hash) return
    const why = blameBlocker(f)
    openMenu(event, [{ label: 'Blame', action: () => openBlame(f.path, hash), disabled: why !== null, title: why ?? undefined }])
  }
</script>

<div class="details">
  {#if error}
    <div class="error">{error}</div>
  {:else if details}
    <div class="info">
      <div class="subject"><Tickets text={details.subject} /></div>
      {#if details.body}<pre class="body"><Tickets text={details.body} /></pre>{/if}
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
      {#if showChecks}
        <ChecksList status={checks} noun={checksNoun} loading={checksLoading} error={checksError} onRetry={() => loadChecks(repoId, hash)} />
      {/if}
      <div class="files">
        {#each details.files as f (f.path)}
          <button
            class="row-item file"
            class:active={file === f}
            title={f.oldPath ? `${f.oldPath} → ${f.path}` : f.path}
            on:click={() => openFile(f)}
            on:contextmenu={(e) => fileMenu(e, f)}
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
        <DiffText text={diff} />
      {/if}
    </div>
  {/if}
</div>

<style>
  .details { display: grid; grid-template-columns: minmax(260px, 36%) 1fr; height: 100%; }
  .info { overflow-y: auto; padding: 12px 8px 12px 14px; border-right: 1px solid var(--border); -webkit-user-select: text; user-select: text; }
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
  .diff { overflow: auto; padding: 8px 0; -webkit-user-select: text; user-select: text; }
  .meta { color: var(--faint); }
  .error { padding: 12px; color: var(--danger); white-space: pre-wrap; }
  .explanation :global(p) { margin: 0 0 6px; }
  .explanation :global(code) { font-family: var(--mono); font-size: 12px; background: var(--hover); padding: 0 4px; border-radius: 4px; }
</style>
