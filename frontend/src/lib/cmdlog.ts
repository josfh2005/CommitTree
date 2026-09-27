import type { ToggleKey } from './terminal'
import type { CommandEntry, CommandOrigin } from './types'

/** Same cap as the backend's per-repository log. */
export const MAX_ENTRIES = 500

export const ORIGIN_LABEL: Record<CommandOrigin, string> = { you: 'You', ai: 'AI', auto: 'Auto' }

/** mergeEntries adds more to list — only entries whose repo matches the key
 *  the backend returned from CommandLog (repoKey; see CommandLogView),
 *  never a frontend-computed Repo.path. An entry list already holds is
 *  replaced only when it is running and the new one is its end: the initial
 *  load and live events overlap, and a late running event must not bring a
 *  finished row back. Newest first, capped; the same array when nothing
 *  changes. */
export function mergeEntries(list: CommandEntry[], more: CommandEntry[], repoKey: string): CommandEntry[] {
  const byId = new Map(list.map((e) => [e.id, e]))
  let changed = false
  for (const e of more) {
    if (e.repo !== repoKey) continue
    const had = byId.get(e.id)
    if (had && (had.outcome !== 'running' || e.outcome === 'running')) continue
    byId.set(e.id, e)
    changed = true
  }
  if (!changed) return list
  return [...byId.values()].sort((a, b) => b.id - a.id).slice(0, MAX_ENTRIES)
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

/** How long a running command has been running at now (ms since epoch). */
export function elapsedMs(e: CommandEntry, now: number): number {
  return Math.max(0, now - Date.parse(e.start))
}

/** Elapsed time of a running command, in whole seconds: it ticks once a second. */
export function formatElapsed(ms: number): string {
  return `${Math.floor(ms / 1000)} s`
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
  if (e.outcome === 'running') return 'Running'
  if (e.outcome === 'cancelled') return 'Cancelled'
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
