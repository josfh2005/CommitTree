import { laneColor } from './geometry'
import type { Ref } from './types'

/** Whether a ref's badge should carry the "current branch" emphasis. Only a
 *  local branch can be the one currently checked out, and only when this
 *  row is HEAD and the ref's name matches the current branch reported by
 *  refs.head — a detached HEAD (currentBranch === '') never matches. Branch
 *  badges are coloured by their row's lane (badgeLaneColor), so two branches
 *  on the same commit share a colour; the bold text is what singles out the
 *  one checked out. */
export function isCurrentBranchRef(ref: Ref, rowIsHead: boolean, currentBranch: string): boolean {
  return ref.kind === 'local' && rowIsHead && currentBranch !== '' && ref.name === currentBranch
}

/** badgeLaneColor is the colour a ref's badge is tinted with: a local or
 *  remote-tracking branch takes the colour of its row's graph dot, so the
 *  label reads as part of the line it names; a tag keeps its own colour
 *  (null). */
export function badgeLaneColor(ref: Ref, rowColor: number): string | null {
  return ref.kind === 'local' || ref.kind === 'remote' ? laneColor(rowColor) : null
}
