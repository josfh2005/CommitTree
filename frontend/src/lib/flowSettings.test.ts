import { get } from 'svelte/store'
import { describe, expect, it, vi } from 'vitest'
import { flowSettingsError, flowSettingsPanel, type FlowSettingsDeps } from './flowSettings'
import type { FlowConfig, FlowSettings } from './types'

const settings = (over: Partial<FlowSettings> = {}): FlowSettings => ({
  initialized: true, problem: '', master: 'main', develop: 'develop',
  prefixes: { feature: 'feature/', release: 'release/', hotfix: 'hotfix/', warmfix: 'warmfix/' },
  ...over,
})
const config = (over: Partial<FlowConfig> = {}): FlowConfig => ({
  master: 'main', develop: 'develop',
  prefixes: { feature: 'feature/', release: 'release/', hotfix: 'hotfix/', warmfix: 'warmfix/' },
  ...over,
})

function deps(over: Partial<FlowSettingsDeps> = {}): FlowSettingsDeps {
  return {
    get: vi.fn(async () => settings()),
    save: vi.fn(async () => {}),
    init: vi.fn(async () => {}),
    afterWrite: vi.fn(async () => {}),
    ...over,
  }
}

describe('flowSettingsError', () => {
  it('accepts the usual names and any prefix without spaces', () => {
    expect(flowSettingsError(config())).toBe('')
    expect(flowSettingsError(config({ master: 'prod', develop: 'dev/next', prefixes: { feature: 'f-', release: 'r_', hotfix: 'hf/', warmfix: 'w/' } }))).toBe('')
  })
  it('asks for every name and prefix', () => {
    expect(flowSettingsError(config({ master: ' ' }))).toBe('Production branch is required')
    expect(flowSettingsError(config({ develop: '' }))).toBe('Development branch is required')
    expect(flowSettingsError(config({ prefixes: { ...config().prefixes, release: '' } }))).toBe('Release prefix is required')
    expect(flowSettingsError(config({ prefixes: { ...config().prefixes, warmfix: '' } }))).toBe('Warmfix prefix is required')
  })
  it('refuses names git would refuse', () => {
    expect(flowSettingsError(config({ master: 'my main' }))).toBe('Production branch is not a valid branch name')
    expect(flowSettingsError(config({ develop: 'a..b' }))).toBe('Development branch is not a valid branch name')
    expect(flowSettingsError(config({ develop: '-dev' }))).toBe('Development branch is not a valid branch name')
    expect(flowSettingsError(config({ master: 'x/' }))).toBe('Production branch is not a valid branch name')
    expect(flowSettingsError(config({ master: 'x.lock' }))).toBe('Production branch is not a valid branch name')
    expect(flowSettingsError(config({ master: 'a~b' }))).toBe('Production branch is not a valid branch name')
  })
  it('wants two different branches', () => {
    expect(flowSettingsError(config({ develop: 'main' }))).toBe('Production and development must be different branches')
  })
  it('refuses spaces and invalid characters in a prefix', () => {
    expect(flowSettingsError(config({ prefixes: { ...config().prefixes, feature: 'my feature/' } }))).toBe('Feature prefix has spaces')
    expect(flowSettingsError(config({ prefixes: { ...config().prefixes, hotfix: 'fix~/' } }))).toBe('Hotfix prefix is not valid')
  })
})

describe('flowSettingsPanel', () => {
  it('loads the repository’s values into the form', async () => {
    const d = deps({ get: vi.fn(async () => settings({ master: 'prod', develop: 'dev', prefixes: { feature: 'f/', release: 'r/', hotfix: 'h/', warmfix: 'w/' } })) })
    const p = flowSettingsPanel(d)
    await p.open('a')
    expect(d.get).toHaveBeenCalledWith('a')
    expect(get(p.form)).toEqual({ master: 'prod', develop: 'dev', prefixes: { feature: 'f/', release: 'r/', hotfix: 'h/', warmfix: 'w/' } })
    expect(get(p.state).settings?.initialized).toBe(true)
    expect(get(p.state).loadError).toBe('')
    expect(get(p.dirty)).toBe(false)
  })

  it('a set-up repository saves through save, trimmed, then refreshes', async () => {
    const d = deps()
    const p = flowSettingsPanel(d)
    await p.open('a')
    p.form.update((f) => ({ ...f, develop: ' integration ' }))
    expect(get(p.dirty)).toBe(true)
    expect(await p.save()).toBe(true)
    expect(d.save).toHaveBeenCalledWith('a', config({ develop: 'integration' }))
    expect(d.init).not.toHaveBeenCalled()
    expect(d.afterWrite).toHaveBeenCalledWith('a')
    expect(d.get).toHaveBeenCalledTimes(2)
  })

  it('a repository that is not set up runs the init path', async () => {
    const d = deps({ get: vi.fn(async () => settings({ initialized: false })) })
    const p = flowSettingsPanel(d)
    await p.open('a')
    expect(await p.save()).toBe(true)
    expect(d.init).toHaveBeenCalledWith('a', config())
    expect(d.save).not.toHaveBeenCalled()
    expect(d.afterWrite).toHaveBeenCalledWith('a')
  })

  it('an invalid form is not sent', async () => {
    const d = deps()
    const p = flowSettingsPanel(d)
    await p.open('a')
    p.form.update((f) => ({ ...f, develop: f.master }))
    expect(await p.save()).toBe(false)
    expect(d.save).not.toHaveBeenCalled()
    expect(get(p.state).error).toBe('Production and development must be different branches')
  })

  it('a refused save keeps what was typed and shows git’s error', async () => {
    const d = deps({ save: vi.fn(async () => { throw new Error('invalid branch name: "x"') }) })
    const p = flowSettingsPanel(d)
    await p.open('a')
    p.form.update((f) => ({ ...f, develop: 'x' }))
    expect(await p.save()).toBe(false)
    expect(get(p.state).error).toContain('invalid branch name')
    expect(get(p.form).develop).toBe('x')
    expect(d.afterWrite).toHaveBeenCalledWith('a')
    expect(await p.save()).toBe(false)
  })

  it('a failed load is reported and nothing can be saved', async () => {
    const p = flowSettingsPanel(deps({ get: vi.fn(async () => { throw new Error('boom') }) }))
    await p.open('a')
    expect(get(p.state).loadError).toBe('boom')
    expect(get(p.state).settings).toBeNull()
    expect(await p.save()).toBe(false)
  })

  it('a load that answers after the dialog moved on is dropped', async () => {
    let resolve!: (s: FlowSettings) => void
    const slow = new Promise<FlowSettings>((r) => (resolve = r))
    const p = flowSettingsPanel(deps({ get: vi.fn((id: string) => (id === 'a' ? slow : Promise.resolve(settings({ master: 'b-main' })))) }))
    const first = p.open('a')
    await p.open('b')
    resolve(settings({ master: 'a-main' }))
    await first
    expect(get(p.state).repoID).toBe('b')
    expect(get(p.form).master).toBe('b-main')
  })
})
