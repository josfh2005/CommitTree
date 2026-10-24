<script lang="ts">
  import Icon from './Icon.svelte'
  import Splitter from './Splitter.svelte'
  import StatusIcon from './StatusIcon.svelte'
  import { api } from '../lib/api'
  import { relativeDate } from '../lib/format'
  import { headStatusText, hostingInfo, isHttps, loadHostingInfo, pullCounts } from '../lib/hosting'
  import { emptyMessage, type PRFilter } from '../lib/prs'
  import { settingsTab } from '../lib/settingsTabs'
  import { detailsHeight, logVersion, repos, selectedPR, settingsOpen } from '../lib/stores'
  import type { PullRequestList } from '../lib/types'
  import { errorMessage } from '../lib/ui'
  import { BrowserOpenURL } from '../../wailsjs/runtime/runtime'

  export let repoId: string

  const FILTERS: { id: PRFilter; label: string }[] = [
    { id: 'open', label: 'Open' },
    { id: 'mine', label: 'Mine' },
    { id: 'review', label: 'Review requested' },
  ]

  let filter: PRFilter = 'open'
  let list: PullRequestList | null = null
  let error = ''
  let loading = false
  // What `list` answers: a repository or filter switched to clears the rows,
  // a refresh of the same one keeps them until the new answer is in.
  let shown = ''
  let request = 0

  $: info = $hostingInfo[repoId]
  $: account = info?.account
  $: name = $repos.find((r) => r.id === repoId)?.name ?? ''
  $: selected = $selectedPR[repoId]
  // Opens, on focus (below), on Refresh, on a filter, when an account changes
  // and after a fetch or any other write (repo:changed makes App refresh the
  // repository, which bumps logVersion).
  $: load(repoId, filter, account, $logVersion)

  async function load(id: string, f: PRFilter, acct: string | undefined, _version: number) {
    const current = ++request
    const key = `${id}|${f}`
    if (key !== shown) {
      list = null
      shown = key
    }
    error = ''
    if (!acct) {
      // The info is not read yet.
      loadHostingInfo(id).catch(() => undefined)
      loading = false
      return
    }
    if (acct !== 'connected') {
      list = null
      loading = false
      return
    }
    loading = true
    try {
      const got = await api.listPullRequests(id, f)
      if (current !== request) return
      list = { items: got.items ?? [], more: got.more }
      // The sidebar badge counts the open ones.
      if (f === 'open') pullCounts.update((m) => ({ ...m, [id]: { n: list!.items.length, more: list!.more } }))
    } catch (e) {
      if (current !== request) return
      error = errorMessage(e)
      // A rejected token marks the account; read it again to say so.
      loadHostingInfo(id).catch(() => undefined)
    } finally {
      if (current === request) loading = false
    }
  }

  const refresh = () => load(repoId, filter, account, 0)
  const onFocus = () => account === 'connected' && refresh()

  function select(number: number) {
    selectedPR.update((m) => ({ ...m, [repoId]: number }))
  }

  function openAccounts() {
    settingsTab.set('accounts')
    settingsOpen.set(true)
  }

  $: empty = info ? emptyMessage(filter, info, error) : null
</script>

<svelte:window on:focus={onFocus} />

<div class="pulls">
  <header class="drag">
    <div class="title">
      <span class="heading">Pull requests</span>
      <span class="repo ellipsis">{name}</span>
    </div>
    <div class="tabs" role="tablist">
      {#each FILTERS as t (t.id)}
        <button class="tab" class:on={filter === t.id} role="tab" aria-selected={filter === t.id} on:click={() => (filter = t.id)}>{t.label}</button>
      {/each}
    </div>
    <button class="icon-btn" title="Refresh" disabled={account !== 'connected' || loading} on:click={refresh}>
      <Icon name="refresh" size={14} />
    </button>
    {#if info && isHttps(info.pullsURL)}
      <button class="btn" on:click={() => BrowserOpenURL(info.pullsURL)}>Open in browser</button>
    {/if}
  </header>

  <div class="list">
    {#if list && list.items.length}
      {#if error}<div class="banner">{error} <button class="link" on:click={refresh}>Retry</button></div>{/if}
      {#each list.items as pr (pr.number)}
        <button class="row" class:active={selected === pr.number} on:click={() => select(pr.number)}>
          <span class="state" title={pr.state && info ? headStatusText(info.noun, pr.state) : ''}>
            {#if pr.state}<StatusIcon state={pr.state} />{/if}
          </span>
          <span class="main">
            <span class="line">
              <span class="id">#{pr.number}</span>
              <span class="subject ellipsis" title={pr.title}>{pr.title}</span>
              {#if pr.draft}<span class="draft">Draft</span>{/if}
            </span>
            <span class="meta ellipsis">{pr.author} · {pr.source} → {pr.target}</span>
          </span>
          <span class="approvals" class:none={!pr.approvals} title="Approvals">✓ {pr.approvals}</span>
          <span class="comments" class:none={!pr.commentCount} title="Comments"><Icon name="chat" size={12} /> {pr.commentCount}</span>
          <span class="when">{relativeDate(pr.updated)}</span>
        </button>
      {/each}
      {#if list.more}
        <div class="more">
          {#if info && isHttps(info.pullsURL)}
            <button class="link" on:click={() => BrowserOpenURL(info.pullsURL)}>Showing 100. Open the rest in the browser</button>
          {:else}
            Showing 100. Open the rest in the browser
          {/if}
        </div>
      {/if}
    {:else if loading}
      <div class="note">Loading…</div>
    {:else if empty}
      <div class="note" class:err={!!error}>
        {empty.text}
        {#if empty.action === 'accounts'}<button class="btn" on:click={openAccounts}>Open Settings</button>{/if}
        {#if error && account === 'connected'}<button class="link" on:click={refresh}>Retry</button>{/if}
      </div>
    {/if}
  </div>

  {#if selected}
    <Splitter direction="horizontal" on:drag={(e) => detailsHeight.set(Math.min(720, Math.max(120, $detailsHeight - e.detail)))} />
    <div class="details" style="height: {$detailsHeight}px">
      <!-- SLOT (Task 13): PullRequestDetail for pull request #{selected} of this
           repository goes here, keyed on $selectedPR[repoId]. -->
      <div class="note">Pull request #{selected}</div>
    </div>
  {/if}
</div>

<style>
  .pulls { display: flex; flex-direction: column; height: 100%; }
  header { display: flex; align-items: center; gap: 8px; height: 60px; padding: 0 10px 0 14px; flex: none; }
  .title { flex: 1; min-width: 0; display: flex; flex-direction: column; align-items: flex-start; font-weight: 500; }
  .repo { font-weight: 400; font-size: 12px; color: var(--faint); max-width: 100%; }
  .tabs { display: flex; gap: 2px; flex: none; }
  .tab { height: 26px; padding: 0 10px; border-radius: 6px; color: var(--muted); }
  .tab:hover { background: var(--hover); color: var(--text); }
  .tab.on { background: var(--active); color: var(--text); font-weight: 500; }
  .list { flex: 1; min-height: 0; overflow-y: auto; border-top: 1px solid var(--border); }
  .row { display: flex; align-items: center; gap: 10px; width: 100%; min-height: 46px; padding: 6px 14px; text-align: left; border-bottom: 1px solid var(--border); }
  .row:hover { background: var(--hover); }
  .row.active { background: var(--active); }
  .state { width: 14px; flex: none; display: inline-grid; place-items: center; }
  .main { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 1px; }
  .line { display: flex; align-items: baseline; gap: 6px; min-width: 0; }
  .id { color: var(--faint); flex: none; }
  .subject { font-weight: 500; }
  .draft { flex: none; font-size: 11px; padding: 0 6px; border: 1px solid var(--border); border-radius: 8px; color: var(--muted); }
  .meta { font-size: 12px; color: var(--muted); }
  .approvals, .comments, .when { flex: none; font-size: 12px; color: var(--ok); display: inline-flex; align-items: center; gap: 3px; }
  .comments { color: var(--muted); }
  .approvals.none, .comments.none { color: var(--faint); }
  .when { color: var(--faint); min-width: 62px; justify-content: flex-end; }
  .banner { padding: 8px 14px; font-size: 12px; color: var(--danger); }
  .more { padding: 10px 14px; font-size: 12px; color: var(--muted); }
  .note { padding: 24px; text-align: center; color: var(--muted); display: flex; flex-direction: column; align-items: center; gap: 10px; }
  .note.err { color: var(--danger); }
  .link { color: var(--accent); }
  .link:hover { text-decoration: underline; }
  .details { flex: none; min-height: 0; overflow: auto; }
</style>
