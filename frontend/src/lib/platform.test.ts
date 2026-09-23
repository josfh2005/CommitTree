import { describe, expect, it } from 'vitest'
import { revealLabel } from './platform'

describe('revealLabel', () => {
  it('names the platform file manager', () => {
    expect(revealLabel('darwin')).toBe('Show in Finder')
    expect(revealLabel('windows')).toBe('Show in Explorer')
    expect(revealLabel('linux')).toBe('Open in File Manager')
  })
  it('falls back to the generic name before the platform is known', () => {
    expect(revealLabel('')).toBe('Open in File Manager')
  })
})
