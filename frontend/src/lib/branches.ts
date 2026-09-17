import type { Branch } from './types'

export interface BranchGroup {
  name: string
  branches: Branch[]
  hasCurrent: boolean
}

export interface GroupedBranches {
  /** Branches shown on their own, with their full name. */
  loose: Branch[]
  /** Prefixes shared by two or more branches, sorted by name. */
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
 * groupBranches puts branches that share a first path segment ("fix/abc",
 * "fix/cdf") into a group, and leaves the rest — including prefixes used by a
 * single branch — on their own. Input order is preserved inside each part.
 */
export function groupBranches(branches: Branch[]): GroupedBranches {
  const counts = new Map<string, number>()
  for (const branch of branches) {
    const group = groupOf(branch.name)
    if (group !== '') counts.set(group, (counts.get(group) ?? 0) + 1)
  }

  const loose: Branch[] = []
  const byName = new Map<string, BranchGroup>()
  for (const branch of branches) {
    const group = groupOf(branch.name)
    if (group === '' || (counts.get(group) ?? 0) < 2) {
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
