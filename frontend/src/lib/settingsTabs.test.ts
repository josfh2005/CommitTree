import { describe, expect, it } from 'vitest'
import { isSettingsTab, SETTINGS_TABS, tabUsesAI } from './settingsTabs'

describe('settings tabs', () => {
  it('lists General first, then the AI tabs', () => {
    expect(SETTINGS_TABS.map((t) => t.label)).toEqual(['General', 'AI models', 'API keys', 'Prompts'])
  })

  it('only accepts a known tab id as the remembered tab', () => {
    expect(isSettingsTab('prompts')).toBe(true)
    expect(isSettingsTab('nope')).toBe(false)
    expect(isSettingsTab(3)).toBe(false)
  })

  it('knows which tabs need the AI settings loaded', () => {
    expect(tabUsesAI('general')).toBe(false)
    expect(tabUsesAI('models')).toBe(true)
    expect(tabUsesAI('keys')).toBe(true)
    expect(tabUsesAI('prompts')).toBe(true)
  })
})
