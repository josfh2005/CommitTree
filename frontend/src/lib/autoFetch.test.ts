import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { FIRST_ROUND_MS, outcome, runRound, startAutoFetch, type AutoFetchDeps } from './autoFetch'
import { autoFetchMinutes } from './stores'
import type { AutoFetchResult, Repo } from './types'

const res = (over: Partial<AutoFetchResult> = {}): AutoFetchResult => ({
  skipped: false, branch: 'main', upstream: 'origin/main', newCommits: 0, refsChanged: false, authFailed: null, ...over,
})
const repo = (id: string, missing = false) => ({ id, name: id, path: `/x/${id}`, missing, branch: 'main' }) as Repo
const sel = { selectedId: 'a', busy: false }

describe('outcome', () => {
  it('does nothing on an error', () => {
    expect(outcome('a', { error: new Error('git fetch: timed out') }, sel)).toEqual({ refresh: false })
    expect(outcome('a', { error: 'anything' }, sel)).toEqual({ refresh: false })
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
    expect(outcome('b', { result: res({ skipped: true }) }, sel)).toEqual({ refresh: false })
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

  it('stops before the next repository once keepGoing turns false', async () => {
    let go = true
    const fetch = vi.fn(async () => { go = false; return res() })
    await runRound(deps({ fetch }), () => go)
    expect(fetch).toHaveBeenCalledTimes(1)
  })

  it('notifies and refreshes from the results', async () => {
    const d = deps({ fetch: vi.fn().mockResolvedValue(res({ newCommits: 1, refsChanged: true })) })
    await runRound(d)
    expect(d.notify).toHaveBeenCalledTimes(2)
    expect(d.refresh).toHaveBeenCalledTimes(1) // only the selected 'a'
  })
})

describe('startAutoFetch', () => {
  beforeEach(() => { vi.useFakeTimers(); autoFetchMinutes.set(15) })
  afterEach(() => {
    vi.useRealTimers()
  })

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

  it('Off or stop mid-round ends the round at the next repository', async () => {
    let release!: () => void
    const fetch = vi.fn(() => new Promise<AutoFetchResult>((r) => { release = () => r(res()) }))
    const d = deps({ fetch }) // repositories a, gone (missing), b
    const stop = startAutoFetch(d)
    await vi.advanceTimersByTimeAsync(FIRST_ROUND_MS)
    expect(fetch).toHaveBeenCalledTimes(1) // a in flight
    autoFetchMinutes.set(0)
    release()
    await vi.advanceTimersByTimeAsync(0)
    expect(fetch).toHaveBeenCalledTimes(1) // b never fetched

    autoFetchMinutes.set(15)
    const d2 = deps({ fetch: vi.fn(() => new Promise<AutoFetchResult>((r) => { release = () => r(res()) })) })
    const stop2 = startAutoFetch(d2)
    await vi.advanceTimersByTimeAsync(FIRST_ROUND_MS)
    stop2()
    release()
    await vi.advanceTimersByTimeAsync(0)
    expect(d2.fetch).toHaveBeenCalledTimes(1)
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
