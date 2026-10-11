import { describe, expect, it, vi } from 'vitest'
import { cancelTask, elapsedText, outcomeIcon, openAction } from './statusBar'
import type { Repo } from './types'

const repos = [{ id: 'a', name: 'alpha', missing: false }, { id: 'm', name: 'gone', missing: true }] as Repo[]

describe('elapsedText', () => {
  it('is empty for the first second, then whole seconds', () => {
    expect(elapsedText(1000, 1999)).toBe('')
    expect(elapsedText(1000, 2000)).toBe('1 s')
    expect(elapsedText(1000, 13_500)).toBe('12 s')
  })
})

describe('outcomeIcon', () => {
  it('maps outcomes to ✓ ✗ ⊘', () => {
    expect(outcomeIcon('ok')).toBe('✓')
    expect(outcomeIcon('failed')).toBe('✗')
    expect(outcomeIcon('cancelled')).toBe('⊘')
  })
})

describe('openAction', () => {
  it('reopens a running clone, but not a finished one', () => {
    expect(openAction({ kind: 'clone', repoId: '' }, true, repos)).toEqual({ kind: 'clone' })
    expect(openAction({ kind: 'clone', repoId: '' }, false, repos)).toBeNull()
  })
  it('opens Settings → Providers for a model download', () => {
    expect(openAction({ kind: 'model', repoId: '' }, false, repos)).toEqual({ kind: 'providers' })
  })
  it('opens the Commands panel of a present repository, nothing otherwise', () => {
    for (const kind of ['op', 'fetch', 'git'] as const) {
      expect(openAction({ kind, repoId: 'a' }, false, repos)).toEqual({ kind: 'commands', repoId: 'a' })
    }
    expect(openAction({ kind: 'op', repoId: 'm' }, false, repos)).toBeNull()
    expect(openAction({ kind: 'op', repoId: 'x' }, false, repos)).toBeNull()
  })
})

describe('cancelTask', () => {
  const command = { kind: 'command', repoId: 'a', entryId: 7 } as const
  const deps = (cancelCommand: () => Promise<void>) => ({ cancelPull: vi.fn().mockResolvedValue(undefined), cancelCommand: vi.fn(cancelCommand), toast: vi.fn() })

  it('keeps Cancelling… after a cancel that went through', async () => {
    const d = deps(async () => {})
    expect(await cancelTask(command, d)).toBe(true)
    expect(d.cancelCommand).toHaveBeenCalledWith('a', 7)
    expect(d.toast).not.toHaveBeenCalled()
  })
  it('cancels a model download with cancelPull', async () => {
    const d = deps(async () => {})
    expect(await cancelTask({ kind: 'model' }, d)).toBe(true)
    expect(d.cancelPull).toHaveBeenCalled()
    expect(d.cancelCommand).not.toHaveBeenCalled()
  })
  it('clears the flag, silently, when the command had already ended', async () => {
    const d = deps(async () => { throw new Error('command is not running') })
    expect(await cancelTask(command, d)).toBe(false)
    expect(d.toast).not.toHaveBeenCalled()
  })
  it('clears the flag and toasts any other failure', async () => {
    const d = deps(async () => { throw new Error('boom') })
    expect(await cancelTask(command, d)).toBe(false)
    expect(d.toast).toHaveBeenCalledWith('boom', 'error')
  })
})
