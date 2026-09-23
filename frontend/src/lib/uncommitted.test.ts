import { describe, expect, it } from 'vitest'
import type { FileStatus, LogRow, WorktreeState } from './types'
import { cleanTreeSelection, followHead, uncommittedCount, uncommittedMarker } from './uncommitted'

const f = (path: string, status = 'M'): FileStatus => ({ path, status })
const state = (s: Partial<WorktreeState>): WorktreeState => ({ staged: [], unstaged: [], untracked: [], merging: false, ...s })
const row = (hash: string, lane: number, isHead = false, color = lane): LogRow => ({
  hash, short: hash.slice(0, 7), parents: [], author: 'a', email: 'a@x', date: '2026-09-23T00:00:00Z',
  subject: hash, refs: [], lane, color, edges: [], isMerge: false, isHead,
})

describe('uncommittedCount', () => {
  it('is 0 for null and for a clean tree', () => {
    expect(uncommittedCount(null)).toBe(0)
    expect(uncommittedCount(state({}))).toBe(0)
  })
  it('counts each list alone', () => {
    expect(uncommittedCount(state({ staged: [f('a')] }))).toBe(1)
    expect(uncommittedCount(state({ unstaged: [f('a'), f('b')] }))).toBe(2)
    expect(uncommittedCount(state({ untracked: [f('n', '?')] }))).toBe(1)
  })
  it('counts a partially staged path once', () => {
    expect(uncommittedCount(state({ staged: [f('a')], unstaged: [f('a')] }))).toBe(1)
  })
  it('adds distinct paths across lists', () => {
    expect(uncommittedCount(state({ staged: [f('a')], unstaged: [f('b')], untracked: [f('c', '?')] }))).toBe(3)
  })
})

describe('uncommittedMarker', () => {
  it('sits straight above HEAD when HEAD is the first row, without reserving a lane', () => {
    expect(uncommittedMarker([row('h', 1, true, 5), row('x', 0)], false)).toEqual({ lane: 1, color: 5, shift: false, line: 'head', headIndex: 0, headLane: 1 })
  })
  it('reserves lane 0 and runs down to HEAD when HEAD is further down', () => {
    expect(uncommittedMarker([row('x', 0), row('y', 1), row('h', 2, true, 3)], false)).toEqual({ lane: 0, color: 3, shift: true, line: 'head', headIndex: 2, headLane: 2 })
  })
  it('runs to the end of the loaded rows while HEAD may still be paged in', () => {
    expect(uncommittedMarker([row('x', 1), row('y', 2)], true)).toEqual({ lane: 0, color: 0, shift: true, line: 'end', headIndex: -1, headLane: 0 })
  })
  it('stands alone in lane 0 when HEAD is not in the log at all', () => {
    expect(uncommittedMarker([row('x', 1), row('y', 2)], false)).toEqual({ lane: 0, color: 0, shift: false, line: 'none', headIndex: -1, headLane: 0 })
    expect(uncommittedMarker([], false)).toEqual({ lane: 0, color: 0, shift: false, line: 'none', headIndex: -1, headLane: 0 })
  })
})

describe('cleanTreeSelection', () => {
  it('does nothing while the row is not selected', () => {
    expect(cleanTreeSelection(false, 0, 'abc')).toBeNull()
  })
  it('does nothing while the tree still has changes', () => {
    expect(cleanTreeSelection(true, 2, 'abc')).toBeNull()
  })
  it('moves the selection to HEAD when the tree becomes clean', () => {
    expect(cleanTreeSelection(true, 0, 'abc')).toBe('abc')
  })
  it('clears the selection when there is no HEAD', () => {
    expect(cleanTreeSelection(true, 0, '')).toBe('')
  })
})

describe('followHead', () => {
  it('selects the new HEAD once refs catch up to the followed old HEAD', () => {
    expect(followHead('old-head', 'old-head', 'new-head')).toBe('new-head')
  })
  it('does nothing once the user has selected something else', () => {
    expect(followHead('old-head', 'something-else', 'new-head')).toBeNull()
  })
  it('does nothing while HEAD has not moved yet', () => {
    expect(followHead('old-head', 'old-head', 'old-head')).toBeNull()
  })
  it('does nothing with an empty headHash', () => {
    expect(followHead('old-head', 'old-head', '')).toBeNull()
  })
  it('does nothing when nothing is being followed', () => {
    expect(followHead('', 'old-head', 'new-head')).toBeNull()
  })
})
