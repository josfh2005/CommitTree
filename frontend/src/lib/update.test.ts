import { get } from 'svelte/store'
import { beforeEach, describe, expect, it, vi, type Mock } from 'vitest'
import {
  FOCUS_SPACING_MS, createUpdateController, manualCheckResult, restartBlockers, setUpdateProgress, shouldCheck, shouldShowCard,
  startUpdate, updateCardOpen, updateInfo, updatePhase,
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

  it('declining leaves the update ready and does not apply', async () => {
    confirm.mockResolvedValue(false)
    await startUpdate({ prepare, apply, confirm, blockers: () => ['x is running'] })
    expect(apply).not.toHaveBeenCalled()
    expect(get(updatePhase).phase).toBe('ready')
  })

  it('shows the download while preparing', async () => {
    let seen = ''
    prepare.mockImplementation(async () => { seen = get(updatePhase).phase })
    await startUpdate({ prepare, apply, confirm, blockers: () => [] })
    expect(seen).toBe('download')
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
