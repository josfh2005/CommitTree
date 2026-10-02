import { get } from 'svelte/store'
import { describe, expect, it, vi } from 'vitest'
import { remotesPanel, type RemotesDeps } from './remotesPanel'
import type { RemoteConfig, RemoteTest } from './types'

const remote = (name: string): RemoteConfig => ({ name, fetchURL: `u/${name}`, pushURL: `u/${name}` })

function deferred<T>() {
  let resolve!: (v: T) => void
  const promise = new Promise<T>((r) => (resolve = r))
  return { promise, resolve }
}

function deps(over: Partial<RemotesDeps> = {}): RemotesDeps {
  return {
    list: vi.fn(async (id: string) => [remote(`${id}-origin`)]),
    test: vi.fn(async () => ({ ok: true, message: 'Connected' })),
    afterWrite: vi.fn(async () => {}),
    ...over,
  }
}

describe('remotesPanel', () => {
  it('a failed write keeps git’s error after the reload and reports failure', async () => {
    const d = deps()
    const p = remotesPanel(d)
    await p.open('a')
    const ok = await p.write(async () => { throw new Error("fatal: 'a..b' is not a valid remote name") })
    expect(ok).toBe(false)
    expect(get(p.state).error).toContain('not a valid remote name')
    expect(d.afterWrite).toHaveBeenCalledWith('a')
    expect(await p.write(async () => {})).toBe(true)
    expect(get(p.state).error).toBe('')
  })

  it('opening another repository clears the old list at once and ignores a late load', async () => {
    const slow = deferred<RemoteConfig[]>()
    const d = deps({ list: vi.fn((id: string) => (id === 'a' ? slow.promise : Promise.resolve([remote('b-origin')]))) })
    const p = remotesPanel(d)
    const first = p.open('a')
    await p.open('b')
    slow.resolve([remote('a-origin')])
    await first
    expect(get(p.state).repoID).toBe('b')
    expect(get(p.state).remotes.map((r) => r.name)).toEqual(['b-origin'])
  })

  it('a test still running when the dialog reopens does not land in the new one', async () => {
    const slow = deferred<RemoteTest>()
    const p = remotesPanel(deps({ test: vi.fn(() => slow.promise) }))
    await p.open('a')
    const running = p.test('origin')
    expect(get(p.state).tests.origin).toBe('running')
    await p.open('a')
    slow.resolve({ ok: true, message: 'Connected' })
    await running
    expect(get(p.state).tests).toEqual({})
  })

  it('a test result shows under its remote', async () => {
    const p = remotesPanel(deps({ test: vi.fn(async () => ({ ok: false, message: 'Authentication failed' })) }))
    await p.open('a')
    await p.test('origin')
    expect(get(p.state).tests.origin).toEqual({ ok: false, message: 'Authentication failed' })
    p.clearTest('origin')
    expect(get(p.state).tests).toEqual({})
  })
})
