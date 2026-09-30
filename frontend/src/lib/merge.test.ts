import { describe, expect, it } from 'vitest'
import { abortWarning, keptEdit, layoutLines, regionSides, commitWarning, conflictActions, conflictHeader, isEmptyStepError, mergeSections, sideLabel, skipWarning, staleMergeChoice, takeLabels } from './merge'
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
      abort: 'Abort merge', confirm: 'Commit merge', ai: true, done: false, skip: false,
    })
  })
  it('offers a rebase its own wording and AI', () => {
    expect(conflictActions(state({ kind: 'rebase' }))).toEqual({
      abort: 'Abort rebase', confirm: 'Continue rebase', ai: true, done: false, skip: true,
    })
  })
  it.each(['revert', 'am'] as const)('gives %s a real abort and continue, no AI', (kind) => {
    const a = conflictActions(state({ kind }))
    expect(a.abort).toBeTruthy()
    expect(a.confirm).toBeTruthy()
    expect(a.ai).toBe(false)
  })
  it('gives a stash conflict Done instead of abort/continue', () => {
    expect(conflictActions(state({ kind: 'stash' }))).toEqual({
      abort: null, confirm: null, ai: false, done: true, skip: false,
    })
  })
  it('offers Resolve with AI for merge, rebase and cherry-pick only', () => {
    const base = { merging: true, from: 'a', into: 'b', conflicts: ['x'], manual: [], staged: [], unstaged: [] }
    for (const kind of ['merge', 'rebase', 'cherry-pick'] as const) expect(conflictActions({ ...base, kind }).ai).toBe(true)
    for (const kind of ['revert', 'am', 'stash'] as const) expect(conflictActions({ ...base, kind }).ai).toBe(false)
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

describe('sideLabel', () => {
  it('falls back and truncates', () => {
    expect(sideLabel(undefined, 'ours')).toBe('ours')
    expect(sideLabel('main', 'ours')).toBe('main')
    expect(sideLabel('a1b2c3 ' + 'x'.repeat(60), 'theirs')).toHaveLength(40)
    expect(sideLabel('a1b2c3 ' + 'x'.repeat(60), 'theirs').endsWith('…')).toBe(true)
  })
})

describe('takeLabels', () => {
  it('uses the backend labels for merge, rebase and cherry-pick', () => {
    const s = state({ kind: 'merge', oursLabel: 'main', theirsLabel: 'feature' })
    expect(takeLabels(s)).toEqual({ ours: 'main', theirs: 'feature' })
  })

  it('keeps "ours (branch) / theirs (commit)" for revert, which has no named sides', () => {
    const s = state({ kind: 'revert', into: 'main', from: 'a1b2c3', oursLabel: undefined, theirsLabel: undefined })
    expect(takeLabels(s)).toEqual({ ours: 'ours (main)', theirs: 'theirs (a1b2c3)' })
  })

  it('names only the side am has (the current branch), not a nonexistent "from"', () => {
    const s = state({ kind: 'am', into: 'main', from: '', oursLabel: undefined, theirsLabel: undefined })
    expect(takeLabels(s)).toEqual({ ours: 'ours (main)', theirs: 'theirs' })
  })

  it('falls back to plain "ours"/"theirs" for stash, which names neither side', () => {
    const s = state({ kind: 'stash', into: '', from: '', oursLabel: undefined, theirsLabel: undefined })
    expect(takeLabels(s)).toEqual({ ours: 'ours', theirs: 'theirs' })
  })

  it('handles no merge state at all', () => {
    expect(takeLabels(null)).toEqual({ ours: 'ours', theirs: 'theirs' })
  })
})

describe('skipWarning', () => {
  it('names the commit being skipped', () => {
    const w = skipWarning({ kind: 'rebase', merging: true, from: 'feature', into: 'main', conflicts: [], manual: [], staged: [], unstaged: [], theirsLabel: 'a1b2c3 fix login' })
    expect(w.title).toBe('Skip this commit')
    expect(w.message).toBe('a1b2c3 fix login will not be applied. Its changes are dropped from the result.')
  })
})

describe('isEmptyStepError', () => {
  it('recognises git’s empty-step messages', () => {
    expect(isEmptyStepError("No changes - did you forget to use 'git add'?")).toBe(true)
    expect(isEmptyStepError('The previous cherry-pick is now empty, possibly due to conflict resolution.')).toBe(true)
    expect(isEmptyStepError('nothing to commit, working tree clean')).toBe(true)
    expect(isEmptyStepError('fatal: bad revision')).toBe(false)
  })
})

describe('staleMergeChoice', () => {
  it('offers the upstream first when the branch is behind', () => {
    const c = staleMergeChoice('master', 'origin/master', 'release/21', { ahead: 0, behind: 3 })
    expect(c.options).toEqual([
      { value: 'origin/master', label: 'Merge origin/master' },
      { value: 'master', label: 'Merge master as it is' },
    ])
    expect(c.value).toBe('origin/master')
    expect(c.message('origin/master')).toBe(
      'master is 3 commits behind origin/master — those commits are only on the remote. Merge origin/master into release/21? A merge commit is always created.',
    )
    expect(c.message('master')).toBe(
      'Only what your local master has will be merged into release/21; the 3 newer commits on origin/master will not. A merge commit is always created.',
    )
    expect(c.confirmLabel('master')).toBe('Merge')
  })

  it('says what merging the upstream leaves out when the branch has its own commits', () => {
    const c = staleMergeChoice('master', 'origin/master', 'main', { ahead: 2, behind: 1 })
    expect(c.message('origin/master')).toBe(
      'master is 1 commit behind origin/master and 2 ahead of it; merging origin/master leaves those 2 out. Merge origin/master into main? A merge commit is always created.',
    )
  })
})

describe('layoutLines', () => {
  const text = 'a\n<<<<<<< HEAD\nours1\nours2\n||||||| base\nbase1\n=======\ntheirs1\n>>>>>>> feature\nz'
  const lines = text.split('\n')
  const regions = [{ id: 'r1', start: 1, end: 9, baseAt: 4, sep: 6 }]

  it('marks each line with the side it belongs to and where regions start', () => {
    const info = layoutLines(lines, regions)
    expect(info.map((l) => l.part)).toEqual([null, 'marker', 'ours', 'ours', 'marker', 'base', 'marker', 'theirs', 'marker', null])
    expect(info.map((l) => l.starts)).toEqual([false, true, false, false, false, false, false, false, false, false])
    expect(info[3].region?.id).toBe('r1')
    expect(info[0].region).toBeNull()
  })

  it('gives each side of a region as text, for Edit…', () => {
    const info = layoutLines(lines, regions)
    expect(regionSides(lines, info, 'r1')).toEqual({ ours: 'ours1\nours2\n', theirs: 'theirs1\n' })
  })

  it('takes the separators from the backend, so a Markdown underline stays content', () => {
    const md = ['<<<<<<< HEAD', 'Title', '==========', 'ours', '=======', 'Other', '>>>>>>> b']
    const info = layoutLines(md, [{ id: 'r', start: 0, end: 7, baseAt: -1, sep: 4 }])
    expect(info.map((l) => l.part)).toEqual(['marker', 'ours', 'ours', 'ours', 'marker', 'theirs', 'marker'])
    expect(regionSides(md, info, 'r')).toEqual({ ours: 'Title\n==========\nours\n', theirs: 'Other\n' })
  })

  it('handles a two-way region with no ancestor', () => {
    const two = ['<<<<<<< HEAD', 'o', '=======', 't', '>>>>>>> x']
    expect(layoutLines(two, [{ id: 'r', start: 0, end: 5, baseAt: -1, sep: 2 }]).map((l) => l.part)).toEqual(['marker', 'ours', 'marker', 'theirs', 'marker'])
  })
})

describe('keptEdit', () => {
  const r = (id: string) => ({ id, start: 0, end: 5, baseAt: -1, sep: 2 })
  it('keeps an edit in progress across a reload while its region is still there', () => {
    const editing = { id: 'a', value: 'typed' }
    expect(keptEdit(editing, [r('b'), r('a')])).toBe(editing)
  })
  it('drops it once the region is gone', () => {
    expect(keptEdit({ id: 'a', value: 'typed' }, [r('b')])).toBeNull()
    expect(keptEdit(null, [r('a')])).toBeNull()
  })
})
