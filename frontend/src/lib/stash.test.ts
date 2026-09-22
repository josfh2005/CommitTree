import { describe, expect, it } from 'vitest'
import { stashFileTitle, stashSections, validSelectedStash } from './stash'
import type { StashEntry, StashFile } from './types'

const tracked = (path: string, status = 'M'): StashFile => ({ path, status, untracked: false })
const untracked = (path: string): StashFile => ({ path, status: 'A', untracked: true })

describe('stashFileTitle', () => {
  it('labels a tracked change as Changed', () => {
    expect(stashFileTitle(tracked('a.txt'))).toBe('Changed')
  })
  it('labels an untracked file as Untracked', () => {
    expect(stashFileTitle(untracked('new.txt'))).toBe('Untracked')
  })
})

describe('stashSections', () => {
  it('splits tracked changes from untracked files', () => {
    const sections = stashSections([tracked('a.txt'), untracked('new.txt')])
    expect(sections).toEqual([
      { title: 'Changed', files: [tracked('a.txt')] },
      { title: 'Untracked', files: [untracked('new.txt')] },
    ])
  })
  it('omits an empty section entirely', () => {
    expect(stashSections([untracked('new.txt')])).toEqual([{ title: 'Untracked', files: [untracked('new.txt')] }])
    expect(stashSections([])).toEqual([])
  })
})

describe('validSelectedStash', () => {
  const entries: StashEntry[] = [
    { index: 0, message: 'wip', branch: 'main', hash: 'a' },
    { index: 1, message: 'older', branch: 'main', hash: 'b' },
  ]

  it('keeps a selection whose index is still in the list', () => {
    expect(validSelectedStash(1, entries)).toBe(1)
  })
  it('clears a selection whose index no longer exists', () => {
    expect(validSelectedStash(2, entries)).toBeNull()
    expect(validSelectedStash(0, [])).toBeNull()
  })
  it('leaves null as null', () => {
    expect(validSelectedStash(null, entries)).toBeNull()
  })
})
