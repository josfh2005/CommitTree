import { get } from 'svelte/store'
import { api } from './api'
import { notify } from './notify'
import { remoteBody, type NotifyEvent } from './notifyRules'
import { autoFetchMinutes, busy, refreshRepo, repos, selectedRepoId } from './stores'
import type { AutoFetchResult, Repo } from './types'

/** Background fetch (docs/spec/05-remote-and-stash.md): rounds over every
 *  repository, one at a time; what a result means is `outcome`'s, pure.
 *  Remotes that need credentials are paused in Go (internal/app/autopause.go). */

export const FIRST_ROUND_MS = 30_000

export interface AutoFetchAction {
  refresh: boolean
  event?: NotifyEvent
}

export function outcome(
  id: string,
  r: { result?: AutoFetchResult; error?: unknown },
  s: { selectedId: string; busy: boolean },
): AutoFetchAction {
  if (r.error !== undefined) return { refresh: false }
  const res = r.result
  if (!res || res.skipped) return { refresh: false }
  // A newly paused remote changed no refs but must show on the toolbar.
  const changed = res.refsChanged || (res.authFailed?.length ?? 0) > 0
  const action: AutoFetchAction = { refresh: changed && id === s.selectedId && !s.busy }
  if (res.newCommits > 0) action.event = { category: 'remote', repoID: id, target: 'repo', body: remoteBody(res.newCommits, res.upstream) }
  return action
}

export interface AutoFetchDeps {
  repos: () => Repo[]
  fetch: (id: string) => Promise<AutoFetchResult>
  online: () => boolean
  selectedId: () => string
  busy: () => boolean
  refresh: () => Promise<void>
  notify: (e: NotifyEvent) => void
}

const defaultDeps: AutoFetchDeps = {
  repos: () => get(repos),
  fetch: (id) => api.autoFetch(id),
  online: () => navigator.onLine,
  selectedId: () => get(selectedRepoId),
  busy: () => get(busy) !== '',
  refresh: refreshRepo,
  notify: (e) => void notify(e),
}

/** runRound fetches each repository in turn; keepGoing is asked before
 *  each one, so Off or stop ends a round between repositories. */
export async function runRound(d: AutoFetchDeps, keepGoing: () => boolean = () => true): Promise<void> {
  if (!d.online()) return
  for (const repo of d.repos()) {
    if (!keepGoing()) return
    if (repo.missing) continue
    let r: { result?: AutoFetchResult; error?: unknown }
    try {
      r = { result: await d.fetch(repo.id) }
    } catch (error) {
      r = { error }
    }
    const a = outcome(repo.id, r, { selectedId: d.selectedId(), busy: d.busy() })
    if (a.event) d.notify(a.event)
    if (a.refresh) await d.refresh()
  }
}

/** startAutoFetch runs rounds: the first FIRST_ROUND_MS after start, each
 *  next one the chosen interval after the previous ends — so rounds never
 *  overlap, even when the interval changes mid-round. Off, or stop, ends a
 *  running round before its next repository (the fetch in flight finishes). */
export function startAutoFetch(over: Partial<AutoFetchDeps> = {}): () => void {
  const d = { ...defaultDeps, ...over }
  let timer: ReturnType<typeof setTimeout> | undefined
  let running = false
  let first = true
  let stopped = false
  const schedule = () => {
    clearTimeout(timer)
    timer = undefined
    const minutes = get(autoFetchMinutes)
    if (stopped || minutes <= 0 || running) return
    timer = setTimeout(round, first ? FIRST_ROUND_MS : minutes * 60_000)
  }
  const round = async () => {
    first = false
    running = true
    try {
      await runRound(d, () => !stopped && get(autoFetchMinutes) > 0)
    } finally {
      running = false
      schedule()
    }
  }
  const unsubscribe = autoFetchMinutes.subscribe(schedule)
  return () => {
    stopped = true
    unsubscribe()
    clearTimeout(timer)
  }
}
