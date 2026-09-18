import { describe, expect, it } from 'vitest'
import { mergeFiles } from './merge'
import type { MergeState } from './types'

const state = (over: Partial<MergeState> = {}): MergeState => ({
  merging: true,
  from: 'feature',
  into: 'main',
  conflicts: [],
  manual: [],
  ...over,
})

describe('mergeFiles', () => {
  it('splits the files the merge started with into resolved and pending', () => {
    const rows = mergeFiles(state({ conflicts: ['b.ts'] }), ['a.ts', 'b.ts'])
    expect(rows).toEqual([
      { path: 'b.ts', status: 'conflict' },
      { path: 'a.ts', status: 'resolved' },
    ])
  })

  it('lists files with no markers as manual, after the conflicts', () => {
    const rows = mergeFiles(state({ conflicts: ['b.ts'], manual: ['logo.png'] }), ['b.ts', 'logo.png'])
    expect(rows).toEqual([
      { path: 'b.ts', status: 'conflict' },
      { path: 'logo.png', status: 'manual' },
    ])
  })

  // After a restart there is no record of how the merge began, so only what
  // git still reports can be shown.
  it('works without the starting list', () => {
    expect(mergeFiles(state({ conflicts: ['b.ts'] }), [])).toEqual([{ path: 'b.ts', status: 'conflict' }])
  })

  it('is empty when nothing is merging', () => {
    expect(mergeFiles(state({ merging: false }), ['a.ts'])).toEqual([])
  })
})
