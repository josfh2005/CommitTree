/**
 * classifyGroupRename decides what renaming a sidebar group from `oldName`
 * to the (untrimmed) `proposedName` actually does, given the other group
 * names currently in use. It is pure so the decision — no-op vs. plain
 * rename vs. merge — can be unit tested without touching the store, the
 * dialog, or the backend.
 *
 * - An empty/whitespace-only proposed name, or one that trims to the exact
 *   old name, is a no-op: nothing should be written.
 * - A trimmed name that matches an *existing, different* group name merges
 *   the two groups — a group is just a string on each repository, so
 *   landing on the same string is what "merge" means here.
 * - Anything else is a plain rename.
 */
export type GroupRenameAction =
  | { kind: 'noop' }
  | { kind: 'rename'; name: string }
  | { kind: 'merge'; name: string }

export function classifyGroupRename(existingNames: string[], oldName: string, proposedName: string): GroupRenameAction {
  const name = proposedName.trim()
  if (!name || name === oldName) return { kind: 'noop' }
  const mergesIntoExisting = existingNames.some((n) => n !== oldName && n === name)
  return mergesIntoExisting ? { kind: 'merge', name } : { kind: 'rename', name }
}

/**
 * renameCollapsedGroup carries a group's persisted collapsed/expanded state
 * across a rename, so the section doesn't jump open or shut just because
 * its name changed. `names` is the list of currently-collapsed group names
 * (see stores.collapsedRepoGroups). When oldName wasn't collapsed, the list
 * is returned unchanged — there is nothing to carry, and newName's own
 * collapsed state (if any, e.g. on a merge) is left alone.
 */
export function renameCollapsedGroup(names: string[], oldName: string, newName: string): string[] {
  if (!names.includes(oldName)) return names
  // On a merge (newName already existed) the result is collapsed if EITHER
  // side was collapsed — a deliberate choice, not an accident of the Set.
  return [...new Set(names.filter((n) => n !== oldName).concat(newName))]
}
