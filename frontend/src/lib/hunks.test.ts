import { describe, expect, it } from 'vitest'
import { clickSelect, diffRows, emptySelection, escapeClears, keyMods, pickSummary, rowKey, selectionKey, toPicks } from './hunks'

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

describe('selectionKey', () => {
  const diff = { text: 'x', hash: 'h1', truncated: false, patchable: true }
  it('is the same for a reload of the same diff, so the selection survives it', () => {
    expect(selectionKey('f.txt', false, { ...diff })).toBe(selectionKey('f.txt', false, diff))
  })
  it('changes with the file, the section or the diff', () => {
    const key = selectionKey('f.txt', false, diff)
    expect(selectionKey('g.txt', false, diff)).not.toBe(key)
    expect(selectionKey('f.txt', true, diff)).not.toBe(key)
    expect(selectionKey('f.txt', false, { ...diff, hash: 'h2' })).not.toBe(key)
  })
})

describe('escapeClears', () => {
  it('clears on Esc only when nothing else is open and the focus is in the diff', () => {
    expect(escapeClears('Escape', { overlayOpen: false, inPane: true })).toBe(true)
    expect(escapeClears('Escape', { overlayOpen: true, inPane: true })).toBe(false)
    expect(escapeClears('Escape', { overlayOpen: false, inPane: false })).toBe(false)
    expect(escapeClears('Enter', { overlayOpen: false, inPane: true })).toBe(false)
  })
})

describe('keyMods', () => {
  const key = (k: string, over: Partial<{ shiftKey: boolean; metaKey: boolean; ctrlKey: boolean }> = {}) => ({ key: k, shiftKey: false, metaKey: false, ctrlKey: false, ...over })
  it('treats Enter and Space on a line like a click, with the same modifiers', () => {
    expect(keyMods(key('Enter'))).toEqual({ shift: false, toggle: false })
    expect(keyMods(key(' ', { shiftKey: true }))).toEqual({ shift: true, toggle: false })
    expect(keyMods(key('Enter', { metaKey: true }))).toEqual({ shift: false, toggle: true })
    expect(keyMods(key(' ', { ctrlKey: true }))).toEqual({ shift: false, toggle: true })
  })
  it('ignores other keys', () => {
    expect(keyMods(key('a'))).toBeNull()
    expect(keyMods(key('Escape'))).toBeNull()
  })
})
