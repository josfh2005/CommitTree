import type { Remote } from './types'

/** What CreateBranch did about the remote branch of the same name (Go's
 *  refs.Tracking): twin is "origin/x" ('' when no remote has it); tracked
 *  is false with a twin when the two have diverged. */
export interface Tracking {
  twin: string
  tracked: boolean
}

/** The remote branch a local branch called name naturally tracks, the same
 *  rule as Go's refs.Twin: origin's when origin has it, else the only
 *  remote's that has it; '' when none, or several and none is origin. */
export function twinOf(name: string, remotes: Remote[]): string {
  const having = remotes.filter((r) => r.branches.some((b) => b.name === name))
  const pick = having.find((r) => r.name === 'origin') ?? (having.length === 1 ? having[0] : undefined)
  return pick ? `${pick.name}/${name}` : ''
}

/** The toast after New branch; '' when there was no remote branch of that name. */
export function createdBranchMessage(name: string, t: Tracking): string {
  if (t.tracked) return `Created ${name}, tracking ${t.twin}.`
  if (t.twin) return `Created ${name}. ${t.twin} exists but has diverged from it, so ${name} has no upstream.`
  return ''
}

/** The toast when a pull first made a branch track its remote twin;
 *  branch '' when the caller can't name it. */
export const nowTracksMessage = (branch: string, twin: string) => (branch ? `${branch} now tracks ${twin}.` : `Now tracking ${twin}.`)
