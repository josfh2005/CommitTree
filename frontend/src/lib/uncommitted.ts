import { GRAPH_PADDING, LANE_WIDTH } from './geometry'
import type { LogRow, WorktreeState } from './types'

/** Distinct paths with any uncommitted change. A partially staged file
 *  (git status "MM") is listed under both Staged and Unstaged, and is still
 *  one file to the person reading "Uncommitted changes (N)". */
export function uncommittedCount(state: WorktreeState | null): number {
  if (!state) return 0
  const paths = new Set<string>()
  for (const f of [...state.staged, ...state.unstaged, ...state.untracked]) paths.add(f.path)
  return paths.size
}

/** Where the "Uncommitted changes" row's dot sits in the graph: HEAD's lane
 *  and colour, joined to HEAD by a dashed line only when HEAD is the first
 *  row — anywhere lower, the line would cross other branches' lanes. With
 *  HEAD not loaded (filters, paging, an unborn branch) it falls back to
 *  lane 0 on its own. */
export function uncommittedMarker(rows: LogRow[]): { lane: number; color: number; joined: boolean } {
  const index = rows.findIndex((r) => r.isHead)
  if (index < 0) return { lane: 0, color: 0, joined: false }
  return { lane: rows[index].lane, color: rows[index].color, joined: index === 0 }
}

/** The graph column width that still shows the marker's lane — HEAD may be
 *  scrolled out of the visible slice that graphWidth measures. */
export function markerWidth(lane: number): number {
  return GRAPH_PADDING * 2 + (lane + 1) * LANE_WIDTH
}

/** What to select once the tree is clean while the row was selected (a
 *  commit, discard or stash emptied it): null = leave things alone, '' =
 *  select nothing, a hash = select that commit (HEAD, so a commit made from
 *  the row is what the details pane shows next). */
export function cleanTreeSelection(selected: boolean, count: number, headHash: string): string | null {
  if (!selected || count > 0) return null
  return headHash
}
