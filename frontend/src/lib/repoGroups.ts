import type { Repo } from './types'

export interface RepoGroup {
  name: string
  repos: Repo[]
}

export interface GroupedRepos {
  /** Repos shown on their own, at the top, exactly as the list looks with
   *  no groups defined. */
  loose: Repo[]
  /** Every group named by some repo's `group` field, sorted by name. */
  groups: RepoGroup[]
}

/**
 * groupRepos splits repos into ungrouped ("loose") and grouped, the same
 * shape groupBranches gives branches. A repo with no group (or an empty
 * one) stays loose. Input order is preserved inside each part.
 */
export function groupRepos(repos: Repo[]): GroupedRepos {
  const loose: Repo[] = []
  const byName = new Map<string, RepoGroup>()
  for (const repo of repos) {
    const group = repo.group ?? ''
    if (group === '') {
      loose.push(repo)
      continue
    }
    let entry = byName.get(group)
    if (!entry) {
      entry = { name: group, repos: [] }
      byName.set(group, entry)
    }
    entry.repos.push(repo)
  }

  return { loose, groups: [...byName.values()].sort((a, b) => a.name.localeCompare(b.name)) }
}
