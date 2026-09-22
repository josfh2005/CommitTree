import { describe, expect, it } from 'vitest'
import { groupRepos } from './repoGroups'
import type { Repo } from './types'

const r = (id: string, extra: Partial<Repo> = {}): Repo => ({
  id,
  name: id,
  path: `/repos/${id}`,
  missing: false,
  branch: 'main',
  ...extra,
})

describe('groupRepos', () => {
  it('leaves every repo loose when none has a group', () => {
    const repos = [r('a'), r('b'), r('c')]
    const { loose, groups } = groupRepos(repos)
    expect(loose.map((x) => x.id)).toEqual(['a', 'b', 'c'])
    expect(groups).toEqual([])
  })

  it('splits repos into loose and named groups, sorted by group name', () => {
    const repos = [
      r('a'),
      r('b', { group: 'work' }),
      r('c', { group: 'personal' }),
      r('d'),
      r('e', { group: 'work' }),
    ]

    const { loose, groups } = groupRepos(repos)

    expect(loose.map((x) => x.id)).toEqual(['a', 'd'])
    expect(groups.map((g) => g.name)).toEqual(['personal', 'work'])
    expect(groups.find((g) => g.name === 'work')?.repos.map((x) => x.id)).toEqual(['b', 'e'])
    expect(groups.find((g) => g.name === 'personal')?.repos.map((x) => x.id)).toEqual(['c'])
  })

  it('treats an empty group string as ungrouped', () => {
    const { loose, groups } = groupRepos([r('a', { group: '' })])
    expect(loose.map((x) => x.id)).toEqual(['a'])
    expect(groups).toEqual([])
  })

  it('handles an empty list', () => {
    expect(groupRepos([])).toEqual({ loose: [], groups: [] })
  })
})
