import { get } from 'svelte/store'
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('./api', () => ({
  api: {
    deleteBranch: vi.fn().mockResolvedValue(undefined),
    deleteRemoteBranch: vi.fn().mockResolvedValue(undefined),
    deleteTag: vi.fn().mockResolvedValue(undefined),
    listRepos: vi.fn().mockResolvedValue([]),
    getRefs: vi.fn().mockResolvedValue(null),
    commitMerge: vi.fn().mockResolvedValue(undefined),
    skipStep: vi.fn().mockResolvedValue(undefined),
  },
}))

vi.mock('./ui', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./ui')>()
  return { ...actual, confirmDialog: vi.fn().mockResolvedValue(true) }
})

import { commitMerge, deleteBranch, deleteTag, skipStep } from './actions'
import { api } from './api'
import { filters, mergeState } from './stores'
import type { Branch, Filters, MergeState } from './types'
import { confirmDialog } from './ui'

function mergeStateOf(over: Partial<MergeState>): MergeState {
  return { kind: 'rebase', merging: true, from: 'feature', into: 'main', conflicts: [], manual: [], staged: [], unstaged: [], ...over }
}

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

describe('commitMerge', () => {
  beforeEach(() => {
    vi.mocked(api.commitMerge).mockReset()
    vi.mocked(api.skipStep).mockReset().mockResolvedValue(undefined)
    vi.mocked(confirmDialog).mockClear().mockResolvedValue(true)
    mergeState.set(null)
  })

  it('offers Skip when a rebase step comes back empty, and skips on confirmation', async () => {
    mergeState.set(mergeStateOf({ kind: 'rebase' }))
    vi.mocked(api.commitMerge).mockRejectedValue(new Error("No changes - did you forget to use 'git add'?"))

    await commitMerge('repo1')

    expect(confirmDialog).toHaveBeenCalledWith(expect.objectContaining({ title: 'Nothing to commit', confirmLabel: 'Skip this commit' }))
    expect(api.skipStep).toHaveBeenCalledWith('repo1')
  })

  it('offers Skip when a cherry-pick step comes back empty', async () => {
    mergeState.set(mergeStateOf({ kind: 'cherry-pick' }))
    vi.mocked(api.commitMerge).mockRejectedValue(new Error('The previous cherry-pick is now empty, possibly due to conflict resolution.'))

    await commitMerge('repo1')

    expect(api.skipStep).toHaveBeenCalledWith('repo1')
  })

  it('does not offer Skip for a plain merge, even on an empty-step-shaped message', async () => {
    mergeState.set(mergeStateOf({ kind: 'merge' }))
    vi.mocked(api.commitMerge).mockRejectedValue(new Error("No changes - did you forget to use 'git add'?"))

    await commitMerge('repo1')

    expect(api.skipStep).not.toHaveBeenCalled()
  })

  it('does not offer Skip for an unrelated failure', async () => {
    mergeState.set(mergeStateOf({ kind: 'rebase' }))
    vi.mocked(api.commitMerge).mockRejectedValue(new Error('fatal: bad revision'))

    await commitMerge('repo1')

    expect(api.skipStep).not.toHaveBeenCalled()
  })
})

describe('skipStep', () => {
  beforeEach(() => {
    vi.mocked(api.skipStep).mockReset().mockResolvedValue(undefined)
    vi.mocked(confirmDialog).mockClear().mockResolvedValue(true)
    mergeState.set(null)
  })

  it('confirms with the commit being skipped, then calls the backend', async () => {
    mergeState.set(mergeStateOf({ kind: 'rebase', theirsLabel: 'a1b2c3 fix login' }))

    await skipStep('repo1')

    expect(confirmDialog).toHaveBeenCalledWith(
      expect.objectContaining({ title: 'Skip this commit', message: 'a1b2c3 fix login will not be applied. Its changes are dropped from the result.' }),
    )
    expect(api.skipStep).toHaveBeenCalledWith('repo1')
  })

  it('does nothing when the confirmation is declined', async () => {
    mergeState.set(mergeStateOf({ kind: 'rebase', theirsLabel: 'a1b2c3 fix login' }))
    vi.mocked(confirmDialog).mockResolvedValueOnce(false)

    await skipStep('repo1')

    expect(api.skipStep).not.toHaveBeenCalled()
  })
})
