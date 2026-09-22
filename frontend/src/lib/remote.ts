import type { MergeState } from './types'

// The toolbar's Pull and Push are disabled by the same rule Commit already
// follows: nothing else running, and no merge/rebase/stash conflict owns
// the repository. Fetch has no such rule — it never touches the worktree
// or the index.
export function canSync(state: MergeState | null, busy: string): boolean {
  return !busy && !state?.merging
}

// conflictOwnsScreen decides whether the conflict view takes over the
// details pane and blocks the Changes view. Every kind but a stash has a
// git-level abort, so the user always has a way out of it and it keeps the
// screen; a stash conflict can only be dismissed, and then the Changes view
// (and the toolbar's "Resolve conflicts" button) take over again.
export function conflictOwnsScreen(state: MergeState | null, stashDismissed: boolean): boolean {
  if (!state?.merging) return false
  return !(state.kind === 'stash' && stashDismissed)
}
