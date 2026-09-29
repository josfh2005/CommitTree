import { describe, expect, it } from 'vitest'
import { isFilterRow, refsView } from './repoRefs'
import type { Refs, StashEntry } from './types'

const refsOf = (head: string): Refs => ({ head, headHash: 'h', detached: false, local: [], remotes: [], tags: [] }) as unknown as Refs
const entry = (index: number): StashEntry => ({ index }) as StashEntry

describe('refsView', () => {
  const selected = { refs: refsOf('develop'), stash: [entry(0)] }
  const side = { other: { refs: refsOf('main'), stash: [] } }

  it('shows the selected repository from the live stores', () => {
    expect(refsView('sel', 'sel', selected, side)).toEqual(selected)
  })

  it('shows any other expanded repository from its own cached refs, never the selected one\'s', () => {
    expect(refsView('other', 'sel', selected, side).refs?.head).toBe('main')
  })

  it('shows nothing for a repository whose refs have not loaded yet', () => {
    expect(refsView('unloaded', 'sel', selected, side)).toEqual({ refs: null, stash: [] })
  })
})

describe('isFilterRow', () => {
  it('marks the ref the selected repository\'s log is filtered by', () => {
    expect(isFilterRow('sel', 'sel', 'refs/heads/master', 'refs/heads/master')).toBe(true)
  })
  it('never marks a row of another repository, whose log is not on screen', () => {
    expect(isFilterRow('other', 'sel', 'refs/heads/master', 'refs/heads/master')).toBe(false)
  })
  it('marks nothing when the log is not filtered', () => {
    expect(isFilterRow('sel', 'sel', '', 'refs/heads/master')).toBe(false)
  })
})
