import { describe, expect, it } from 'vitest'
import { blameBlocker, blocksIn, lineBlocks, previousBlocker, rangeAt, revLabel, selectLine, singleCommit } from './blame'
import type { Blame, BlameBlock } from './types'

const blk = (hash: string, start: number, count: number, extra: Partial<BlameBlock> = {}): BlameBlock => ({
  hash, short: hash.slice(0, 7), author: 'A', email: 'a@x', date: '2026-01-01T00:00:00Z', summary: 's', filename: 'f', start, count, ...extra,
})
const blame: Blame = {
  path: 'f', rev: 'HEAD', startLine: 1, truncated: false,
  lines: ['a', 'b', 'c', 'd', 'e'],
  blocks: [blk('1111111aaa', 1, 2, { previous: 'p1', prevPath: 'f' }), blk('2222222bbb', 3, 1), blk('1111111aaa', 4, 2, { previous: 'p1', prevPath: 'f' })],
}

describe('blameBlocker', () => {
  it('allows ordinary files', () => expect(blameBlocker({ status: 'M' })).toBeNull())
  it('refuses deleted, untracked and submodules', () => {
    expect(blameBlocker({ status: 'D' })).toBe('The file was deleted')
    expect(blameBlocker({ status: '?' })).toBe('Untracked files have no history')
    expect(blameBlocker({ status: 'M', submodule: true })).toBe('Submodules have no blame')
  })
})

describe('lines and blocks', () => {
  it('maps each line to its block', () => expect(lineBlocks(blame)).toEqual([0, 0, 1, 2, 2]))
  it('honours startLine for ranged blames', () => {
    const ranged = { ...blame, startLine: 3, lines: ['c', 'd'], blocks: [blk('x', 3, 1), blk('y', 4, 1)] }
    expect(lineBlocks(ranged)).toEqual([0, 1])
  })
  it('finds the blocks a range touches', () => expect(blocksIn(blame, { start: 2, end: 3 }).map((b) => b.start)).toEqual([1, 3]))
  it('uses the selection when the line is inside it, else the line\'s block', () => {
    expect(rangeAt(blame, { start: 2, end: 4 }, 3)).toEqual({ start: 2, end: 4 })
    expect(rangeAt(blame, { start: 2, end: 4 }, 5)).toEqual({ start: 4, end: 5 })
    expect(rangeAt(blame, null, 1)).toEqual({ start: 1, end: 2 })
  })
})

describe('selectLine', () => {
  it('selects one line, then extends with shift in either direction', () => {
    let s = selectLine(null, null, 4, false)
    expect(s).toEqual({ selection: { start: 4, end: 4 }, anchor: 4 })
    s = selectLine(s.selection, s.anchor, 2, true)
    expect(s).toEqual({ selection: { start: 2, end: 4 }, anchor: 4 })
    s = selectLine(s.selection, s.anchor, 7, false)
    expect(s).toEqual({ selection: { start: 7, end: 7 }, anchor: 7 })
  })
})

describe('commit actions', () => {
  it('treats repeated blocks of one commit as a single commit', () => {
    expect(singleCommit([blame.blocks[0], blame.blocks[2]])?.hash).toBe('1111111aaa')
    expect(singleCommit(blame.blocks)).toBeNull()
    expect(singleCommit([blk('0000000000', 1, 1, { uncommitted: true })])).toBeNull()
  })
  it('explains why Blame previous revision is unavailable', () => {
    expect(previousBlocker([blame.blocks[0]])).toBeNull()
    expect(previousBlocker(blame.blocks)).toBe('The lines come from more than one commit')
    expect(previousBlocker([blk('0000000000', 1, 1, { uncommitted: true })])).toBe('Not committed yet')
    expect(previousBlocker([blk('3333333', 1, 1, { boundary: true })])).toBe('History before this commit is not available')
    expect(previousBlocker([blk('3333333', 1, 1)])).toBe('These lines were added in this commit')
  })
})

it('labels revisions', () => {
  expect(revLabel('')).toBe('Working tree')
  expect(revLabel('abcdef123456')).toBe('abcdef1')
})
