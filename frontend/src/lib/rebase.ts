import type { ConflictKind, RebasePreview } from './types'
import { NOTHING_TO_APPLY, PICKED, REBASED, UP_TO_DATE } from './types'

const plural = (n: number, one: string, many: string) => (n === 1 ? `1 ${one}` : `${n} ${many}`)

/** rebaseMessage is the confirmation before a rebase; warnings only when they apply. */
export function rebaseMessage(head: string, onto: string, p: RebasePreview): string {
  const parts = [`Rebase ${head} onto ${onto}? ${plural(p.commits, 'commit', 'commits')} will be replayed on top of ${onto}.`]
  if (p.published > 0 && p.upstream) {
    const these = p.published === 1 ? '1 of these commits is' : `${p.published} of these commits are`
    parts.push(`⚠ ${these} already on ${p.upstream}. After rebasing you'll need to force-push, which git-ui doesn't do.`)
  }
  if (p.merges > 0) {
    parts.push(`⚠ ${plural(p.merges, 'merge commit', 'merge commits')} in this range will be flattened.`)
  }
  return parts.join('\n\n')
}

const inProgress = (kind: ConflictKind | '') => (kind ? `Finish the ${kind} in progress first` : null)

/** rebaseBlocker is why a rebase entry is disabled, or null when it is not. */
export function rebaseBlocker(
  o: { isHead: boolean; contained: boolean; detached: boolean; busy: boolean; kind: ConflictKind | '' },
  head: string,
  target: string,
): string | null {
  if (o.isHead) return 'This is the current branch'
  if (o.detached) return 'No branch is checked out'
  if (o.busy) return 'Another operation is running'
  const blocked = inProgress(o.kind)
  if (blocked) return blocked
  if (o.contained) return `${head} already contains ${target}`
  return null
}

/** cherryPickBlocker is why the cherry-pick entry is disabled, or null. */
export function cherryPickBlocker(o: { contained: boolean; isMerge: boolean; detached: boolean; busy: boolean; kind: ConflictKind | '' }): string | null {
  if (o.isMerge) return "Cherry-picking a merge commit isn't supported"
  if (o.detached) return 'No branch is checked out'
  if (o.busy) return 'Another operation is running'
  const blocked = inProgress(o.kind)
  if (blocked) return blocked
  if (o.contained) return 'Already on this branch'
  return null
}

/** doneMessage is the notice after a rebase or cherry-pick that did not stop on conflicts. */
export function doneMessage(outcome: number, w: { op: 'rebase' | 'cherry-pick'; head: string; target: string; commits?: number }): string | null {
  if (outcome === REBASED) return `Rebased ${w.head} onto ${w.target}${w.commits ? ` — ${plural(w.commits, 'commit', 'commits')}` : ''}`
  if (outcome === UP_TO_DATE) return 'Already up to date'
  if (outcome === PICKED) return `Cherry-picked ${w.target} onto ${w.head}`
  if (outcome === NOTHING_TO_APPLY) return `Nothing to apply: those changes are already on ${w.head}`
  return null
}
