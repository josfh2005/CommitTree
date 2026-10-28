import type { Branch, BranchPushResult, Refs } from './types'

/** Push: the rules around the Push dialog and its results
 *  (docs/spec/05-remote-and-stash.md). Pure, so every rule is tested. */

/** One row of a results dialog. */
export interface ResultRow { mark: string; label: string; detail: string; tone: 'ok' | 'muted' | 'error' }

const commits = (n: number) => (n === 1 ? '1 commit' : `${n} commits`)

/** The local branches besides the current one that "Push main branches" ticks:
 *  official (decided in Go: main, master, develop, release/* or the git-flow
 *  names) and ahead of a live upstream on a remote, as the last fetch saw it. */
export function othersAhead(refs: Refs | null): Branch[] {
  return (refs?.local ?? []).filter((b) => !b.current && !!b.official && !!b.upstream && !b.upstreamGone && !b.upstreamLocal && (b.ahead ?? 0) > 0)
}

export const pushFailed = (r: BranchPushResult) => r.status === 'rejected' || r.status === 'failed'

/** The toast after a push in which nothing failed. The tags row, when the
 *  push also sent tags, is not a branch: it reads "and tags" after them. */
export function pushedMessage(results: BranchPushResult[]): string {
  if (results.length === 0) return 'Nothing to push'
  const tags = results.find((r) => r.tags)?.status === 'pushed'
  const rows = results.filter((r) => !r.tags)
  const pushed = rows.filter((r) => r.status === 'pushed')
  if (pushed.length === 0) return tags ? withUpToDate('Pushed tags', rows.length) : 'Everything up to date'
  const head = pushed.length === 1 ? `Pushed ${pushed[0].branch}` : `Pushed ${pushed.length} branches`
  return withUpToDate(tags ? `${head} and tags` : head, rows.length - pushed.length)
}

const withUpToDate = (head: string, upToDate: number) => (upToDate > 0 ? `${head}, ${upToDate} already up to date` : head)

export function failedMessage(results: BranchPushResult[]): string {
  const rows = results.filter((r) => !r.tags)
  const failed = rows.filter(pushFailed).length
  return failed === 0 ? 'Tags were not pushed' : `${failed} of ${rows.length} branches were not pushed`
}

export function pushResultRows(results: BranchPushResult[]): ResultRow[] {
  return results.map((r) => {
    const label = `${r.branch} → ${r.target}`
    if (pushFailed(r)) return { mark: '✗', label, detail: r.reason ?? '', tone: 'error' }
    if (r.status === 'upToDate') return { mark: '—', label, detail: 'Up to date', tone: 'muted' }
    return { mark: '✓', label, detail: 'Pushed', tone: 'ok' }
  })
}

/** Thrown inside track() so a push with branches left behind notifies as a
 *  failed push; carries the results for the dialog. */
export class PartialPushError extends Error {
  results: BranchPushResult[]
  constructor(results: BranchPushResult[]) {
    super(failedMessage(results))
    this.results = results
  }
}

/** The toolbar Push button's tooltip: the button opens the Push dialog. */
export function pushTitle(): string {
  return 'Push — choose branches to push'
}

/** The ahead/behind badges' tooltip on a local branch row; '' when there
 *  is nothing to show (remote-tracking rows never show badges). */
export function trackTitle(b: Branch): string {
  if (b.remote) return ''
  const parts: string[] = []
  if (b.ahead) parts.push(`${commits(b.ahead)} to push to ${b.upstream}`)
  if (b.behind) parts.push(`${commits(b.behind)} to pull`)
  return parts.length ? `${parts.join(' · ')}, as of the last fetch` : ''
}
