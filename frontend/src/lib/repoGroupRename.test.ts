import { describe, expect, it } from 'vitest'
import { classifyGroupRename, renameCollapsedGroup } from './repoGroupRename'

describe('classifyGroupRename', () => {
  it('is a no-op when the proposed name is empty', () => {
    expect(classifyGroupRename(['work', 'personal'], 'work', '')).toEqual({ kind: 'noop' })
  })

  it('is a no-op when the proposed name is whitespace-only', () => {
    expect(classifyGroupRename(['work'], 'work', '   ')).toEqual({ kind: 'noop' })
  })

  it('is a no-op when the trimmed name equals the old name', () => {
    expect(classifyGroupRename(['work', 'personal'], 'work', 'work')).toEqual({ kind: 'noop' })
  })

  it('is a no-op when the trimmed name equals the old name with surrounding whitespace', () => {
    expect(classifyGroupRename(['work'], 'work', '  work  ')).toEqual({ kind: 'noop' })
  })

  it('is a plain rename to a name no other group has', () => {
    expect(classifyGroupRename(['work', 'personal'], 'work', 'job')).toEqual({ kind: 'rename', name: 'job' })
  })

  it('trims the proposed name for a plain rename', () => {
    expect(classifyGroupRename(['work'], 'work', '  job  ')).toEqual({ kind: 'rename', name: 'job' })
  })

  it('is a merge when the trimmed name matches a different existing group', () => {
    expect(classifyGroupRename(['work', 'personal'], 'work', 'personal')).toEqual({ kind: 'merge', name: 'personal' })
  })

  it('trims the proposed name for a merge', () => {
    expect(classifyGroupRename(['work', 'personal'], 'work', '  personal  ')).toEqual({ kind: 'merge', name: 'personal' })
  })

  it('is a plain rename when oldName is not among existingNames (defensive)', () => {
    expect(classifyGroupRename(['personal'], 'work', 'job')).toEqual({ kind: 'rename', name: 'job' })
  })
})

describe('renameCollapsedGroup', () => {
  it('leaves the list unchanged when the old name was not collapsed', () => {
    expect(renameCollapsedGroup(['personal'], 'work', 'job')).toEqual(['personal'])
  })

  it('replaces the old name with the new name when it was collapsed', () => {
    expect(renameCollapsedGroup(['work', 'personal'], 'work', 'job')).toEqual(['personal', 'job'])
  })

  it('dedupes when the new name is already in the collapsed list (merge into a collapsed group)', () => {
    expect(renameCollapsedGroup(['work', 'personal'], 'work', 'personal')).toEqual(['personal'])
  })

  it('handles an empty list', () => {
    expect(renameCollapsedGroup([], 'work', 'job')).toEqual([])
  })
})
