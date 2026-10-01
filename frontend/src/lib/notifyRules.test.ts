import { describe, expect, it } from 'vitest'
import { conflictsBody, decide, doneBody, failureBody, firstLine, formatSeconds, notificationId, statusText, type NotifyContext, type NotifyEvent } from './notifyRules'

const ctx = (over: Partial<NotifyContext> = {}): NotifyContext => ({
  enabled: true, done: true, ai: true, problem: true, focused: false, activeRepoID: 'a', chatOpen: true, ...over,
})
const ev = (over: Partial<NotifyEvent> = {}): NotifyEvent => ({ category: 'problem', repoID: 'a', body: 'x', target: 'repo', ...over })

describe('decide', () => {
  it('is silent when the master switch or the category is off', () => {
    expect(decide(ev(), ctx({ enabled: false }))).toBe('none')
    expect(decide(ev({ category: 'ai' }), ctx({ ai: false }))).toBe('none')
    expect(decide(ev({ category: 'done', durationMs: 20_000 }), ctx({ done: false }))).toBe('none')
    expect(decide(ev(), ctx({ problem: false }))).toBe('none')
  })

  it('drops successes under 10 s but never problems or ai events', () => {
    expect(decide(ev({ category: 'done', durationMs: 9_999 }), ctx())).toBe('none')
    expect(decide(ev({ category: 'done' }), ctx())).toBe('none')
    expect(decide(ev({ category: 'done', durationMs: 10_000 }), ctx())).toBe('system')
    expect(decide(ev({ category: 'problem', durationMs: 1 }), ctx())).toBe('system')
    expect(decide(ev({ category: 'ai', durationMs: 1 }), ctx())).toBe('system')
  })

  it('sends a system notification whenever the window is not focused', () => {
    expect(decide(ev({ repoID: 'a' }), ctx({ focused: false, activeRepoID: 'a' }))).toBe('system')
  })

  it('is silent when focused and in sight', () => {
    expect(decide(ev({ repoID: 'a' }), ctx({ focused: true }))).toBe('none')
    expect(decide(ev({ category: 'ai', target: 'chat' }), ctx({ focused: true, chatOpen: true }))).toBe('none')
  })

  it('toasts when focused but out of sight', () => {
    expect(decide(ev({ repoID: 'b' }), ctx({ focused: true }))).toBe('toast')
    expect(decide(ev({ category: 'ai', target: 'chat' }), ctx({ focused: true, chatOpen: false }))).toBe('toast')
  })
})

describe('texts', () => {
  it('failureBody keeps the first line, 120 chars', () => {
    const long = 'a'.repeat(200)
    expect(failureBody('push', `fatal: Authentication failed\nhint: more`)).toBe('Push failed: fatal: Authentication failed')
    expect(failureBody('pull', long)).toBe(`Pull failed: ${'a'.repeat(120)}`)
    expect(firstLine('\n\n  second line  \nthird')).toBe('second line')
  })

  it('formats the other bodies', () => {
    expect(formatSeconds(14_400)).toBe('14 s')
    expect(doneBody('push', 14_400)).toBe('Push finished · 14 s')
    expect(doneBody('submodules', 10_000)).toBe('Submodule update finished · 10 s')
    expect(conflictsBody('merge', 1)).toBe('Conflicts in 1 file after the merge')
    expect(conflictsBody('stash', 3)).toBe('Conflicts in 3 files after the stash')
  })

  it('system id is repo plus category', () => {
    expect(notificationId(ev({ repoID: 'r1', category: 'done' }))).toBe('r1:done')
    expect(notificationId(ev({ repoID: 'r2', category: 'done' }))).toBe('r2:done')
  })

  it('describes the permission status', () => {
    expect(statusText('allowed')).toBe('System notifications are allowed.')
    expect(statusText('not allowed')).toMatch(/not allowed yet/)
    expect(statusText('unavailable: no bundle')).toBe('System notifications are unavailable (no bundle); in-app toasts are used instead.')
    expect(statusText('')).toBe('')
  })
})
