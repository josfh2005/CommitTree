import type { Branch, BranchPushResult, PushScope, Refs } from './types'
import type { ChoiceOptions } from './ui'

/** Push: what a click on Push and the "main branches" choice do
 *  (docs/spec/05-remote-and-stash.md). Pure, so every rule is tested. */

export type PushChoice = 'current' | 'all'

/** One row of a results dialog. */
export interface ResultRow { mark: string; label: string; detail: string; tone: 'ok' | 'muted' | 'error' }

const commits = (n: number) => (n === 1 ? '1 commit' : `${n} commits`)
const branches = (n: number) => (n === 1 ? '1 branch' : `${n} branches`)

/** The local branches besides the current one that "Main branches" pushes:
 *  official (decided in Go: main, master, develop, release/* or the git-flow
 *  names) and ahead of a live upstream on a remote, as the last fetch saw it. */
export function othersAhead(refs: Refs | null): Branch[] {
  return (refs?.local ?? []).filter((b) => !b.current && !!b.official && !!b.upstream && !b.upstreamGone && !b.upstreamLocal && (b.ahead ?? 0) > 0)
}

/** N in "Main branches (N)": the current branch (when it is official, HEAD
 *  is not detached and it does not track another local branch) plus the
 *  others ahead. */
export function pushCount(refs: Refs | null): number {
  const current = refs && !refs.detached ? refs.local.find((b) => b.current) : undefined
  return (current?.official && !current.upstreamLocal ? 1 : 0) + othersAhead(refs).length
}

/** What a click on Push does: the setting, except that "ask" pushes the
 *  current branch without asking when no other branch has commits to push. */
export function pushDecision(scope: PushScope, refs: Refs | null): PushChoice | 'ask' {
  if (scope !== 'ask') return scope
  return othersAhead(refs).length > 0 ? 'ask' : 'current'
}

export function pushChoiceOptions(repoName: string, refs: Refs | null): ChoiceOptions<PushChoice> {
  const n = pushCount(refs)
  const options: { value: PushChoice; label: string }[] = []
  if (refs && !refs.detached) options.push({ value: 'current', label: `Current branch (${refs.head})` })
  options.push({ value: 'all', label: `Main branches (${n})` })
  return {
    title: `Push ${repoName}`,
    label: 'Push',
    options,
    value: options[0].value,
    message: () => 'Change the default in Settings → General.',
    confirmLabel: (v) => (v === 'all' ? `Push ${branches(n)}` : 'Push'),
  }
}

export const pushFailed = (r: BranchPushResult) => r.status === 'rejected' || r.status === 'failed'

/** The toast after a push of the main branches in which nothing failed. */
export function pushedMessage(results: BranchPushResult[]): string {
  if (results.length === 0) return 'Nothing to push'
  const pushed = results.filter((r) => r.status === 'pushed')
  if (pushed.length === 0) return 'Everything up to date'
  const head = pushed.length === 1 ? `Pushed ${pushed[0].branch}` : `Pushed ${pushed.length} branches`
  const upToDate = results.length - pushed.length
  return upToDate > 0 ? `${head}, ${upToDate} already up to date` : head
}

export const failedMessage = (results: BranchPushResult[]) =>
  `${results.filter(pushFailed).length} of ${results.length} branches were not pushed`

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

/** The toolbar Push button's tooltip: what a click will do. */
export function pushTitle(scope: PushScope, refs: Refs | null): string {
  if (scope === 'all') return 'Push main branches'
  if (scope === 'ask') return 'Push — asks current or main branches'
  return refs?.head && !refs.detached ? `Push ${refs.head}` : 'Push'
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
