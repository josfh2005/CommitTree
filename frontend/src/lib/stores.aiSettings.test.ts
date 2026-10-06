import { get } from 'svelte/store'
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('./api', () => ({ api: { getRepoAISettings: vi.fn(), getAISettings: vi.fn() } }))

import { api } from './api'
import { aiOff, aiSettings, loadAISettings, repoAI, selectedRepoId } from './stores'
import type { AISettings, RepoAIInfo } from './types'

const settings = (model: string) => ({ chatProvider: 'ollama', chatModel: model }) as unknown as AISettings
const info = (model: string, aiOffFlag = false) => ({ effective: settings(model), aiOff: aiOffFlag }) as unknown as RepoAIInfo

function deferred<T>() {
  let resolve!: (v: T) => void
  const promise = new Promise<T>((r) => (resolve = r))
  return { promise, resolve }
}

beforeEach(() => {
  vi.mocked(api.getRepoAISettings).mockReset()
  vi.mocked(api.getAISettings).mockReset()
  selectedRepoId.set('')
  repoAI.set(null)
  aiSettings.set(null)
})

describe('loadAISettings', () => {
  it("loads the selected repository's effective settings and derives aiOff", async () => {
    vi.mocked(api.getRepoAISettings).mockResolvedValue(info('m1', true))
    selectedRepoId.set('a')
    await loadAISettings()
    expect(get(aiSettings)?.chatModel).toBe('m1')
    expect(get(aiOff)).toBe(true)
    repoAI.set(info('m1', false))
    expect(get(aiOff)).toBe(false)
    repoAI.set(null)
    expect(get(aiOff)).toBe(false)
  })

  it('loads the global settings when no repository is selected', async () => {
    vi.mocked(api.getAISettings).mockResolvedValue(settings('g'))
    await loadAISettings()
    expect(get(repoAI)).toBeNull()
    expect(get(aiSettings)?.chatModel).toBe('g')
  })

  it("clears the previous repository's settings while the next load is in flight", async () => {
    vi.mocked(api.getRepoAISettings).mockResolvedValueOnce(info('a-model'))
    selectedRepoId.set('a')
    await loadAISettings()
    const d = deferred<RepoAIInfo>()
    vi.mocked(api.getRepoAISettings).mockReturnValueOnce(d.promise)
    selectedRepoId.set('b')
    const pending = loadAISettings()
    expect(get(repoAI)).toBeNull()
    expect(get(aiSettings)).toBeNull()
    d.resolve(info('b-model'))
    await pending
    expect(get(aiSettings)?.chatModel).toBe('b-model')
  })

  it('discards a stale result that resolves after a newer load', async () => {
    const slow = deferred<RepoAIInfo>()
    vi.mocked(api.getRepoAISettings).mockReturnValueOnce(slow.promise)
    selectedRepoId.set('a')
    const first = loadAISettings()
    vi.mocked(api.getRepoAISettings).mockResolvedValueOnce(info('fresh'))
    await loadAISettings()
    slow.resolve(info('stale'))
    await first
    expect(get(aiSettings)?.chatModel).toBe('fresh')
  })

  it('discards a repository load that resolves after the selection changed', async () => {
    const slow = deferred<RepoAIInfo>()
    vi.mocked(api.getRepoAISettings).mockReturnValueOnce(slow.promise)
    selectedRepoId.set('a')
    const first = loadAISettings()
    selectedRepoId.set('')
    vi.mocked(api.getAISettings).mockResolvedValueOnce(settings('g'))
    await loadAISettings()
    slow.resolve(info('a-model'))
    await first
    expect(get(repoAI)).toBeNull()
    expect(get(aiSettings)?.chatModel).toBe('g')
  })

  it('clears to null when the load fails, never keeping another repository’s settings', async () => {
    vi.mocked(api.getRepoAISettings).mockResolvedValueOnce(info('a-model'))
    selectedRepoId.set('a')
    await loadAISettings()
    vi.mocked(api.getRepoAISettings).mockRejectedValueOnce(new Error('boom'))
    selectedRepoId.set('b')
    await loadAISettings()
    expect(get(repoAI)).toBeNull()
    expect(get(aiSettings)).toBeNull()
    expect(get(aiOff)).toBe(false)
  })
})
