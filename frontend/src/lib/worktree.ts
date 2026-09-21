import type { FileStatus, WorktreeState } from './types'

export interface WorktreeSection {
  title: string
  files: FileStatus[]
}

/** worktreeSections groups the change lists for the file list, in the order
 *  the user acts on them: what will be committed, then what will not. */
export function worktreeSections(state: WorktreeState): WorktreeSection[] {
  return [
    { title: 'Staged', files: state.staged },
    { title: 'Unstaged', files: state.unstaged },
    { title: 'Untracked', files: state.untracked },
  ].filter((s) => s.files.length > 0)
}

/** changedCount is what the sidebar row shows: distinct paths, so a file that
 *  is both staged and modified counts once. */
export function changedCount(state: WorktreeState | null): number {
  if (!state) return 0
  const paths = new Set<string>()
  for (const list of [state.staged, state.unstaged, state.untracked]) {
    for (const f of list) paths.add(f.path)
  }
  return paths.size
}

/** discardMessage is the confirmation before throwing changes away — the only
 *  destructive action here, and for an untracked file git cannot undo it. */
export function discardMessage(file: FileStatus, staged: boolean): string {
  if (file.status === '?') {
    return `This will delete ${file.path} for good — it was never committed, so this cannot be undone.`
  }
  const also = staged ? ' Its staged changes are thrown away too.' : ''
  return `Discard your changes to ${file.path}?${also} This cannot be undone.`
}
