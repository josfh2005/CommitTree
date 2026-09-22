import type { StashEntry, StashFile } from './types'

/** stashFileTitle labels which of the two rows a stash file belongs to: an
 *  ordinary tracked change, or one of the untracked files a stash pushed
 *  with -u also captured. */
export function stashFileTitle(file: StashFile): string {
  return file.untracked ? 'Untracked' : 'Changed'
}

/** stashSections groups a stash's files for FileList, tracked changes
 *  before the untracked files it also captured — the same shape
 *  worktreeSections gives the Changes view. */
export function stashSections(files: StashFile[]): { title: string; files: StashFile[] }[] {
  return [
    { title: 'Changed', files: files.filter((f) => !f.untracked) },
    { title: 'Untracked', files: files.filter((f) => f.untracked) },
  ].filter((s) => s.files.length > 0)
}

/** stashApplyAction decides which backend call the apply-stash confirmation
 *  should make: the checkbox on that dialog ("delete the stash after
 *  applying it") turns a plain apply into a pop — pop already handles the
 *  conflict case correctly (the entry is kept and dropped automatically once
 *  the conflict is resolved), so this must never be reimplemented as
 *  apply-then-drop. */
export function stashApplyAction(deleteAfterApply: boolean): 'apply' | 'pop' {
  return deleteAfterApply ? 'pop' : 'apply'
}

/** Which stash the sidebar has selected: its hash is the identity, index is
 *  only where the backend's index-based calls (GetStashFiles, StashApply, …)
 *  currently find it. */
export interface SelectedStash {
  index: number
  hash: string
}

/** validSelectedStash keeps the sidebar's stash preview from outliving, or
 *  silently swapping, its entry. The index alone can't be trusted: applying,
 *  popping or dropping any entry below the selected one shifts every index
 *  above it — including from another session, since the stash stack is
 *  shared across worktrees — so a stale index can end up naming a different
 *  stash entirely, and checking it alone would pass and quietly preview
 *  that other stash's files. The hash is the one thing that survives a
 *  shift, so this looks the selection up by hash and re-syncs its index to
 *  wherever that hash currently sits; when the hash isn't in the list at
 *  all — dropped, or popped into a completed merge — the selection clears
 *  instead of falling back to whatever now occupies its old index. */
export function validSelectedStash(selected: SelectedStash | null, entries: StashEntry[]): SelectedStash | null {
  if (selected === null) return null
  const entry = entries.find((e) => e.hash === selected.hash)
  return entry ? { index: entry.index, hash: entry.hash } : null
}
