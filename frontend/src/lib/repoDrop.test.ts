import { describe, expect, it } from 'vitest'
import { resolveRepoDrop } from './repoDrop'
import type { Repo } from './types'

const r = (extra: Partial<Repo> = {}): Repo => ({
  id: 'a',
  name: 'a',
  path: '/repos/a',
  missing: false,
  branch: 'main',
  ...extra,
})

describe('resolveRepoDrop', () => {
  it('moves an ungrouped repo into a group', () => {
    expect(resolveRepoDrop(r(), { group: 'work' })).toBe('work')
  })

  it('is a no-op dropping an ungrouped repo onto the loose area', () => {
    expect(resolveRepoDrop(r(), { group: '' })).toBeNull()
  })

  it('is a no-op dropping onto the group the repo is already in', () => {
    expect(resolveRepoDrop(r({ group: 'work' }), { group: 'work' })).toBeNull()
  })

  it('moves a grouped repo into a different group', () => {
    expect(resolveRepoDrop(r({ group: 'work' }), { group: 'personal' })).toBe('personal')
  })

  it('takes a grouped repo out of its group when dropped on the loose area', () => {
    expect(resolveRepoDrop(r({ group: 'work' }), { group: '' })).toBe('')
  })

  it('treats an empty group string the same as no group', () => {
    expect(resolveRepoDrop(r({ group: '' }), { group: 'work' })).toBe('work')
  })
})
