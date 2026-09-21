import type { FileStatus, WorktreeState } from './types'

export interface WorktreeSection {
  title: string
  files: FileStatus[]
}

/** worktreeSections groups the change lists for the file list, in the order
 *  the user acts on them: what will be committed, then what will not. */
export function worktreeSections(state: WorktreeState): WorktreeSection[] {
  return [
    { title: 'Staged', files: state.staged },
    { title: 'Unstaged', files: state.unstaged },
    { title: 'Untracked', files: state.untracked },
  ].filter((s) => s.files.length > 0)
}

/** changedCount is what the sidebar row shows: distinct paths, so a file that
 *  is both staged and modified counts once. */
export function changedCount(state: WorktreeState | null): number {
  if (!state) return 0
  const paths = new Set<string>()
  for (const list of [state.staged, state.unstaged, state.untracked]) {
    for (const f of list) paths.add(f.path)
  }
  return paths.size
}

/** discardMessage is the confirmation before throwing changes away — the only
 *  destructive action here, and for an untracked file git cannot undo it.
 *  `staged` must reflect whether the PATH has a staged entry anywhere in the
 *  worktree state (see hasStagedChanges), not which section the row the user
 *  clicked came from: `git restore --staged --worktree` discards both a
 *  file's staged and unstaged content together, regardless of which row of a
 *  partially staged file (git status "MM") triggered the discard. */
export function discardMessage(file: FileStatus, staged: boolean): string {
  if (file.status === '?') {
    return `This will delete ${file.path} for good — it was never committed, so this cannot be undone.`
  }
  const also = staged ? ' Its staged changes are thrown away too.' : ''
  return `Discard your changes to ${file.path}?${also} This cannot be undone.`
}

/** hasStagedChanges reports whether path has a staged entry in state — the
 *  question discardMessage needs answered. A partially staged file (git
 *  status "MM") lists the same path under both Staged and Unstaged, so this
 *  must be computed from the path's presence in state.staged, never from
 *  which section the triggering row belongs to. */
export function hasStagedChanges(state: WorktreeState, path: string): boolean {
  return state.staged.some((f) => f.path === path)
}

/** A row's identity for selection purposes: which section it's in (needed
 *  only to break a tie — see nextSelection) plus its path. */
export interface SelectionKey {
  section: string
  path: string
}

/** The minimal shape nextSelection needs from a section — satisfied
 *  structurally by both WorktreeSection and merge.ts's MergeSection, so one
 *  function serves both the Changes view and the merge view. */
interface SectionLike {
  title: string
  files: { path: string }[]
}

/**
 * nextSelection decides which row stays selected after the underlying state
 * changes, given what was selected before. Continuity is judged by PATH, not
 * by section: staging, unstaging or resolving a conflict moves a file from
 * one section to another (Conflicts → Unstaged → Staged, or Unstaged →
 * Staged), and it is still "the same file" the user had open even though its
 * row (section + path) changes. The section in the returned key is kept only
 * to break a tie when the same path is listed in two sections at once — a
 * partially staged file (git status "MM") appears under both Staged and
 * Unstaged — so the row the user actually had selected wins over the other
 * section's row for that path, rather than the two being indistinguishable.
 *
 * Returns null when there is nothing to select (every section is empty).
 * When `previous` is null, or its path is no longer present anywhere, this
 * falls back to the first row.
 */
export function nextSelection(previous: SelectionKey | null, sections: SectionLike[]): SelectionKey | null {
  const all = sections.flatMap((s) => s.files.map((f) => ({ section: s.title, path: f.path })))
  if (all.length === 0) return null
  if (!previous) return all[0]
  const matches = all.filter((f) => f.path === previous.path)
  if (matches.length === 0) return all[0]
  return matches.find((f) => f.section === previous.section) ?? matches[0]
}
