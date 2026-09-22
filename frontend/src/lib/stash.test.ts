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

  it('keeps a selection whose hash is still in the list', () => {
    expect(validSelectedStash({ index: 1, hash: 'b' }, entries)).toEqual({ index: 1, hash: 'b' })
  })

  // Applying, popping or dropping an entry below the selected one shifts
  // every index above it down by one — the stash it was selected against is
  // still there, just no longer at the index it was selected at. Identity
  // is the hash; the index must follow it, not the other way around.
  it('follows its hash to a new index when entries below it are removed', () => {
    const shifted: StashEntry[] = [{ index: 0, message: 'older', branch: 'main', hash: 'b' }]
    expect(validSelectedStash({ index: 1, hash: 'b' }, shifted)).toEqual({ index: 0, hash: 'b' })
  })

  // The dangerous case this guards against: the selected stash is gone, but
  // some other, unrelated stash now sits at the very index it used to
  // occupy. Checking the index alone would pass and silently preview that
  // other stash's files; checking the hash must clear the selection
  // instead.
  it('clears a selection when a different stash now sits at the same index', () => {
    const different: StashEntry[] = [{ index: 0, message: 'new', branch: 'main', hash: 'c' }]
    expect(validSelectedStash({ index: 0, hash: 'a' }, different)).toBeNull()
  })

  it('clears a selection whose hash no longer exists at all', () => {
    expect(validSelectedStash({ index: 0, hash: 'a' }, [])).toBeNull()
  })

  it('leaves null as null', () => {
    expect(validSelectedStash(null, entries)).toBeNull()
  })
})
