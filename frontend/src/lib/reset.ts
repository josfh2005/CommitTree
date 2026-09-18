import type { ResetInfo, ResetMode } from './types'

/**
 * resetMessage is the confirmation for moving `branch` to commit `short`:
 * what happens to the undone commits' changes in each mode, and a warning
 * when some of them are already on the upstream.
 */
export function resetMessage(mode: ResetMode, branch: string, short: string, info: ResetInfo): string {
  const parts = [`Move ${branch} to ${short}?`]
  const n = info.undone
  const undone = n === 0 ? 'No commits are undone' : n === 1 ? '1 commit is undone' : `${n} commits are undone`
  const its = n === 1 ? 'its' : 'their'
  if (mode === 'hard') {
    parts.push(`${undone}, and this will discard every uncommitted change in the working tree. Undone commits stay recoverable from the reflog for a while.`)
  } else if (n === 0) {
    parts.push(`${undone}.`)
  } else if (mode === 'soft') {
    parts.push(`${undone}; ${its} changes stay staged.`)
  } else {
    parts.push(`${undone}; ${its} changes stay in the working tree, unstaged.`)
  }
  if (info.pushed > 0) {
    parts.push(`${info.pushed} of them are already on ${info.upstream}; pushing afterwards will need a force push.`)
  }
  return parts.join(' ')
}
