import type { ConflictKind, MergeState } from './types'

export type MergeFileStatus = 'conflict' | 'manual' | 'unstaged' | 'staged'

export interface MergeFile {
  path: string
  status: MergeFileStatus
}

export interface MergeSection {
  title: string
  files: MergeFile[]
}

/**
 * mergeSections groups the merge's files for the panel, all read from git so
 * they survive a restart: what is still conflicted or needs a human, what is
 * settled but not staged, and what is staged for the merge commit. Empty
 * sections are left out.
 */
export function mergeSections(state: MergeState): MergeSection[] {
  if (!state.merging) return []
  const rows = (paths: string[], status: MergeFileStatus) => paths.map((path) => ({ path, status }))
  return [
    { title: 'Conflicts', files: [...rows(state.conflicts, 'conflict'), ...rows(state.manual, 'manual')] },
    { title: 'Unstaged', files: rows(state.unstaged, 'unstaged') },
    { title: 'Staged', files: rows(state.staged, 'staged') },
  ].filter((s) => s.files.length > 0)
}

/**
 * commitWarning is the question to ask before committing the merge, or null
 * when there is nothing to warn about. An unstaged file keeps our side in the
 * merge commit, silently dropping theirs.
 */
export function commitWarning(state: MergeState): string | null {
  if (state.kind && state.kind !== 'merge') return null
  const n = state.unstaged.length
  if (n === 0) return null
  const files = n === 1 ? `${state.unstaged[0]} is` : `${n} files are`
  return `${files} not staged and won't be in the merge commit, which keeps this branch's version instead. Commit anyway?`
}

/**
 * takeMessage is the confirmation before taking one side of a Manual file.
 * Taking overwrites the worktree copy, and Unstage does not bring it back.
 */
export function takeMessage(path: string, branch: string): string {
  return `Replace ${path} with ${branch}'s version, or delete it if ${branch} deleted it? Edits you made to it are lost; Unstage won't bring them back.`
}

export interface ConflictHeader {
  lead: string
  from: string
  connector: string
  into: string
  detail: string
}

// One table, so the six kinds' wording lives in one place instead of spread
// through MergeView's template.
const HEADINGS: Record<Exclude<ConflictKind, ''>, { lead: string; connector: string }> = {
  merge: { lead: 'Merging', connector: 'into' },
  rebase: { lead: 'Rebasing', connector: 'onto' },
  'cherry-pick': { lead: 'Cherry-picking', connector: 'onto' },
  revert: { lead: 'Reverting', connector: 'on' },
  am: { lead: 'Applying patch', connector: '' },
  stash: { lead: 'Resolving stashed changes', connector: '' },
}

export function conflictHeader(state: MergeState): ConflictHeader {
  const kind = state.kind || 'merge'
  const { lead, connector } = HEADINGS[kind]
  if (kind === 'stash') return { lead, from: '', connector: '', into: '', detail: '' }
  const detail =
    kind === 'rebase' && state.total
      ? `commit ${state.step} of ${state.total}${state.subject ? `: ${state.subject}` : ''}`
      : kind === 'merge'
        ? ''
        : (state.subject ?? '')
  return { lead, from: state.from, connector: state.into ? connector : '', into: state.into, detail }
}

export interface ConflictActions {
  abort: string | null
  confirm: string | null
  ai: boolean
  done: boolean
}

// "Resolve with AI" is merge-only: ResolveConflicts refuses anything without
// MERGE_HEAD, so showing the button elsewhere only offers an error. A stash
// conflict has no git-level abort or continue — it gets Done (and, when a
// conflicted Pop still owes one, Drop stash, which MergeView adds itself
// from OwedStashDrop rather than from this table).
const ACTIONS: Record<Exclude<ConflictKind, ''>, ConflictActions> = {
  merge: { abort: 'Abort merge', confirm: 'Commit merge', ai: true, done: false },
  rebase: { abort: 'Abort rebase', confirm: 'Continue rebase', ai: false, done: false },
  'cherry-pick': { abort: 'Abort cherry-pick', confirm: 'Continue cherry-pick', ai: false, done: false },
  revert: { abort: 'Abort revert', confirm: 'Continue revert', ai: false, done: false },
  am: { abort: 'Abort patch', confirm: 'Continue applying', ai: false, done: false },
  stash: { abort: null, confirm: null, ai: false, done: true },
}

export function conflictActions(state: MergeState): ConflictActions {
  return ACTIONS[state.kind || 'merge']
}

// abortWarning is the confirmation before throwing a resolution away. The
// wording has to follow the kind: "Abort merge / go back to where the
// branch was" is plainly wrong for a rebase or a cherry-pick.
export function abortWarning(state: MergeState): { title: string; message: string; confirmLabel: string } {
  const label = conflictActions(state).abort ?? 'Abort'
  const what = {
    merge: 'this merge',
    rebase: 'this rebase',
    'cherry-pick': 'this cherry-pick',
    revert: 'this revert',
    am: 'this patch',
    stash: 'this',
  }[state.kind || 'merge']
  return {
    title: label,
    message: `Throw away every resolution from ${what} and go back to where the branch was?`,
    confirmLabel: label,
  }
}
