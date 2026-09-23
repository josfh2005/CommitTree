import { describe, expect, it } from 'vitest'
import { compareRepos, groupRepos, type RepoNode } from './repoGroups'
import type { Repo } from './types'

const r = (id: string, extra: Partial<Repo> = {}): Repo => ({
  id,
  name: id,
  path: `/repos/${id}`,
  missing: false,
  branch: 'main',
  ...extra,
})
const ids = (nodes: RepoNode[]) => nodes.map((n) => n.repo.id)

describe('groupRepos', () => {
  it('leaves every repo loose when none has a group', () => {
    const { loose, groups } = groupRepos([r('a'), r('b'), r('c')])
    expect(ids(loose)).toEqual(['a', 'b', 'c'])
    expect(groups).toEqual([])
  })

  it('splits repos into loose and named groups, sorted by group name', () => {
    const repos = [r('a'), r('b', { group: 'work' }), r('c', { group: 'personal' }), r('d'), r('e', { group: 'work' })]
    const { loose, groups } = groupRepos(repos)
    expect(ids(loose)).toEqual(['a', 'd'])
    expect(groups.map((g) => g.name)).toEqual(['personal', 'work'])
    expect(ids(groups.find((g) => g.name === 'work')!.repos)).toEqual(['b', 'e'])
    expect(ids(groups.find((g) => g.name === 'personal')!.repos)).toEqual(['c'])
  })

  it('treats an empty group string as ungrouped', () => {
    const { loose, groups } = groupRepos([r('a', { group: '' })])
    expect(ids(loose)).toEqual(['a'])
    expect(groups).toEqual([])
  })

  it('handles an empty list', () => {
    expect(groupRepos([])).toEqual({ loose: [], groups: [] })
  })

  it('sorts by name case-insensitively, ties broken by path', () => {
    const repos = [r('z', { name: 'beta' }), r('y', { name: 'Alpha' }), r('x', { name: 'alpha', path: '/a' })]
    expect(ids(groupRepos(repos).loose)).toEqual(['x', 'y', 'z'])
  })

  it('nests worktrees under their parent, loose or grouped, sorted by name', () => {
    const repos = [
      r('main1'),
      r('wtb', { parentId: 'main1', name: 'b-wt', worktree: true }),
      r('wta', { parentId: 'main1', name: 'a-wt', worktree: true }),
      r('main2', { group: 'work' }),
      r('wtc', { parentId: 'main2', group: 'other', name: 'c-wt' }),
    ]
    const { loose, groups } = groupRepos(repos)
    expect(ids(loose)).toEqual(['main1'])
    expect(loose[0].children.map((c) => c.id)).toEqual(['wta', 'wtb'])
    expect(groups.map((g) => g.name)).toEqual(['work'])
    expect(groups[0].repos[0].children.map((c) => c.id)).toEqual(['wtc'])
  })

  it('shows a child whose parent is not listed at the top level', () => {
    const { loose } = groupRepos([r('orphan', { parentId: 'gone' })])
    expect(ids(loose)).toEqual(['orphan'])
  })

  it('drops submodules entirely: never loose, grouped, or a child', () => {
    const repos = [
      r('t'),
      r('sub', { submodule: true, parentId: 't' }),
      r('grouped', { group: 'work' }),
      r('sub2', { submodule: true, parentId: 'grouped', group: 'work' }),
    ]
    const { loose, groups } = groupRepos(repos)
    expect(ids(loose)).toEqual(['t'])
    expect(loose[0].children).toEqual([])
    expect(ids(groups.find((g) => g.name === 'work')!.repos)).toEqual(['grouped'])
    expect(groups.find((g) => g.name === 'work')!.repos[0].children).toEqual([])
  })
})

describe('compareRepos', () => {
  it('orders by name then path', () => {
    expect(compareRepos(r('a', { name: 'X' }), r('b', { name: 'x', path: '/z' }))).toBeLessThan(0)
  })
})
