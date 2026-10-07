/** The repository row's context menu, as groups the menu separates
 *  (docs/spec/01-repositories-and-sidebar.md). Pure, so the order is tested. */
export type RepoMenuId = 'locate' | 'fetch' | 'pull' | 'push' | 'push-all' | 'reveal' | 'terminal' | 'settings' | 'move' | 'remove' | 'remove-worktree'

export function repoMenuGroups(r: { missing: boolean; worktree: boolean; child: boolean }): RepoMenuId[][] {
  const sync: RepoMenuId[] = ['fetch', 'pull', 'push', 'push-all']
  const open: RepoMenuId[] = ['reveal', 'terminal']
  if (r.worktree) return [sync, open, ['remove-worktree']]
  return [
    ...(r.missing ? [['locate'] as RepoMenuId[]] : []),
    sync,
    open,
    ['settings'],
    r.child ? ['remove'] : ['move', 'remove'],
  ]
}
