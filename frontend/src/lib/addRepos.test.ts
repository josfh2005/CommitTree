import { describe, expect, it } from 'vitest'
import {
  addLabel, chosenGroup, createLatest, folderName, fromScan, groupOptions, NEW_GROUP, selectAll, selectNone, skippedLine, summarize, ticked, toggle,
} from './addRepos'
import type { AddFailure, Repo, ScanResult } from './types'

const scan: ScanResult = {
  parent: '/p',
  skipped: 2,
  repos: [
    { name: 'a', path: '/p/a', added: false },
    { name: 'b', path: '/p/b', added: true },
    { name: 'c', path: '/p/c', added: false },
  ],
}

describe('rows and ticks', () => {
  it('ticks only repositories not yet added', () => {
    const v = fromScan(scan)
    expect(v.rows.map((r) => r.ticked)).toEqual([true, false, true])
    expect(ticked(v)).toEqual(['/p/a', '/p/c'])
    expect(v.skipped).toBe(2)
    expect(v.parent).toBe('/p')
  })
  it('toggle flips an enabled row and ignores an added one', () => {
    const v = fromScan(scan)
    expect(ticked(toggle(v, '/p/a'))).toEqual(['/p/c'])
    expect(ticked(toggle(v, '/p/b'))).toEqual(['/p/a', '/p/c'])
  })
  it('select all / none act on enabled rows only', () => {
    const none = selectNone(fromScan(scan))
    expect(ticked(none)).toEqual([])
    expect(none.rows[1].ticked).toBe(false)
    const all = selectAll(none)
    expect(ticked(all)).toEqual(['/p/a', '/p/c'])
    expect(all.rows[1].ticked).toBe(false)
  })
  it('no view means nothing ticked', () => expect(ticked(null)).toEqual([]))
})

describe('labels', () => {
  it.each([[0, 'Add repositories'], [1, 'Add 1 repository'], [7, 'Add 7 repositories']])('addLabel(%i)', (n, want) =>
    expect(addLabel(n)).toBe(want))
  it.each([
    [0, ''],
    [1, '1 folder skipped (not a git repository).'],
    [3, '3 folders skipped (not git repositories).'],
  ])('skippedLine(%i)', (n, want) => expect(skippedLine(n)).toBe(want))
})

describe('groups', () => {
  const repo = (group?: string) => ({ id: group ?? 'x', name: 'n', path: '/n', missing: false, branch: '', group }) as Repo
  it('lists existing groups once, sorted', () => {
    expect(groupOptions([repo('work'), repo(), repo('Apps'), repo('work')])).toEqual(['Apps', 'work'])
  })
  it('chosenGroup', () => {
    expect(chosenGroup('', 'ignored')).toBe('')
    expect(chosenGroup('work', '')).toBe('work')
    expect(chosenGroup(NEW_GROUP, '  projects ')).toBe('projects')
    expect(chosenGroup(NEW_GROUP, '   ')).toBeNull()
  })
  it.each([
    ['/Users/me/projects', 'projects'],
    ['/Users/me/projects/', 'projects'],
    ['~/code', 'code'],
    ['/', ''],
  ])('folderName(%s)', (p, want) => expect(folderName(p)).toBe(want))
})

describe('summarize', () => {
  const repo = { id: 'i', name: 'n', path: '/n', missing: false, branch: '' } as Repo
  const fail = (name: string): AddFailure => ({ path: '/p/' + name, name, reason: 'not a git repository' })

  it('all added', () => {
    expect(summarize({ added: [repo], failed: [] })).toEqual({ kind: 'info', text: 'Added 1 repository.', keepOpen: false })
    expect(summarize({ added: [repo, repo], failed: [] }).text).toBe('Added 2 repositories.')
  })
  it('nothing new: everything was already listed', () => {
    expect(summarize({ added: [], failed: [] })).toEqual({
      kind: 'info', text: 'Nothing added: those repositories are already in the list.', keepOpen: false,
    })
  })
  it('partial failure names at most three', () => {
    expect(summarize({ added: [repo], failed: [fail('x')] })).toEqual({
      kind: 'error', text: "Added 1 repository; 1 couldn't be added: x (not a git repository).", keepOpen: false,
    })
    const five = ['a', 'b', 'c', 'd', 'e'].map(fail)
    expect(summarize({ added: [repo, repo], failed: five }).text).toBe(
      "Added 2 repositories; 5 couldn't be added: a (not a git repository), b (not a git repository), c (not a git repository), and 2 more.",
    )
  })
  it('all failed keeps the dialog open', () => {
    expect(summarize({ added: [], failed: [fail('a'), fail('b'), fail('c')] })).toEqual({
      kind: 'error',
      text: 'No repository could be added: a (not a git repository), b (not a git repository), c (not a git repository).',
      keepOpen: true,
    })
  })
})

describe('createLatest', () => {
  it('only the newest request is current', () => {
    const l = createLatest()
    const first = l.next()
    const second = l.next()
    expect(l.isCurrent(first)).toBe(false)
    expect(l.isCurrent(second)).toBe(true)
  })
  it('invalidate drops the request in flight (field edited during a scan)', () => {
    const l = createLatest()
    const n = l.next()
    l.invalidate()
    expect(l.isCurrent(n)).toBe(false)
  })
})
