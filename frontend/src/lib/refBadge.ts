import type { Ref } from './types'

/** Whether a ref's badge should carry the "current branch" emphasis. Only a
 *  local branch can be the one currently checked out, and only when this
 *  row is HEAD and the ref's name matches the current branch reported by
 *  refs.head — a detached HEAD (currentBranch === '') never matches. This
 *  is the one non-trivial decision left once badges stopped being coloured
 *  per-lane and started being coloured per ref kind: local, remote and the
 *  current branch all share the same branch hue, so the only remaining
 *  question is which single badge (if any) gets the extra visual weight
 *  that used to come from the lane-specific "local" styling. */
export function isCurrentBranchRef(ref: Ref, rowIsHead: boolean, currentBranch: string): boolean {
  return ref.kind === 'local' && rowIsHead && currentBranch !== '' && ref.name === currentBranch
}
