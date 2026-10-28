import { describe, expect, it } from 'vitest'
import { failedMessage, othersAhead, PartialPushError, pushedMessage, pushResultRows, pushTitle, trackTitle } from './push'
import type { Branch, BranchPushResult, Refs } from './types'

const b = (name: string, over: Partial<Branch> = {}): Branch => ({ name, remote: '', hash: 'h', current: false, upstream: `origin/${name}`, official: true, ...over })
const refs = (local: Branch[], over: Partial<Refs> = {}): Refs => ({ head: 'main', headHash: 'h', detached: false, local, remotes: [], tags: [], ...over })
const res = (branch: string, status: BranchPushResult['status'], reason?: string): BranchPushResult => ({ branch, target: `origin/${branch}`, status, ...(reason ? { reason } : {}) })

describe('othersAhead', () => {
  const all = refs([
    b('main', { current: true, ahead: 2 }),
    b('feature', { ahead: 1 }),
    b('topic', { ahead: 1, official: false }),
    b('quiet'),
    b('behind', { behind: 3 }),
    b('gone', { ahead: 1, upstreamGone: true }),
    b('child', { ahead: 1, upstream: 'main', upstreamLocal: true }),
    b('loose', { upstream: '', ahead: 0 }),
  ])
  it('keeps only other branches ahead of a live remote upstream', () => {
    expect(othersAhead(all).map((x) => x.name)).toEqual(['feature'])
  })
  it('leaves out a branch that is not official', () => {
    expect(othersAhead(all).some((x) => x.name === 'topic')).toBe(false)
  })
  it('leaves out a branch tracking a local branch', () => {
    expect(othersAhead(all).some((x) => x.name === 'child')).toBe(false)
  })
})

describe('result summaries', () => {
  it('sums up a push in which nothing failed', () => {
    expect(pushedMessage([])).toBe('Nothing to push')
    expect(pushedMessage([res('main', 'pushed')])).toBe('Pushed main')
    expect(pushedMessage([res('main', 'pushed'), res('a', 'pushed')])).toBe('Pushed 2 branches')
    expect(pushedMessage([res('main', 'upToDate'), res('a', 'pushed'), res('b', 'pushed')])).toBe('Pushed 2 branches, 1 already up to date')
    expect(pushedMessage([res('main', 'upToDate')])).toBe('Everything up to date')
  })
  it('reads a push of tags only, and tags after the branches', () => {
    const tags = (status: BranchPushResult['status'], reason?: string): BranchPushResult => ({ branch: 'Tags', target: 'origin', tags: true, status, ...(reason ? { reason } : {}) })
    expect(pushedMessage([tags('pushed')])).toBe('Pushed tags')
    expect(pushedMessage([tags('upToDate')])).toBe('Everything up to date')
    expect(pushedMessage([res('feature/x', 'pushed'), tags('pushed')])).toBe('Pushed feature/x and tags')
    expect(pushedMessage([res('a', 'pushed'), res('b', 'pushed'), res('c', 'upToDate'), tags('pushed')])).toBe('Pushed 2 branches and tags, 1 already up to date')
    expect(pushedMessage([res('a', 'upToDate'), tags('pushed')])).toBe('Pushed tags, 1 already up to date')
    expect(pushedMessage([res('a', 'upToDate'), tags('upToDate')])).toBe('Everything up to date')
  })
  it('counts only branches when tags are in the report', () => {
    const tags: BranchPushResult = { branch: 'Tags', target: 'origin', tags: true, status: 'rejected', reason: 'x' }
    expect(failedMessage([res('a', 'rejected', 'pull'), res('b', 'pushed'), tags])).toBe('1 of 2 branches were not pushed')
    expect(failedMessage([res('a', 'pushed'), tags])).toBe('Tags were not pushed')
  })
  it('names the remote on the tags row', () => {
    const tags: BranchPushResult = { branch: 'Tags', target: 'origin', tags: true, status: 'pushed' }
    expect(pushResultRows([tags])).toEqual([{ mark: '✓', label: 'Tags → origin', detail: 'Pushed', tone: 'ok' }])
  })
  it('counts the branches that were not pushed', () => {
    const r = [res('main', 'rejected', 'pull'), res('a', 'pushed'), res('b', 'failed', 'auth')]
    expect(failedMessage(r)).toBe('2 of 3 branches were not pushed')
    const e = new PartialPushError(r)
    expect(e.message).toBe('2 of 3 branches were not pushed')
    expect(e.results).toBe(r)
    expect(e).toBeInstanceOf(Error)
  })
  it('makes one dialog row per branch, in order', () => {
    expect(pushResultRows([res('main', 'rejected', 'The remote has commits you don\'t have — pull main first'), res('a', 'pushed'), res('b', 'upToDate')])).toEqual([
      { mark: '✗', label: 'main → origin/main', detail: "The remote has commits you don't have — pull main first", tone: 'error' },
      { mark: '✓', label: 'a → origin/a', detail: 'Pushed', tone: 'ok' },
      { mark: '—', label: 'b → origin/b', detail: 'Up to date', tone: 'muted' },
    ])
  })
})

describe('tooltips', () => {
  it('says the Push button opens the push window', () => {
    expect(pushTitle(refs([b('main', { current: true })]))).toBe('Push — choose branches to push')
    expect(pushTitle(null)).toBe('Push — choose branches to push')
  })
  it('explains the badges, leaving out a zero part', () => {
    expect(trackTitle(b('main', { ahead: 2, behind: 1 }))).toBe('2 commits to push to origin/main · 1 commit to pull, as of the last fetch')
    expect(trackTitle(b('main', { ahead: 1 }))).toBe('1 commit to push to origin/main, as of the last fetch')
    expect(trackTitle(b('main', { behind: 3 }))).toBe('3 commits to pull, as of the last fetch')
    expect(trackTitle(b('main'))).toBe('')
    expect(trackTitle(b('main', { remote: 'origin', ahead: 2 }))).toBe('')
  })
})
