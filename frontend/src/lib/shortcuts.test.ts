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

import { isApplyKey } from './shortcuts'

describe('isApplyKey', () => {
  const key = (over: Partial<KeyboardEvent>) => ({ key: 'Enter', metaKey: false, ctrlKey: false, ...over })
  it('is ⌘↵ on macOS and Ctrl+Enter elsewhere', () => {
    expect(isApplyKey(key({ metaKey: true }))).toBe(true)
    expect(isApplyKey(key({ ctrlKey: true }))).toBe(true)
  })
  it('is not a plain Enter or another key', () => {
    expect(isApplyKey(key({}))).toBe(false)
    expect(isApplyKey(key({ key: 'a', ctrlKey: true }))).toBe(false)
  })
})
