<script lang="ts">
  import { onDestroy, tick } from 'svelte'
  import { api } from '../lib/api'
  import { checkoutCommit, newBranch, newTag, resetBranch } from '../lib/actions'
  import { relativeDate } from '../lib/format'
  import {
    arrowAt, DOT_RADIUS, edgeSegment, graphWidth, laneColor, laneX, ROW_HEIGHT, rowCenterY, visibleRange,
  } from '../lib/geometry'
  import { isCurrentBranchRef } from '../lib/refBadge'
  import { busy, chatOpen, filters, jumpTo, logOrder, logVersion, mergeState, refs, selectedHash, selectUncommitted, uncommittedSelected, worktreeState } from '../lib/stores'
  import type { LogRow } from '../lib/types'
  import { copyText, errorMessage, openMenu, toast } from '../lib/ui'
  import { cleanTreeSelection, followHead, markerWidth, uncommittedCount, uncommittedMarker } from '../lib/uncommitted'

  export let repoId: string

  const PAGE = 500
  const MAX_JUMP_PAGES = 20

  let rows: LogRow[] = []
  let byHash = new Map<string, number>()
  let hasMore = false
  let graphVisible = true
  let shallow = false
  let error = ''
  let generation = 0
  let loadingGen = -1

  let scroller: HTMLDivElement
  let canvas: HTMLCanvasElement
  let scrollTop = 0
  let viewport = 0
  let hover: { x: number; y: number; text: string } | null = null

  // The synthetic "Uncommitted changes" row sits above the first commit;
  // every real row is pushed down by `lead` pixels while it is shown. Row
  // indices never change — only the index↔pixel mapping does.
  $: count = uncommittedCount($worktreeState)
  $: lead = count > 0 ? ROW_HEIGHT : 0
  $: marker = uncommittedMarker(rows)

  // The row appearing/disappearing shifts every commit below it by
  // ROW_HEIGHT; keep on-screen content still by shifting scrollTop the same
  // amount, unless we're already at the top (scrollTop 0), where the new
  // row should become visible instead of being scrolled past.
  let prevLead = lead
  $: if (lead !== prevLead) {
    if (scroller && scroller.scrollTop > 0) scroller.scrollTop += lead - prevLead
    prevLead = lead
  }

  // A commit, discard or stash that empties the tree removes the row; if it
  // was selected, show HEAD instead of leaving an empty details pane.
  //
  // refreshRepo() loads refs before it bumps logVersion, so $refs.headHash
  // can still be the parent at this point (the backend emits
  // worktree:changed, which clears count, before CommitChanges/api call
  // resolves and refreshRepo's later loadRefs runs). Remember the hash we
  // selected as `followed`, along with the logVersion at the time, so the
  // block below can jump to the real new HEAD once refs catch up.
  let followed = ''
  let followedVersion = -1
  $: {
    const next = cleanTreeSelection($uncommittedSelected, count, $refs?.headHash ?? '')
    if (next !== null) {
      uncommittedSelected.set(false)
      if (next) {
        selectedHash.set(next)
        followed = next
        followedVersion = $logVersion
      }
    }
  }

  // Fires on the next logVersion bump after a follow was set (not the same
  // tick it was set in). If the selection is still the followed hash and
  // HEAD has since moved on, follow it there; otherwise drop the follow
  // without touching the selection.
  $: if (followed && $logVersion !== followedVersion) {
    const next = followHead(followed, $selectedHash, $refs?.headHash ?? '')
    followed = ''
    if (next) selectedHash.set(next)
  }

  $: reload(repoId, $filters, $logOrder, $logVersion)

  async function reload(..._deps: unknown[]) {
    const gen = ++generation
    rows = []
    byHash = new Map()
    hasMore = false
    error = ''
    if (scroller) scroller.scrollTop = 0
    scrollTop = 0
    api.isShallow(repoId).then((s) => gen === generation && (shallow = s)).catch(() => {})
    await loadPage(gen)
  }

  async function loadPage(gen: number) {
    if (loadingGen === gen) return
    loadingGen = gen
    try {
      const page = await api.getLog(repoId, $filters, $logOrder, rows.length, PAGE)
      if (gen !== generation) return
      page.rows.forEach((r, i) => byHash.set(r.hash, rows.length + i))
      rows = rows.concat(page.rows)
      hasMore = page.hasMore
      graphVisible = page.graphVisible
    } catch (e) {
      if (gen !== generation) return
      const message = errorMessage(e)
      if (message === 'log page is stale; reload from the start') reload()
      else error = message
    } finally {
      if (loadingGen === gen) loadingGen = -1
    }
  }

  function onScroll() {
    scrollTop = scroller.scrollTop
    hover = null
    const nearEnd = scrollTop - lead + viewport > (rows.length - 100) * ROW_HEIGHT
    if (hasMore && loadingGen === -1 && nearEnd) loadPage(generation)
  }

  $: range = visibleRange(Math.max(0, scrollTop - lead), viewport, rows.length)
  // Size the graph column to the rows on screen so one wide stretch of history
  // doesn't squeeze the messages everywhere else — plus the marker's lane
  // while the uncommitted row is on screen, since HEAD may be scrolled away.
  $: width = graphVisible
    ? Math.max(graphWidth(rows.slice(range.start, Math.min(rows.length, range.end + 1))), lead && scrollTop < lead ? markerWidth(marker.lane) : 0)
    : 12
  $: draw(canvas, rows, range, width, viewport, scrollTop, graphVisible, lead, marker)

  function draw(..._deps: unknown[]) {
    if (!canvas || !graphVisible) return
    const dpr = window.devicePixelRatio || 1
    canvas.width = width * dpr
    canvas.height = viewport * dpr
    const ctx = canvas.getContext('2d')
    if (!ctx) return
    ctx.setTransform(dpr, 0, 0, dpr, 0, (lead - scrollTop) * dpr)
    ctx.clearRect(0, scrollTop - lead, width, viewport)
    ctx.lineCap = 'round'
    ctx.lineJoin = 'round'

    const last = Math.min(rows.length, range.end + 1)
    for (let i = range.start; i < last; i++) {
      for (const edge of rows[i].edges) {
        const s = edgeSegment(edge, i)
        ctx.strokeStyle = laneColor(s.color)
        ctx.lineWidth = 1.6
        ctx.beginPath()
        ctx.moveTo(s.x1, s.y1)
        ctx.lineTo(s.x2, s.y2)
        ctx.stroke()
        if (s.arrow !== 'none') {
          const tip = s.arrow === 'down' ? s.y2 : s.y1
          const back = s.arrow === 'down' ? -5 : 5
          ctx.beginPath()
          ctx.moveTo(s.x1 - 4, tip + back)
          ctx.lineTo(s.x1, tip)
          ctx.lineTo(s.x1 + 4, tip + back)
          ctx.stroke()
        }
      }
    }

    const surface = getComputedStyle(canvas).getPropertyValue('--surface').trim() || '#fff'
    for (let i = range.start; i < range.end; i++) {
      const r = rows[i]
      ctx.beginPath()
      ctx.arc(laneX(r.lane), rowCenterY(i), DOT_RADIUS, 0, Math.PI * 2)
      if (r.isHead) {
        ctx.fillStyle = surface
        ctx.fill()
        ctx.strokeStyle = laneColor(r.color)
        ctx.lineWidth = 2
        ctx.stroke()
      } else {
        ctx.fillStyle = laneColor(r.color)
        ctx.fill()
      }
    }

    // The uncommitted row's marker, one row above the first commit: a hollow
    // dashed dot, joined to HEAD only when HEAD is directly below it.
    if (lead) {
      const x = laneX(marker.lane)
      const y = rowCenterY(-1)
      ctx.setLineDash([2, 2])
      ctx.strokeStyle = laneColor(marker.color)
      ctx.lineWidth = 1.6
      if (marker.joined) {
        ctx.beginPath()
        ctx.moveTo(x, y + DOT_RADIUS)
        ctx.lineTo(x, rowCenterY(0) - DOT_RADIUS)
        ctx.stroke()
      }
      ctx.beginPath()
      ctx.arc(x, y, DOT_RADIUS, 0, Math.PI * 2)
      ctx.fillStyle = surface
      ctx.fill()
      ctx.stroke()
      ctx.setLineDash([])
    }
  }

  function graphPoint(event: MouseEvent) {
    const rect = canvas.getBoundingClientRect()
    return { x: event.clientX - rect.left, y: event.clientY - rect.top + scrollTop - lead }
  }

  function onGraphMove(event: MouseEvent) {
    const p = graphPoint(event)
    const s = arrowAt(rows, p.x, p.y)
    if (!s?.target) {
      hover = null
      return
    }
    const index = byHash.get(s.target)
    hover = {
      x: event.clientX + 12,
      y: event.clientY + 12,
      text: index === undefined ? `Go to ${s.target.slice(0, 8)}` : rows[index].subject,
    }
  }

  function onGraphClick(event: MouseEvent) {
    const p = graphPoint(event)
    const s = arrowAt(rows, p.x, p.y)
    if (s?.target) {
      jumpTo.set(s.target)
      return
    }
    if (p.y < 0) {
      if (lead) selectUncommitted()
      return
    }
    const row = rows[Math.floor(p.y / ROW_HEIGHT)]
    if (row) selectedHash.set(row.hash)
  }

  function onGraphContext(event: MouseEvent) {
    const p = graphPoint(event)
    if (p.y < 0) return
    const row = rows[Math.floor(p.y / ROW_HEIGHT)]
    if (row) commitMenu(event, row)
  }

  // Explaining a commit answers in the chat panel, so the answer is kept in
  // the repository's conversation.
  async function explain(row: LogRow) {
    chatOpen.set(true)
    try {
      await api.explainInChat(repoId, row.hash, '', crypto.randomUUID())
    } catch (e) {
      toast(errorMessage(e), 'error')
    }
  }

  // Reset moves the checked-out branch, so it needs one, and not mid-merge:
  // a hard reset would abort the merge without asking.
  function resetItem(row: LogRow) {
    const branch = $refs?.head ?? ''
    return {
      label: `Reset ${branch || 'branch'} to here…`,
      action: () => resetBranch(repoId, row.hash, row.short, branch),
      disabled: !branch || !!$refs?.detached || !!$mergeState?.merging || row.hash === $refs?.headHash || !!$busy,
    }
  }

  function commitMenu(event: MouseEvent, row: LogRow) {
    selectedHash.set(row.hash)
    openMenu(event, [
      { label: '✨ Explain in chat', action: () => explain(row) },
      { label: 'Check out (detached)…', action: () => checkoutCommit(repoId, row.hash) },
      { label: 'New branch here…', action: () => newBranch(repoId, row.hash, row.short) },
      { label: 'New tag here…', action: () => newTag(repoId, row.hash, row.short) },
      resetItem(row),
      { label: 'Copy hash', action: () => copyText(row.hash) },
    ])
  }

  async function jump(hash: string) {
    const gen = generation
    while (loadingGen === gen) await new Promise((resolve) => setTimeout(resolve, 50))
    for (let n = 0; !byHash.has(hash) && hasMore && n < MAX_JUMP_PAGES && gen === generation; n++) {
      await loadPage(gen)
    }
    const index = byHash.get(hash)
    if (index === undefined) {
      toast('That commit is not in the current view. Clear the filters and try again.')
      return
    }
    selectedHash.set(hash)
    await tick()
    scroller.scrollTop = Math.max(0, index * ROW_HEIGHT + lead - viewport / 2)
  }

  const stopJump = jumpTo.subscribe((hash) => {
    if (!hash) return
    jumpTo.set('')
    jump(hash)
  })
  onDestroy(stopJump)
</script>

<div class="log">
  <div class="scroller" bind:this={scroller} bind:clientHeight={viewport} on:scroll={onScroll}>
    <div class="spacer" style="height: {(rows.length + (shallow && !hasMore && rows.length ? 1 : 0)) * ROW_HEIGHT + lead}px">
      {#if graphVisible}
        <canvas
          bind:this={canvas}
          style="top: {scrollTop}px; width: {width}px; height: {viewport}px"
          on:mousemove={onGraphMove}
          on:mouseleave={() => (hover = null)}
          on:click={onGraphClick}
          on:contextmenu={onGraphContext}
        ></canvas>
      {/if}
      {#if lead}
        <button
          class="row uncommitted"
          class:selected={$uncommittedSelected}
          style="top: 0; padding-left: {width}px"
          on:click={selectUncommitted}
          on:contextmenu|preventDefault
        >
          <span class="subject ellipsis">Uncommitted changes ({count})</span>
          <span></span><span></span><span></span>
        </button>
      {/if}
      {#each rows.slice(range.start, range.end) as row, i (row.hash)}
        <button
          class="row"
          class:selected={row.hash === $selectedHash}
          class:merge={row.isMerge}
          style="top: {(range.start + i) * ROW_HEIGHT + lead}px; padding-left: {width}px"
          on:click={() => selectedHash.set(row.hash)}
          on:contextmenu={(e) => commitMenu(e, row)}
        >
          <span class="subject ellipsis">
            {#each row.refs.filter((r) => r.kind !== 'head') as ref}
              <span class="badge {ref.kind}" class:current={isCurrentBranchRef(ref, row.isHead, $refs?.head ?? '')}>{ref.name}</span>
            {/each}
            {row.subject}
          </span>
          <span class="author ellipsis">{row.author}</span>
          <span class="date">{relativeDate(row.date)}</span>
          <span class="hash mono">{row.short}</span>
        </button>
      {/each}
      {#if shallow && !hasMore && rows.length}
        <div class="note" style="top: {rows.length * ROW_HEIGHT + lead}px">Shallow clone: older history is not available locally.</div>
      {/if}
    </div>
  </div>

  {#if error}
    <div class="overlay error">{error}</div>
  {:else if rows.length === 0 && loadingGen === -1}
    <div class="overlay">No commits yet</div>
  {/if}

  {#if hover}
    <div class="tooltip" style="left: {hover.x}px; top: {hover.y}px">{hover.text}</div>
  {/if}
</div>

<style>
  .log { position: relative; height: 100%; }
  .scroller { height: 100%; overflow-y: auto; overflow-x: hidden; }
  .spacer { position: relative; }
  canvas { position: absolute; left: 0; z-index: 2; }
  .row {
    position: absolute;
    left: 0;
    right: 0;
    height: 28px;
    display: grid;
    grid-template-columns: minmax(0, 1fr) 150px 90px 72px;
    align-items: center;
    gap: 12px;
    padding-right: 12px;
    text-align: left;
  }
  .row.selected { background: var(--selection); }
  .merge .subject { color: var(--merge-text); }
  .uncommitted .subject { font-style: italic; color: var(--muted); }
  .author, .date, .hash { color: var(--muted); font-size: 12px; }
  .badge { display: inline-block; margin-right: 6px; padding: 1px 6px; border-radius: 6px; font-size: 11px; line-height: 15px; }
  .badge.local, .badge.remote { background: var(--branch-badge-bg); color: var(--branch-badge-text); }
  .badge.tag { background: var(--tag-badge-bg); color: var(--tag-badge-text); }
  .badge.current { font-weight: 600; }
  .note { position: absolute; left: 0; right: 0; height: 28px; line-height: 28px; text-align: center; font-size: 12px; color: var(--faint); }
  .overlay { position: absolute; inset: 0; display: grid; place-items: center; color: var(--muted); pointer-events: none; }
  .overlay.error { color: var(--danger); padding: 24px; white-space: pre-wrap; user-select: text; }
  .tooltip { position: fixed; z-index: 30; max-width: 360px; padding: 4px 8px; border-radius: 6px; background: var(--text); color: var(--bg); font-size: 12px; pointer-events: none; }
</style>
