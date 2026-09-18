import { describe, expect, it } from 'vitest'
import { commitWarning, mergeSections } from './merge'
import type { MergeState } from './types'

const state = (over: Partial<MergeState> = {}): MergeState => ({
  merging: true,
  from: 'feature',
  into: 'main',
  conflicts: [],
  manual: [],
  staged: [],
  unstaged: [],
  ...over,
})

describe('mergeSections', () => {
  it('groups conflicts (manual last), unstaged and staged files', () => {
    const sections = mergeSections(state({ conflicts: ['b.ts'], manual: ['logo.png'], unstaged: ['c.ts'], staged: ['a.ts'] }))
    expect(sections).toEqual([
      { title: 'Conflicts', files: [{ path: 'b.ts', status: 'conflict' }, { path: 'logo.png', status: 'manual' }] },
      { title: 'Unstaged', files: [{ path: 'c.ts', status: 'unstaged' }] },
      { title: 'Staged', files: [{ path: 'a.ts', status: 'staged' }] },
    ])
  })

  it('leaves out empty sections', () => {
    expect(mergeSections(state({ staged: ['a.ts'] }))).toEqual([{ title: 'Staged', files: [{ path: 'a.ts', status: 'staged' }] }])
  })

  it('is empty when nothing is merging', () => {
    expect(mergeSections(state({ merging: false, staged: ['a.ts'] }))).toEqual([])
  })
})

describe('commitWarning', () => {
  it('is null when every settled file is staged', () => {
    expect(commitWarning(state({ staged: ['a.ts'] }))).toBeNull()
  })

  it('names a single unstaged file', () => {
    expect(commitWarning(state({ unstaged: ['greet.go'] }))).toMatch(/^greet\.go is not staged/)
  })

  it('counts several unstaged files', () => {
    expect(commitWarning(state({ unstaged: ['a.ts', 'b.ts'] }))).toMatch(/^2 files are not staged/)
  })
})
