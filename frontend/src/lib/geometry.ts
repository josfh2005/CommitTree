import { EDGE_ARROW_DOWN, EDGE_ARROW_UP, EDGE_LINE, type Edge, type LogRow } from './types'

export const ROW_HEIGHT = 28
export const LANE_WIDTH = 16
export const GRAPH_PADDING = 10
export const DOT_RADIUS = 4
export const OVERSCAN = 10

export const LANE_COLORS = ['#4f9d4f', '#a4478f', '#b58a3a', '#3f7fbf', '#c0504d', '#2a9d8f', '#7b61c9', '#d9822b']

export const laneX = (lane: number) => GRAPH_PADDING + lane * LANE_WIDTH + LANE_WIDTH / 2
export const rowCenterY = (index: number) => index * ROW_HEIGHT + ROW_HEIGHT / 2
export const laneColor = (color: number) => LANE_COLORS[((color % LANE_COLORS.length) + LANE_COLORS.length) % LANE_COLORS.length]

export interface Segment {
  x1: number
  y1: number
  x2: number
  y2: number
  color: number
  arrow: 'none' | 'down' | 'up'
  target?: string
}

export function edgeSegment(edge: Edge, rowIndex: number): Segment {
  const x1 = laneX(edge.from)
  const x2 = laneX(edge.to)
  const center = rowCenterY(rowIndex)
  const top = center - ROW_HEIGHT / 2
  switch (edge.kind) {
    case EDGE_ARROW_DOWN:
      return { x1, y1: rowCenterY(rowIndex - 1), x2, y2: top, color: edge.color, arrow: 'down', target: edge.target }
    case EDGE_ARROW_UP:
      return { x1, y1: top, x2, y2: center, color: edge.color, arrow: 'up', target: edge.target }
    default:
      return { x1, y1: rowCenterY(rowIndex - 1), x2, y2: center, color: edge.color, arrow: 'none' }
  }
}

export function visibleRange(scrollTop: number, viewportHeight: number, total: number) {
  const start = Math.max(0, Math.floor(scrollTop / ROW_HEIGHT) - OVERSCAN)
  const end = Math.min(total, Math.ceil((scrollTop + viewportHeight) / ROW_HEIGHT) + OVERSCAN)
  return { start, end }
}

export function graphWidth(rows: LogRow[]): number {
  let max = 0
  for (const r of rows) {
    max = Math.max(max, r.lane)
    for (const e of r.edges) max = Math.max(max, e.from, e.to)
  }
  return GRAPH_PADDING * 2 + (max + 1) * LANE_WIDTH
}

export function arrowAt(rows: LogRow[], x: number, y: number): Segment | null {
  const index = Math.floor(y / ROW_HEIGHT)
  for (const i of [index, index + 1]) {
    const row = rows[i]
    if (!row) continue
    for (const e of row.edges) {
      if (e.kind === EDGE_LINE) continue
      const s = edgeSegment(e, i)
      const top = Math.min(s.y1, s.y2)
      const bottom = Math.max(s.y1, s.y2)
      if (Math.abs(x - s.x1) <= LANE_WIDTH / 2 && y >= top && y <= bottom) return s
    }
  }
  return null
}
