import { get } from 'svelte/store'
import { api } from './api'
import { notify } from './notify'
import { remoteBody, type NotifyEvent } from './notifyRules'
import { autoFetchMinutes, busy, refreshRepo, repos, selectedRepoId } from './stores'
import type { AutoFetchResult, Repo } from './types'
import { errorMessage } from './ui'

/** Background fetch (docs/spec/05-remote-and-stash.md): rounds over every
 *  repository, one at a time; what a result means is `outcome`'s, pure. */

export const FIRST_ROUND_MS = 30_000

export interface AutoFetchAction {
  pause: boolean
  refresh: boolean
  event?: NotifyEvent
}

const isAuth = (e: unknown) => (typeof e === 'string' ? e : errorMessage(e)).includes('auto-fetch auth')

export function outcome(
  id: string,
  r: { result?: AutoFetchResult; error?: unknown },
  s: { selectedId: string; busy: boolean },
): AutoFetchAction {
  if (r.error !== undefined) return { pause: isAuth(r.error), refresh: false }
  const res = r.result
  if (!res || res.skipped) return { pause: false, refresh: false }
  const action: AutoFetchAction = { pause: false, refresh: res.refsChanged && id === s.selectedId && !s.busy }
  if (res.newCommits > 0) action.event = { category: 'remote', repoID: id, target: 'repo', body: remoteBody(res.newCommits, res.upstream) }
  return action
}

/** Repositories whose background fetch failed for want of credentials;
 *  a manual Fetch or Pull that succeeds takes one out. In memory only. */
export const pausedRepos = new Set<string>()
export const resumeAutoFetch = (id: string) => void pausedRepos.delete(id)

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

export async function runRound(d: AutoFetchDeps): Promise<void> {
  if (!d.online()) return
  for (const repo of d.repos()) {
    if (repo.missing || pausedRepos.has(repo.id)) continue
    let r: { result?: AutoFetchResult; error?: unknown }
    try {
      r = { result: await d.fetch(repo.id) }
    } catch (error) {
      r = { error }
    }
    const a = outcome(repo.id, r, { selectedId: d.selectedId(), busy: d.busy() })
    if (a.pause) pausedRepos.add(repo.id)
    if (a.event) d.notify(a.event)
    if (a.refresh) await d.refresh()
  }
}

/** startAutoFetch runs rounds: the first FIRST_ROUND_MS after start, each
 *  next one the chosen interval after the previous ends — so rounds never
 *  overlap, even when the interval changes mid-round. Off stops it. */
export function startAutoFetch(over: Partial<AutoFetchDeps> = {}): () => void {
  const d = { ...defaultDeps, ...over }
  let timer: ReturnType<typeof setTimeout> | undefined
  let running = false
  let first = true
  const schedule = () => {
    clearTimeout(timer)
    timer = undefined
    const minutes = get(autoFetchMinutes)
    if (minutes <= 0 || running) return
    timer = setTimeout(round, first ? FIRST_ROUND_MS : minutes * 60_000)
  }
  const round = async () => {
    first = false
    running = true
    try {
      await runRound(d)
    } finally {
      running = false
      schedule()
    }
  }
  const unsubscribe = autoFetchMinutes.subscribe(schedule)
  return () => {
    unsubscribe()
    clearTimeout(timer)
  }
}
