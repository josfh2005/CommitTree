import { describe, expect, it } from 'vitest'
import { amendWarning, canCommit, changedCount, discardMessage, hasStagedChanges, nextSelection, shouldAutoGenerate, worktreeSections } from './worktree'
import type { CommitInfo, WorktreeState } from './types'

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

const info = (over: Partial<CommitInfo> = {}): CommitInfo => ({
  stagedCount: 1,
  canAmend: true,
  lastMessage: 'previous',
  pushed: false,
  upstream: '',
  ...over,
})

describe('canCommit', () => {
  it('needs a message and something staged', () => {
    expect(canCommit(info(), 'a message', false)).toBe(true)
    expect(canCommit(info(), '   ', false)).toBe(false)
    expect(canCommit(info({ stagedCount: 0 }), 'a message', false)).toBe(false)
  })

  it('allows an amend with nothing staged, which only rewrites the message', () => {
    expect(canCommit(info({ stagedCount: 0 }), 'better subject', true)).toBe(true)
  })

  it('is false without a preview', () => {
    expect(canCommit(null, 'a message', false)).toBe(false)
  })
})

describe('shouldAutoGenerate', () => {
  it('generates for a local provider in auto-local', () => {
    expect(shouldAutoGenerate('auto-local', 'ollama', '', false)).toBe(true)
    expect(shouldAutoGenerate('auto-local', 'anthropic', '', false)).toBe(false)
  })

  it('generates for any provider in auto, and never in manual', () => {
    expect(shouldAutoGenerate('auto', 'anthropic', '', false)).toBe(true)
    expect(shouldAutoGenerate('manual', 'ollama', '', false)).toBe(false)
  })

  it('never overwrites what the user typed', () => {
    expect(shouldAutoGenerate('auto', 'ollama', 'my own message', false)).toBe(false)
    expect(shouldAutoGenerate('auto', 'ollama', '', true)).toBe(false)
  })
})

describe('amendWarning', () => {
  it('warns when the commit is already on the upstream', () => {
    const m = amendWarning(info({ pushed: true, upstream: 'origin/main' }))
    expect(m).toContain('origin/main')
    expect(m).toMatch(/force push/i)
  })

  it('is null for a commit that was never pushed', () => {
    expect(amendWarning(info())).toBeNull()
  })
})
