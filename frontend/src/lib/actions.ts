import { get } from 'svelte/store'
import { api } from './api'
import { busy, chatOpen, collapsedRepoGroups, filters, loadMergeState, loadRefs, loadRepos, loadWorktreeState, logVersion, mergeState, refreshRepo, repos, selectRepo, selectedRepoId, stashConflictDismissed } from './stores'
import type { Branch, FileStatus, MergeState, Repo, ResetInfo, ResetMode, WorktreeState } from './types'
import { PULL_UP_TO_DATE, UP_TO_DATE } from './types'
import { abortWarning, commitWarning, takeMessage } from './merge'
import { resetMessage } from './reset'
import { discardMessage, neverCommitted } from './worktree'
import { stashApplyAction } from './stash'
import { choiceDialog, confirmDialog, confirmDialogWithCheckbox, errorMessage, promptDialog, toast } from './ui'
import { resolveRepoDrop } from './repoDrop'
import { classifyGroupRename, renameCollapsedGroup } from './repoGroupRename'

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

// moveRepoToGroup offers every existing group (from the other repos in the
// list — there is no separate groups table, see internal/repos) plus
// "New group…" and "No group", so choosing one covers creating, renaming
// into an existing group, and ungrouping in a single menu.
const NEW_GROUP = '__new_group__'

export async function moveRepoToGroup(repo: Repo) {
  const groupNames = [...new Set(get(repos).map((r) => r.group).filter((g): g is string => !!g))].sort((a, b) =>
    a.localeCompare(b),
  )
  const choice = await choiceDialog<string>({
    title: 'Move to group',
    label: 'Group',
    options: [
      { value: '', label: 'No group' },
      ...groupNames.map((name) => ({ value: name, label: name })),
      { value: NEW_GROUP, label: 'New group…' },
    ],
    value: repo.group || '',
    message: (v) => (v === NEW_GROUP ? `Create a new group for ${repo.name}.` : `Move ${repo.name} to a group.`),
    confirmLabel: () => 'Move',
  })
  if (choice === null) return

  let group = choice
  if (choice === NEW_GROUP) {
    const result = await promptDialog({ title: 'New group', label: 'Group name', submitLabel: 'Create' })
    group = result?.value.trim() ?? ''
    if (!group) return
  }

  try {
    await api.setRepoGroup(repo.id, group)
    await loadRepos()
  } catch (e) {
    toast(errorMessage(e), 'error')
  }
}

// dropRepoOnGroup is the drag-and-drop counterpart to moveRepoToGroup's
// menu: same decision (resolveRepoDrop) and the same App method, just
// triggered by a drop instead of a dialog choice. A no-op drop (dropped on
// the group it is already in) never calls the backend.
export async function dropRepoOnGroup(repoId: string, group: string) {
  const repo = get(repos).find((r) => r.id === repoId)
  if (!repo) return
  const next = resolveRepoDrop(repo, { group })
  if (next === null) return

  try {
    await api.setRepoGroup(repo.id, next)
    await loadRepos()
  } catch (e) {
    toast(errorMessage(e), 'error')
  }
}

// renameGroup opens the same prompt dialog moveRepoToGroup's "New group…"
// uses, pre-filled with the group's current name. Renaming into a name
// another group already has merges the two groups (a group is just a
// string on each repository — see classifyGroupRename), so that case gets
// an extra confirmation spelling out the merge before it happens. The
// group's collapsed/expanded state is carried over to the new name so the
// section doesn't jump open or shut.
export async function renameGroup(name: string) {
  const result = await promptDialog({ title: 'Rename group', label: 'Group name', value: name, submitLabel: 'Rename' })
  if (result === null) return

  const existingNames = [...new Set(get(repos).map((r) => r.group).filter((g): g is string => !!g))]
  const action = classifyGroupRename(existingNames, name, result.value)
  if (action.kind === 'noop') return

  if (action.kind === 'merge') {
    const ok = await confirmDialog({
      title: 'Merge groups',
      message: `A group named "${action.name}" already exists. Renaming "${name}" to "${action.name}" merges the two into one group named "${action.name}" — every repository from both ends up together, and this cannot be undone.`,
      confirmLabel: 'Merge',
      danger: true,
    })
    if (!ok) return
  }

  try {
    await api.renameRepoGroup(name, action.name)
    collapsedRepoGroups.update((names) => renameCollapsedGroup(names, name, action.name))
    await loadRepos()
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

export async function resetBranch(id: string, hash: string, short: string, branch: string) {
  let info: ResetInfo
  try {
    info = await api.getResetPreview(id, hash)
  } catch (e) {
    toast(errorMessage(e), 'error')
    return
  }
  const mode = await choiceDialog<ResetMode>({
    title: `Reset ${branch} to ${short}`,
    label: 'Mode',
    options: [
      { value: 'soft', label: 'Soft — keep the changes staged' },
      { value: 'mixed', label: 'Mixed — keep the changes unstaged' },
      { value: 'hard', label: 'Hard — discard the changes' },
    ],
    value: 'soft',
    message: (m) => resetMessage(m, branch, short, info),
    confirmLabel: (m) => (m === 'hard' ? 'Reset and discard' : 'Reset'),
    danger: (m) => m === 'hard',
  })
  if (mode) await run('Resetting…', () => api.resetBranch(id, hash, mode))
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
    loadWorktreeState()
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
  const state = get(mergeState)
  const warning = abortWarning(state ?? ({ kind: 'merge' } as MergeState))
  const ok = await confirmDialog({ ...warning, danger: true })
  if (ok) await run(`${warning.title}…`, () => api.abortMerge(id))
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
export async function takeMergeSide(id: string, path: string, side: 'ours' | 'theirs', branch: string) {
  const ok = await confirmDialog({
    title: side === 'ours' ? 'Take ours' : 'Take theirs',
    message: takeMessage(path, branch),
    confirmLabel: side === 'ours' ? 'Take ours' : 'Take theirs',
    danger: true,
  })
  if (ok) await run(side === 'ours' ? 'Taking ours…' : 'Taking theirs…', () => api.takeMergeSide(id, path, side))
}

export async function resolveConflicts(id: string) {
  chatOpen.set(true)
  try {
    await api.resolveConflicts(id, crypto.randomUUID())
  } catch (e) {
    toast(errorMessage(e), 'error')
  }
}

export const stageFile = (id: string, path: string) => run('Staging…', () => api.stageFile(id, path))
export const unstageFile = (id: string, path: string) => run('Unstaging…', () => api.unstageFile(id, path))

// commitChanges goes through the same run()/refreshRepo() path as every other
// mutation, so a commit bumps logVersion and reloads refs like the spec
// says, instead of only refreshing the worktree state as CommitBox used to.
export const commitChanges = (id: string, message: string, amend: boolean) =>
  run(amend ? 'Amending…' : 'Committing…', () => api.commitChanges(id, message, amend))

export const fetchRemote = (id: string) => run('Fetching…', () => api.fetch(id))

export const push = (id: string) => run('Pushing…', () => api.push(id))

export async function pull(id: string) {
  busy.set('Pulling…')
  try {
    const result = await api.pull(id)
    if (result.outcome === PULL_UP_TO_DATE) toast('Already up to date.', 'info')
  } catch (e) {
    toast(errorMessage(e), 'error')
  } finally {
    busy.set('')
    await refreshRepo()
  }
}

export async function stashChanges(id: string) {
  const result = await promptDialog({
    title: 'Stash changes',
    label: 'Message (optional)',
    checkboxLabel: 'Include untracked files',
    submitLabel: 'Stash',
  })
  if (!result) return
  await run('Stashing…', () => api.stashPush(id, result.value.trim(), result.checked))
}

export async function stashApply(id: string, index: number) {
  const result = await confirmDialogWithCheckbox({
    title: 'Apply stash',
    message: 'Apply this stash to the working tree? It stays in the list unless you choose to delete it below.',
    confirmLabel: 'Apply',
    checkboxLabel: 'Delete the stash after applying it',
  })
  if (!result.ok) return
  if (stashApplyAction(result.checked) === 'pop') await run('Popping stash…', () => api.stashPop(id, index))
  else await run('Applying stash…', () => api.stashApply(id, index))
}

// The conflict view's "Done" for a stash conflict: the files stay exactly as
// they are (conflicted or not), the view just stops owning the screen. The
// flag is cleared by loadMergeState whenever the kind changes or the
// conflict goes away, so a later stash conflict shows the view again.
export const dismissStashConflict = () => stashConflictDismissed.set(true)

export async function stashPop(id: string, index: number) {
  const ok = await confirmDialog({
    title: 'Pop stash',
    message: 'Apply this stash and remove it from the list? If it conflicts, it stays until the conflict is resolved.',
    confirmLabel: 'Pop',
  })
  if (ok) await run('Popping stash…', () => api.stashPop(id, index))
}

export async function stashDrop(id: string, index: number) {
  const ok = await confirmDialog({
    title: 'Drop stash',
    message: 'Delete this stash entry for good? This cannot be undone.',
    confirmLabel: 'Drop',
    danger: true,
  })
  if (ok) await run('Dropping stash…', () => api.stashDrop(id, index))
}

export async function discardFile(id: string, state: WorktreeState, file: FileStatus, staged: boolean) {
  // Both the wording and the labels follow the PATH's state, not the row the
  // user clicked: an "AM" file is deleted for good from either of its rows.
  const hard = neverCommitted(state, file.path)
  const ok = await confirmDialog({
    title: hard ? 'Delete file' : 'Discard changes',
    message: discardMessage(state, file, staged),
    confirmLabel: hard ? 'Delete' : 'Discard',
    danger: true,
  })
  if (ok) await run('Discarding…', () => api.discardFile(id, file.path))
}
