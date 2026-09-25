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
    getWorktreeRemovalInfo: vi.fn(),
    removeWorktree: vi.fn().mockResolvedValue(undefined),
  },
}))

vi.mock('./ui', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./ui')>()
  return {
    ...actual,
    confirmDialog: vi.fn().mockResolvedValue(true),
    confirmDialogWithCheckbox: vi.fn().mockResolvedValue({ ok: true, checked: false }),
  }
})

import { commitMerge, deleteBranch, deleteTag, removeWorktree, skipStep } from './actions'
import { api } from './api'
import { filters, mergeState } from './stores'
import type { Branch, Filters, MergeState, Repo, WorktreeRemovalInfo } from './types'
import { confirmDialog, confirmDialogWithCheckbox } from './ui'

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

function worktreeRepo(over: Partial<Repo> = {}): Repo {
  return { id: 'wt1', name: 'wt', path: '/p/wt', missing: false, branch: 'feature', parentId: 'main1', worktree: true, ...over }
}

function removalInfo(over: Partial<WorktreeRemovalInfo> = {}): WorktreeRemovalInfo {
  return { branch: 'feature', detached: false, changes: 0, locked: false, merged: false, ...over }
}

describe('removeWorktree', () => {
  beforeEach(() => {
    vi.mocked(api.getWorktreeRemovalInfo).mockReset()
    vi.mocked(api.removeWorktree).mockReset().mockResolvedValue(undefined)
    vi.mocked(api.deleteBranch).mockReset().mockResolvedValue(undefined)
    vi.mocked(confirmDialog).mockReset().mockResolvedValue(true)
    vi.mocked(confirmDialogWithCheckbox).mockReset().mockResolvedValue({ ok: true, checked: false })
  })

  it('warns with the plural wording and forces removal when there are several uncommitted changes', async () => {
    vi.mocked(api.getWorktreeRemovalInfo).mockResolvedValue(removalInfo({ changes: 3 }))
    await removeWorktree(worktreeRepo())
    expect(confirmDialogWithCheckbox).toHaveBeenCalledWith(
      expect.objectContaining({
        title: 'Remove worktree wt?',
        message: expect.stringContaining('It has 3 uncommitted changes that will be lost.'),
        confirmLabel: 'Remove anyway',
      }),
    )
    expect(api.removeWorktree).toHaveBeenCalledWith('wt1', true, false)
  })

  it('uses the singular wording for exactly one uncommitted change', async () => {
    vi.mocked(api.getWorktreeRemovalInfo).mockResolvedValue(removalInfo({ changes: 1 }))
    await removeWorktree(worktreeRepo())
    expect(confirmDialogWithCheckbox).toHaveBeenCalledWith(
      expect.objectContaining({ message: expect.stringContaining('It has 1 uncommitted change that will be lost.') }),
    )
  })

  it('does not force and labels the button Remove for a clean worktree', async () => {
    vi.mocked(api.getWorktreeRemovalInfo).mockResolvedValue(removalInfo({ changes: 0 }))
    await removeWorktree(worktreeRepo())
    expect(confirmDialogWithCheckbox).toHaveBeenCalledWith(
      expect.objectContaining({ confirmLabel: 'Remove', message: expect.not.stringContaining('uncommitted') }),
    )
    expect(api.removeWorktree).toHaveBeenCalledWith('wt1', false, false)
  })

  it('checks the delete-branch checkbox by default only when the branch is merged', async () => {
    vi.mocked(api.getWorktreeRemovalInfo).mockResolvedValue(removalInfo({ merged: true }))
    await removeWorktree(worktreeRepo())
    expect(confirmDialogWithCheckbox).toHaveBeenCalledWith(
      expect.objectContaining({ checked: true, checkboxLabel: 'Also delete branch feature' }),
    )
  })

  it('leaves the checkbox unchecked by default when the branch is not merged', async () => {
    vi.mocked(api.getWorktreeRemovalInfo).mockResolvedValue(removalInfo({ merged: false }))
    await removeWorktree(worktreeRepo())
    expect(confirmDialogWithCheckbox).toHaveBeenCalledWith(expect.objectContaining({ checked: false }))
  })

  it('uses a plain confirmation with no checkbox for a detached worktree', async () => {
    vi.mocked(api.getWorktreeRemovalInfo).mockResolvedValue(removalInfo({ detached: true, branch: '' }))
    await removeWorktree(worktreeRepo())
    expect(confirmDialogWithCheckbox).not.toHaveBeenCalled()
    expect(confirmDialog).toHaveBeenCalledWith(expect.objectContaining({ title: 'Remove worktree wt?' }))
    expect(api.removeWorktree).toHaveBeenCalledWith('wt1', false, false)
  })

  it('does nothing when the confirmation is declined', async () => {
    vi.mocked(api.getWorktreeRemovalInfo).mockResolvedValue(removalInfo())
    vi.mocked(confirmDialogWithCheckbox).mockResolvedValueOnce({ ok: false, checked: false })
    await removeWorktree(worktreeRepo())
    expect(api.removeWorktree).not.toHaveBeenCalled()
  })

  it('refuses without any confirmation when the worktree is locked', async () => {
    vi.mocked(api.getWorktreeRemovalInfo).mockResolvedValue(removalInfo({ locked: true }))
    await removeWorktree(worktreeRepo())
    expect(confirmDialogWithCheckbox).not.toHaveBeenCalled()
    expect(confirmDialog).not.toHaveBeenCalled()
    expect(api.removeWorktree).not.toHaveBeenCalled()
  })

  // The worktree is already gone by the time git refuses the branch delete
  // (RemoveWorktree removes the worktree before attempting it), so the
  // follow-up must run against the worktree's main repository, not its own
  // id, which no longer resolves to anything.
  it('offers the same force-delete confirmation as deleteBranch when the branch turns out unmerged', async () => {
    vi.mocked(api.getWorktreeRemovalInfo).mockResolvedValue(removalInfo({ merged: false }))
    vi.mocked(confirmDialogWithCheckbox).mockResolvedValueOnce({ ok: true, checked: true })
    vi.mocked(api.removeWorktree).mockRejectedValueOnce(new Error('branch is not fully merged: feature'))
    vi.mocked(confirmDialog).mockResolvedValueOnce(true)

    await removeWorktree(worktreeRepo({ parentId: 'main1' }))

    expect(api.removeWorktree).toHaveBeenCalledWith('wt1', false, true)
    expect(confirmDialog).toHaveBeenCalledWith(expect.objectContaining({ title: 'Branch not merged' }))
    expect(api.deleteBranch).toHaveBeenCalledWith('main1', 'feature', true)
  })

  it('does not offer a force-delete follow-up when the branch was not asked to be deleted', async () => {
    vi.mocked(api.getWorktreeRemovalInfo).mockResolvedValue(removalInfo({ merged: false }))
    vi.mocked(confirmDialogWithCheckbox).mockResolvedValueOnce({ ok: true, checked: false })
    vi.mocked(api.removeWorktree).mockRejectedValueOnce(new Error('branch is not fully merged: feature'))

    await removeWorktree(worktreeRepo({ parentId: 'main1' }))

    expect(api.deleteBranch).not.toHaveBeenCalled()
  })
})

describe('startCommit', () => {
  it('selects the uncommitted row and asks the commit box for focus', async () => {
    const { startCommit } = await import('./actions')
    const { focusCommitBox, selectedHash, uncommittedSelected } = await import('./stores')
    selectedHash.set('abc')
    startCommit()
    expect(get(uncommittedSelected)).toBe(true)
    expect(get(selectedHash)).toBe('')
    expect(get(focusCommitBox)).toBe(true)
  })
})
