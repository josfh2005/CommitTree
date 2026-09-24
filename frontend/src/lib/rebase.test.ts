import { describe, expect, it } from 'vitest'
import { cherryPickBlocker, doneMessage, rebaseBlocker, rebaseMessage } from './rebase'
import { NOTHING_TO_APPLY, PICKED, REBASED, UP_TO_DATE } from './types'

const p = { commits: 3, merges: 0, published: 0, upstream: '' }
const free = { isHead: false, contained: false, detached: false, busy: false, kind: '' as const }

describe('rebaseMessage', () => {
  it('says how many commits are replayed', () => {
    expect(rebaseMessage('feature', 'main', p)).toBe('Rebase feature onto main? 3 commits will be replayed on top of main.')
  })
  it('warns about published commits and flattened merges', () => {
    const m = rebaseMessage('feature', 'main', { commits: 1, merges: 1, published: 1, upstream: 'origin/feature' })
    expect(m).toContain('1 commit will be replayed')
    expect(m).toContain("1 of these commits is already on origin/feature. After rebasing you'll need to force-push, which git-ui doesn't do.")
    expect(m).toContain('1 merge commit in this range will be flattened.')
  })
})

describe('rebaseBlocker', () => {
  it('allows a free rebase', () => expect(rebaseBlocker(free, 'feature', 'main')).toBeNull())
  it('explains each refusal', () => {
    expect(rebaseBlocker({ ...free, isHead: true }, 'feature', 'feature')).toBe('This is the current branch')
    expect(rebaseBlocker({ ...free, contained: true }, 'feature', 'main')).toBe('feature already contains main')
    expect(rebaseBlocker({ ...free, detached: true }, '', 'main')).toBe('No branch is checked out')
    expect(rebaseBlocker({ ...free, busy: true }, 'feature', 'main')).toBe('Another operation is running')
    expect(rebaseBlocker({ ...free, kind: 'merge' }, 'feature', 'main')).toBe('Finish the merge in progress first')
  })
})

describe('cherryPickBlocker', () => {
  const c = { contained: false, isMerge: false, detached: false, busy: false, kind: '' as const }
  it('allows a free pick', () => expect(cherryPickBlocker(c)).toBeNull())
  it('explains each refusal', () => {
    expect(cherryPickBlocker({ ...c, contained: true })).toBe('Already on this branch')
    expect(cherryPickBlocker({ ...c, isMerge: true })).toBe("Cherry-picking a merge commit isn't supported")
    expect(cherryPickBlocker({ ...c, kind: 'rebase' })).toBe('Finish the rebase in progress first')
  })
})

describe('doneMessage', () => {
  it('words each outcome', () => {
    expect(doneMessage(REBASED, { op: 'rebase', head: 'feature', target: 'main', commits: 3 })).toBe('Rebased feature onto main — 3 commits')
    expect(doneMessage(REBASED, { op: 'rebase', head: 'feature', target: 'main', commits: 0 })).toBe('Rebased feature onto main')
    expect(doneMessage(UP_TO_DATE, { op: 'rebase', head: 'feature', target: 'main' })).toBe('Already up to date')
    expect(doneMessage(PICKED, { op: 'cherry-pick', head: 'feature', target: 'a1b2c3' })).toBe('Cherry-picked a1b2c3 onto feature')
    expect(doneMessage(NOTHING_TO_APPLY, { op: 'cherry-pick', head: 'feature', target: 'a1b2c3' })).toBe('Nothing to apply: those changes are already on feature')
  })
})
