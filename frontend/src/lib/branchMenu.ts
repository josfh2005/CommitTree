import type { Branch } from './types'

/** The sidebar branch context menu as groups the menu separates
 *  (docs/spec/01-repositories-and-sidebar.md). Pure, so the layout, labels
 *  and disabled reasons are tested. */
export type BranchMenuId = 'checkout' | 'fetch' | 'pull' | 'push' | 'merge' | 'rebase' | 'new-branch' | 'new-tag' | 'delete'

export interface BranchMenuEntry {
  id: BranchMenuId
  label: string
  disabled?: boolean
  title?: string
  danger?: boolean
}

export interface BranchMenuOptions {
  head: string
  detached: boolean
  busy: boolean
  /** A merge, rebase or cherry-pick is in progress. */
  merging: boolean
  /** Why Rebase is disabled (rebaseBlocker), or null. */
  rebaseWhy: string | null
  /** The repository's remote names. */
  remotes: string[]
}

/** The name the merge and rebase entries use: remote/name for a remote row. */
export const branchLabel = (b: Branch) => (b.remote ? `${b.remote}/${b.name}` : b.name)

/** The remote "Fetch <remote>" fetches and "Publish" publishes to: a remote
 *  row's own, a local branch's upstream remote (the longest remote name that
 *  prefixes the upstream, since names may contain a slash), else origin, else
 *  the only remote; '' when there is no way to tell. */
export function branchRemote(b: Branch, remotes: string[]): string {
  if (b.remote) return b.remote
  if (b.upstream && !b.upstreamLocal) {
    const found = remotes.filter((r) => b.upstream.startsWith(r + '/')).sort((x, y) => y.length - x.length)[0]
    if (found) return found
  }
  if (remotes.includes('origin')) return 'origin'
  return remotes.length === 1 ? remotes[0] : ''
}

const BUSY = 'Another operation is running'

function syncReason(b: Branch, o: BranchMenuOptions): string | undefined {
  if (o.busy) return BUSY
  if (o.merging) return 'Finish the operation in progress first'
  if (b.upstreamLocal) return 'Tracks a local branch'
  return undefined
}

export function branchMenuGroups(b: Branch, o: BranchMenuOptions): BranchMenuEntry[][] {
  const label = branchLabel(b)
  const checkout: BranchMenuEntry = { id: 'checkout', label: 'Check out', disabled: b.current || !!b.worktree || o.busy }
  const remote = branchRemote(b, o.remotes)
  const fetchWhy = o.busy ? BUSY : remote ? undefined : 'No remote'
  const fetch: BranchMenuEntry = { id: 'fetch', label: remote ? `Fetch ${remote}` : 'Fetch', disabled: !!fetchWhy, title: fetchWhy }
  const integrate: BranchMenuEntry[] = [
    { id: 'merge', label: `Merge ${label} into ${o.head}`, disabled: b.current || o.busy || o.detached || o.merging },
    { id: 'rebase', label: `Rebase ${o.head} onto ${label}`, disabled: o.rebaseWhy !== null, title: o.rebaseWhy ?? undefined },
  ]
  const create: BranchMenuEntry[] = [
    { id: 'new-branch', label: 'New branch from here…' },
    { id: 'new-tag', label: 'New tag here…' },
  ]
  const del: BranchMenuEntry = { id: 'delete', label: b.remote ? 'Delete on remote…' : 'Delete…', danger: true, disabled: b.current || !!b.worktree }
  if (b.remote) return [[checkout], [fetch], integrate, create, [del]]

  // Pull needs a live upstream and a branch no other worktree holds; a
  // branch with none (or whose upstream was deleted) publishes instead.
  const pullWhy =
    syncReason(b, o) ??
    (!b.upstream ? 'No upstream' : b.upstreamGone ? 'The upstream was deleted on the remote' : b.worktree ? 'Checked out in another worktree' : undefined)
  const publishes = !b.upstream || !!b.upstreamGone
  const pushWhy = syncReason(b, o) ?? (publishes && !remote ? 'No remote' : undefined)
  const pull: BranchMenuEntry = { id: 'pull', label: `Pull ${b.name}`, disabled: !!pullWhy, title: pullWhy }
  const push: BranchMenuEntry = {
    id: 'push',
    label: publishes ? `Publish ${b.name}${remote ? ` to ${remote}` : ''}` : `Push ${b.name}`,
    disabled: !!pushWhy,
    title: pushWhy,
  }
  return [[checkout], [fetch, pull, push], integrate, create, [del]]
}
