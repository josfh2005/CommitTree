import { describe, expect, it } from 'vitest'
import { refsView } from './repoRefs'
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
