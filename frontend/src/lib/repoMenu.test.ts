import { describe, expect, it } from 'vitest'
import { repoMenuGroups } from './repoMenu'

describe('repoMenuGroups', () => {
  it('groups a main repository', () => {
    expect(repoMenuGroups({ missing: false, worktree: false, child: false })).toEqual([
      ['fetch', 'pull', 'push'], ['reveal', 'terminal'], ['settings'], ['move', 'remove'],
    ])
  })
  it('puts Locate first when the folder is missing', () => {
    expect(repoMenuGroups({ missing: true, worktree: false, child: false })[0]).toEqual(['locate'])
  })
  it('a nested worktree row cannot move to a group', () => {
    expect(repoMenuGroups({ missing: false, worktree: false, child: true }).at(-1)).toEqual(['remove'])
  })
  it('a linked worktree has no settings and removes the worktree', () => {
    expect(repoMenuGroups({ missing: false, worktree: true, child: true })).toEqual([
      ['fetch', 'pull', 'push'], ['reveal', 'terminal'], ['remove-worktree'],
    ])
  })
})
