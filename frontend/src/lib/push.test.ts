import { describe, expect, it } from 'vitest'
import { failedMessage, othersAhead, PartialPushError, pushChoiceOptions, pushCount, pushDecision, pushedMessage, pushResultRows, pushTitle, trackTitle } from './push'
import type { Branch, BranchPushResult, Refs } from './types'

const b = (name: string, over: Partial<Branch> = {}): Branch => ({ name, remote: '', hash: 'h', current: false, upstream: `origin/${name}`, ...over })
const refs = (local: Branch[], over: Partial<Refs> = {}): Refs => ({ head: 'main', headHash: 'h', detached: false, local, remotes: [], tags: [], ...over })
const res = (branch: string, status: BranchPushResult['status'], reason?: string): BranchPushResult => ({ branch, target: `origin/${branch}`, status, ...(reason ? { reason } : {}) })

describe('othersAhead / pushCount', () => {
  const all = refs([
    b('main', { current: true, ahead: 2 }),
    b('feature', { ahead: 1 }),
    b('quiet'),
    b('behind', { behind: 3 }),
    b('gone', { ahead: 1, upstreamGone: true }),
    b('child', { ahead: 1, upstream: 'main', upstreamLocal: true }),
    b('loose', { upstream: '', ahead: 0 }),
  ])
  it('keeps only other branches ahead of a live remote upstream', () => {
    expect(othersAhead(all).map((x) => x.name)).toEqual(['feature'])
  })
  it('leaves out a branch tracking a local branch', () => {
    expect(othersAhead(all).some((x) => x.name === 'child')).toBe(false)
  })
  it('counts the current branch plus the others', () => {
    expect(pushCount(all)).toBe(2)
    expect(pushCount(refs([b('feature', { ahead: 1 })], { detached: true, head: '' }))).toBe(1)
    expect(pushCount(refs([b('main', { current: true, upstream: 'dev', upstreamLocal: true }), b('feature', { ahead: 1 })]))).toBe(1)
    expect(pushCount(null)).toBe(0)
  })
})

describe('pushDecision', () => {
  const lone = refs([b('main', { current: true, ahead: 1 })])
  const more = refs([b('main', { current: true }), b('feature', { ahead: 1 })])
  it('follows a fixed setting', () => {
    expect(pushDecision('current', more)).toBe('current')
    expect(pushDecision('all', lone)).toBe('all')
  })
  it('asks only when another branch has commits to push', () => {
    expect(pushDecision('ask', lone)).toBe('current')
    expect(pushDecision('ask', more)).toBe('ask')
    expect(pushDecision('ask', null)).toBe('current')
  })
})

describe('pushChoiceOptions', () => {
  it('offers the current branch first and names the count', () => {
    const o = pushChoiceOptions('alpha', refs([b('main', { current: true }), b('a', { ahead: 1 }), b('c', { ahead: 4 })]))
    expect(o.title).toBe('Push alpha')
    expect(o.label).toBe('Push')
    expect(o.options).toEqual([{ value: 'current', label: 'Current branch (main)' }, { value: 'all', label: 'All branches (3)' }])
    expect(o.value).toBe('current')
    expect(o.message('current')).toBe('Change the default in Settings → General.')
    expect(o.confirmLabel('current')).toBe('Push')
    expect(o.confirmLabel('all')).toBe('Push 3 branches')
  })
  it('offers only all branches with a detached HEAD', () => {
    const o = pushChoiceOptions('alpha', refs([b('a', { ahead: 1 })], { detached: true, head: '' }))
    expect(o.options).toEqual([{ value: 'all', label: 'All branches (1)' }])
    expect(o.value).toBe('all')
    expect(o.confirmLabel('all')).toBe('Push 1 branch')
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
  it('says what a click on Push does', () => {
    const r = refs([b('main', { current: true })])
    expect(pushTitle('current', r)).toBe('Push main')
    expect(pushTitle('current', refs([], { detached: true, head: '' }))).toBe('Push')
    expect(pushTitle('all', r)).toBe('Push all branches')
    expect(pushTitle('ask', r)).toBe('Push — asks current or all branches')
  })
  it('explains the badges, leaving out a zero part', () => {
    expect(trackTitle(b('main', { ahead: 2, behind: 1 }))).toBe('2 commits to push to origin/main · 1 commit to pull, as of the last fetch')
    expect(trackTitle(b('main', { ahead: 1 }))).toBe('1 commit to push to origin/main, as of the last fetch')
    expect(trackTitle(b('main', { behind: 3 }))).toBe('3 commits to pull, as of the last fetch')
    expect(trackTitle(b('main'))).toBe('')
    expect(trackTitle(b('main', { remote: 'origin', ahead: 2 }))).toBe('')
  })
})
