import { describe, expect, it } from 'vitest'
import { arrowAt, edgeSegment, graphWidth, laneColor, laneX, LANE_COLORS, refBadgeBorderColor, rowCenterY, visibleRange } from './geometry'
import { EDGE_ARROW_DOWN, EDGE_ARROW_UP, EDGE_LINE, type Edge, type LogRow } from './types'

const row = (lane: number, edges: Edge[] = []): LogRow =>
  ({ lane, color: 0, edges, isMerge: false, isHead: false, hash: '', short: '', parents: [], author: '', email: '', date: '', subject: '', refs: [] })

describe('positions', () => {
  it('centers lanes and rows', () => {
    expect(laneX(0)).toBe(18)
    expect(laneX(2)).toBe(50)
    expect(rowCenterY(0)).toBe(14)
    expect(rowCenterY(3)).toBe(98)
  })
})

describe('edgeSegment', () => {
  it('draws lines from the previous row center to this row center', () => {
    expect(edgeSegment({ from: 0, to: 1, color: 3, kind: EDGE_LINE }, 2)).toEqual({
      x1: 18, y1: 42, x2: 34, y2: 70, color: 3, arrow: 'none',
    })
  })

  it('ends a down arrow at the top of its row', () => {
    expect(edgeSegment({ from: 1, to: 1, color: 1, kind: EDGE_ARROW_DOWN, target: 'abc' }, 5)).toEqual({
      x1: 34, y1: 126, x2: 34, y2: 140, color: 1, arrow: 'down', target: 'abc',
    })
  })

  it('starts an up arrow at the top of its row', () => {
    expect(edgeSegment({ from: 1, to: 1, color: 1, kind: EDGE_ARROW_UP, target: 'abc' }, 5)).toEqual({
      x1: 34, y1: 140, x2: 34, y2: 154, color: 1, arrow: 'up', target: 'abc',
    })
  })
})

describe('visibleRange', () => {
  it('adds overscan and clamps to the row count', () => {
    expect(visibleRange(280, 280, 1000)).toEqual({ start: 0, end: 30 })
    expect(visibleRange(2800, 280, 1000)).toEqual({ start: 90, end: 120 })
    expect(visibleRange(0, 100, 5)).toEqual({ start: 0, end: 5 })
  })
})

describe('graphWidth', () => {
  it('fits the widest lane used by any dot or edge', () => {
    expect(graphWidth([row(0, [{ from: 2, to: 0, color: 0, kind: EDGE_LINE }]), row(1)])).toBe(68)
    expect(graphWidth([])).toBe(36)
  })
})

describe('arrowAt', () => {
  const rows = [row(0), row(0), row(0), row(0), row(0), row(1, [{ from: 1, to: 1, color: 1, kind: EDGE_ARROW_UP, target: 'abc' }])]

  it('finds an arrow under the pointer', () => {
    expect(arrowAt(rows, 34, 145)?.target).toBe('abc')
  })

  it('ignores points away from the arrow', () => {
    expect(arrowAt(rows, 18, 145)).toBeNull()
    expect(arrowAt(rows, 34, 20)).toBeNull()
  })
})

describe('laneColor', () => {
  it('cycles through the palette', () => {
    expect(laneColor(0)).toBe(LANE_COLORS[0])
    expect(laneColor(LANE_COLORS.length + 1)).toBe(LANE_COLORS[1])
  })
})

describe('refBadgeBorderColor', () => {
  it('takes the lane color for local and remote branches', () => {
    expect(refBadgeBorderColor('local', 0)).toBe(LANE_COLORS[0])
    expect(refBadgeBorderColor('remote', 2)).toBe(LANE_COLORS[2])
  })

  it('gives the same color to two refs sharing a commit', () => {
    expect(refBadgeBorderColor('local', 3)).toBe(refBadgeBorderColor('remote', 3))
  })

  it('leaves tags and head with no lane color', () => {
    expect(refBadgeBorderColor('tag', 1)).toBeNull()
    expect(refBadgeBorderColor('head', 1)).toBeNull()
  })
})
