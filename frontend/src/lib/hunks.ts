import type { HunkPick, WorktreeDiff } from './types'

export type RowKind = 'meta' | 'hunk' | 'context' | 'add' | 'del' | 'note'

/** One row of the diff pane. `line` is the index among the hunk's body lines
 *  (context, + and -), numbered exactly as worktree.ParseDiff does in Go;
 *  -1 for headers, @@ rows and notes. `hunk` is -1 before the first @@. */
export interface DiffRow {
  text: string
  kind: RowKind
  hunk: number
  line: number
}

/**
 * diffRows splits the pane's diff text into rows. Inside a hunk a row is read
 * by its first character only, so an added "++ x" (shown "+++ x") is a change.
 * Every hunk takes actions except the last one of a truncated diff, which may
 * be cut short.
 */
export function diffRows(text: string, truncated: boolean): { rows: DiffRow[]; actionable: Set<number> } {
  const lines = text.split('\n')
  if (lines.length && lines[lines.length - 1] === '') lines.pop()
  const rows: DiffRow[] = []
  let hunk = -1
  let line = -1
  for (const t of lines) {
    if (t.startsWith('@@ ')) {
      hunk++
      line = -1
      rows.push({ text: t, kind: 'hunk', hunk, line: -1 })
    } else if (hunk < 0) {
      rows.push({ text: t, kind: 'meta', hunk, line: -1 })
    } else if (t.startsWith('\\') || (truncated && t === '[truncated]')) {
      rows.push({ text: t, kind: 'note', hunk, line: -1 })
    } else {
      const kind: RowKind = t.startsWith('+') ? 'add' : t.startsWith('-') ? 'del' : 'context'
      rows.push({ text: t, kind, hunk, line: ++line })
    }
  }
  const actionable = new Set<number>()
  for (let h = 0; h <= (truncated ? hunk - 1 : hunk); h++) actionable.add(h)
  return { rows, actionable }
}

export const rowKey = (row: DiffRow) => `${row.hunk}:${row.line}`

export function selectable(row: DiffRow | undefined, actionable: Set<number>): boolean {
  return !!row && (row.kind === 'add' || row.kind === 'del') && actionable.has(row.hunk)
}

export interface LineSelection {
  keys: Set<string>
  /** Row index of the last plain or toggle click, where a Shift range starts. */
  anchor: number | null
}

export const emptySelection = (): LineSelection => ({ keys: new Set(), anchor: null })

/**
 * clickSelect applies a click on rows[index]: a plain click selects that line
 * alone, Shift selects every change line between the anchor and it (replacing
 * the selection), toggle (Cmd or Ctrl) adds or removes it. A click on a row
 * that is not a selectable change returns `sel` unchanged.
 */
export function clickSelect(
  rows: DiffRow[],
  actionable: Set<number>,
  sel: LineSelection,
  index: number,
  mods: { shift: boolean; toggle: boolean },
): LineSelection {
  const row = rows[index]
  if (!selectable(row, actionable)) return sel
  if (mods.shift && sel.anchor !== null) {
    const [from, to] = sel.anchor < index ? [sel.anchor, index] : [index, sel.anchor]
    const keys = new Set<string>()
    for (let i = from; i <= to; i++) if (selectable(rows[i], actionable)) keys.add(rowKey(rows[i]))
    return { keys, anchor: sel.anchor }
  }
  if (mods.toggle) {
    const keys = new Set(sel.keys)
    const key = rowKey(row)
    if (keys.has(key)) keys.delete(key)
    else keys.add(key)
    return { keys, anchor: index }
  }
  return { keys: new Set([rowKey(row)]), anchor: index }
}

/** toPicks turns selected row keys into what Go takes, grouped by hunk and sorted. */
export function toPicks(keys: Set<string>): HunkPick[] {
  const byHunk = new Map<number, number[]>()
  for (const key of keys) {
    const [hunk, line] = key.split(':').map(Number)
    byHunk.set(hunk, [...(byHunk.get(hunk) ?? []), line])
  }
  return [...byHunk.entries()]
    .sort((a, b) => a[0] - b[0])
    .map(([hunk, lines]) => ({ hunk, lines: lines.sort((a, b) => a - b) }))
}

/** pickSummary is the "1 hunk" / "3 lines" part of the discard toast. */
export function pickSummary(picks: HunkPick[]): string {
  if (picks.every((p) => p.lines.length === 0)) return picks.length === 1 ? '1 hunk' : `${picks.length} hunks`
  const n = picks.reduce((sum, p) => sum + p.lines.length, 0)
  return n === 1 ? '1 line' : `${n} lines`
}

/** selectionKey names the diff a line selection belongs to. Row indices stay
 *  valid while it is unchanged, so a reload that brings back the same diff
 *  (a window focus, an unrelated change) keeps the selection. */
export const selectionKey = (path: string, staged: boolean, diff: WorktreeDiff) => `${staged ? 'staged' : 'unstaged'}:${diff.hash}:${path}`
