import type { MergeState } from './types'

export type MergeFileStatus = 'conflict' | 'manual' | 'resolved'

export interface MergeFile {
  path: string
  status: MergeFileStatus
}

/**
 * mergeFiles lists the merge's files for the panel: what is still conflicted,
 * what needs a human, and what has already been resolved. `started` is the
 * conflict list from when the merge began, which is the only way to know a
 * file was resolved during it; after a restart it is empty and only the
 * outstanding files are shown.
 */
export function mergeFiles(state: MergeState, started: string[]): MergeFile[] {
  if (!state.merging) return []
  const rows: MergeFile[] = state.conflicts.map((path) => ({ path, status: 'conflict' as const }))
  rows.push(...state.manual.map((path) => ({ path, status: 'manual' as const })))
  const outstanding = new Set([...state.conflicts, ...state.manual])
  rows.push(...started.filter((path) => !outstanding.has(path)).map((path) => ({ path, status: 'resolved' as const })))
  return rows
}
