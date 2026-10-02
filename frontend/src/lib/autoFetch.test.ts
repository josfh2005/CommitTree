import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { FIRST_ROUND_MS, outcome, pausedRepos, resumeAutoFetch, runRound, startAutoFetch, type AutoFetchDeps } from './autoFetch'
import { autoFetchMinutes } from './stores'
import type { AutoFetchResult, Repo } from './types'

const res = (over: Partial<AutoFetchResult> = {}): AutoFetchResult => ({
  skipped: false, branch: 'main', upstream: 'origin/main', newCommits: 0, refsChanged: false, ...over,
})
const repo = (id: string, missing = false) => ({ id, name: id, path: `/x/${id}`, missing, branch: 'main' }) as Repo
const sel = { selectedId: 'a', busy: false }

describe('outcome', () => {
  it('pauses on an auth error and ignores any other error', () => {
    expect(outcome('a', { error: new Error('auto-fetch auth: git fetch: could not read Username') }, sel)).toEqual({ pause: true, refresh: false })
    expect(outcome('a', { error: new Error('git fetch: timed out') }, sel)).toEqual({ pause: false, refresh: false })
    expect(outcome('a', { error: 'auto-fetch auth: x' }, sel)).toEqual({ pause: true, refresh: false })
  })

  it('refreshes the selected repository only when refs changed and nothing is busy', () => {
    expect(outcome('a', { result: res({ refsChanged: true }) }, sel).refresh).toBe(true)
    expect(outcome('b', { result: res({ refsChanged: true }) }, sel).refresh).toBe(false)
    expect(outcome('a', { result: res({ refsChanged: true }) }, { selectedId: 'a', busy: true }).refresh).toBe(false)
    expect(outcome('a', { result: res() }, sel).refresh).toBe(false)
  })

  it('turns new commits into a remote event', () => {
    expect(outcome('b', { result: res({ newCommits: 2, refsChanged: true }) }, sel).event).toEqual({
      category: 'remote', repoID: 'b', target: 'repo', body: '2 new commits on origin/main',
    })
    expect(outcome('b', { result: res({ skipped: true }) }, sel)).toEqual({ pause: false, refresh: false })
  })
})

function deps(over: Partial<AutoFetchDeps> = {}): AutoFetchDeps {
  return {
    repos: () => [repo('a'), repo('gone', true), repo('b')],
    fetch: vi.fn().mockResolvedValue(res()),
    online: () => true,
    selectedId: () => 'a',
    busy: () => false,
    refresh: vi.fn().mockResolvedValue(undefined),
    notify: vi.fn(),
    ...over,
  }
}

describe('runRound', () => {
  beforeEach(() => pausedRepos.clear())

  it('fetches present, unpaused repositories one at a time, in order', async () => {
    const order: string[] = []
    let inFlight = 0
    const fetch = vi.fn(async (id: string) => {
      inFlight++
      expect(inFlight).toBe(1)
      order.push(id)
      await Promise.resolve()
      inFlight--
      return res()
    })
    await runRound(deps({ fetch }))
    expect(order).toEqual(['a', 'b'])
  })

  it('skips the whole round offline', async () => {
    const d = deps({ online: () => false })
    await runRound(d)
    expect(d.fetch).not.toHaveBeenCalled()
  })

  it('pauses a repository on an auth failure until resumed', async () => {
    const d = deps({ fetch: vi.fn(async (id: string) => { if (id === 'a') throw new Error('auto-fetch auth: no'); return res() }) })
    await runRound(d)
    expect(pausedRepos.has('a')).toBe(true)
    await runRound(d)
    expect(vi.mocked(d.fetch).mock.calls.filter(([id]) => id === 'a')).toHaveLength(1)
    resumeAutoFetch('a')
    expect(pausedRepos.has('a')).toBe(false)
  })

  it('notifies and refreshes from the results', async () => {
    const d = deps({ fetch: vi.fn().mockResolvedValue(res({ newCommits: 1, refsChanged: true })) })
    await runRound(d)
    expect(d.notify).toHaveBeenCalledTimes(2)
    expect(d.refresh).toHaveBeenCalledTimes(1) // only the selected 'a'
  })
})

describe('startAutoFetch', () => {
  beforeEach(() => { vi.useFakeTimers(); pausedRepos.clear(); autoFetchMinutes.set(15) })
  afterEach(() => vi.useRealTimers())

  it('runs the first round 30 s after start, then every interval after a round ends', async () => {
    const d = deps()
    const stop = startAutoFetch(d)
    await vi.advanceTimersByTimeAsync(FIRST_ROUND_MS - 1)
    expect(d.fetch).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(1)
    expect(d.fetch).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(15 * 60_000)
    expect(d.fetch).toHaveBeenCalledTimes(4)
    stop()
  })

  it('stops when Off and never overlaps a running round when the interval changes', async () => {
    let release!: () => void
    const fetch = vi.fn(() => new Promise<AutoFetchResult>((r) => { release = () => r(res()) }))
    const d = deps({ repos: () => [repo('a')], fetch })
    const stop = startAutoFetch(d)
    await vi.advanceTimersByTimeAsync(FIRST_ROUND_MS)
    expect(fetch).toHaveBeenCalledTimes(1) // round in flight
    autoFetchMinutes.set(5)
    await vi.advanceTimersByTimeAsync(5 * 60_000)
    expect(fetch).toHaveBeenCalledTimes(1) // still the same round
    release()
    await vi.advanceTimersByTimeAsync(5 * 60_000)
    expect(fetch).toHaveBeenCalledTimes(2)
    release()
    autoFetchMinutes.set(0)
    await vi.advanceTimersByTimeAsync(60 * 60_000)
    expect(fetch).toHaveBeenCalledTimes(2)
    stop()
  })
})
