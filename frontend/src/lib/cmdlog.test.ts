import { describe, expect, it } from 'vitest'
import { commandLine, commandsShortcutLabel, elapsedMs, emptyMessage, formatClock, formatDuration, formatElapsed, isCommandsToggle, MAX_ENTRIES, mergeEntries, mergeLoad, outcomeText, visibleEntries } from './cmdlog'
import type { CommandEntry } from './types'

const entry = (id: number, over: Partial<CommandEntry> = {}): CommandEntry => ({
  id, repo: '/r', args: ['status'], origin: 'auto', kind: 'read', start: '2026-09-27T10:04:05Z',
  durationMs: 12, exitCode: 0, outcome: 'ok', outputTruncated: false, outputDropped: false, ...over,
})
const key = (over: Partial<KeyboardEvent>) => ({ code: 'KeyJ', ctrlKey: false, metaKey: false, altKey: false, shiftKey: false, ...over })

describe('mergeEntries', () => {
  it('keeps newest first and drops duplicates and other repos', () => {
    const list = mergeEntries([entry(2)], [entry(1), entry(3), entry(2), entry(4, { repo: '/other' })], '/r')
    expect(list.map((e) => e.id)).toEqual([3, 2, 1])
  })
  it('returns the same array when nothing is added', () => {
    const list = [entry(1)]
    expect(mergeEntries(list, [entry(1)], '/r')).toBe(list)
  })
  it('caps the list', () => {
    const many = Array.from({ length: MAX_ENTRIES + 20 }, (_, i) => entry(i + 1))
    const list = mergeEntries([], many, '/r')
    expect(list).toHaveLength(MAX_ENTRIES)
    expect(list[0].id).toBe(MAX_ENTRIES + 20)
  })
})

describe('visibleEntries and emptyMessage', () => {
  const list = [entry(1), entry(2, { kind: 'write', origin: 'you', args: ['commit'] })]
  it('hides reads unless asked', () => {
    expect(visibleEntries(list, false).map((e) => e.id)).toEqual([2])
    expect(visibleEntries(list, true)).toHaveLength(2)
  })
  it('explains an empty panel', () => {
    expect(emptyMessage([], false)).toBe('none')
    expect(emptyMessage([entry(1)], false)).toBe('onlyReads')
    expect(emptyMessage([entry(1)], true)).toBe('')
    expect(emptyMessage(list, false)).toBe('')
  })
})

describe('formatting', () => {
  it('formats durations', () => {
    expect(formatDuration(42)).toBe('42 ms')
    expect(formatDuration(999)).toBe('999 ms')
    expect(formatDuration(1300)).toBe('1.3 s')
  })
  it('formats the clock as HH:MM:SS local time', () => {
    const d = new Date('2026-09-27T10:04:05Z')
    const pad = (n: number) => String(n).padStart(2, '0')
    expect(formatClock(d.toISOString())).toBe(`${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`)
  })
  it('quotes arguments only when needed', () => {
    expect(commandLine(['log', '--format=%H', '-n', '10'])).toBe('git log --format=%H -n 10')
    expect(commandLine(['commit', '-m', "it's done"])).toBe(`git commit -m 'it'\\''s done'`)
    expect(commandLine(['diff', ''])).toBe("git diff ''")
  })
  it('describes the outcome', () => {
    expect(outcomeText(entry(1))).toBe('Exit code 0')
    expect(outcomeText(entry(1, { outcome: 'failed', exitCode: 128 }))).toBe('Failed · exit code 128')
    expect(outcomeText(entry(1, { outcome: 'timeout', exitCode: -1 }))).toBe('Timed out')
  })
})

describe('running entries', () => {
  const running = (id: number) => entry(id, { kind: 'write', origin: 'you', args: ['push'], outcome: 'running', durationMs: 0 })
  it('replaces a running entry with its end', () => {
    const list = mergeEntries([], [running(5)], '/r')
    const done = entry(5, { kind: 'write', args: ['push'], outcome: 'ok', durationMs: 900 })
    const next = mergeEntries(list, [done], '/r')
    expect(next).toHaveLength(1)
    expect(next[0].outcome).toBe('ok')
  })
  it('never turns a finished entry back into running', () => {
    const done = entry(5, { kind: 'write', args: ['push'], outcome: 'cancelled' })
    const list = [done]
    expect(mergeEntries(list, [running(5)], '/r')).toBe(list)
  })
  it('ignores a repeated running event', () => {
    const list = mergeEntries([], [running(5)], '/r')
    expect(mergeEntries(list, [running(5)], '/r')).toBe(list)
  })
  it('counts elapsed time in whole seconds', () => {
    const e = running(1)
    const start = Date.parse(e.start)
    expect(elapsedMs(e, start + 2500)).toBe(2500)
    expect(elapsedMs(e, start - 10)).toBe(0)
    expect(formatElapsed(2500)).toBe('2 s')
    expect(formatElapsed(0)).toBe('0 s')
  })
  it('describes running and cancelled', () => {
    expect(outcomeText(running(1))).toBe('Running')
    expect(outcomeText(entry(1, { outcome: 'cancelled', exitCode: -1 }))).toBe('Cancelled')
  })
})

describe('mergeLoad', () => {
  const running = (id: number) => entry(id, { kind: 'write', origin: 'you', args: ['push'], outcome: 'running', durationMs: 0 })
  it('applies a buffered end event on top of a snapshot that still shows running', () => {
    const done = entry(5, { kind: 'write', args: ['push'], outcome: 'ok', durationMs: 900 })
    const list = mergeLoad([running(5)], [done], '/r')
    expect(list).toHaveLength(1)
    expect(list[0].outcome).toBe('ok')
  })
  it('ignores buffered events for another repo', () => {
    const list = mergeLoad([entry(1)], [entry(2, { repo: '/other' })], '/r')
    expect(list.map((e) => e.id)).toEqual([1])
  })
  it('keeps a snapshot row running when nothing buffered ended it', () => {
    const list = mergeLoad([running(5)], [], '/r')
    expect(list[0].outcome).toBe('running')
  })
})

describe('shortcut', () => {
  it('is Cmd+Shift+J on macOS', () => {
    expect(isCommandsToggle(key({ metaKey: true, shiftKey: true }), 'darwin', false)).toBe(true)
    expect(isCommandsToggle(key({ metaKey: true }), 'darwin', false)).toBe(false)
    expect(commandsShortcutLabel('darwin')).toBe('⌘⇧J')
  })
  it('is Ctrl+Shift+J elsewhere, left to the shell inside the terminal', () => {
    expect(isCommandsToggle(key({ ctrlKey: true, shiftKey: true }), 'linux', false)).toBe(true)
    expect(isCommandsToggle(key({ ctrlKey: true, shiftKey: true }), 'linux', true)).toBe(false)
    expect(commandsShortcutLabel('linux')).toBe('Ctrl+Shift+J')
  })
})
