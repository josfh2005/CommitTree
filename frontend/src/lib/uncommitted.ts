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

/** How the "Uncommitted changes" row is drawn in the graph, Sourcetree
 *  style: a hollow dashed dot joined to HEAD by a dashed line.
 *  - HEAD is the first row: the dot sits in HEAD's own lane straight above
 *    it; nothing else needs to move.
 *  - HEAD is further down: lane 0 is reserved for the line (`shift` — the
 *    real graph moves one lane right), which runs down to HEAD's row and
 *    hops into HEAD's lane there, so it never crosses another branch.
 *  - HEAD is not loaded yet but more pages are coming: the line runs to the
 *    end of what is loaded and joins HEAD once it is paged in.
 *  - HEAD is not in the log at all (filtered out, unborn branch): just the
 *    dot, in lane 0, with nothing reserved. */
export interface UncommittedMarker {
  lane: number
  color: number
  shift: boolean
  line: 'head' | 'end' | 'none'
  headIndex: number
  headLane: number
}

export function uncommittedMarker(rows: LogRow[], hasMore: boolean): UncommittedMarker {
  const index = rows.findIndex((r) => r.isHead)
  if (index === 0) {
    const head = rows[0]
    return { lane: head.lane, color: head.color, shift: false, line: 'head', headIndex: 0, headLane: head.lane }
  }
  if (index > 0) {
    const head = rows[index]
    return { lane: 0, color: head.color, shift: true, line: 'head', headIndex: index, headLane: head.lane }
  }
  if (hasMore) return { lane: 0, color: 0, shift: true, line: 'end', headIndex: -1, headLane: 0 }
  return { lane: 0, color: 0, shift: false, line: 'none', headIndex: -1, headLane: 0 }
}

/** What to select once the tree is clean while the row was selected (a
 *  commit, discard or stash emptied it): null = leave things alone, '' =
 *  select nothing, a hash = select that commit (HEAD, so a commit made from
 *  the row is what the details pane shows next). */
export function cleanTreeSelection(selected: boolean, count: number, headHash: string): string | null {
  if (!selected || count > 0) return null
  return headHash
}

/** Once cleanTreeSelection lands the selection on `$refs.headHash` right
 *  after a commit, refs may still be one round trip behind the worktree
 *  state (refreshRepo loads refs before it bumps logVersion). `followed`
 *  is that hash, remembered until the next logVersion tick: if the
 *  selection is still there and headHash has since moved on, jump to the
 *  now-current headHash so a commit or amend from the row lands on the new
 *  HEAD, not the stale parent (or orphaned pre-amend commit). Returns null
 *  to leave the selection alone — the caller drops the follow either way. */
export function followHead(followed: string, selected: string, headHash: string): string | null {
  if (!followed || selected !== followed) return null
  if (!headHash || headHash === followed) return null
  return headHash
}
