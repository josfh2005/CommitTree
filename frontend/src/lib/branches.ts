import type { Branch } from './types'

export interface BranchGroup {
  name: string
  branches: Branch[]
  hasCurrent: boolean
}

export interface GroupedBranches {
  /** Branches shown on their own, with their full name. */
  loose: Branch[]
  /** Every first path segment among branches with a slash, sorted by name. */
  groups: BranchGroup[]
}

/** groupOf returns the part before the first slash, or '' when there is none. */
export function groupOf(name: string): string {
  const slash = name.indexOf('/')
  return slash <= 0 ? '' : name.slice(0, slash)
}

/** leafName strips the group prefix, keeping any deeper path as-is. */
export function leafName(name: string, group: string): string {
  return group === '' ? name : name.slice(group.length + 1)
}

/**
 * groupBranches puts every branch whose name contains a slash ("fix/abc",
 * "release/only-one") into a group keyed by its first path segment, even
 * when that group holds a single branch. A branch with no slash stays
 * loose. Input order is preserved inside each part.
 */
export function groupBranches(branches: Branch[]): GroupedBranches {
  const loose: Branch[] = []
  const byName = new Map<string, BranchGroup>()
  for (const branch of branches) {
    const group = groupOf(branch.name)
    if (group === '') {
      loose.push(branch)
      continue
    }
    let entry = byName.get(group)
    if (!entry) {
      entry = { name: group, branches: [], hasCurrent: false }
      byName.set(group, entry)
    }
    entry.branches.push(branch)
    if (branch.current) entry.hasCurrent = true
  }

  return { loose, groups: [...byName.values()].sort((a, b) => a.name.localeCompare(b.name)) }
}
