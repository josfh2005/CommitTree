import { describe, expect, it } from 'vitest'
import { checkoutChoices, checkoutNotice } from './checkout'
import type { Branch, Ref, Refs } from './types'

const local = (name: string, extra: Partial<Branch> = {}): Branch => ({ name, remote: '', hash: 'h', current: false, upstream: '', ...extra })
const remote = (remoteName: string, name: string): Branch => ({ name, remote: remoteName, hash: 'h', current: false, upstream: '' })

function refsOf(over: Partial<Refs> = {}): Refs {
  return {
    head: 'main',
    headHash: 'h0',
    detached: false,
    local: [local('main', { current: true }), local('develop', { upstream: 'origin/develop' })],
    remotes: [
      { name: 'origin', branches: [remote('origin', 'develop'), remote('origin', 'release/21')] },
      { name: 'upstream', branches: [remote('upstream', 'release/21')] },
    ],
    tags: [],
    ...over,
  }
}

const r = (kind: Ref['kind'], name: string): Ref => ({ kind, name })

describe('checkoutChoices', () => {
  it('shows a local branch and its remote twin once, as the local branch', () => {
    const choices = checkoutChoices([r('local', 'develop'), r('remote', 'origin/develop')], refsOf())
    expect(choices).toEqual([{ name: 'develop', remote: '', current: false, worktree: '' }])
  })

  it('offers a remote-only branch through its remote', () => {
    const choices = checkoutChoices([r('remote', 'origin/develop')], refsOf())
    expect(choices).toEqual([{ name: 'develop', remote: 'origin', current: false, worktree: '' }])
  })

  it('keeps slashes in branch names and prefers origin when no local branch tracks one', () => {
    const choices = checkoutChoices([r('remote', 'upstream/release/21'), r('remote', 'origin/release/21')], refsOf())
    expect(choices).toEqual([{ name: 'release/21', remote: 'origin', current: false, worktree: '' }])
  })

  it('prefers the remote the local branch tracks', () => {
    const refs = refsOf({ local: [local('release/21', { upstream: 'upstream/release/21' })] })
    const choices = checkoutChoices([r('remote', 'origin/release/21'), r('remote', 'upstream/release/21')], refs)
    expect(choices[0].remote).toBe('upstream')
  })

  it('marks the current branch and a branch checked out in another worktree', () => {
    const refs = refsOf({ local: [local('main', { current: true }), local('develop', { worktree: '/tmp/wt' })] })
    const choices = checkoutChoices([r('head', 'HEAD'), r('local', 'main'), r('remote', 'origin/develop')], refs)
    expect(choices).toEqual([
      { name: 'main', remote: '', current: true, worktree: '' },
      { name: 'develop', remote: 'origin', current: false, worktree: '/tmp/wt' },
    ])
  })

  it('ignores tags, HEAD and origin/HEAD', () => {
    expect(checkoutChoices([r('tag', 'v1'), r('head', 'HEAD'), r('remote', 'origin/HEAD')], refsOf())).toEqual([])
  })

  it('has no current branch while HEAD is detached', () => {
    const choices = checkoutChoices([r('local', 'main')], refsOf({ detached: true }))
    expect(choices[0].current).toBe(false)
  })
})

describe('checkoutNotice', () => {
  it('says when the local branch moved up or was left behind', () => {
    expect(checkoutNotice('fastForwarded', 'origin', 'develop')).toBe('develop fast-forwarded to origin/develop')
    expect(checkoutNotice('diverged', 'origin', 'develop')).toBe(
      'develop has commits not on origin/develop — checked out your local develop, not the remote commit',
    )
    expect(checkoutNotice('switched', 'origin', 'develop')).toBe('')
    expect(checkoutNotice('created', 'origin', 'develop')).toBe('')
  })
})
