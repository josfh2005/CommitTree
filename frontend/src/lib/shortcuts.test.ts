import { describe, expect, it } from 'vitest'
import { isSettingsShortcut } from './shortcuts'

const key = (code: string, mods: Partial<Record<'ctrlKey' | 'metaKey' | 'altKey' | 'shiftKey', boolean>> = {}) => ({
  code, ctrlKey: false, metaKey: false, altKey: false, shiftKey: false, ...mods,
})

describe('isSettingsShortcut', () => {
  it('is Ctrl+, on Linux', () => {
    expect(isSettingsShortcut(key('Comma', { ctrlKey: true }), 'linux')).toBe(true)
    expect(isSettingsShortcut(key('Comma'), 'linux')).toBe(false)
    expect(isSettingsShortcut(key('Comma', { ctrlKey: true, shiftKey: true }), 'linux')).toBe(false)
    expect(isSettingsShortcut(key('Period', { ctrlKey: true }), 'linux')).toBe(false)
  })

  it('is left to the native menu on macOS', () => {
    expect(isSettingsShortcut(key('Comma', { metaKey: true }), 'darwin')).toBe(false)
    expect(isSettingsShortcut(key('Comma', { ctrlKey: true }), 'darwin')).toBe(false)
  })
})
