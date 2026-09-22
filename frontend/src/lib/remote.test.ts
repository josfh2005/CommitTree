import { describe, expect, it } from 'vitest'
import { canSync, conflictOwnsScreen } from './remote'
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

describe('canSync', () => {
  it('is true with nothing in progress and nothing busy', () => {
    expect(canSync(null, '')).toBe(true)
  })
  it('is false while busy', () => {
    expect(canSync(null, 'Committing…')).toBe(false)
  })
  it('is false during a merge, a rebase, or a stash conflict', () => {
    expect(canSync(merging('merge'), '')).toBe(false)
    expect(canSync(merging('rebase'), '')).toBe(false)
    expect(canSync(merging('stash'), '')).toBe(false)
  })
})
