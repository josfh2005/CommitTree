import type { Repo } from './types'

/** How the sidebar orders repositories and groups: by name, or in the
 *  stored order the user arranges by dragging. */
export type RepoSortOrder = 'name' | 'manual'

export const isRepoSortOrder = (v: unknown): v is RepoSortOrder => v === 'name' || v === 'manual'

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
 * at the top level. Submodules (`repo.submodule`) are dropped entirely —
 * they are shown in their own Submodules section, not the sidebar tree.
 * By name, repositories are sorted with compareRepos and groups by name; in
 * manual order both keep the order of `repos` (the stored order), a group
 * placed where its first repository is. Worktrees are always by name.
 */
export function groupRepos(repos: Repo[], order: RepoSortOrder = 'name'): GroupedRepos {
  repos = repos.filter((r) => !r.submodule)
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
  for (const repo of order === 'name' ? tops.sort(compareRepos) : tops) {
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

  const groups = [...byName.values()]
  return { loose, groups: order === 'name' ? groups.sort((a, b) => a.name.localeCompare(b.name)) : groups }
}

/** Where a dragged repository lands: in `group` ('' is the loose area),
 *  before `beforeId`, or at the end of that area when beforeId is null. */
export interface RepoPlace {
  group: string
  beforeId: string | null
}

const areaOf = (r: Repo) => r.group ?? ''

/** moveRepo is the stored order after dropping `id` at `place`: every id of
 *  `repos`, in order, with `id` moved. Changing its group is a separate
 *  write (setRepoGroup); this only decides where it sits. */
export function moveRepo(repos: Repo[], id: string, place: RepoPlace): string[] {
  const order = repos.map((r) => r.id).filter((x) => x !== id)
  let at = place.beforeId !== null ? order.indexOf(place.beforeId) : -1
  if (at < 0) {
    const members = repos.filter((r) => r.id !== id && !r.parentId && areaOf(r) === place.group)
    at = members.length ? order.indexOf(members[members.length - 1].id) + 1 : order.length
  }
  order.splice(at, 0, id)
  return order
}

/** moveGroup is the stored order after dragging group `name` before group
 *  `before` (or to the end when null): its repositories move as a block,
 *  keeping their own order. */
export function moveGroup(repos: Repo[], name: string, before: string | null): string[] {
  const inGroup = (r: Repo) => !r.parentId && areaOf(r) === name
  const block = repos.filter(inGroup).map((r) => r.id)
  const rest = repos.filter((r) => !inGroup(r))
  const at = before === null ? -1 : rest.findIndex((r) => !r.parentId && areaOf(r) === before)
  const ids = rest.map((r) => r.id)
  ids.splice(at < 0 ? ids.length : at, 0, ...block)
  return ids
}

/** nameOrder is the by-name display order as a stored order: what a first
 *  switch to manual order starts from, so nothing jumps. */
export function nameOrder(repos: Repo[]): string[] {
  const { loose, groups } = groupRepos(repos, 'name')
  return [...loose, ...groups.flatMap((g) => g.repos)].flatMap((n) => [n.repo.id, ...n.children.map((c) => c.id)])
}
