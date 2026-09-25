export type SettingsTab = 'general' | 'models' | 'keys' | 'prompts'

/** The Settings dialog's tabs, in the order the left column lists them.
 *  `ai` marks the tabs that need the AI settings loaded; General does not,
 *  so it opens even when those fail. */
export const SETTINGS_TABS: { id: SettingsTab; label: string; ai: boolean }[] = [
  { id: 'general', label: 'General', ai: false },
  { id: 'models', label: 'AI models', ai: true },
  { id: 'keys', label: 'API keys', ai: true },
  { id: 'prompts', label: 'Prompts', ai: true },
]

/** Validates the remembered tab read back from storage. */
export function isSettingsTab(v: unknown): v is SettingsTab {
  return SETTINGS_TABS.some((t) => t.id === v)
}

export function tabUsesAI(tab: SettingsTab): boolean {
  return SETTINGS_TABS.find((t) => t.id === tab)?.ai ?? false
}
