import { get } from 'svelte/store'
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('./api', () => ({ api: { finishFlow: vi.fn() } }))

// refreshRepo reloads everything through the api; these tests only care
// about the finish itself.
vi.mock('./stores', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./stores')>()),
  refreshRepo: vi.fn().mockResolvedValue(undefined),
}))

import { forgetPendingFinish, offerContinueFinish, runFinish } from './flowActions'
import { api } from './api'
import { pendingFinish } from './stores'
import { toasts } from './ui'

beforeEach(() => {
  pendingFinish.set(null)
  toasts.set([])
  vi.mocked(api.finishFlow).mockReset()
})

describe('runFinish', () => {
  it('remembers a conflicted finish', async () => {
    vi.mocked(api.finishFlow).mockResolvedValue({ outcome: 'conflicted', target: 'develop', conflicts: ['a'], merged: ['master'], notes: [] })
    await runFinish('r1', 'hotfix/h', [])
    expect(get(pendingFinish)).toEqual({ repoId: 'r1', branch: 'hotfix/h', releases: [], target: 'develop' })
  })

  it('reports a finished one and forgets any pending', async () => {
    pendingFinish.set({ repoId: 'r1', branch: 'hotfix/h', releases: [], target: 'develop' })
    vi.mocked(api.finishFlow).mockResolvedValue({ outcome: 'finished', target: '', conflicts: [], merged: ['develop'], notes: [] })
    await runFinish('r1', 'hotfix/h', [])
    expect(get(pendingFinish)).toBeNull()
    expect(get(toasts).at(-1)?.message).toBe('Finished hotfix/h: merged into develop; branch deleted.')
  })

  it('shows an error as an error toast', async () => {
    vi.mocked(api.finishFlow).mockRejectedValue(new Error('commit or stash your changes first'))
    await runFinish('r1', 'feature/f', [])
    expect(get(toasts).at(-1)).toMatchObject({ kind: 'error', message: 'commit or stash your changes first' })
  })
})

describe('after the conflicted merge', () => {
  it('offers to continue once the merge is committed', () => {
    pendingFinish.set({ repoId: 'r1', branch: 'hotfix/h', releases: [], target: 'develop' })
    offerContinueFinish('r1')
    expect(get(pendingFinish)).toBeNull()
    expect(get(toasts).at(-1)?.action?.label).toBe('Continue finishing hotfix/h')
  })

  it('does nothing for another repository', () => {
    pendingFinish.set({ repoId: 'r1', branch: 'hotfix/h', releases: [], target: 'develop' })
    offerContinueFinish('r2')
    expect(get(pendingFinish)).not.toBeNull()
  })

  it('forgets it when the merge is aborted', () => {
    pendingFinish.set({ repoId: 'r1', branch: 'hotfix/h', releases: [], target: 'develop' })
    forgetPendingFinish('r1')
    expect(get(pendingFinish)).toBeNull()
  })
})
