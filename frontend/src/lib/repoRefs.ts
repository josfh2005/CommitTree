import type { Refs, StashEntry } from './types'

export interface RepoRefsData {
  refs: Refs | null
  stash: StashEntry[]
}

/**
 * refsView is what one expanded repository's Branches/Remotes/Tags/Stash
 * sections show. The selected repository reads the live stores (refs,
 * stashEntries), which every action refreshes; any other expanded one reads
 * its own entry in sideRefs — never the selected repository's data — and
 * shows nothing until that has loaded.
 */
export function refsView(repoId: string, selectedId: string, selected: RepoRefsData, side: Record<string, RepoRefsData>): RepoRefsData {
  if (repoId === selectedId) return selected
  return side[repoId] ?? { refs: null, stash: [] }
}
