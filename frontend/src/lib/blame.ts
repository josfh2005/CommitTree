import type { Blame, BlameBlock } from './types'

/** What the Blame view shows: a file at a revision ('' = working tree), and
 *  the view Back returns to once no earlier revision is left to go back to. */
export interface BlameTarget { path: string; rev: string; from: 'log' | 'changes' }

/** File line numbers, inclusive. */
export interface LineRange { start: number; end: number }

/** Why a file row cannot be blamed, as a tooltip, or null when it can. */
export function blameBlocker(file: { status: string; submodule?: boolean }): string | null {
  if (file.submodule) return 'Submodules have no blame'
  if (file.status === 'D') return 'The file was deleted'
  if (file.status === '?') return 'Untracked files have no history'
  return null
}

/** The index into blame.blocks of each entry of blame.lines. */
export function lineBlocks(blame: Blame): number[] {
  const out: number[] = []
  blame.blocks.forEach((b, i) => {
    for (let n = 0; n < b.count; n++) out.push(i)
  })
  return out.slice(0, blame.lines.length)
}

export function blocksIn(blame: Blame, range: LineRange): BlameBlock[] {
  return blame.blocks.filter((b) => b.start <= range.end && b.start + b.count - 1 >= range.start)
}

/** What a right-click on `line` acts on: the selection when the line is in
 *  it, otherwise the block the line belongs to. */
export function rangeAt(blame: Blame, selection: LineRange | null, line: number): LineRange {
  if (selection && line >= selection.start && line <= selection.end) return selection
  const b = blame.blocks.find((x) => line >= x.start && line < x.start + x.count)
  return b ? { start: b.start, end: b.start + b.count - 1 } : { start: line, end: line }
}

/** A click on a line number selects it; a shift-click extends from the anchor. */
export function selectLine(selection: LineRange | null, anchor: number | null, line: number, shift: boolean): { selection: LineRange; anchor: number } {
  if (shift && selection && anchor !== null) {
    return { selection: { start: Math.min(anchor, line), end: Math.max(anchor, line) }, anchor }
  }
  return { selection: { start: line, end: line }, anchor: line }
}

/** The one commit behind these blocks, or null when there are several or the
 *  lines are not committed. */
export function singleCommit(blocks: BlameBlock[]): BlameBlock | null {
  const hashes = new Set(blocks.map((b) => b.hash))
  if (hashes.size !== 1 || blocks[0].uncommitted) return null
  return blocks[0]
}

/** Why Blame previous revision is unavailable for these blocks, or null. */
export function previousBlocker(blocks: BlameBlock[]): string | null {
  if (new Set(blocks.map((b) => b.hash)).size > 1) return 'The lines come from more than one commit'
  const b = blocks[0]
  if (!b) return 'Nothing selected'
  if (b.uncommitted) return 'Not committed yet'
  if (b.boundary) return 'History before this commit is not available'
  if (!b.previous) return 'These lines were added in this commit'
  return null
}

export function revLabel(rev: string): string {
  return rev ? rev.slice(0, 7) : 'Working tree'
}
