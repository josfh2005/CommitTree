import type { Repo } from './types'

/** A top-level repository row and the worktrees shown under it. */
export interface RepoNode {
  repo: Repo
  children: Repo[]
}

export interface RepoGroup {
  name: string
  repos: RepoNode[]
}

export interface GroupedRepos {
  /** Repos shown on their own, at the top, exactly as the list looks with
   *  no groups defined. */
  loose: RepoNode[]
  /** Every group named by some repo's `group` field, sorted by name. */
  groups: RepoGroup[]
}

/** Display order everywhere in the sidebar: name, ignoring case and
 *  accents, then path so two same-named repositories keep a stable order. */
export function compareRepos(a: Repo, b: Repo): number {
  return a.name.localeCompare(b.name, undefined, { sensitivity: 'base' }) || a.path.localeCompare(b.path)
}

/**
 * groupRepos splits repos into ungrouped ("loose") and grouped, the same
 * shape groupBranches gives branches, and hangs each worktree (an item with
 * a `parentId`) under its parent — wherever the parent is; a worktree's own
 * `group` is ignored. A `parentId` that names no listed repo leaves the item
 * at the top level. Everything is sorted with compareRepos.
 */
export function groupRepos(repos: Repo[]): GroupedRepos {
  const ids = new Set(repos.map((r) => r.id))
  const children = new Map<string, Repo[]>()
  const tops: Repo[] = []
  for (const repo of repos) {
    if (repo.parentId && repo.parentId !== repo.id && ids.has(repo.parentId)) {
      const list = children.get(repo.parentId) ?? []
      list.push(repo)
      children.set(repo.parentId, list)
    } else {
      tops.push(repo)
    }
  }

  const loose: RepoNode[] = []
  const byName = new Map<string, RepoGroup>()
  for (const repo of tops.sort(compareRepos)) {
    const node = { repo, children: (children.get(repo.id) ?? []).sort(compareRepos) }
    const group = repo.group ?? ''
    if (group === '') {
      loose.push(node)
      continue
    }
    let entry = byName.get(group)
    if (!entry) {
      entry = { name: group, repos: [] }
      byName.set(group, entry)
    }
    entry.repos.push(node)
  }

  return { loose, groups: [...byName.values()].sort((a, b) => a.name.localeCompare(b.name)) }
}
