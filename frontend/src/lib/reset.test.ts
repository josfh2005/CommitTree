import { describe, expect, it } from 'vitest'
import { resetMessage } from './reset'
import { takeMessage } from './merge'

const none = { undone: 0, gained: 0, pushed: 0, upstream: '' }

describe('resetMessage', () => {
  it('soft says the changes stay staged', () => {
    const m = resetMessage('soft', 'main', 'abc1234', { ...none, undone: 2 })
    expect(m).toContain('Move main to abc1234?')
    expect(m).toContain('2 commits are undone')
    expect(m).toContain('stay staged')
  })

  it('mixed says the changes stay in the working tree, unstaged', () => {
    expect(resetMessage('mixed', 'main', 'abc1234', { ...none, undone: 1 })).toContain('1 commit is undone; its changes stay in the working tree, unstaged')
  })

  it('hard warns that uncommitted work is discarded and mentions the reflog', () => {
    const m = resetMessage('hard', 'main', 'abc1234', { ...none, undone: 2 })
    expect(m).toContain('discard every uncommitted change')
    expect(m).toContain('reflog')
  })

  it('warns about a force push when undone commits are on the upstream', () => {
    const m = resetMessage('soft', 'main', 'abc1234', { undone: 3, gained: 0, pushed: 2, upstream: 'origin/main' })
    expect(m).toContain('2 of them are already on origin/main')
    expect(m).toContain('force push')
  })

  it('says nothing is undone when the target is not behind HEAD', () => {
    expect(resetMessage('soft', 'main', 'abc1234', none)).toContain('No commits are undone')
  })

  it('says when the branch also gains commits from another line of history', () => {
    const m = resetMessage('soft', 'main', 'abc1234', { ...none, undone: 2, gained: 1 })
    expect(m).toContain('main also gains 1 commit it does not have now')
  })
})

describe('takeMessage', () => {
  it('warns that the file is replaced and hand edits are lost', () => {
    const m = takeMessage('deps.lock', 'feature')
    expect(m).toContain("Replace deps.lock with feature's version")
    expect(m).toContain('Edits you made to it are lost')
  })
})
