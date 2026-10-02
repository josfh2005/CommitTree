import { describe, expect, it } from 'vitest'
import { isSettingsTab, migrateRememberedTab, SETTINGS_TABS, tabUsesAI } from './settingsTabs'

function memoryStorage(initial: Record<string, string>) {
  const data = { ...initial }
  return { data, getItem: (k: string) => data[k] ?? null, setItem: (k: string, v: string) => void (data[k] = v) }
}

describe('settings tabs', () => {
  it('lists General first, then the AI tabs', () => {
    expect(SETTINGS_TABS.map((t) => t.label)).toEqual(['General', 'Providers', 'AI models', 'Prompts'])
  })

  it('only accepts a known tab id as the remembered tab', () => {
    expect(isSettingsTab('prompts')).toBe(true)
    expect(isSettingsTab('providers')).toBe(true)
    expect(isSettingsTab('keys')).toBe(false)
    expect(isSettingsTab('nope')).toBe(false)
    expect(isSettingsTab(3)).toBe(false)
  })

  it('knows which tabs need the AI settings loaded', () => {
    expect(tabUsesAI('general')).toBe(false)
    expect(tabUsesAI('providers')).toBe(true)
    expect(tabUsesAI('models')).toBe(true)
    expect(tabUsesAI('prompts')).toBe(true)
  })

  it('opens a remembered API keys tab on Providers', () => {
    const s = memoryStorage({ settingsTab: '"keys"' })
    migrateRememberedTab(s, 'settingsTab')
    expect(s.data.settingsTab).toBe('"providers"')
  })

  it('leaves any other remembered tab alone', () => {
    const s = memoryStorage({ settingsTab: '"models"' })
    migrateRememberedTab(s, 'settingsTab')
    expect(s.data.settingsTab).toBe('"models"')
    const empty = memoryStorage({})
    migrateRememberedTab(empty, 'settingsTab')
    expect(empty.data).toEqual({})
  })
})
