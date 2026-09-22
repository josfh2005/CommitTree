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

/** validSelectedStash keeps the sidebar's stash preview from outliving its
 *  entry: applying or dropping a different stash first shifts every index
 *  below it, dropping the selected one removes it outright, and switching
 *  repositories makes any previously selected index meaningless. Given the
 *  previously selected index and the current list, this returns the index
 *  to keep selected, or null when the preview should close. */
export function validSelectedStash(selected: number | null, entries: StashEntry[]): number | null {
  if (selected === null) return null
  return entries.some((e) => e.index === selected) ? selected : null
}
