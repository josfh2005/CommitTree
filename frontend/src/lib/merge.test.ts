import { describe, expect, it } from 'vitest'
import { abortWarning, commitWarning, conflictActions, conflictHeader, mergeSections } from './merge'
import type { MergeState } from './types'

const state = (over: Partial<MergeState> = {}): MergeState => ({
  kind: 'merge',
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

describe('conflictHeader', () => {
  it('names both sides of a merge', () => {
    expect(conflictHeader(state({ kind: 'merge' }))).toEqual({
      lead: 'Merging', from: 'feature', connector: 'into', into: 'main', detail: '',
    })
  })
  it('says onto for a rebase, with the commit counter', () => {
    const h = conflictHeader(state({ kind: 'rebase', step: 2, total: 5, subject: 'tidy up' }))
    expect(h.lead).toBe('Rebasing')
    expect(h.connector).toBe('onto')
    expect(h.detail).toBe('commit 2 of 5: tidy up')
  })
  it('names the picked commit for a cherry-pick', () => {
    const h = conflictHeader(state({ kind: 'cherry-pick', from: '5e6df51', subject: 'tidy up' }))
    expect(h.lead).toBe('Cherry-picking')
    expect(h.detail).toBe('tidy up')
  })
  it('has nothing to name for a stash conflict', () => {
    const h = conflictHeader(state({ kind: 'stash' }))
    expect(h).toEqual({ lead: 'Resolving stashed changes', from: '', connector: '', into: '', detail: '' })
  })
  it('says on for a revert, with the reverted commit as detail', () => {
    expect(conflictHeader(state({ kind: 'revert', from: '5e6df51', subject: 'tidy up' }))).toEqual({
      lead: 'Reverting', from: '5e6df51', connector: 'on', into: 'main', detail: 'tidy up',
    })
  })
  it('names the applied patch for git am, with no connector', () => {
    expect(conflictHeader(state({ kind: 'am', subject: 'tidy up' }))).toEqual({
      lead: 'Applying patch', from: 'feature', connector: '', into: 'main', detail: 'tidy up',
    })
  })
})

describe('conflictActions', () => {
  it('offers AI, abort and commit for a merge', () => {
    expect(conflictActions(state({ kind: 'merge' }))).toEqual({
      abort: 'Abort merge', confirm: 'Commit merge', ai: true, done: false,
    })
  })
  it('offers a rebase its own wording and no AI', () => {
    expect(conflictActions(state({ kind: 'rebase' }))).toEqual({
      abort: 'Abort rebase', confirm: 'Continue rebase', ai: false, done: false,
    })
  })
  it.each(['cherry-pick', 'revert', 'am'] as const)('gives %s a real abort and continue', (kind) => {
    const a = conflictActions(state({ kind }))
    expect(a.abort).toBeTruthy()
    expect(a.confirm).toBeTruthy()
    expect(a.ai).toBe(false)
  })
  it('gives a stash conflict Done instead of abort/continue', () => {
    expect(conflictActions(state({ kind: 'stash' }))).toEqual({
      abort: null, confirm: null, ai: false, done: true,
    })
  })
})

describe('abortWarning', () => {
  it('says rebase, not merge, for a rebase', () => {
    const w = abortWarning(state({ kind: 'rebase' }))
    expect(w.title).toBe('Abort rebase')
    expect(w.message).not.toMatch(/merge/)
    expect(w.confirmLabel).toBe('Abort rebase')
  })
})

describe('commitWarning', () => {
  it('is null for anything but a merge — nothing else writes a merge commit', () => {
    expect(commitWarning(state({ kind: 'rebase', unstaged: ['a.ts'] }))).toBeNull()
    expect(commitWarning(state({ kind: 'cherry-pick', unstaged: ['a.ts'] }))).toBeNull()
  })
})
