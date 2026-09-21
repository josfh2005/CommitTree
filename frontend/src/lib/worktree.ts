import type { CommitInfo, FileStatus, WorktreeState } from './types'

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

/** neverCommitted reports whether a discard of this file has no HEAD copy to
 *  restore to, so Discard deletes it outright rather than reverting it. That
 *  is true for an untracked file ('?') and equally true for a file that is
 *  staged as newly added ('A'): git has no history for that path either way,
 *  so `git restore --source=HEAD --staged --worktree` finds nothing at HEAD
 *  and removes it for good. Gating on '?' alone missed the staged-'A' case —
 *  a file created and staged, never committed — which looked like an
 *  ordinary, reversible discard but was just as permanent. */
export function neverCommitted(file: FileStatus): boolean {
  return file.status === '?' || file.status === 'A'
}

/** discardMessage is the confirmation before throwing changes away — the only
 *  destructive action here, and for a file with no HEAD copy git cannot undo
 *  it. `staged` must reflect whether the PATH has a staged entry anywhere in
 *  the worktree state (see hasStagedChanges), not which section the row the
 *  user clicked came from: `git restore --staged --worktree` discards both a
 *  file's staged and unstaged content together, regardless of which row of a
 *  partially staged file (git status "MM") triggered the discard. */
export function discardMessage(file: FileStatus, staged: boolean): string {
  if (neverCommitted(file)) {
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

/** canCommit gates the Commit button. An amend may have nothing staged: it
 *  then only rewrites the message, which is the common use of it. */
export function canCommit(info: CommitInfo | null, message: string, amend: boolean): boolean {
  if (!info || message.trim() === '') return false
  if (amend) return info.canAmend
  return info.stagedCount > 0
}

/** shouldAutoGenerate decides whether to write the message without being
 *  asked. It never overwrites what the user typed: `touched` stays true once
 *  they edit the box, until they clear it. It also never fires with nothing
 *  staged — Go's GenerateCommitMessage rejects that call with
 *  ErrNothingStaged, and the defaults (taskProvider "ollama", commitMessage
 *  "auto-local") would otherwise auto-generate, and error, on every clean
 *  tree and after every commit. */
export function shouldAutoGenerate(mode: string, taskProvider: string, message: string, touched: boolean, stagedCount: number): boolean {
  if (stagedCount <= 0) return false
  if (message.trim() !== '' || touched) return false
  if (mode === 'manual') return false
  if (mode === 'auto') return true
  return taskProvider === 'ollama' // auto-local: only the local, free provider
}

/** amendWarning is the confirmation before rewriting a commit the upstream
 *  already has, or null when there is nothing to warn about. */
export function amendWarning(info: CommitInfo): string | null {
  if (!info.pushed || !info.upstream) return null
  return `This commit is already on ${info.upstream}. Amending rewrites it, so pushing afterwards will need a force push. Amend anyway?`
}
