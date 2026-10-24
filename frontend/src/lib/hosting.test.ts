import { get } from 'svelte/store'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('./api', () => ({
  api: {
    hostingInfo: vi.fn(),
    commitStatuses: vi.fn(),
    pullRequestCount: vi.fn(),
  },
}))

import { api } from './api'
import {
  applyLimit, applyWatchEvent, commitStatuses, headStatusText, hostingInfo, hostingLimitText, hostingLimits, isHttps, pullCounts, refreshPullCount, refreshStatuses,
  requestHeadStatus, requestStatuses, startHostingPolling, stateIcon, statusTooltip,
} from './hosting'
import type { Check, CommitStatus, HostingInfo, HostState } from './types'

const info = (over: Partial<HostingInfo> = {}): HostingInfo => ({
  supported: true, host: 'bitbucket', owner: 'o', name: 'n', remote: 'origin', webURL: '', pullsURL: '',
  noun: 'Pipelines', account: 'connected', ...over,
})
const st = (hash: string, state: HostState, checks: Check[] | null = null): CommitStatus => ({ hash, state, checks })
const check = (over: Partial<Check> = {}): Check => ({
  key: 'k', name: 'build', state: 'success', url: '', started: '', durationSec: 0, created: '', ...over,
})
const deferred = <T>() => {
  let resolve!: (v: T) => void
  const promise = new Promise<T>((r) => (resolve = r))
  return { promise, resolve }
}

const statuses = vi.mocked(api.commitStatuses)
const infoCall = vi.mocked(api.hostingInfo)

beforeEach(() => {
  vi.useFakeTimers()
  statuses.mockReset()
  infoCall.mockReset()
  infoCall.mockResolvedValue(info())
  hostingInfo.set({})
  commitStatuses.set({})
  hostingLimits.set({})
  pullCounts.set({})
})
afterEach(() => {
  vi.useRealTimers()
})

describe('requestStatuses', () => {
  it('debounces: three calls within 500 ms make one API call with the union of hashes', async () => {
    statuses.mockResolvedValue({})
    requestStatuses('A', ['h1', 'h2'])
    await vi.advanceTimersByTimeAsync(300)
    requestStatuses('A', ['h2', 'h3'])
    await vi.advanceTimersByTimeAsync(300)
    requestStatuses('A', ['h4'])
    await vi.advanceTimersByTimeAsync(499)
    expect(statuses).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(1)
    expect(statuses).toHaveBeenCalledTimes(1)
    expect(statuses.mock.calls[0][0]).toBe('A')
    expect([...statuses.mock.calls[0][1]].sort()).toEqual(['h1', 'h2', 'h3', 'h4'])
  })

  it('late answer for another repo is not mixed in', async () => {
    const a = deferred<Record<string, CommitStatus>>()
    statuses.mockImplementation((id) => (id === 'A' ? a.promise : Promise.resolve({ b1: st('b1', 'success') })))
    requestStatuses('A', ['a1'])
    await vi.advanceTimersByTimeAsync(500)
    // The user switches to B while A is in flight.
    requestStatuses('B', ['b1'])
    await vi.advanceTimersByTimeAsync(500)
    a.resolve({ a1: st('a1', 'failed') })
    await vi.advanceTimersByTimeAsync(0)
    const all = get(commitStatuses)
    expect(Object.keys(all.B)).toEqual(['b1'])
    expect(all.B.a1).toBeUndefined()
    expect(Object.keys(all.A)).toEqual(['a1'])
  })

  it('asks nothing for a repository that is unsupported or has no connected account', async () => {
    infoCall.mockResolvedValue(info({ account: 'none' }))
    requestStatuses('A', ['h1'])
    await vi.advanceTimersByTimeAsync(500)
    expect(statuses).not.toHaveBeenCalled()
    infoCall.mockResolvedValue(info({ supported: false }))
    requestStatuses('C', ['h1'])
    await vi.advanceTimersByTimeAsync(500)
    expect(statuses).not.toHaveBeenCalled()
  })

  it('does not ask again for a commit whose state is final', async () => {
    commitStatuses.set({ A: { h1: st('h1', 'success'), h2: st('h2', 'running') } })
    statuses.mockResolvedValue({})
    requestStatuses('A', ['h1', 'h2', 'h3'])
    await vi.advanceTimersByTimeAsync(500)
    expect([...statuses.mock.calls[0][1]].sort()).toEqual(['h2', 'h3'])
  })
})

describe('refreshStatuses', () => {
  it('drops the unfinished entries of the repository and asks for the last visible set', async () => {
    statuses.mockResolvedValueOnce({ h1: st('h1', 'success'), h2: st('h2', 'running'), h3: st('h3', 'none') })
    requestStatuses('A', ['h1', 'h2', 'h3'])
    await vi.advanceTimersByTimeAsync(500)
    statuses.mockResolvedValueOnce({ h2: st('h2', 'success'), h3: st('h3', 'none') })
    refreshStatuses('A')
    expect(Object.keys(get(commitStatuses).A)).toEqual(['h1'])
    await vi.advanceTimersByTimeAsync(500)
    expect([...statuses.mock.calls[1][1]].sort()).toEqual(['h2', 'h3'])
    expect(get(commitStatuses).A.h2.state).toBe('success')
  })

  it('does nothing for a repository the log has not asked about', async () => {
    refreshStatuses('Z')
    await vi.advanceTimersByTimeAsync(1000)
    expect(infoCall).not.toHaveBeenCalled()
    expect(statuses).not.toHaveBeenCalled()
  })
})

describe('startHostingPolling', () => {
  it('re-asks only running', async () => {
    statuses.mockResolvedValueOnce({ h1: st('h1', 'running'), h2: st('h2', 'success'), h3: st('h3', 'failed') })
    requestStatuses('A', ['h1', 'h2', 'h3'])
    await vi.advanceTimersByTimeAsync(500)
    expect(statuses).toHaveBeenCalledTimes(1)
    statuses.mockResolvedValue({ h1: st('h1', 'success') })
    const stop = startHostingPolling()
    await vi.advanceTimersByTimeAsync(30_000)
    await vi.advanceTimersByTimeAsync(500)
    expect(statuses).toHaveBeenCalledTimes(2)
    expect(statuses.mock.calls[1]).toEqual(['A', ['h1']])
    // Nothing is running any more: the next cycle asks nothing.
    await vi.advanceTimersByTimeAsync(31_000)
    expect(statuses).toHaveBeenCalledTimes(2)
    stop()
  })
})

describe('events', () => {
  it('applyWatchEvent merges the state under the event repository', () => {
    commitStatuses.set({ A: { h1: st('h1', 'running', [check()]) } })
    applyWatchEvent({ repoID: 'A', branch: 'main', hash: 'h1', state: 'success', final: true, noun: 'Pipelines' })
    applyWatchEvent({ repoID: 'B', branch: 'main', hash: 'h9', state: 'failed', final: true, noun: 'Checks' })
    const all = get(commitStatuses)
    expect(all.A.h1.state).toBe('success')
    expect(all.B.h9.state).toBe('failed')
  })

  it('applyWatchEvent drops the cached checks when the state changes, and keeps them when it does not', () => {
    commitStatuses.set({ A: { h1: st('h1', 'running', [check()]), h2: st('h2', 'failed', [check()]) } })
    applyWatchEvent({ repoID: 'A', branch: 'main', hash: 'h1', state: 'failed', final: true, noun: 'Pipelines' })
    applyWatchEvent({ repoID: 'A', branch: 'main', hash: 'h2', state: 'failed', final: false, noun: 'Pipelines' })
    const all = get(commitStatuses)
    expect(all.A.h1).toEqual({ hash: 'h1', state: 'failed', checks: null })
    expect(all.A.h2.checks).toHaveLength(1)
  })

  it('applyLimit stores the limit by host and removes it when until is null', () => {
    applyLimit({ host: 'github', until: '2026-10-24T10:00:00Z', known: true })
    expect(get(hostingLimits).github.known).toBe(true)
    applyLimit({ host: 'github', until: null, known: false })
    expect(get(hostingLimits).github).toBeUndefined()
  })
})

describe('statusTooltip', () => {
  const now = new Date('2026-10-24T12:00:00Z')
  const ago = (min: number) => new Date(now.getTime() - min * 60_000).toISOString()

  it('one Bitbucket pipeline, success', () => {
    const s = st('h', 'success', [check({ name: 'Pipeline #412', durationSec: 180 })])
    expect(statusTooltip('Pipelines', s, now)).toBe('Pipeline #412 passed · 3 min')
  })

  it('one running', () => {
    const s = st('h', 'running', [check({ name: 'Pipeline #413', state: 'running', started: ago(2) })])
    expect(statusTooltip('Pipelines', s, now)).toBe('Pipeline running · started 2 min ago')
  })

  it('several checks', () => {
    const three = (a: HostState, b: HostState, c: HostState) =>
      [check({ key: '1', state: a }), check({ key: '2', state: b }), check({ key: '3', state: c })]
    expect(statusTooltip('Checks', st('h', 'failed', three('failed', 'success', 'failed')), now)).toBe('2 of 3 checks failed')
    expect(statusTooltip('Checks', st('h', 'success', three('success', 'success', 'success')), now)).toBe('3 checks passed')
    expect(statusTooltip('Checks', st('h', 'running', [check({ state: 'running' }), check({ key: '2' })]), now)).toBe('1 of 2 checks running')
  })

  it('draws nothing for none', () => {
    expect(statusTooltip('Checks', st('h', 'none'), now)).toBe('')
  })
})

describe('stateIcon', () => {
  it('none is null', () => {
    expect(stateIcon('none')).toBeNull()
  })
  it('the other states have an icon and a tone', () => {
    expect(stateIcon('success')?.tone).toBe('ok')
    expect(stateIcon('failed')?.tone).toBe('bad')
    expect(stateIcon('running')?.tone).toBe('busy')
    expect(stateIcon('stopped')?.tone).toBe('muted')
  })
})

describe('isHttps', () => {
  it('accepts https only', () => {
    expect(isHttps('https://bitbucket.org/o/n/pipelines/results/1')).toBe(true)
    expect(isHttps('http://example.com')).toBe(false)
    expect(isHttps('javascript:alert(1)')).toBe(false)
    expect(isHttps('')).toBe(false)
  })
})

describe('hostingLimitText', () => {
  it('says the time for a limit the host gave', () => {
    const until = new Date(2026, 9, 24, 9, 5).toISOString()
    expect(hostingLimitText({ host: 'github', until, known: true })).toBe('GitHub: request limit reached, retrying at 09:05')
  })

  it('says shortly for a guess', () => {
    const until = new Date(2026, 9, 24, 9, 5).toISOString()
    expect(hostingLimitText({ host: 'bitbucket', until, known: false })).toBe('Bitbucket: request limit reached, retrying shortly')
  })
})

describe('headStatusText', () => {
  it('uses Pipeline for Pipelines and Checks for Checks, nothing for none', () => {
    expect(headStatusText('Pipelines', 'success')).toBe('Pipeline passed')
    expect(headStatusText('Pipelines', 'failed')).toBe('Pipeline failed')
    expect(headStatusText('Pipelines', 'running')).toBe('Pipeline running')
    expect(headStatusText('Checks', 'failed')).toBe('Checks failed')
    expect(headStatusText('Checks', 'running')).toBe('Checks running')
    expect(headStatusText('Checks', 'none')).toBe('')
  })
})

describe('requestHeadStatus', () => {
  it('polls a running HEAD with no log ever asking, and follows the repository it was last asked for', async () => {
    statuses.mockResolvedValue({})
    requestStatuses('Old', ['o1'])
    commitStatuses.set({ Old: { o1: st('o1', 'running') }, New: { head: st('head', 'running') } })
    requestHeadStatus('New', 'head')
    await vi.advanceTimersByTimeAsync(500)
    statuses.mockClear()
    const stop = startHostingPolling()
    await vi.advanceTimersByTimeAsync(30_000 + 500)
    stop()
    expect(statuses).toHaveBeenCalledTimes(1)
    expect(statuses.mock.calls[0][0]).toBe('New')
    expect(statuses.mock.calls[0][1]).toEqual(['head'])
  })

  it('asks again for a final state that a watch event left without checks', async () => {
    statuses.mockResolvedValue({ head: st('head', 'success', [check()]) })
    commitStatuses.set({ A: { head: st('head', 'running', [check({ state: 'running' })]) } })
    applyWatchEvent({ repoID: 'A', branch: 'main', hash: 'head', state: 'success', final: true, noun: 'Pipelines' })
    requestHeadStatus('A', 'head')
    await vi.advanceTimersByTimeAsync(500)
    expect(statuses).toHaveBeenCalledTimes(1)
    expect(get(commitStatuses).A.head.checks).toHaveLength(1)
    requestHeadStatus('A', 'head')
    await vi.advanceTimersByTimeAsync(500)
    expect(statuses).toHaveBeenCalledTimes(1)
  })

  it('asks for HEAD without replacing the log set, and refresh asks for both', async () => {
    statuses.mockResolvedValue({})
    requestStatuses('A', ['h1', 'h2'])
    requestHeadStatus('A', 'head')
    await vi.advanceTimersByTimeAsync(500)
    expect(statuses.mock.calls[0][1].sort()).toEqual(['h1', 'h2', 'head'])
    statuses.mockClear()
    refreshStatuses('A')
    await vi.advanceTimersByTimeAsync(500)
    expect(statuses.mock.calls[0][1].sort()).toEqual(['h1', 'h2', 'head'])
  })

  it('polls HEAD while it runs, though it is not in the log set', async () => {
    commitStatuses.set({ A: { head: st('head', 'running') } })
    requestStatuses('A', ['h1'])
    requestHeadStatus('A', 'head')
    await vi.advanceTimersByTimeAsync(500)
    statuses.mockReset().mockResolvedValue({})
    const stop = startHostingPolling()
    await vi.advanceTimersByTimeAsync(30_000 + 500)
    stop()
    expect(statuses.mock.calls[0][1]).toEqual(['head'])
  })
})

describe('refreshPullCount', () => {
  const count = vi.mocked(api.pullRequestCount)
  beforeEach(() => {
    count.mockReset()
  })

  it('keeps the count under the repository it asked about', async () => {
    count.mockResolvedValue({ n: 7, more: false })
    await refreshPullCount('A')
    expect(get(pullCounts)).toEqual({ A: { n: 7, more: false } })
    expect(count).toHaveBeenCalledWith('A')
  })

  it('asks nothing and shows no badge without a connected account', async () => {
    pullCounts.set({ A: { n: 3, more: false } })
    infoCall.mockResolvedValue(info({ account: 'none' }))
    await refreshPullCount('A')
    expect(count).not.toHaveBeenCalled()
    expect(get(pullCounts)).toEqual({})
  })

  it('drops the count when the host cannot answer', async () => {
    pullCounts.set({ A: { n: 3, more: false } })
    count.mockImplementation(async () => {
      throw new Error('Bitbucket rejected the token')
    })
    await refreshPullCount('A')
    expect(get(pullCounts)).toEqual({})
  })

  it('a late answer for A does not touch B', async () => {
    const a = deferred<{ n: number; more: boolean }>()
    count.mockImplementation((id) => (id === 'A' ? a.promise : Promise.resolve({ n: 2, more: true })))
    const first = refreshPullCount('A')
    await refreshPullCount('B')
    a.resolve({ n: 9, more: false })
    await first
    expect(get(pullCounts)).toEqual({ A: { n: 9, more: false }, B: { n: 2, more: true } })
  })
})
