import type { Repo } from './types'

/** Custom drag data MIME type used to identify a repo row being dragged,
 *  so drop targets can tell this drag apart from any other browser drag
 *  (e.g. dragging text or a file) and ignore it. */
export const REPO_DRAG_MIME = 'application/x-git-ui-repo'

/** Where a repo can be dropped: the loose (ungrouped) area is `''`, same
 *  as `Repo.group`'s "no group" value; anything else names a group. */
export interface RepoDropTarget {
  group: string
}

/**
 * resolveRepoDrop decides what group `repo` should end up in after being
 * dropped on `target`, or returns null when the drop is a no-op — the
 * repo is already in that exact group (or already loose, for the loose
 * area). Mirrors the group value moveRepoToGroup passes to setRepoGroup.
 */
export function resolveRepoDrop(repo: Pick<Repo, 'group'>, target: RepoDropTarget): string | null {
  const current = repo.group ?? ''
  return current === target.group ? null : target.group
}
