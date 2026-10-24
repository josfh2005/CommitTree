import { persisted } from './stores'

export type SettingsTab = 'general' | 'accounts' | 'providers' | 'models' | 'prompts'

/** The Settings dialog's tabs, in the order the left column lists them.
 *  `ai` marks the tabs that need the AI settings loaded; General and
 *  Accounts do not, so they open even when those fail. */
export const SETTINGS_TABS: { id: SettingsTab; label: string; ai: boolean }[] = [
  { id: 'general', label: 'General', ai: false },
  { id: 'accounts', label: 'Accounts', ai: false },
  { id: 'providers', label: 'Providers', ai: true },
  { id: 'models', label: 'AI models', ai: true },
  { id: 'prompts', label: 'Prompts', ai: true },
]

/** Validates the remembered tab read back from storage. */
export function isSettingsTab(v: unknown): v is SettingsTab {
  return SETTINGS_TABS.some((t) => t.id === v)
}

export function tabUsesAI(tab: SettingsTab): boolean {
  return SETTINGS_TABS.find((t) => t.id === tab)?.ai ?? false
}

/** The API keys tab became Providers: a remembered "keys" opens there
 *  instead of falling back to General. */
export function migrateRememberedTab(storage: Pick<Storage, 'getItem' | 'setItem'>, key: string) {
  try {
    if (storage.getItem(key) === JSON.stringify('keys')) storage.setItem(key, JSON.stringify('providers'))
  } catch {
    // Storage unavailable: nothing remembered to migrate.
  }
}

// The tab the Settings dialog opens on: the last one used. Shared so other
// places (the status bar) can open a given tab.
try {
  migrateRememberedTab(localStorage, 'settingsTab')
} catch {
  // Storage unavailable: the tab falls back to General.
}
export const settingsTab = persisted('settingsTab', 'general', isSettingsTab)
