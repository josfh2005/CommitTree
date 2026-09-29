import { describe, expect, it } from 'vitest'
import { clickSelect, diffRows, emptySelection, pickSummary, rowKey, toPicks } from './hunks'

const header = 'diff --git a/f.txt b/f.txt\nindex 1..2 100644\n--- a/f.txt\n+++ b/f.txt\n'
const text = header + '@@ -1,3 +1,3 @@\n one\n-two\n+TWO\n\\ No newline at end of file\n@@ -10,2 +10,2 @@\n-ten\n+TEN\n'

describe('diffRows', () => {
  it('numbers body lines per hunk the way Go does, skipping notes', () => {
    const { rows, actionable } = diffRows(text, false)
    expect(rows.map((r) => [r.kind, r.hunk, r.line])).toEqual([
      ['meta', -1, -1], ['meta', -1, -1], ['meta', -1, -1], ['meta', -1, -1],
      ['hunk', 0, -1], ['context', 0, 0], ['del', 0, 1], ['add', 0, 2], ['note', 0, -1],
      ['hunk', 1, -1], ['del', 1, 0], ['add', 1, 1],
    ])
    expect([...actionable]).toEqual([0, 1])
  })

  it('reads header look-alikes inside a hunk as changes', () => {
    const { rows } = diffRows(header + '@@ -1 +1 @@\n--- y\n+++ x\n', false)
    expect(rows.slice(5).map((r) => r.kind)).toEqual(['del', 'add'])
  })

  it('leaves the last hunk of a truncated diff without actions', () => {
    const { rows, actionable } = diffRows(text + '[truncated]', true)
    expect([...actionable]).toEqual([0])
    expect(rows[rows.length - 1].kind).toBe('note')
  })
})

describe('clickSelect', () => {
  const { rows, actionable } = diffRows(text, false)
  const idx = (hunk: number, line: number) => rows.findIndex((r) => r.hunk === hunk && r.line === line)
  const plain = { shift: false, toggle: false }

  it('selects one change line and ignores context and header rows', () => {
    const sel = clickSelect(rows, actionable, emptySelection(), idx(0, 1), plain)
    expect([...sel.keys]).toEqual(['0:1'])
    expect(clickSelect(rows, actionable, sel, idx(0, 0), plain)).toBe(sel)
    expect(clickSelect(rows, actionable, sel, 4, plain)).toBe(sel)
  })

  it('selects a range with Shift, across hunks, skipping what is not a change', () => {
    let sel = clickSelect(rows, actionable, emptySelection(), idx(0, 2), plain)
    sel = clickSelect(rows, actionable, sel, idx(1, 1), { shift: true, toggle: false })
    expect([...sel.keys].sort()).toEqual(['0:2', '1:0', '1:1'])
  })

  it('toggles single lines with Cmd or Ctrl', () => {
    let sel = clickSelect(rows, actionable, emptySelection(), idx(0, 1), plain)
    sel = clickSelect(rows, actionable, sel, idx(1, 0), { shift: false, toggle: true })
    expect([...sel.keys].sort()).toEqual(['0:1', '1:0'])
    sel = clickSelect(rows, actionable, sel, idx(0, 1), { shift: false, toggle: true })
    expect([...sel.keys]).toEqual(['1:0'])
  })

  it('does not select lines of a hunk without actions', () => {
    const cut = diffRows(text, true)
    const last = cut.rows.findIndex((r) => r.hunk === 1 && r.kind === 'del')
    expect(clickSelect(cut.rows, cut.actionable, emptySelection(), last, plain).keys.size).toBe(0)
  })

  it('keys rows by hunk and line', () => {
    expect(rowKey(rows[idx(1, 1)])).toBe('1:1')
  })
})

describe('toPicks and pickSummary', () => {
  it('groups selected lines by hunk, sorted', () => {
    expect(toPicks(new Set(['1:1', '0:2', '1:0']))).toEqual([{ hunk: 0, lines: [2] }, { hunk: 1, lines: [0, 1] }])
  })

  it('describes what an action touched', () => {
    expect(pickSummary([{ hunk: 0, lines: [] }])).toBe('1 hunk')
    expect(pickSummary([{ hunk: 0, lines: [] }, { hunk: 1, lines: [] }])).toBe('2 hunks')
    expect(pickSummary([{ hunk: 0, lines: [1] }])).toBe('1 line')
    expect(pickSummary([{ hunk: 0, lines: [1] }, { hunk: 1, lines: [0, 1] }])).toBe('3 lines')
  })
})
