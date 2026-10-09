import { describe, expect, it } from 'vitest'
import { branchMenuGroups, branchRemote, type BranchMenuOptions } from './branchMenu'
import type { Branch } from './types'

const local = (over: Partial<Branch> = {}): Branch => ({ name: 'feature', remote: '', hash: 'h', current: false, upstream: 'origin/feature', official: false, ...over })
const remote = (over: Partial<Branch> = {}): Branch => ({ name: 'feature', remote: 'origin', hash: 'h', current: false, upstream: '', official: false, ...over })
const opts = (over: Partial<BranchMenuOptions> = {}): BranchMenuOptions => ({ head: 'main', detached: false, busy: false, merging: false, rebaseWhy: null, remotes: ['origin'], ...over })
const labels = (groups: { label: string }[][]) => groups.map((g) => g.map((e) => e.label))
const find = (groups: ReturnType<typeof branchMenuGroups>, id: string) => groups.flat().find((e) => e.id === id)!

describe('branchMenuGroups', () => {
  it('lays out a non-current local branch', () => {
    expect(labels(branchMenuGroups(local(), opts()))).toEqual([
      ['Check out'],
      ['Fetch origin', 'Pull feature', 'Push feature'],
      ['Merge feature into main', 'Rebase main onto feature'],
      ['New branch from here…', 'New tag here…'],
      ['Delete…'],
    ])
  })

  it('a branch with no upstream publishes to origin', () => {
    const groups = branchMenuGroups(local({ upstream: '' }), opts())
    expect(find(groups, 'push').label).toBe('Publish feature to origin')
    expect(find(groups, 'push').disabled).toBeFalsy()
    expect(find(groups, 'fetch').label).toBe('Fetch origin')
    expect(find(groups, 'pull')).toMatchObject({ disabled: true, title: 'No upstream' })
  })

  it('a branch tracking a local branch can neither pull nor push', () => {
    const groups = branchMenuGroups(local({ upstream: 'main', upstreamLocal: true }), opts())
    expect(find(groups, 'pull')).toMatchObject({ disabled: true, title: 'Tracks a local branch' })
    expect(find(groups, 'push')).toMatchObject({ disabled: true, title: 'Tracks a local branch' })
    expect(find(groups, 'fetch').label).toBe('Fetch origin')
  })

  it('the current branch pulls and pushes', () => {
    const groups = branchMenuGroups(local({ name: 'main', current: true, upstream: 'backup/main' }), opts({ remotes: ['origin', 'backup'] }))
    expect(find(groups, 'fetch').label).toBe('Fetch backup')
    expect(find(groups, 'pull')).toMatchObject({ label: 'Pull main', disabled: false })
    expect(find(groups, 'push')).toMatchObject({ label: 'Push main', disabled: false })
    expect(find(groups, 'checkout').disabled).toBe(true)
    expect(find(groups, 'merge').disabled).toBe(true)
    expect(find(groups, 'delete').disabled).toBe(true)
  })

  it('the current branch without an upstream cannot pull', () => {
    const groups = branchMenuGroups(local({ name: 'main', current: true, upstream: '' }), opts())
    expect(find(groups, 'pull')).toMatchObject({ disabled: true, title: 'No upstream' })
    expect(find(groups, 'push').label).toBe('Publish main to origin')
  })

  it('a remote branch has Fetch but no Pull or Push', () => {
    expect(labels(branchMenuGroups(remote(), opts()))).toEqual([
      ['Check out'],
      ['Fetch origin'],
      ['Merge origin/feature into main', 'Rebase main onto origin/feature'],
      ['New branch from here…', 'New tag here…'],
      ['Delete on remote…'],
    ])
  })

  it('fetches the remote the row belongs to', () => {
    expect(find(branchMenuGroups(remote({ remote: 'backup' }), opts({ remotes: ['origin', 'backup'] })), 'fetch').label).toBe('Fetch backup')
  })

  it('Pull and Push are disabled while busy or mid merge, with a reason', () => {
    for (const over of [{ busy: true }, { merging: true }]) {
      const groups = branchMenuGroups(local(), opts(over))
      expect(find(groups, 'pull').disabled).toBe(true)
      expect(find(groups, 'push').disabled).toBe(true)
      expect(find(groups, 'pull').title).toBeTruthy()
    }
    expect(find(branchMenuGroups(local(), opts({ busy: true })), 'fetch').disabled).toBe(true)
    expect(find(branchMenuGroups(local(), opts({ merging: true })), 'fetch').disabled).toBeFalsy()
  })

  it('keeps the existing entry rules', () => {
    const groups = branchMenuGroups(local({ worktree: '/wt' }), opts({ rebaseWhy: 'main already contains feature', detached: true }))
    expect(find(groups, 'checkout').disabled).toBe(true)
    expect(find(groups, 'merge').disabled).toBe(true)
    expect(find(groups, 'rebase')).toMatchObject({ disabled: true, title: 'main already contains feature' })
    expect(find(groups, 'delete')).toMatchObject({ disabled: true, danger: true })
  })
})

describe('branchRemote', () => {
  it('uses the longest remote name that prefixes the upstream', () => {
    expect(branchRemote(local({ upstream: 'team/fork/feature' }), ['team', 'team/fork'])).toBe('team/fork')
  })
  it('falls back to origin', () => {
    expect(branchRemote(local({ upstream: '' }), ['backup'])).toBe('origin')
    expect(branchRemote(local({ upstream: 'main', upstreamLocal: true }), ['origin'])).toBe('origin')
  })
})
