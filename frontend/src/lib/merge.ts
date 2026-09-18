import type { MergeState } from './types'

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
