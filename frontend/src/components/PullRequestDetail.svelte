<script lang="ts">
  import ChecksList from './ChecksList.svelte'
  import DiffText from './DiffText.svelte'
  import FileList, { rowKey } from './FileList.svelte'
  import { warnMovedSubmodules } from '../lib/actions'
  import { api } from '../lib/api'
  import { checkoutNotice } from '../lib/checkout'
  import { relativeDate } from '../lib/format'
  import { isHttps } from '../lib/hosting'
  import { commentTree, fileSections, renderUntrusted, safeHref } from '../lib/prs'
  import { mainView, refreshRepo, refs, showCommitInLog } from '../lib/stores'
  import { beginOp } from '../lib/tasks'
  import type { HostingInfo, PRCommit, PRFile, PullRequestDetail } from '../lib/types'
  import { errorMessage, toast } from '../lib/ui'
  import { BrowserOpenURL } from '../../wailsjs/runtime/runtime'

  export let repoId: string
  export let number: number
  export let info: HostingInfo

  const FORK_NOTE = 'From a fork. Open it in the browser.'
  const VERDICTS: Record<string, string> = { approved: 'Approved', changes: 'Changes requested', pending: 'Pending' }

  let pr: PullRequestDetail | null = null
  let error = ''
  let files: PRFile[] = []
  let filesError = ''
  let filesLoading = false
  let selectedPath = ''
  // Commits found not to be in the local log, by hash.
  let notFetched: Record<string, boolean> = {}
  let checkingOut = false
  let request = 0

  $: load(repoId, number)
  $: sections = fileSections(files)
  $: selectedFile = files.find((f) => f.path === selectedPath) ?? null
  $: selected = selectedFile ? rowKey('Changed files', selectedFile.path) : ''
  $: thread = commentTree(pr?.comments ?? [])
  $: link = pr && safeHref(pr.url) ? pr.url : ''
  $: hasChecks = !!pr?.checks?.checks?.length

  async function load(id: string, n: number) {
    const current = ++request
    pr = null
    error = ''
    files = []
    filesError = ''
    filesLoading = true
    selectedPath = ''
    notFetched = {}
    api.getPullRequest(id, n).then(
      (got) => {
        if (current === request) pr = got
      },
      (e) => {
        if (current === request) error = errorMessage(e)
      },
    )
    try {
      const got = await api.getPullRequestFiles(id, n)
      if (current !== request) return
      files = got ?? []
      selectedPath = files[0]?.path ?? ''
    } catch (e) {
      if (current === request) filesError = errorMessage(e)
    } finally {
      if (current === request) filesLoading = false
    }
  }

  function selectFile(key: string) {
    const f = files.find((ff) => rowKey('Changed files', ff.path) === key)
    if (f) selectedPath = f.path
  }

  function open(url: string) {
    if (isHttps(url)) BrowserOpenURL(url)
  }

  // Links inside rendered host text are plain anchors: none may navigate the
  // webview, and only https ones open (in the system browser).
  function onTextClick(event: MouseEvent) {
    const a = (event.target as Element | null)?.closest?.('a')
    if (!a || !a.closest('.rendered')) return
    event.preventDefault()
    const href = safeHref(a.getAttribute('href') ?? '')
    if (href) BrowserOpenURL(href)
  }

  async function openCommit(c: PRCommit) {
    try {
      await api.getDetails(repoId, c.hash)
    } catch {
      notFetched = { ...notFetched, [c.hash]: true }
      return
    }
    showCommitInLog(c.hash)
  }

  async function checkout() {
    if (!pr || checkingOut) return
    const source = pr.source
    checkingOut = true
    const end = beginOp(`Checkout ${source}`, repoId)
    let failure: unknown
    let outcome = ''
    try {
      outcome = await api.checkoutPullRequest(repoId, number)
    } catch (e) {
      failure = e
    } finally {
      end(failure)
      checkingOut = false
    }
    await refreshRepo()
    if (failure !== undefined) return
    const notice = checkoutNotice(outcome, info.remote, source)
    if (notice) toast(notice)
    await warnMovedSubmodules(repoId)
    mainView.set('log')
    const tip = $refs?.local.find((b) => b.name === source)?.hash
    showCommitInLog(tip || pr?.headHash || '')
  }
</script>

<div class="detail">
  {#if error}
    <div class="error">{error} <button class="link" on:click={() => load(repoId, number)}>Retry</button></div>
  {:else if !pr}
    <div class="note">Loading…</div>
  {:else}
    <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions (links in the text are anchors, which take the keyboard) -->
    <div class="info" on:click={onTextClick}>
      <div class="head">
        <div class="title"><span class="id">#{pr.number}</span> {pr.title}</div>
        <div class="meta">
          <span>{pr.draft ? 'Draft' : 'Open'} · {pr.author}</span>
          <span class="mono">{pr.source} → {pr.target}</span>
          <span>
            <span title={new Date(pr.created).toLocaleString()}>Created {relativeDate(pr.created)}</span>
            · <span title={new Date(pr.updated).toLocaleString()}>updated {relativeDate(pr.updated)}</span>
          </span>
        </div>
      </div>

      <div class="actions">
        <button class="btn" disabled={pr.fork || checkingOut} title={pr.fork ? FORK_NOTE : ''} on:click={checkout}>Checkout branch</button>
        {#if link}<button class="btn" on:click={() => open(link)}>Open in browser</button>{/if}
      </div>
      {#if pr.fork}<div class="hint">{FORK_NOTE}</div>{/if}

      {#if pr.description.trim()}
        <div class="rendered text">{@html renderUntrusted(pr.description)}</div>
      {/if}

      {#if pr.reviewers?.length}
        <div class="section">Reviewers</div>
        {#each pr.reviewers as r (r.name)}
          <div class="reviewer">
            <span class="ellipsis">{r.name}</span>
            <span class="verdict v-{r.verdict}">{VERDICTS[r.verdict] ?? r.verdict}</span>
          </div>
        {/each}
      {/if}

      {#if hasChecks}
        <ChecksList status={pr.checks} noun={info.noun} loading={false} error="" onRetry={() => load(repoId, number)} />
      {/if}

      {#if pr.commits?.length}
        <div class="section">Commits</div>
        {#each pr.commits as c (c.hash)}
          <button class="row-item commit" title={c.subject} on:click={() => openCommit(c)}>
            <span class="mono sha">{c.short}</span>
            <span class="ellipsis">{c.subject}</span>
            {#if notFetched[c.hash]}<span class="nf">Not fetched</span>{/if}
          </button>
        {/each}
      {/if}

      {#if thread.length}
        <div class="section">Comments</div>
        {#each thread as { comment, depth } (comment.id)}
          <div class="comment" style="margin-left: {depth * 14}px">
            <div class="c-head">
              <span class="author">{comment.author}</span>
              <span class="when" title={new Date(comment.created).toLocaleString()}>{relativeDate(comment.created)}</span>
              {#if comment.verdict}<span class="verdict">{comment.verdict}</span>{/if}
            </div>
            {#if comment.path}<div class="c-path mono">{comment.path}{comment.line ? `:${comment.line}` : ''}</div>{/if}
            <div class="rendered text">{@html renderUntrusted(comment.body)}</div>
          </div>
        {/each}
      {/if}
      {#if pr.commentsMore}
        <div class="more">
          {#if link}<button class="link" on:click={() => open(link)}>Showing 500. Open the rest in the browser</button>{:else}Showing 500. Open the rest in the browser{/if}
        </div>
      {/if}
    </div>

    <div class="right">
      <FileList {sections} {selected} onSelect={selectFile} emptyMessage={filesLoading ? 'Loading…' : filesError || 'No changed files'} />
      <div class="diff">
        {#if selectedFile?.noDiff}
          <div class="nodiff">
            No diff available here.
            {#if link}<button class="link" on:click={() => open(link)}>Open in browser</button>{:else}Open in browser{/if}
          </div>
        {:else if selectedFile}
          <div class="mono"><DiffText text={selectedFile.patch} /></div>
        {/if}
      </div>
    </div>
  {/if}
</div>

<style>
  .detail { display: grid; grid-template-columns: minmax(260px, 36%) 1fr; height: 100%; min-height: 0; }
  .detail > .error, .detail > .note { grid-column: 1 / -1; }
  .info { overflow-y: auto; padding: 12px 8px 12px 14px; border-right: 1px solid var(--border); -webkit-user-select: text; user-select: text; font-size: 12px; }
  .title { font-size: 13px; font-weight: 600; margin-bottom: 4px; }
  .id { color: var(--faint); font-weight: 400; }
  .meta { display: flex; flex-direction: column; gap: 2px; color: var(--muted); margin-bottom: 10px; }
  .actions { display: flex; flex-wrap: wrap; gap: 8px; margin-bottom: 6px; }
  .hint { color: var(--faint); margin-bottom: 8px; }
  .section { font-weight: 600; margin: 12px 0 4px; }
  .text { margin: 10px 0; overflow-wrap: anywhere; }
  .text :global(p) { margin: 0 0 6px; }
  .text :global(ul) { margin: 4px 0 6px; padding-left: 18px; }
  .text :global(code) { font-family: var(--mono); font-size: 12px; padding: 0 4px; border-radius: 4px; background: var(--hover); }
  .text :global(pre) { margin: 6px 0; padding: 8px; border-radius: 8px; background: var(--hover); overflow-x: auto; }
  .text :global(pre code) { padding: 0; background: none; }
  .text :global(a) { color: var(--accent); cursor: pointer; }
  .reviewer { display: flex; justify-content: space-between; gap: 8px; min-height: 22px; align-items: center; }
  .verdict { flex: none; color: var(--muted); }
  .v-approved { color: var(--ok); }
  .v-changes { color: var(--danger); }
  .commit { height: 24px; font-size: 12px; }
  .sha { flex: none; color: var(--accent); }
  .nf { flex: none; margin-left: auto; color: var(--faint); }
  .comment { margin: 8px 0; padding-left: 8px; border-left: 2px solid var(--border); }
  .c-head { display: flex; gap: 8px; align-items: baseline; }
  .author { font-weight: 600; }
  .when { color: var(--faint); }
  .c-path { color: var(--muted); margin-top: 2px; overflow-wrap: anywhere; }
  .comment .text { margin: 4px 0 0; }
  .more { margin-top: 10px; color: var(--muted); }
  .link { color: var(--accent); }
  .link:hover { text-decoration: underline; }
  .right { display: flex; flex-direction: column; min-width: 0; min-height: 0; }
  .right :global(.files) { flex: none; max-height: 34%; border-right: none; border-bottom: 1px solid var(--border); }
  .diff { flex: 1; min-height: 0; overflow: auto; padding: 8px 0; -webkit-user-select: text; user-select: text; }
  .nodiff { padding: 12px; color: var(--muted); }
  .error { padding: 12px; color: var(--danger); white-space: pre-wrap; }
  .note { padding: 24px; text-align: center; color: var(--muted); }
</style>
