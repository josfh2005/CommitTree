import { get } from 'svelte/store'
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('./api', () => ({
  api: {
    deleteBranch: vi.fn().mockResolvedValue(undefined),
    deleteRemoteBranch: vi.fn().mockResolvedValue(undefined),
    deleteTag: vi.fn().mockResolvedValue(undefined),
    listRepos: vi.fn().mockResolvedValue([]),
    getRefs: vi.fn().mockResolvedValue(null),
  },
}))

vi.mock('./ui', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./ui')>()
  return { ...actual, confirmDialog: vi.fn().mockResolvedValue(true) }
})

import { deleteBranch, deleteTag } from './actions'
import { filters } from './stores'
import type { Branch, Filters } from './types'

function emptyFilters(): Filters {
  return { text: '', branch: '', author: '', since: '', until: '', paths: [] }
}

function branch(name: string, remote = ''): Branch {
  return { name, remote, hash: 'h', current: false, upstream: '' }
}

describe('deleteBranch / deleteTag clear a matching branch filter', () => {
  beforeEach(() => {
    filters.set(emptyFilters())
  })

  it('clears filters.branch when deleting the filtered local branch', async () => {
    filters.update((f) => ({ ...f, branch: 'refs/heads/stale-cleanup' }))
    await deleteBranch('repo1', branch('stale-cleanup'))
    expect(get(filters).branch).toBe('')
  })

  it('leaves filters.branch alone when deleting an unrelated local branch', async () => {
    filters.update((f) => ({ ...f, branch: 'refs/heads/keep-me' }))
    await deleteBranch('repo1', branch('other'))
    expect(get(filters).branch).toBe('refs/heads/keep-me')
  })

  it('clears filters.branch when deleting the filtered remote branch', async () => {
    filters.update((f) => ({ ...f, branch: 'refs/remotes/origin/stale-cleanup' }))
    await deleteBranch('repo1', branch('stale-cleanup', 'origin'))
    expect(get(filters).branch).toBe('')
  })

  it('clears filters.branch when deleting the filtered tag', async () => {
    filters.update((f) => ({ ...f, branch: 'refs/tags/v1' }))
    await deleteTag('repo1', 'v1')
    expect(get(filters).branch).toBe('')
  })

  it('leaves filters.branch alone when deleting an unrelated tag', async () => {
    filters.update((f) => ({ ...f, branch: 'refs/tags/keep' }))
    await deleteTag('repo1', 'v1')
    expect(get(filters).branch).toBe('refs/tags/keep')
  })
})
