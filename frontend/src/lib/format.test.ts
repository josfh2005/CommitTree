import { describe, expect, it } from 'vitest'
import { relativeDate } from './format'

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
