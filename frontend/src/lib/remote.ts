import type { MergeState } from './types'

// conflictOwnsScreen decides whether the conflict view takes over the
// details pane and blocks the Changes view. Every kind but a stash has a
// git-level abort, so the user always has a way out of it and it keeps the
// screen; a stash conflict can only be dismissed, and then the Changes view
// (and the toolbar's "Resolve conflicts" button) take over again.
export function conflictOwnsScreen(state: MergeState | null, stashDismissed: boolean): boolean {
  if (!state?.merging) return false
  return !(state.kind === 'stash' && stashDismissed)
}
