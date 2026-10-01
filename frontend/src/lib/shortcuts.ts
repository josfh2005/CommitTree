import type { ToggleKey } from './terminal'

/** Ctrl+, opens Settings on Linux. On macOS the native menu's Settings… item
 *  owns ⌘, (see internal/app/menu.go) and reports it as a menu:settings
 *  event, so the page must not handle it too. */
export function isSettingsShortcut(e: ToggleKey, platform: string): boolean {
  if (platform === 'darwin') return false
  return e.code === 'Comma' && e.ctrlKey && !e.metaKey && !e.altKey && !e.shiftKey
}

/** ⌘↵ (Ctrl+Enter off macOS) applies an inline editor: the Merge view's
 *  Edit… and a decision card's Other…. */
export function isApplyKey(e: Pick<KeyboardEvent, 'key' | 'metaKey' | 'ctrlKey'>): boolean {
  return e.key === 'Enter' && (e.metaKey || e.ctrlKey)
}
