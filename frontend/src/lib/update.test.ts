import { get } from 'svelte/store'
import { beforeEach, describe, expect, it, vi, type Mock } from 'vitest'
import {
  FOCUS_SPACING_MS, createUpdateController, manualCheckResult, restartBlockers, setUpdateProgress, shouldCheck, shouldShowCard,
  escClosesCard, progressText, startUpdate, updateCardOpen, updateInfo, updatePhase,
} from './update'
import type { UpdateInfo } from './types'

const info = (latest: string, newer = true): UpdateInfo => ({ current: '0.9.0', latest, newer, pageURL: `https://x/${latest}`, canInstall: true, reason: '' })

describe('shouldCheck', () => {
  it('start follows the setting', () => {
    expect(shouldCheck('start', true, null, 0)).toBe(true)
    expect(shouldCheck('start', false, null, 0)).toBe(false)
  })
  it('focus needs the setting and 6 h since the last check', () => {
    expect(shouldCheck('focus', false, null, 0)).toBe(false)
    expect(shouldCheck('focus', true, null, 0)).toBe(true)
    expect(shouldCheck('focus', true, 1000, 1000 + FOCUS_SPACING_MS - 1)).toBe(false)
    expect(shouldCheck('focus', true, 1000, 1000 + FOCUS_SPACING_MS)).toBe(true)
  })
  it('manual always checks', () => {
    expect(shouldCheck('manual', false, 5, 6)).toBe(true)
  })
})

describe('shouldShowCard', () => {
  it('never when not newer', () => {
    for (const t of ['start', 'focus', 'manual'] as const) expect(shouldShowCard(t, info('0.9.0', false), [])).toBe(false)
  })
  it('start shows even a version already shown', () => {
    expect(shouldShowCard('start', info('0.10.0'), ['0.10.0'])).toBe(true)
  })
  it('focus shows a version once', () => {
    expect(shouldShowCard('focus', info('0.10.0'), ['0.10.0'])).toBe(false)
    expect(shouldShowCard('focus', info('0.10.0'), ['0.9.5'])).toBe(true)
  })
  it('manual shows', () => {
    expect(shouldShowCard('manual', info('0.10.0'), ['0.10.0'])).toBe(true)
  })
})

describe('createUpdateController', () => {
  let t: number
  let auto: boolean
  let check: Mock<[], Promise<UpdateInfo>>
  const make = () => createUpdateController({ check, now: () => t, auto: () => auto })

  beforeEach(() => {
    t = 1_000_000
    auto = true
    check = vi.fn().mockResolvedValue(info('0.10.0'))
    updateInfo.set(null)
    updateCardOpen.set(false)
    manualCheckResult.set('')
  })

  it('start with a newer version opens the card', async () => {
    await make().run('start')
    expect(get(updateCardOpen)).toBe(true)
    expect(get(updateInfo)?.latest).toBe('0.10.0')
  })

  it('start does nothing when automatic checks are off', async () => {
    auto = false
    await make().run('start')
    expect(check).not.toHaveBeenCalled()
    expect(get(updateCardOpen)).toBe(false)
  })

  it('a focus within 6 h does not check', async () => {
    const c = make()
    await c.run('start')
    t += FOCUS_SPACING_MS - 1
    await c.run('focus')
    expect(check).toHaveBeenCalledTimes(1)
  })

  it('a focus after 6 h finding the same version does not reopen the card', async () => {
    const c = make()
    await c.run('start')
    updateCardOpen.set(false)
    t += FOCUS_SPACING_MS
    await c.run('focus')
    expect(check).toHaveBeenCalledTimes(2)
    expect(get(updateCardOpen)).toBe(false)
    expect(get(updateInfo)?.latest).toBe('0.10.0')
  })

  it('a focus finding a newer version opens the card', async () => {
    const c = make()
    await c.run('start')
    updateCardOpen.set(false)
    check.mockResolvedValue(info('0.11.0'))
    t += FOCUS_SPACING_MS
    await c.run('focus')
    expect(get(updateCardOpen)).toBe(true)
    expect(get(updateInfo)?.latest).toBe('0.11.0')
  })

  it('a manual check counts as a showing', async () => {
    const c = make()
    await c.run('manual')
    expect(get(updateCardOpen)).toBe(true)
    updateCardOpen.set(false)
    t += FOCUS_SPACING_MS
    await c.run('focus')
    expect(get(updateCardOpen)).toBe(false)
  })

  it('a focus that finds nothing newer leaves a shown version alone', async () => {
    const c = make()
    await c.run('start')
    check.mockResolvedValue(info('0.9.0', false))
    t += FOCUS_SPACING_MS
    await c.run('focus')
    expect(get(updateCardOpen)).toBe(true)
  })

  it('manual results', async () => {
    const c = make()
    await c.run('manual')
    expect(get(manualCheckResult)).toBe('0.10.0 is available')
    check.mockResolvedValue(info('0.9.0', false))
    await c.run('manual')
    expect(get(manualCheckResult)).toBe("You're up to date")
    check.mockRejectedValue(new Error('offline'))
    await c.run('manual')
    expect(get(manualCheckResult)).toBe('offline')
  })

  it('a manual check works with automatic checks off', async () => {
    auto = false
    await make().run('manual')
    expect(check).toHaveBeenCalledTimes(1)
  })

  it('swallows errors on start and focus, and still advances lastCheck', async () => {
    check.mockRejectedValue(new Error('offline'))
    const c = make()
    await expect(c.run('start')).resolves.toBeUndefined()
    expect(get(updateInfo)).toBeNull()
    expect(get(manualCheckResult)).toBe('')
    t += FOCUS_SPACING_MS - 1
    await c.run('focus')
    expect(check).toHaveBeenCalledTimes(1)
    t += 1
    await expect(c.run('focus')).resolves.toBeUndefined()
    expect(check).toHaveBeenCalledTimes(2)
    expect(get(updateInfo)).toBeNull()
  })

  it('a failed check keeps the info already known', async () => {
    const c = make()
    await c.run('start')
    check.mockRejectedValue(new Error('offline'))
    t += FOCUS_SPACING_MS
    await c.run('focus')
    expect(get(updateInfo)?.latest).toBe('0.10.0')
  })
})

describe('restartBlockers', () => {
  it('is empty when nothing blocks', () => {
    expect(restartBlockers([], '', '')).toEqual([])
  })
  it('ignores a whitespace-only draft', () => {
    expect(restartBlockers([], '', ' \n\t ')).toEqual([])
  })
  it('lists every reason in order', () => {
    expect(restartBlockers([{ label: 'git push' }, { label: 'git fetch' }], 'repo1', 'wip')).toEqual([
      'git push is running', 'git fetch is running', 'The chat is answering', 'The commit message has unsaved text',
    ])
  })
})

describe('startUpdate', () => {
  let prepare: Mock<[], Promise<void>>
  let apply: Mock<[], Promise<void>>
  let confirm: Mock<[string[]], Promise<boolean>>

  beforeEach(() => {
    prepare = vi.fn().mockResolvedValue(undefined)
    apply = vi.fn().mockResolvedValue(undefined)
    confirm = vi.fn().mockResolvedValue(true)
    updatePhase.set({ phase: 'idle', done: 0, total: 0, error: '' })
  })

  it('applies without asking when nothing blocks', async () => {
    await startUpdate({ prepare, apply, confirm, blockers: () => [] })
    expect(confirm).not.toHaveBeenCalled()
    expect(prepare).toHaveBeenCalledTimes(1)
    expect(apply).toHaveBeenCalledTimes(1)
  })

  it('asks with the reasons, after prepare and before apply', async () => {
    const order: string[] = []
    prepare.mockImplementation(async () => { order.push('prepare') })
    confirm.mockImplementation(async () => { order.push('confirm'); return true })
    apply.mockImplementation(async () => { order.push('apply') })
    await startUpdate({ prepare, apply, confirm, blockers: () => ['The chat is answering'] })
    expect(confirm).toHaveBeenCalledWith(['The chat is answering'])
    expect(order).toEqual(['prepare', 'confirm', 'apply'])
  })

  it('declining does not apply and puts the card back to idle', async () => {
    confirm.mockResolvedValue(false)
    await startUpdate({ prepare, apply, confirm, blockers: () => ['x is running'] })
    expect(apply).not.toHaveBeenCalled()
    expect(get(updatePhase)).toEqual({ phase: 'idle', done: 0, total: 0, error: '' })
  })

  it('shows the download while preparing', async () => {
    let seen = ''
    prepare.mockImplementation(async () => { seen = get(updatePhase).phase })
    await startUpdate({ prepare, apply, confirm, blockers: () => [] })
    expect(seen).toBe('download')
  })

  it('starts the download with an unknown total, so the card reads "Downloading…"', async () => {
    let seen: { done: number; total: number } | null = null
    prepare.mockImplementation(async () => { const p = get(updatePhase); seen = { done: p.done, total: p.total } })
    await startUpdate({ prepare, apply, confirm, blockers: () => [] })
    expect(seen).toEqual({ done: 0, total: -1 })
    expect(progressText(seen!.done, seen!.total)).toBe('Downloading…')
  })

  it('waits for the dialogs to close before asking and before applying', async () => {
    const order: string[] = []
    let release!: () => void
    const quiet = () => new Promise<void>((r) => { release = r; order.push('quiet') })
    confirm.mockImplementation(async () => { order.push('confirm'); return true })
    apply.mockImplementation(async () => { order.push('apply') })
    const run = startUpdate({ prepare, apply, confirm, blockers: () => ['x is running'], quiet })
    await vi.waitFor(() => expect(order).toEqual(['quiet']))
    expect(get(updatePhase).phase).toBe('ready')
    expect(confirm).not.toHaveBeenCalled()
    release()
    await run
    expect(order).toEqual(['quiet', 'confirm', 'apply'])
  })

  it('does not apply while a dialog is open even when nothing blocks', async () => {
    let release!: () => void
    const quiet = () => new Promise<void>((r) => { release = r })
    const run = startUpdate({ prepare, apply, confirm, blockers: () => [], quiet })
    await vi.waitFor(() => expect(get(updatePhase).phase).toBe('ready'))
    await Promise.resolve()
    expect(apply).not.toHaveBeenCalled()
    release()
    await run
    expect(apply).toHaveBeenCalledTimes(1)
  })

  it('"already up to date" refreshes the check and drops the card once nothing is newer', async () => {
    updateInfo.set(info('0.10.0'))
    prepare.mockRejectedValue('CommitTree is already up to date')
    const refresh = vi.fn(async () => updateInfo.set(info('0.9.0', false)))
    await startUpdate({ prepare, apply, confirm, blockers: () => [], refresh })
    expect(refresh).toHaveBeenCalledTimes(1)
    expect(get(updateInfo)?.newer).toBe(false)
    expect(get(updatePhase)).toEqual({ phase: 'idle', done: 0, total: 0, error: '' })
  })

  it('"already up to date" keeps the error when the refresh fails or still finds an update', async () => {
    updateInfo.set(info('0.10.0'))
    prepare.mockRejectedValue('CommitTree is already up to date')
    await startUpdate({ prepare, apply, confirm, blockers: () => [], refresh: async () => { throw new Error('offline') } })
    expect(get(updatePhase).phase).toBe('error')
  })

  it('other errors do not refresh the check', async () => {
    prepare.mockRejectedValue("Couldn't download the update")
    const refresh = vi.fn(async () => {})
    await startUpdate({ prepare, apply, confirm, blockers: () => [], refresh })
    expect(refresh).not.toHaveBeenCalled()
  })

  it('names the staged version when it differs from what the card showed', async () => {
    updateInfo.set(info('0.10.0'))
    await startUpdate({ prepare, apply, confirm, blockers: () => [], staged: async () => '0.11.0' })
    expect(get(updateInfo)?.latest).toBe('0.11.0')
  })

  it('keeps the card as it was when the staged version matches or is unknown', async () => {
    updateInfo.set(info('0.10.0'))
    await startUpdate({ prepare, apply, confirm, blockers: () => [], staged: async () => '0.10.0' })
    expect(get(updateInfo)).toEqual(info('0.10.0'))
    await startUpdate({ prepare, apply, confirm, blockers: () => [], staged: async () => { throw new Error('x') } })
    expect(get(updateInfo)).toEqual(info('0.10.0'))
  })

  it('a rejected prepare is an error with its message', async () => {
    prepare.mockRejectedValue('Couldn\'t download the update')
    await startUpdate({ prepare, apply, confirm, blockers: () => [] })
    expect(get(updatePhase)).toMatchObject({ phase: 'error', error: "Couldn't download the update" })
    expect(apply).not.toHaveBeenCalled()
  })

  it('a cancelled prepare goes back to idle without an error', async () => {
    prepare.mockRejectedValue('context canceled')
    await startUpdate({ prepare, apply, confirm, blockers: () => [] })
    expect(get(updatePhase)).toEqual({ phase: 'idle', done: 0, total: 0, error: '' })
    expect(apply).not.toHaveBeenCalled()
  })

  it('a rejected apply is an error with its message', async () => {
    apply.mockRejectedValue(new Error("Couldn't install the update: busy"))
    await startUpdate({ prepare, apply, confirm, blockers: () => [] })
    expect(get(updatePhase)).toMatchObject({ phase: 'error', error: "Couldn't install the update: busy" })
  })

  it('ignores a second start while one is running', async () => {
    updatePhase.set({ phase: 'download', done: 1, total: 2, error: '' })
    await startUpdate({ prepare, apply, confirm, blockers: () => [] })
    expect(prepare).not.toHaveBeenCalled()
  })

  it('ignores a second start while the restart question is open', async () => {
    updatePhase.set({ phase: 'ready', done: 0, total: 0, error: '' })
    await startUpdate({ prepare, apply, confirm, blockers: () => [] })
    expect(prepare).not.toHaveBeenCalled()
    expect(apply).not.toHaveBeenCalled()
    expect(get(updatePhase).phase).toBe('ready')
  })

  it('after an error a new start downloads again', async () => {
    updatePhase.set({ phase: 'error', done: 0, total: 0, error: 'x' })
    await startUpdate({ prepare, apply, confirm, blockers: () => [] })
    expect(prepare).toHaveBeenCalledTimes(1)
  })

  it('after declining, a new start downloads again', async () => {
    confirm.mockResolvedValue(false)
    await startUpdate({ prepare, apply, confirm, blockers: () => ['x is running'] })
    await startUpdate({ prepare, apply, confirm, blockers: () => ['x is running'] })
    expect(prepare).toHaveBeenCalledTimes(2)
  })
})

describe('progressText', () => {
  it('shows a percentage when the total is known', () => {
    expect(progressText(42, 100)).toBe('Downloading 42 %')
    expect(progressText(0, 100)).toBe('Downloading 0 %')
  })
  it('shows the bytes received when the total is unknown', () => {
    expect(progressText(3 * 1024 * 1024, -1)).toBe('Downloading 3 MB')
  })
  it('shows no number before the first byte of an unknown total', () => {
    expect(progressText(0, -1)).toBe('Downloading…')
  })
})

describe('setUpdateProgress', () => {
  beforeEach(() => updatePhase.set({ phase: 'idle', done: 0, total: 0, error: '' }))
  it('follows progress events while an install runs', () => {
    updatePhase.set({ phase: 'download', done: 0, total: 0, error: '' })
    setUpdateProgress({ phase: 'download', done: 5, total: 10 })
    expect(get(updatePhase)).toMatchObject({ phase: 'download', done: 5, total: 10 })
    setUpdateProgress({ phase: 'install', done: 10, total: 10 })
    expect(get(updatePhase).phase).toBe('install')
  })
  it('ignores a late event once the install ended', () => {
    setUpdateProgress({ phase: 'download', done: 5, total: 10 })
    expect(get(updatePhase).phase).toBe('idle')
  })
})

describe('escClosesCard', () => {
  const esc = { key: 'Escape', defaultPrevented: false }
  it('closes on a plain Esc', () => expect(escClosesCard(esc, false)).toBe(true))
  it('ignores other keys', () => expect(escClosesCard({ key: 'a', defaultPrevented: false }, false)).toBe(false))
  it('leaves Esc to a dialog or Settings that was open', () => expect(escClosesCard(esc, true)).toBe(false))
  it('leaves an Esc somebody already handled', () => expect(escClosesCard({ ...esc, defaultPrevented: true }, false)).toBe(false))
})
