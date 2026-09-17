import { describe, expect, it } from 'vitest'
import { groupBranches, groupOf, leafName } from './branches'
import type { Branch } from './types'

const b = (name: string, extra: Partial<Branch> = {}): Branch => ({
  name,
  remote: '',
  hash: 'h',
  current: false,
  upstream: '',
  ...extra,
})

describe('groupOf / leafName', () => {
  it('splits on the first slash only', () => {
    expect(groupOf('fix/abc')).toBe('fix')
    expect(groupOf('feature/NEXO-1/x')).toBe('feature')
    expect(groupOf('main')).toBe('')
    expect(leafName('feature/NEXO-1/x', 'feature')).toBe('NEXO-1/x')
    expect(leafName('main', '')).toBe('main')
  })
})

describe('groupBranches', () => {
  it('groups prefixes shared by two or more branches', () => {
    const branches = [
      b('main', { current: true }),
      b('develop'),
      b('fix/abc'),
      b('fix/cdf'),
      b('hotfix/only-one'),
      b('feature/NEXO-2'),
      b('feature/NEXO-1/x'),
    ]

    const { loose, groups } = groupBranches(branches)

    expect(loose.map((x) => x.name)).toEqual(['main', 'develop', 'hotfix/only-one'])
    expect(groups.map((g) => g.name)).toEqual(['feature', 'fix'])
    expect(groups[0].branches.map((x) => x.name)).toEqual(['feature/NEXO-2', 'feature/NEXO-1/x'])
    expect(groups[1].branches.map((x) => x.name)).toEqual(['fix/abc', 'fix/cdf'])
  })

  it('marks the group holding the current branch', () => {
    const { groups } = groupBranches([b('fix/a', { current: true }), b('fix/b'), b('release/1'), b('release/2')])

    expect(groups.map((g) => [g.name, g.hasCurrent])).toEqual([
      ['fix', true],
      ['release', false],
    ])
  })

  it('handles an empty list', () => {
    expect(groupBranches([])).toEqual({ loose: [], groups: [] })
  })
})
