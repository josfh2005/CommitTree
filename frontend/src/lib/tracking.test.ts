import { describe, expect, it } from 'vitest'
import { createdBranchMessage, nowTracksMessage, twinOf } from './tracking'
import type { Branch, Remote } from './types'

const rb = (remote: string, name: string): Branch => ({ name, remote, hash: 'h', current: false, upstream: '', official: false })
const remote = (name: string, ...branches: string[]): Remote => ({ name, branches: branches.map((b) => rb(name, b)) })

describe('twinOf', () => {
  it("is origin's branch of the same name when origin has it", () => {
    expect(twinOf('topic', [remote('upstream', 'topic'), remote('origin', 'topic')])).toBe('origin/topic')
  })

  it("is the only remote's branch when origin doesn't have it", () => {
    expect(twinOf('topic', [remote('origin', 'main'), remote('fork', 'topic')])).toBe('fork/topic')
  })

  it('is none when no remote has it, or several do and none is origin', () => {
    expect(twinOf('topic', [remote('origin', 'main')])).toBe('')
    expect(twinOf('topic', [remote('one', 'topic'), remote('two', 'topic')])).toBe('')
    expect(twinOf('topic', [])).toBe('')
  })
})

describe('createdBranchMessage', () => {
  it('says which remote branch a new branch tracks, or why it does not', () => {
    expect(createdBranchMessage('topic', { twin: 'origin/topic', tracked: true })).toBe('Created topic, tracking origin/topic.')
    expect(createdBranchMessage('topic', { twin: 'origin/topic', tracked: false }))
      .toBe('Created topic. origin/topic exists but has diverged from it, so topic has no upstream.')
  })

  it('says nothing when there is no remote branch of that name', () => {
    expect(createdBranchMessage('topic', { twin: '', tracked: false })).toBe('')
  })
})

describe('nowTracksMessage', () => {
  it('names the branch and its new upstream', () => {
    expect(nowTracksMessage('feature/x', 'origin/feature/x')).toBe('feature/x now tracks origin/feature/x.')
    expect(nowTracksMessage('', 'origin/feature/x')).toBe('Now tracking origin/feature/x.')
  })
})
