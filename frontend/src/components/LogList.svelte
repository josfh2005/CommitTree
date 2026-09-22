<script lang="ts">
  import { onDestroy, tick } from 'svelte'
  import { api } from '../lib/api'
  import { checkoutCommit, newBranch, newTag, resetBranch } from '../lib/actions'
  import { relativeDate } from '../lib/format'
  import {
    arrowAt, DOT_RADIUS, edgeSegment, graphWidth, laneColor, laneX, ROW_HEIGHT, rowCenterY, visibleRange,
  } from '../lib/geometry'
  import { busy, chatOpen, filters, jumpTo, logOrder, logVersion, mergeState, refs, selectedHash } from '../lib/stores'
  import type { LogRow } from '../lib/types'
  import { copyText, errorMessage, openMenu, toast } from '../lib/ui'

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
    const nearEnd = scrollTop + viewport > (rows.length - 100) * ROW_HEIGHT
    if (hasMore && loadingGen === -1 && nearEnd) loadPage(generation)
  }

  $: range = visibleRange(scrollTop, viewport, rows.length)
  // Size the graph column to the rows on screen so one wide stretch of history
  // doesn't squeeze the messages everywhere else.
  $: width = graphVisible ? graphWidth(rows.slice(range.start, Math.min(rows.length, range.end + 1))) : 12
  $: draw(canvas, rows, range, width, viewport, scrollTop, graphVisible)

  function draw(..._deps: unknown[]) {
    if (!canvas || !graphVisible) return
    const dpr = window.devicePixelRatio || 1
    canvas.width = width * dpr
    canvas.height = viewport * dpr
    const ctx = canvas.getContext('2d')
    if (!ctx) return
    ctx.setTransform(dpr, 0, 0, dpr, 0, -scrollTop * dpr)
    ctx.clearRect(0, scrollTop, width, viewport)
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
  }

  function graphPoint(event: MouseEvent) {
    const rect = canvas.getBoundingClientRect()
    return { x: event.clientX - rect.left, y: event.clientY - rect.top + scrollTop }
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
    const row = rows[Math.floor(p.y / ROW_HEIGHT)]
    if (row) selectedHash.set(row.hash)
  }

  function onGraphContext(event: MouseEvent) {
    const row = rows[Math.floor(graphPoint(event).y / ROW_HEIGHT)]
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
    scroller.scrollTop = Math.max(0, index * ROW_HEIGHT - viewport / 2)
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
    <div class="spacer" style="height: {(rows.length + (shallow && !hasMore && rows.length ? 1 : 0)) * ROW_HEIGHT}px">
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
      {#each rows.slice(range.start, range.end) as row, i (row.hash)}
        <button
          class="row"
          class:selected={row.hash === $selectedHash}
          class:merge={row.isMerge}
          style="top: {(range.start + i) * ROW_HEIGHT}px; padding-left: {width}px"
          on:click={() => selectedHash.set(row.hash)}
          on:contextmenu={(e) => commitMenu(e, row)}
        >
          <span class="subject ellipsis">
            {#each row.refs.filter((r) => r.kind !== 'head') as ref}
              <span class="badge {ref.kind}">{ref.name}</span>
            {/each}
            {row.subject}
          </span>
          <span class="author ellipsis">{row.author}</span>
          <span class="date">{relativeDate(row.date)}</span>
          <span class="hash mono">{row.short}</span>
        </button>
      {/each}
      {#if shallow && !hasMore && rows.length}
        <div class="note" style="top: {rows.length * ROW_HEIGHT}px">Shallow clone: older history is not available locally.</div>
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
  .row:hover { background: var(--hover); }
  .row.selected { background: var(--selection); }
  .merge .subject { color: var(--merge-text); }
  .author, .date, .hash { color: var(--muted); font-size: 12px; }
  .badge { display: inline-block; margin-right: 6px; padding: 0 6px; border-radius: 4px; font-size: 11px; line-height: 17px; background: var(--hover); color: var(--muted); }
  .badge.local { color: var(--text); background: var(--active); }
  .badge.tag { color: var(--accent); }
  .note { position: absolute; left: 0; right: 0; height: 28px; line-height: 28px; text-align: center; font-size: 12px; color: var(--faint); }
  .overlay { position: absolute; inset: 0; display: grid; place-items: center; color: var(--muted); pointer-events: none; }
  .overlay.error { color: var(--danger); padding: 24px; white-space: pre-wrap; user-select: text; }
  .tooltip { position: fixed; z-index: 30; max-width: 360px; padding: 4px 8px; border-radius: 6px; background: var(--text); color: var(--bg); font-size: 12px; pointer-events: none; }
</style>
