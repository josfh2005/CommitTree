import { describe, expect, it } from 'vitest'
import { formatBytes, percent, relativeDate, untilDisplayValue, untilFilterValue } from './format'

describe('relativeDate', () => {
  const now = new Date('2026-09-16T12:00:00Z')

  it('uses short relative units for recent dates', () => {
    expect(relativeDate('2026-09-16T11:59:30Z', now)).toBe('just now')
    expect(relativeDate('2026-09-16T11:55:00Z', now)).toBe('5m ago')
    expect(relativeDate('2026-09-16T09:00:00Z', now)).toBe('3h ago')
    expect(relativeDate('2026-09-14T12:00:00Z', now)).toBe('2d ago')
  })

  it('uses a calendar date after a week', () => {
    expect(relativeDate('2026-03-04T12:00:00Z', now)).toBe('Mar 4')
    expect(relativeDate('2025-03-04T12:00:00Z', now)).toBe('Mar 4, 2025')
  })
})

describe('untilFilterValue', () => {
  it('appends end-of-day so the chosen day is included', () => {
    expect(untilFilterValue('2026-01-01')).toBe('2026-01-01 23:59:59')
  })

  it('passes an empty date through unchanged', () => {
    expect(untilFilterValue('')).toBe('')
  })
})

describe('untilDisplayValue', () => {
  it('strips the end-of-day suffix for the date input', () => {
    expect(untilDisplayValue('2026-01-01 23:59:59')).toBe('2026-01-01')
  })

  it('passes an empty stored value through unchanged', () => {
    expect(untilDisplayValue('')).toBe('')
  })
})

describe('formatBytes', () => {
  it('uses binary units with one decimal for GB', () => {
    expect(formatBytes(0)).toBe('0 B')
    expect(formatBytes(512 * 1024 * 1024)).toBe('512 MB')
    expect(formatBytes(4683087332)).toBe('4.4 GB')
  })
})

describe('percent', () => {
  it('clamps and rounds', () => {
    expect(percent(50, 200)).toBe(25)
    expect(percent(5, 0)).toBe(0)
    expect(percent(300, 200)).toBe(100)
  })
})
