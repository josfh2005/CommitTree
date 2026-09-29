import { describe, expect, it } from 'vitest'
import { compareRepos, groupRepos, moveGroup, moveRepo, nameOrder, type RepoNode } from './repoGroups'
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

describe('groupRepos in manual order', () => {
  const repos = [r('c'), r('b', { group: 'work' }), r('a'), r('z', { group: 'home' }), r('y', { group: 'work' })]

  it('keeps the stored order of repositories and groups', () => {
    const { loose, groups } = groupRepos(repos, 'manual')
    expect(ids(loose)).toEqual(['c', 'a'])
    expect(groups.map((g) => [g.name, ids(g.repos)])).toEqual([
      ['work', ['b', 'y']],
      ['home', ['z']],
    ])
  })

  it('still lists worktrees under their repository by name', () => {
    const list = [r('main'), r('wt-b', { parentId: 'main' }), r('wt-a', { parentId: 'main' })]
    expect(groupRepos(list, 'manual').loose[0].children.map((c) => c.id)).toEqual(['wt-a', 'wt-b'])
  })
})

describe('moveRepo', () => {
  const repos = [r('a'), r('b'), r('c', { group: 'work' }), r('d', { group: 'work' })]

  it('places a repository before another in the same area', () => {
    expect(moveRepo(repos, 'b', { group: '', beforeId: 'a' })).toEqual(['b', 'a', 'c', 'd'])
  })

  it('places a repository inside another group at the spot it was dropped', () => {
    expect(moveRepo(repos, 'a', { group: 'work', beforeId: 'd' })).toEqual(['b', 'c', 'a', 'd'])
  })

  it('puts a repository at the end of a group when dropped on its empty space', () => {
    expect(moveRepo(repos, 'a', { group: 'work', beforeId: null })).toEqual(['b', 'c', 'd', 'a'])
  })

  it('puts a repository at the end of the loose area', () => {
    expect(moveRepo(repos, 'c', { group: '', beforeId: null })).toEqual(['a', 'b', 'c', 'd'])
  })
})

describe('moveGroup', () => {
  const repos = [r('a'), r('b', { group: 'work' }), r('c', { group: 'home' }), r('d', { group: 'work' })]

  it('moves a group\'s repositories as a block before another group', () => {
    expect(moveGroup(repos, 'home', 'work')).toEqual(['a', 'c', 'b', 'd'])
  })

  it('moves a group to the end', () => {
    expect(moveGroup(repos, 'work', null)).toEqual(['a', 'c', 'b', 'd'])
  })
})

describe('nameOrder', () => {
  it('is the by-name display order, the start of a first manual arrangement', () => {
    const repos = [r('c'), r('b', { group: 'work' }), r('a'), r('z', { group: 'home' })]
    expect(nameOrder(repos)).toEqual(['a', 'c', 'z', 'b'])
  })
})
