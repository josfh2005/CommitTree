import { describe, expect, it } from 'vitest'
import { isCurrentBranchRef } from './refBadge'
import type { Ref } from './types'

const ref = (kind: Ref['kind'], name: string): Ref => ({ kind, name })

describe('isCurrentBranchRef', () => {
  it('matches the local branch checked out as HEAD', () => {
    expect(isCurrentBranchRef(ref('local', 'main'), true, 'main')).toBe(true)
  })

  it('does not match a different local branch on the same HEAD row', () => {
    expect(isCurrentBranchRef(ref('local', 'other'), true, 'main')).toBe(false)
  })

  it('does not match when the row is not HEAD', () => {
    expect(isCurrentBranchRef(ref('local', 'main'), false, 'main')).toBe(false)
  })

  it('never matches a remote-tracking branch or a tag', () => {
    expect(isCurrentBranchRef(ref('remote', 'main'), true, 'main')).toBe(false)
    expect(isCurrentBranchRef(ref('tag', 'main'), true, 'main')).toBe(false)
  })

  it('does not match on a detached HEAD (no current branch)', () => {
    expect(isCurrentBranchRef(ref('local', 'main'), true, '')).toBe(false)
  })
})
