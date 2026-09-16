import { get } from 'svelte/store'
import { api } from './api'
import { busy, loadRefs, loadRepos, logVersion, refreshRepo, selectRepo, selectedRepoId } from './stores'
import type { Branch, Repo } from './types'
import { confirmDialog, errorMessage, promptDialog, toast } from './ui'

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
    if (ok) await run('Deleting remote branch…', () => api.deleteRemoteBranch(id, branch.remote, branch.name))
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
    if (force) await run('Deleting branch…', () => api.deleteBranch(id, branch.name, true))
    return
  } finally {
    busy.set('')
  }
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
  if (ok) await run('Deleting tag…', () => api.deleteTag(id, name))
}

// Reloads refs and log when the repo changed outside the app while the window
// was in the background.
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
