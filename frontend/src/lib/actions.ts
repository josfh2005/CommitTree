import { get } from 'svelte/store'
import { api } from './api'
import { busy, chatOpen, filters, loadMergeState, loadRefs, loadRepos, logVersion, mergeState, refreshRepo, selectRepo, selectedRepoId } from './stores'
import type { Branch, Repo, ResetInfo, ResetMode } from './types'
import { UP_TO_DATE } from './types'
import { commitWarning } from './merge'
import { resetMessage } from './reset'
import { confirmDialog, errorMessage, promptDialog, toast } from './ui'

export function branchRef(branch: Branch): string {
  return branch.remote ? `refs/remotes/${branch.remote}/${branch.name}` : `refs/heads/${branch.name}`
}

// Clears the branch filter when it points at a ref that was just deleted, so
// the log doesn't keep asking the backend for a ref that no longer exists.
function clearBranchFilter(ref: string) {
  filters.update((f) => (f.branch === ref ? { ...f, branch: '' } : f))
}

async function run(label: string, fn: () => Promise<unknown>): Promise<boolean> {
  busy.set(label)
  try {
    await fn()
    return true
  } catch (e) {
    toast(errorMessage(e), 'error')
    return false
  } finally {
    busy.set('')
    await refreshRepo()
  }
}

export async function addRepo() {
  try {
    const repo = await api.addRepo()
    if (!repo.id) return
    await loadRepos()
    selectRepo(repo.id)
  } catch (e) {
    toast(errorMessage(e), 'error')
  }
}

export async function removeRepo(repo: Repo) {
  const ok = await confirmDialog({
    title: 'Remove repository',
    message: `Remove ${repo.name} from the list? Files on disk are not touched.`,
    confirmLabel: 'Remove',
  })
  if (!ok) return
  try {
    await api.removeRepo(repo.id)
    if (get(selectedRepoId) === repo.id) selectedRepoId.set('')
    await loadRepos()
    await loadRefs()
  } catch (e) {
    toast(errorMessage(e), 'error')
  }
}

export async function relocateRepo(id: string) {
  try {
    const repo = await api.relocateRepo(id)
    if (!repo.id) return
    await refreshRepo()
  } catch (e) {
    toast(errorMessage(e), 'error')
  }
}

export const fetchRepo = (id: string) => run('Fetching…', () => api.fetch(id))
export const pullRepo = (id: string) => run('Pulling…', () => api.pull(id))

export const checkoutBranch = (id: string, branch: Branch) =>
  run('Checking out…', () =>
    branch.remote ? api.checkoutRemote(id, branch.remote, branch.name) : api.checkout(id, branch.name))

export async function checkoutCommit(id: string, hash: string) {
  const ok = await confirmDialog({
    title: 'Check out commit',
    message: `HEAD will be detached at ${hash.slice(0, 8)}. New commits made there won't belong to any branch.`,
    confirmLabel: 'Check out',
  })
  if (ok) await run('Checking out…', () => api.checkoutDetached(id, hash))
}

export async function resetBranch(id: string, hash: string, short: string, branch: string, mode: ResetMode) {
  let info: ResetInfo
  try {
    info = await api.getResetPreview(id, hash)
  } catch (e) {
    toast(errorMessage(e), 'error')
    return
  }
  const ok = await confirmDialog({
    title: `Reset ${branch} (${mode})`,
    message: resetMessage(mode, branch, short, info),
    confirmLabel: mode === 'hard' ? 'Reset and discard' : 'Reset',
    danger: mode === 'hard',
  })
  if (ok) await run('Resetting…', () => api.resetBranch(id, hash, mode))
}

export async function newBranch(id: string, target: string, targetLabel: string) {
  const result = await promptDialog({
    title: 'New branch',
    label: `Branch name (from ${targetLabel})`,
    checkboxLabel: 'Check out after creating',
    checked: true,
    submitLabel: 'Create',
  })
  const name = result?.value.trim()
  if (!result || !name) return
  await run('Creating branch…', () => api.createBranch(id, name, target, result.checked))
}

export async function deleteBranch(id: string, branch: Branch) {
  if (branch.remote) {
    const ok = await confirmDialog({
      title: 'Delete remote branch',
      message: `Delete ${branch.name} on remote "${branch.remote}"? This affects everyone who uses that remote.`,
      confirmLabel: 'Delete on remote',
      danger: true,
    })
    if (ok) {
      const deleted = await run('Deleting remote branch…', () => api.deleteRemoteBranch(id, branch.remote, branch.name))
      if (deleted) clearBranchFilter(branchRef(branch))
    }
    return
  }

  const ok = await confirmDialog({
    title: 'Delete branch',
    message: `Delete local branch ${branch.name}?`,
    confirmLabel: 'Delete',
    danger: true,
  })
  if (!ok) return
  busy.set('Deleting branch…')
  try {
    await api.deleteBranch(id, branch.name, false)
  } catch (e) {
    const message = errorMessage(e)
    busy.set('')
    if (!message.includes('not fully merged')) {
      toast(message, 'error')
      return
    }
    const force = await confirmDialog({
      title: 'Branch not merged',
      message: `${branch.name} has commits that are not merged into any other branch. Force deleting loses them.`,
      confirmLabel: 'Force delete',
      danger: true,
    })
    if (force) {
      const deleted = await run('Deleting branch…', () => api.deleteBranch(id, branch.name, true))
      if (deleted) clearBranchFilter(branchRef(branch))
    }
    return
  } finally {
    busy.set('')
  }
  clearBranchFilter(branchRef(branch))
  await refreshRepo()
}

export async function newTag(id: string, target: string, targetLabel: string) {
  const result = await promptDialog({
    title: 'New tag',
    label: `Tag name (at ${targetLabel})`,
    secondLabel: 'Message (optional, creates an annotated tag)',
    submitLabel: 'Create',
  })
  const name = result?.value.trim()
  if (!result || !name) return
  await run('Creating tag…', () => api.createTag(id, name, target, result.second.trim()))
}

export async function deleteTag(id: string, name: string) {
  const ok = await confirmDialog({
    title: 'Delete tag',
    message: `Delete local tag ${name}? Tags already pushed stay on the remote.`,
    confirmLabel: 'Delete',
    danger: true,
  })
  if (ok) {
    const deleted = await run('Deleting tag…', () => api.deleteTag(id, name))
    if (deleted) clearBranchFilter(`refs/tags/${name}`)
  }
}

// Reloads refs and log when the repo changed outside the app while the window
// was in the background. The merge state is reloaded on every focus: the
// fingerprint covers refs and HEAD only, so a `git add` or `git merge --abort`
// in a terminal would never reach the merge view through it.
export function startFocusRefresh(): () => void {
  let known = ''
  let knownId = ''

  const remember = async () => {
    const id = get(selectedRepoId)
    if (!id) return
    try {
      known = await api.fingerprint(id)
      knownId = id
    } catch {
      known = ''
    }
  }

  const onFocus = async () => {
    const id = get(selectedRepoId)
    if (!id) return
    loadMergeState()
    try {
      const current = await api.fingerprint(id)
      if (id === knownId && known && current !== known) await refreshRepo()
      known = current
      knownId = id
    } catch {
      // Missing repo: the sidebar already shows it.
    }
  }

  const stopVersion = logVersion.subscribe(() => remember())
  const stopRepo = selectedRepoId.subscribe(() => remember())
  window.addEventListener('focus', onFocus)
  return () => {
    stopVersion()
    stopRepo()
    window.removeEventListener('focus', onFocus)
  }
}

export async function mergeBranch(id: string, branch: Branch, into: string) {
  const label = branch.remote ? `${branch.remote}/${branch.name}` : branch.name
  const ok = await confirmDialog({
    title: 'Merge branch',
    message: `Merge ${label} into ${into}? A merge commit is always created.`,
    confirmLabel: 'Merge',
  })
  if (!ok) return
  busy.set('Merging…')
  try {
    const result = await api.mergeBranch(id, label)
    if (result.outcome === UP_TO_DATE) toast(`${into} is already up to date with ${label}.`, 'info')
  } catch (e) {
    toast(errorMessage(e), 'error')
  } finally {
    busy.set('')
    await refreshRepo()
  }
}

export async function abortMerge(id: string) {
  const ok = await confirmDialog({
    title: 'Abort merge',
    message: 'Throw away every resolution from this merge and go back to where the branch was?',
    confirmLabel: 'Abort merge',
    danger: true,
  })
  if (ok) await run('Aborting merge…', () => api.abortMerge(id))
}

export async function commitMerge(id: string) {
  const state = get(mergeState)
  const warning = state ? commitWarning(state) : null
  if (warning) {
    const ok = await confirmDialog({ title: 'Commit merge', message: warning, confirmLabel: 'Commit anyway', danger: true })
    if (!ok) return
  }
  await run('Committing merge…', () => api.commitMerge(id))
}

export const stageMergeFile = (id: string, path: string) => run('Staging…', () => api.stageMergeFile(id, path))
export const unstageMergeFile = (id: string, path: string) => run('Unstaging…', () => api.unstageMergeFile(id, path))
export const takeMergeSide = (id: string, path: string, side: 'ours' | 'theirs') =>
  run(side === 'ours' ? 'Taking ours…' : 'Taking theirs…', () => api.takeMergeSide(id, path, side))

export async function resolveConflicts(id: string) {
  chatOpen.set(true)
  try {
    await api.resolveConflicts(id, crypto.randomUUID())
  } catch (e) {
    toast(errorMessage(e), 'error')
  }
}
