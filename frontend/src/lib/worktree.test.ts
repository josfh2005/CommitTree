import { describe, expect, it } from 'vitest'
import { changedCount, discardMessage, hasStagedChanges, nextSelection, worktreeSections } from './worktree'
import type { WorktreeState } from './types'

const state = (over: Partial<WorktreeState> = {}): WorktreeState => ({
  staged: [],
  unstaged: [],
  untracked: [],
  merging: false,
  ...over,
})

describe('worktreeSections', () => {
  it('groups staged, unstaged and untracked in that order', () => {
    const sections = worktreeSections(
      state({
        staged: [{ path: 'a.ts', status: 'M' }],
        unstaged: [{ path: 'b.ts', status: 'M' }],
        untracked: [{ path: 'c.ts', status: '?' }],
      }),
    )
    expect(sections.map((s) => s.title)).toEqual(['Staged', 'Unstaged', 'Untracked'])
  })

  it('leaves out empty sections', () => {
    expect(worktreeSections(state({ staged: [{ path: 'a.ts', status: 'M' }] })).map((s) => s.title)).toEqual(['Staged'])
  })

  it('is empty when nothing changed', () => {
    expect(worktreeSections(state())).toEqual([])
  })

  it('returns the same FileStatus objects the state holds, not copies — a row lookup by identity depends on this', () => {
    const staged = [{ path: 'a.ts', status: 'M' }]
    const unstaged = [{ path: 'a.ts', status: 'M' }]
    const s = state({ staged, unstaged })
    const sections = worktreeSections(s)
    expect(sections.find((sec) => sec.title === 'Staged')!.files[0]).toBe(staged[0])
    expect(sections.find((sec) => sec.title === 'Unstaged')!.files[0]).toBe(unstaged[0])
  })
})

describe('changedCount', () => {
  it('counts a file that is both staged and modified once', () => {
    const s = state({ staged: [{ path: 'a.ts', status: 'M' }], unstaged: [{ path: 'a.ts', status: 'M' }] })
    expect(changedCount(s)).toBe(1)
  })

  it('is zero with no state', () => {
    expect(changedCount(null)).toBe(0)
  })
})

describe('discardMessage', () => {
  it('warns that an untracked file is deleted for good', () => {
    const m = discardMessage({ path: 'notes.txt', status: '?' }, false)
    expect(m).toContain('notes.txt')
    expect(m).toContain('delete')
    expect(m).toMatch(/cannot be undone|unrecoverable/i)
  })

  it('says a staged change is thrown away too', () => {
    expect(discardMessage({ path: 'a.ts', status: 'M' }, true)).toContain('staged')
  })

  it('an untracked file stays "deleted for good" even when the staged flag is true', () => {
    const m = discardMessage({ path: 'notes.txt', status: '?' }, true)
    expect(m).toContain('notes.txt')
    expect(m).toMatch(/cannot be undone|unrecoverable/i)
    expect(m).not.toContain('staged')
  })
})

describe('hasStagedChanges', () => {
  it('is true for a path staged elsewhere, whichever row asks — a partially staged file lists the same path in both sections', () => {
    const s = state({ staged: [{ path: 'a.ts', status: 'M' }], unstaged: [{ path: 'a.ts', status: 'M' }] })
    expect(hasStagedChanges(s, 'a.ts')).toBe(true)
  })

  it('is false for a path with no staged counterpart', () => {
    const s = state({ unstaged: [{ path: 'b.ts', status: 'M' }] })
    expect(hasStagedChanges(s, 'b.ts')).toBe(false)
  })

  it('warns about staged changes for a path present in both lists, whichever row it was called from', () => {
    const s = state({ staged: [{ path: 'a.ts', status: 'M' }], unstaged: [{ path: 'a.ts', status: 'M' }] })
    // The Unstaged row's own FileStatus object is what's passed to discardMessage,
    // but the "staged" flag must come from hasStagedChanges(s, path), not the row.
    const unstagedRow = s.unstaged[0]
    const m = discardMessage(unstagedRow, hasStagedChanges(s, unstagedRow.path))
    expect(m).toContain('staged')
  })
})

describe('nextSelection', () => {
  it('keeps the selection on the same path when it moves to a new section', () => {
    const previous = { section: 'Unstaged', path: 'a.ts' }
    const sections = [{ title: 'Staged', files: [{ path: 'a.ts' }] }]
    expect(nextSelection(previous, sections)).toEqual({ section: 'Staged', path: 'a.ts' })
  })

  it('falls back to the first row when the previous path is gone', () => {
    const previous = { section: 'Unstaged', path: 'gone.ts' }
    const sections = [{ title: 'Staged', files: [{ path: 'x.ts' }] }]
    expect(nextSelection(previous, sections)).toEqual({ section: 'Staged', path: 'x.ts' })
  })

  it('returns null when every section is empty', () => {
    expect(nextSelection({ section: 'Staged', path: 'a.ts' }, [])).toBeNull()
  })

  it('keeps the row the user actually had selected when the path is in two sections at once', () => {
    const previous = { section: 'Unstaged', path: 'a.ts' }
    const sections = [
      { title: 'Staged', files: [{ path: 'a.ts' }] },
      { title: 'Unstaged', files: [{ path: 'a.ts' }] },
    ]
    expect(nextSelection(previous, sections)).toEqual({ section: 'Unstaged', path: 'a.ts' })
  })

  it('picks the first row when nothing was previously selected', () => {
    const sections = [{ title: 'Staged', files: [{ path: 'a.ts' }] }]
    expect(nextSelection(null, sections)).toEqual({ section: 'Staged', path: 'a.ts' })
  })
})
