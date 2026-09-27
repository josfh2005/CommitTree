import type { ToggleKey } from './terminal'
import type { CommandEntry, CommandOrigin } from './types'

/** Same cap as the backend's per-repository log. */
export const MAX_ENTRIES = 500

export const ORIGIN_LABEL: Record<CommandOrigin, string> = { you: 'You', ai: 'AI', auto: 'Auto' }

/** mergeEntries adds more to list — only repoPath's, without duplicates
 *  (the initial load and live events overlap) — newest first, capped. */
export function mergeEntries(list: CommandEntry[], more: CommandEntry[], repoPath: string): CommandEntry[] {
  const seen = new Set(list.map((e) => e.id))
  const add = more.filter((e) => e.repo === repoPath && !seen.has(e.id) && seen.add(e.id))
  if (add.length === 0) return list
  return [...list, ...add].sort((a, b) => b.id - a.id).slice(0, MAX_ENTRIES)
}

export function visibleEntries(list: CommandEntry[], showReads: boolean): CommandEntry[] {
  return showReads ? list : list.filter((e) => e.kind === 'write')
}

/** Why the panel shows no rows: nothing yet, or only hidden reads. */
export function emptyMessage(list: CommandEntry[], showReads: boolean): '' | 'none' | 'onlyReads' {
  if (list.length === 0) return 'none'
  return visibleEntries(list, showReads).length === 0 ? 'onlyReads' : ''
}

export function formatDuration(ms: number): string {
  return ms < 1000 ? `${ms} ms` : `${(ms / 1000).toFixed(1)} s`
}

export function formatClock(iso: string): string {
  const d = new Date(iso)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

const PLAIN = /^[A-Za-z0-9_\-.,/:=@%+^~{}]+$/

/** commandLine is args as a shell command someone could paste. */
export function commandLine(args: string[]): string {
  const quote = (a: string) => (PLAIN.test(a) ? a : `'${a.replace(/'/g, `'\\''`)}'`)
  return ['git', ...args.map(quote)].join(' ')
}

export function outcomeText(e: CommandEntry): string {
  if (e.outcome === 'timeout') return 'Timed out'
  if (e.outcome === 'failed') return `Failed · exit code ${e.exitCode}`
  return `Exit code ${e.exitCode}`
}

/** ⌘⇧J on macOS, Ctrl+Shift+J elsewhere — left to the shell while focus
 *  is in the terminal, like Ctrl+J. Matched on the physical key. */
export function isCommandsToggle(e: ToggleKey, platform: string, inTerminal: boolean): boolean {
  if (e.code !== 'KeyJ' || !e.shiftKey || e.altKey) return false
  if (platform === 'darwin') return e.metaKey && !e.ctrlKey
  return e.ctrlKey && !e.metaKey && !inTerminal
}

export function commandsShortcutLabel(platform: string): string {
  return platform === 'darwin' ? '⌘⇧J' : 'Ctrl+Shift+J'
}
