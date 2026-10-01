import { get } from 'svelte/store'
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('./api', () => ({ api: { finishFlow: vi.fn(), planFinish: vi.fn(), notify: vi.fn().mockResolvedValue(undefined) } }))

// refreshRepo reloads everything through the api; these tests only care
// about the finish itself.
vi.mock('./stores', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./stores')>()),
  refreshRepo: vi.fn().mockResolvedValue(undefined),
}))

import { runFinish } from './flowActions'
import { api } from './api'
import { mergeState, pendingFinish, selectedRepoId } from './stores'
import type { FlowPlan, MergeState } from './types'
import { toasts } from './ui'
import { windowFocused } from './notify'

beforeEach(() => {
  pendingFinish.set(null)
  toasts.set([])
  vi.mocked(api.finishFlow).mockReset()
  vi.mocked(api.planFinish).mockReset()
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
    selectedRepoId.set('r1')
    vi.mocked(api.finishFlow).mockRejectedValue(new Error('commit or stash your changes first'))
    await runFinish('r1', 'feature/f', [])
    expect(get(toasts).at(-1)).toMatchObject({ kind: 'error', message: 'commit or stash your changes first' })
  })

  it('a conflicted finish notifies the conflicts', async () => {
    windowFocused.set(false)
    vi.mocked(api.finishFlow).mockResolvedValue({ outcome: 'conflicted', target: 'develop', conflicts: ['a', 'b'], merged: [], notes: [] })
    await runFinish('r1', 'hotfix/h', [])
    expect(vi.mocked(api.notify).mock.calls.at(-1)?.[0]).toMatchObject({ id: 'r1:problem', body: 'Conflicts in 2 files after the git-flow' })
  })
})

describe('after the conflicted merge', () => {
  const state = (merging: boolean) => ({ kind: merging ? 'merge' : '', merging }) as unknown as MergeState
  const plan = (done: boolean): FlowPlan => ({ branch: 'hotfix/h', type: 'hotfix', steps: [{ target: 'master', done: true }, { target: 'develop', done }], ending: 'develop' })
  const endMerge = async (repo: string) => {
    selectedRepoId.set(repo)
    mergeState.set(state(true))
    mergeState.set(state(false))
    await new Promise((r) => setTimeout(r))
  }

  beforeEach(() => {
    pendingFinish.set({ repoId: 'r1', branch: 'hotfix/h', releases: [], target: 'develop' })
  })

  it('offers to continue once the target has the branch, however the merge was committed', async () => {
    vi.mocked(api.planFinish).mockResolvedValue(plan(true))
    await endMerge('r1')
    expect(get(pendingFinish)).toBeNull()
    expect(get(toasts).at(-1)?.action?.label).toBe('Continue finishing hotfix/h')
  })

  it('drops it quietly when the merge was aborted', async () => {
    vi.mocked(api.planFinish).mockResolvedValue(plan(false))
    await endMerge('r1')
    expect(get(pendingFinish)).toBeNull()
    expect(get(toasts)).toEqual([])
  })

  it('leaves another repository’s finish alone', async () => {
    await endMerge('r2')
    expect(get(pendingFinish)).not.toBeNull()
    expect(api.planFinish).not.toHaveBeenCalled()
  })
})
