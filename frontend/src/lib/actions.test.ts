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
    mergeBranch: vi.fn().mockResolvedValue({ outcome: 0 }),
    applyHunkSelection: vi.fn().mockResolvedValue(undefined),
    setRepoGroup: vi.fn().mockResolvedValue(undefined),
    reorderRepos: vi.fn().mockResolvedValue(undefined),
    chooseRegionOption: vi.fn(),
    push: vi.fn().mockResolvedValue(undefined),
    pushAll: vi.fn().mockResolvedValue([]),
    fetchRemote: vi.fn().mockResolvedValue(undefined),
    fastForwardBranch: vi.fn().mockResolvedValue(true),
    pushBranch: vi.fn(),
    pull: vi.fn(),
    notify: vi.fn().mockResolvedValue(undefined),
  },
}))

vi.mock('./ui', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./ui')>()
  return {
    ...actual,
    confirmDialog: vi.fn().mockResolvedValue(true),
    confirmDialogWithCheckbox: vi.fn().mockResolvedValue({ ok: true, checked: false }),
    pickDialog: vi.fn().mockResolvedValue(null),
    choiceDialog: vi.fn().mockResolvedValue(null),
    resultsDialog: vi.fn().mockResolvedValue(undefined),
  }
})

import { applyHunkSelection, chooseRegionOption, commitMerge, fetchBranchRemote, mergeBranch, pull, pullBranch, push, pushAll, pushBranch, placeRepo, placeRepoGroup, setRepoSortOrder, deleteBranch, deleteTag, removeWorktree, skipStep } from './actions'
import { api } from './api'
import { filters, gitSettings, mergeState, repos, selectedRepoId } from './stores'
import { windowFocused } from './notify'
import type { Branch, Filters, MergeState, Refs, Repo, WorktreeRemovalInfo } from './types'
import { choiceDialog, confirmDialog, confirmDialogWithCheckbox, resultsDialog, toasts } from './ui'

function mergeStateOf(over: Partial<MergeState>): MergeState {
  return { kind: 'rebase', merging: true, from: 'feature', into: 'main', conflicts: [], manual: [], staged: [], unstaged: [], ...over }
}

function emptyFilters(): Filters {
  return { text: '', branch: '', author: '', since: '', until: '', paths: [] }
}

function branch(name: string, remote = ''): Branch {
  return { name, remote, hash: 'h', current: false, upstream: '', official: false }
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

  it('commits a merge with the edited message and reports success', async () => {
    mergeState.set(mergeStateOf({ kind: 'merge' }))
    vi.mocked(api.commitMerge).mockResolvedValue(undefined)

    expect(await commitMerge('repo1', 'Merge feature: keep both')).toBe(true)
    expect(api.commitMerge).toHaveBeenCalledWith('repo1', 'Merge feature: keep both')
  })

  it('reports failure so the message editor stays open', async () => {
    mergeState.set(mergeStateOf({ kind: 'merge' }))
    vi.mocked(api.commitMerge).mockRejectedValue(new Error('the commit message is empty'))

    expect(await commitMerge('repo1', ' ')).toBe(false)
  })

  it('reports a cancelled warning as not committed', async () => {
    mergeState.set(mergeStateOf({ kind: 'merge', unstaged: ['a.txt'] }))
    vi.mocked(confirmDialog).mockResolvedValue(false)

    expect(await commitMerge('repo1', 'msg')).toBe(false)
    expect(api.commitMerge).not.toHaveBeenCalled()
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
    focusCommitBox.set(false)
    uncommittedSelected.set(false)
  })
})

describe('pickAndMerge', () => {
  const b = (name: string, remote = '', current = false) => ({ name, remote, hash: 'h', current, upstream: '', official: false })
  const withRefs = async (detached = false) => {
    const { refs } = await import('./stores')
    refs.set({ head: 'main', headHash: 'abc', detached, local: [b('main', '', true), b('dev')], remotes: [{ name: 'origin', branches: [b('HEAD', 'origin'), b('feature', 'origin')] }], tags: [] })
  }
  beforeEach(async () => {
    const { api } = await import('./api')
    const ui = await import('./ui')
    vi.mocked(api.mergeBranch).mockClear()
    vi.mocked(ui.pickDialog).mockReset().mockResolvedValue(null)
  })

  it('merges the chosen remote branch into the current one', async () => {
    const { pickAndMerge } = await import('./actions')
    const { api } = await import('./api')
    const ui = await import('./ui')
    await withRefs()
    vi.mocked(ui.pickDialog).mockResolvedValue('origin/feature')
    await pickAndMerge('r1')
    expect(vi.mocked(ui.pickDialog).mock.calls[0][0]).toMatchObject({ title: 'Merge into main', submitLabel: 'Merge' })
    expect(vi.mocked(ui.pickDialog).mock.calls[0][0].items.map((i) => i.key)).toEqual(['dev', 'origin/feature'])
    expect(api.mergeBranch).toHaveBeenCalledWith('r1', 'origin/feature')
  })

  it('does nothing when the picker is cancelled', async () => {
    const { pickAndMerge } = await import('./actions')
    const { api } = await import('./api')
    await withRefs()
    await pickAndMerge('r1')
    expect(api.mergeBranch).not.toHaveBeenCalled()
  })

  it('never opens the picker on a detached HEAD', async () => {
    const { pickAndMerge } = await import('./actions')
    const ui = await import('./ui')
    await withRefs(true)
    await pickAndMerge('r1')
    expect(ui.pickDialog).not.toHaveBeenCalled()
  })
})

describe('applyHunkSelection', () => {
  // Undo always restores the repository's latest discard, so an older
  // discard's toast must not stay around offering to bring its own back.
  it('keeps only the latest discard toast of a repository', async () => {
    toasts.set([])
    await applyHunkSelection('r1', 'a.txt', false, 'h', [{ hunk: 0, lines: [] }], 'discard', '1 hunk')
    await applyHunkSelection('r1', 'b.txt', false, 'h', [{ hunk: 0, lines: [1] }], 'discard', '1 line')
    const undo = get(toasts).filter((t) => t.action?.label === 'Undo')
    expect(undo.map((t) => t.message)).toEqual(['Discarded 1 line in b.txt'])
  })
})

describe('manual repository order', () => {
  const repo = (id: string, group = ''): Repo => ({ id, name: id, path: `/r/${id}`, missing: false, branch: 'main', ...(group ? { group } : {}) }) as Repo

  it('starts the first manual arrangement from the by-name order, once', async () => {
    const { repos, repoSortOrder, repoManualSeeded } = await import('./stores')
    repoManualSeeded.set(false)
    repos.set([repo('b'), repo('a')])
    vi.mocked(api.reorderRepos).mockClear()
    await setRepoSortOrder('manual')
    expect(api.reorderRepos).toHaveBeenCalledWith(['a', 'b'])
    expect(get(repoSortOrder)).toBe('manual')
    await setRepoSortOrder('name')
    await setRepoSortOrder('manual')
    expect(api.reorderRepos).toHaveBeenCalledTimes(1)
  })

  it('places a repository in another group at the drop spot', async () => {
    const { repos } = await import('./stores')
    repos.set([repo('a'), repo('c', 'work'), repo('d', 'work')])
    vi.mocked(api.reorderRepos).mockClear()
    await placeRepo('a', { group: 'work', beforeId: 'd' })
    expect(api.setRepoGroup).toHaveBeenCalledWith('a', 'work')
    expect(api.reorderRepos).toHaveBeenCalledWith(['c', 'a', 'd'])
  })

  it('moves a group as a block', async () => {
    const { repos } = await import('./stores')
    repos.set([repo('a', 'work'), repo('b', 'home')])
    vi.mocked(api.reorderRepos).mockClear()
    await placeRepoGroup('home', 'work')
    expect(api.reorderRepos).toHaveBeenCalledWith(['b', 'a'])
  })
})

describe('chooseRegionOption', () => {
  beforeEach(() => toasts.set([]))
  it('applies the pick and names the staged file', async () => {
    vi.mocked(api.chooseRegionOption).mockResolvedValue({ left: 0, staged: true })
    expect(await chooseRegionOption('r', 'c1', 0, '', 'config/settings.json')).toBe(true)
    expect(api.chooseRegionOption).toHaveBeenCalledWith('r', 'c1', 0, '')
    expect(get(toasts).map((t) => t.message)).toContain('config/settings.json resolved and staged')
  })
  it('says a region was resolved when regions are left', async () => {
    vi.mocked(api.chooseRegionOption).mockResolvedValue({ left: 1, staged: false })
    expect(await chooseRegionOption('r', 'c1', -1, 'mine\n', 'a.txt')).toBe(true)
    expect(get(toasts).map((t) => t.message)).toContain('Region resolved')
  })
  it('says nothing when the region was already settled another way', async () => {
    vi.mocked(api.chooseRegionOption).mockResolvedValue({ left: 0, staged: false, settled: true })
    expect(await chooseRegionOption('r', 'c1', 0, '', 'a.txt')).toBe(true)
    expect(get(toasts)).toEqual([])
  })
  it('shows the error and reports failure', async () => {
    vi.mocked(api.chooseRegionOption).mockRejectedValue(new Error('the AI is busy'))
    expect(await chooseRegionOption('r', 'c1', 0, '', 'a.txt')).toBe(false)
    expect(get(toasts).some((t) => t.kind === 'error' && t.message.includes('the AI is busy'))).toBe(true)
  })
})

describe('operations notify', () => {
  beforeEach(() => {
    toasts.set([])
    vi.mocked(api.notify).mockClear()
    repos.set([{ id: 'r1', name: 'alpha', path: '/a', missing: false, branch: 'main' } as Repo, { id: 'r2', name: 'beta', path: '/b', missing: false, branch: 'main' } as Repo])
    selectedRepoId.set('r1')
    windowFocused.set(false)
  })

  it('a slow push notifies finished', async () => {
    const now = vi.spyOn(Date, 'now').mockReturnValueOnce(0).mockReturnValueOnce(12_000)
    await push('r1')
    now.mockRestore()
    expect(vi.mocked(api.notify).mock.calls[0][0]).toMatchObject({ id: 'r1:done', body: 'Push finished · 12 s' })
  })

  it('a failed push on another repository: one error toast with its name, plus the problem notification', async () => {
    vi.mocked(api.push).mockRejectedValueOnce(new Error('fatal: Authentication failed'))
    await push('r2')
    expect(get(toasts).map((t) => [t.message, t.kind])).toEqual([['beta: fatal: Authentication failed', 'error']])
    expect(vi.mocked(api.notify).mock.calls[0][0]).toMatchObject({ id: 'r2:problem', body: 'Push failed: fatal: Authentication failed' })
  })

  it('a pull with conflicts notifies the conflicts, not finished', async () => {
    vi.mocked(api.pull).mockResolvedValueOnce({ outcome: 2, conflicts: ['a.txt'] })
    await pull('r1')
    expect(api.notify).toHaveBeenCalledTimes(1)
    expect(vi.mocked(api.notify).mock.calls[0][0]).toMatchObject({ id: 'r1:problem', body: 'Conflicts in 1 file after the pull' })
  })

  it('a merge with conflicts notifies the conflicts', async () => {
    vi.mocked(api.mergeBranch).mockResolvedValueOnce({ outcome: 2, conflicts: ['a', 'b'] })
    await mergeBranch('r1', { name: 'feature' } as Branch, 'main')
    expect(vi.mocked(api.notify).mock.calls[0][0]).toMatchObject({ body: 'Conflicts in 2 files after the merge' })
  })
})

describe('push follows the push scope', () => {
  const ahead = (over: Partial<Refs> = {}): Refs => ({
    head: 'main', headHash: 'h', detached: false, remotes: [], tags: [],
    local: [
      { name: 'main', remote: '', hash: 'h', current: true, upstream: 'origin/main', official: true },
      { name: 'develop', remote: '', hash: 'h', current: false, upstream: 'origin/develop', ahead: 2, official: true },
      { name: 'feature', remote: '', hash: 'h', current: false, upstream: 'origin/feature', ahead: 2, official: false },
    ],
    ...over,
  })
  beforeEach(() => {
    toasts.set([])
    vi.mocked(api.push).mockClear()
    vi.mocked(api.pushAll).mockReset().mockResolvedValue([])
    vi.mocked(api.getRefs).mockReset().mockResolvedValue(null as unknown as Refs)
    vi.mocked(choiceDialog).mockReset().mockResolvedValue(null)
    vi.mocked(resultsDialog).mockClear()
    repos.set([{ id: 'r1', name: 'alpha', path: '/a', missing: false, branch: 'main' } as Repo, { id: 'r2', name: 'beta', path: '/b', missing: false, branch: 'main' } as Repo])
    selectedRepoId.set('r1')
    gitSettings.set({ pullStrategy: 'auto', pushScope: 'ask' })
  })

  it('pushes the current branch without asking when no other branch is ahead', async () => {
    await push('r1')
    expect(choiceDialog).not.toHaveBeenCalled()
    expect(api.push).toHaveBeenCalledWith('r1')
  })

  it('asks with the other repository\'s refs and names it', async () => {
    vi.mocked(api.getRefs).mockResolvedValue(ahead())
    await push('r2')
    expect(api.getRefs).toHaveBeenCalledWith('r2')
    expect(vi.mocked(choiceDialog).mock.calls[0][0]).toMatchObject({ title: 'Push beta' })
    expect(api.push).not.toHaveBeenCalled() // cancelled
    expect(api.pushAll).not.toHaveBeenCalled()
  })

  it('runs the branch chosen in the dialog', async () => {
    vi.mocked(api.getRefs).mockResolvedValue(ahead())
    vi.mocked(choiceDialog).mockResolvedValue('all')
    await push('r1')
    expect(api.pushAll).toHaveBeenCalledWith('r1')
  })

  it('a fixed setting never asks', async () => {
    vi.mocked(api.getRefs).mockResolvedValue(ahead())
    gitSettings.set({ pullStrategy: 'auto', pushScope: 'current' })
    await push('r1')
    gitSettings.set({ pullStrategy: 'auto', pushScope: 'all' })
    await push('r1')
    expect(choiceDialog).not.toHaveBeenCalled()
    expect(api.push).toHaveBeenCalledTimes(1)
    expect(api.pushAll).toHaveBeenCalledTimes(1)
  })

  it('toasts a push of the main branches in which nothing failed', async () => {
    vi.mocked(api.pushAll).mockResolvedValue([
      { branch: 'main', target: 'origin/main', status: 'upToDate' },
      { branch: 'feature', target: 'origin/feature', status: 'pushed' },
    ])
    expect(await pushAll('r1')).toBe(true)
    expect(get(toasts).map((t) => [t.message, t.kind])).toEqual([['Pushed feature, 1 already up to date', 'info']])
    expect(resultsDialog).not.toHaveBeenCalled()
  })

  it('names another repository in the toast', async () => {
    await pushAll('r2')
    expect(get(toasts).map((t) => t.message)).toEqual(['beta: Nothing to push'])
  })

  it('shows the results dialog after a partial failure, not an error toast', async () => {
    windowFocused.set(false)
    vi.mocked(api.notify).mockClear()
    vi.mocked(api.pushAll).mockResolvedValue([
      { branch: 'main', target: 'origin/main', status: 'rejected', reason: "The remote has commits you don't have — pull main first" },
      { branch: 'feature', target: 'origin/feature', status: 'pushed' },
    ])
    expect(await pushAll('r1')).toBe(false)
    expect(get(toasts)).toEqual([])
    expect(vi.mocked(resultsDialog).mock.calls[0][0]).toMatchObject({ title: 'Push results — alpha' })
    expect(vi.mocked(resultsDialog).mock.calls[0][0].rows).toHaveLength(2)
    expect(vi.mocked(api.notify).mock.calls[0][0]).toMatchObject({ id: 'r1:problem', body: 'Push failed: 1 of 2 branches were not pushed' })
  })

  it('a push of the main branches that cannot start shows the error toast', async () => {
    vi.mocked(api.pushAll).mockRejectedValue(new Error('another operation is running'))
    expect(await pushAll('r1')).toBe(false)
    expect(get(toasts).map((t) => t.kind)).toEqual(['error'])
    expect(resultsDialog).not.toHaveBeenCalled()
  })
})

describe('the branch menu\'s Fetch, Pull and Push', () => {
  const feature = (over: Partial<Branch> = {}): Branch => ({ name: 'feature', remote: '', hash: 'h', current: false, upstream: 'origin/feature', official: false, ...over })
  beforeEach(() => {
    toasts.set([])
    vi.mocked(api.fetchRemote).mockReset().mockResolvedValue(undefined)
    vi.mocked(api.fastForwardBranch).mockReset().mockResolvedValue(true)
    vi.mocked(api.pushBranch).mockReset().mockResolvedValue({ branch: 'feature', target: 'origin/feature', status: 'pushed' })
    vi.mocked(api.pull).mockReset().mockResolvedValue({ outcome: 0, conflicts: [] })
    vi.mocked(api.getRefs).mockClear()
    const list = [{ id: 'r1', name: 'alpha', path: '/a', missing: false, branch: 'main' } as Repo, { id: 'r2', name: 'beta', path: '/b', missing: false, branch: 'main' } as Repo]
    vi.mocked(api.listRepos).mockReset().mockResolvedValue(list)
    repos.set(list)
    selectedRepoId.set('r1')
  })

  it('fetches the named remote and refreshes the refs', async () => {
    await fetchBranchRemote('r1', 'backup')
    expect(api.fetchRemote).toHaveBeenCalledWith('r1', 'backup')
    expect(api.getRefs).toHaveBeenCalled()
  })

  it('a failed fetch toasts the error', async () => {
    vi.mocked(api.fetchRemote).mockRejectedValue(new Error('network down'))
    await fetchBranchRemote('r1', 'origin')
    expect(get(toasts).map((t) => [t.message, t.kind])).toEqual([['network down', 'error']])
  })

  it('pulls the current branch with the existing pull', async () => {
    await pullBranch('r1', feature({ current: true }))
    expect(api.pull).toHaveBeenCalledWith('r1')
    expect(api.fastForwardBranch).not.toHaveBeenCalled()
  })

  it('fast-forwards another branch without a pull', async () => {
    await pullBranch('r1', feature())
    expect(api.fastForwardBranch).toHaveBeenCalledWith('r1', 'feature')
    expect(api.pull).not.toHaveBeenCalled()
    expect(api.getRefs).toHaveBeenCalled()
    expect(get(toasts).map((t) => [t.message, t.kind])).toEqual([['Pulled feature', 'info']])
  })

  it('says so when the branch had nothing to pull', async () => {
    vi.mocked(api.fastForwardBranch).mockResolvedValue(false)
    await pullBranch('r1', feature())
    expect(get(toasts).map((t) => t.message)).toEqual(['feature is up to date'])
  })

  it('names another repository in the pull toast', async () => {
    await pullBranch('r2', feature())
    expect(get(toasts).map((t) => t.message)).toEqual(['beta: Pulled feature'])
  })

  it('a diverged branch toasts the error', async () => {
    vi.mocked(api.fastForwardBranch).mockRejectedValue('feature has diverged — check it out to pull')
    await pullBranch('r1', feature())
    expect(get(toasts).map((t) => [t.message, t.kind])).toEqual([['feature has diverged — check it out to pull', 'error']])
  })

  it('pushes one branch and toasts it', async () => {
    expect(await pushBranch('r1', feature())).toBe(true)
    expect(api.pushBranch).toHaveBeenCalledWith('r1', 'feature')
    expect(api.getRefs).toHaveBeenCalled()
    expect(get(toasts).map((t) => [t.message, t.kind])).toEqual([['Pushed feature', 'info']])
  })

  it('names another repository in the push toast', async () => {
    await pushBranch('r2', feature())
    expect(get(toasts).map((t) => t.message)).toEqual(['beta: Pushed feature'])
  })

  it('a branch already on the remote says so', async () => {
    vi.mocked(api.pushBranch).mockResolvedValue({ branch: 'feature', target: 'origin/feature', status: 'upToDate' })
    await pushBranch('r1', feature())
    expect(get(toasts).map((t) => t.message)).toEqual(['Everything up to date'])
  })

  it('a rejected push toasts the pull-first reason as an error', async () => {
    vi.mocked(api.pushBranch).mockResolvedValue({
      branch: 'feature', target: 'origin/feature', status: 'rejected', reason: "The remote has commits you don't have — pull feature first",
    })
    expect(await pushBranch('r1', feature())).toBe(false)
    expect(get(toasts).map((t) => [t.message, t.kind])).toEqual([["The remote has commits you don't have — pull feature first", 'error']])
  })
})
