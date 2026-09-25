import { describe, expect, it } from 'vitest'
import { conflictOwnsScreen } from './remote'
import type { MergeState } from './types'

const merging = (kind: MergeState['kind']): MergeState => ({
  kind,
  merging: true,
  from: 'a',
  into: 'b',
  conflicts: [],
  manual: [],
  staged: [],
  unstaged: [],
})

describe('conflictOwnsScreen', () => {
  it('is true for any unresolved conflict', () => {
    expect(conflictOwnsScreen(merging('merge'), false)).toBe(true)
    expect(conflictOwnsScreen(merging('rebase'), true)).toBe(true) // a dismissal is stash-only
    expect(conflictOwnsScreen(merging('stash'), false)).toBe(true)
  })
  it('is false for a dismissed stash conflict, and with nothing in progress', () => {
    expect(conflictOwnsScreen(merging('stash'), true)).toBe(false)
    expect(conflictOwnsScreen(null, false)).toBe(false)
  })
})
