import { get } from 'svelte/store'
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('./api', () => ({ api: { notify: vi.fn(), listRepos: vi.fn().mockResolvedValue([]) } }))

// The real selectRepo loads refs, merge state, … through api calls this
// mock lacks; only the selection matters here.
vi.mock('./stores', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./stores')>()
  return { ...actual, selectRepo: vi.fn((id: string) => actual.selectedRepoId.set(id)) }
})

import { api } from './api'
import { notify, notifyPipelineEvent, notifyStashConflicts, openTarget, opError, startNotifications, track, windowFocused } from './notify'
import { chatOpen, mainView, mergeState, notifyEnabled, notifyPipelines, notifyRemote, repos, selectedHash, selectedRepoId } from './stores'
import type { MergeState, Repo } from './types'
import { toasts } from './ui'

const repo = (id: string, name: string) => ({ id, name, path: `/x/${id}`, missing: false, branch: 'main' }) as Repo

beforeEach(() => {
  vi.mocked(api.notify).mockReset().mockResolvedValue(undefined)
  toasts.set([])
  repos.set([repo('a', 'alpha'), repo('b', 'beta')])
  selectedRepoId.set('a')
  chatOpen.set(true)
  notifyEnabled.set(true)
  notifyRemote.set(true)
  notifyPipelines.set(true)
  windowFocused.set(true)
})

describe('notify', () => {
  it('honours the New commits on the remote switch', async () => {
    windowFocused.set(false)
    notifyRemote.set(false)
    expect(await notify({ category: 'remote', repoID: 'b', body: '1 new commit on origin/main', target: 'repo' })).toBe('none')
    notifyRemote.set(true)
    expect(await notify({ category: 'remote', repoID: 'b', body: '1 new commit on origin/main', target: 'repo' })).toBe('system')
  })

  it('honours the Pipelines switch and sends the commit with the notification', async () => {
    windowFocused.set(false)
    notifyPipelines.set(false)
    const e = { category: 'pipelines' as const, repoID: 'b', body: 'Pipeline passed on main', target: 'repo' as const, hash: 'h1' }
    expect(await notify(e)).toBe('none')
    notifyPipelines.set(true)
    expect(await notify(e)).toBe('system')
    expect(api.notify).toHaveBeenCalledWith({ id: 'b:pipelines', title: 'beta', body: 'Pipeline passed on main', repoID: 'b', target: 'repo', hash: 'h1' })
  })

  it('sends a system notification when unfocused', async () => {
    windowFocused.set(false)
    expect(await notify({ category: 'problem', repoID: 'b', body: 'Push failed: x', target: 'repo' })).toBe('system')
    expect(api.notify).toHaveBeenCalledWith({ id: 'b:problem', title: 'beta', body: 'Push failed: x', repoID: 'b', target: 'repo', hash: undefined })
    expect(get(toasts)).toEqual([])
  })

  it('falls back to a toast when the OS notification fails', async () => {
    windowFocused.set(false)
    vi.mocked(api.notify).mockRejectedValue('notifications require a valid bundle identifier')
    await notify({ category: 'ai', repoID: 'a', body: 'The AI has a decision for you', target: 'chat' })
    expect(get(toasts).map((t) => [t.message, t.kind, t.action?.label])).toEqual([['alpha: The AI has a decision for you', 'info', 'View']])
  })

  it('toasts out-of-sight events and stays silent for in-sight ones', async () => {
    await notify({ category: 'problem', repoID: 'a', body: 'Conflicts in 1 file after the merge', target: 'repo' })
    expect(get(toasts)).toEqual([])
    await notify({ category: 'problem', repoID: 'b', body: 'Conflicts in 1 file after the merge', target: 'repo' })
    expect(get(toasts).map((t) => t.message)).toEqual(['beta: Conflicts in 1 file after the merge'])
    expect(api.notify).not.toHaveBeenCalled()
  })

  it('a toasted failure adds no second toast', async () => {
    await notify({ category: 'problem', repoID: 'b', body: 'Push failed: x', target: 'repo', toasted: true })
    expect(get(toasts)).toEqual([])
  })

  it('falls back to the app name for an unknown repository', async () => {
    windowFocused.set(false)
    await notify({ category: 'problem', repoID: 'gone', body: 'x', target: 'repo' })
    expect(vi.mocked(api.notify).mock.calls[0][0].title).toBe('CommitTree')
  })

  it('titles a clone credential notification (no repository id) "CommitTree"', async () => {
    windowFocused.set(false)
    await notify({ category: 'problem', repoID: '', body: 'CommitTree needs credentials for github.com (Clone · github.com/o/r)', target: 'repo' })
    expect(vi.mocked(api.notify).mock.calls[0][0].title).toBe('CommitTree')
    vi.mocked(api.notify).mockRejectedValue('no notifications')
    await notify({ category: 'problem', repoID: '', body: 'x', target: 'repo' })
    expect(get(toasts).map((t) => t.message)).toEqual(['CommitTree: x'])
  })
})

describe('notifyPipelineEvent', () => {
  const we = (over = {}) => ({ repoID: 'b', branch: 'feature/x', hash: 'h1', state: 'failed' as const, final: true, noun: 'Checks' as const, ...over })

  it('notifies a final event with the branch and the commit', async () => {
    windowFocused.set(false)
    notifyPipelineEvent(we())
    await Promise.resolve()
    expect(api.notify).toHaveBeenCalledWith({ id: 'b:pipelines', title: 'beta', body: 'Checks failed on feature/x', repoID: 'b', target: 'repo', hash: 'h1' })
  })

  it('ignores an event that is not final', async () => {
    windowFocused.set(false)
    notifyPipelineEvent(we({ final: false, state: 'running' }))
    await Promise.resolve()
    expect(api.notify).not.toHaveBeenCalled()
  })
})

describe('openTarget', () => {
  it('selects the repository and opens the chat for chat targets', () => {
    chatOpen.set(false)
    openTarget('b', 'chat')
    expect(get(selectedRepoId)).toBe('b')
    expect(get(chatOpen)).toBe(true)
  })

  it('selects that commit in the log when a hash is given', () => {
    openTarget('b', 'repo', 'h1')
    expect(get(selectedRepoId)).toBe('b')
    expect(get(selectedHash)).toBe('h1')
    expect(get(mainView)).toBe('log')
  })

  it('openTarget ignores unknown repositories', () => {
    openTarget('gone', 'repo')
    expect(get(selectedRepoId)).toBe('a')
  })
})

describe('opError', () => {
  it('keeps the plain error toast for the selected repository', () => {
    opError('a', new Error('boom'))
    expect(get(toasts).map((t) => [t.message, t.kind, t.action])).toEqual([['boom', 'error', undefined]])
  })

  it('opError prefixes other repositories', () => {
    opError('b', 'boom')
    expect(get(toasts).map((t) => [t.message, t.kind, t.action?.label])).toEqual([['beta: boom', 'error', 'View']])
  })
})

describe('track', () => {
  it('notifies done only past the threshold', async () => {
    windowFocused.set(false)
    const now = vi.spyOn(Date, 'now')
    now.mockReturnValueOnce(0).mockReturnValueOnce(4_000)
    await track('a', 'push', async () => undefined)
    expect(api.notify).not.toHaveBeenCalled()
    now.mockReturnValueOnce(0).mockReturnValueOnce(14_400)
    await track('a', 'push', async () => undefined)
    expect(vi.mocked(api.notify).mock.calls[0][0]).toMatchObject({ id: 'a:done', body: 'Push finished · 14 s' })
    now.mockRestore()
  })

  it('conflicts beat finished', async () => {
    windowFocused.set(false)
    const r = await track('a', 'merge', async () => ({ conflicts: ['x', 'y'] }), (r) => r.conflicts.length)
    expect(r.conflicts).toEqual(['x', 'y'])
    expect(api.notify).toHaveBeenCalledTimes(1)
    expect(vi.mocked(api.notify).mock.calls[0][0]).toMatchObject({ id: 'a:problem', body: 'Conflicts in 2 files after the merge' })
  })

  it('a failure notifies the problem and rethrows', async () => {
    windowFocused.set(false)
    await expect(track('a', 'pull', async () => { throw new Error('fatal: Authentication failed\nmore') })).rejects.toThrow('fatal')
    expect(api.notify).toHaveBeenCalledTimes(1)
    expect(vi.mocked(api.notify).mock.calls[0][0]).toMatchObject({ id: 'a:problem', body: 'Pull failed: fatal: Authentication failed' })
  })
})

describe('notifyStashConflicts', () => {
  const state = (over: Partial<MergeState>): MergeState => ({ kind: 'stash', merging: true, from: '', into: '', conflicts: ['f'], manual: [], staged: [], unstaged: [], ...over })

  it('notifies a stash conflict on the selected repository only', async () => {
    windowFocused.set(false)
    mergeState.set(state({}))
    notifyStashConflicts('b')
    notifyStashConflicts('a')
    await Promise.resolve()
    expect(api.notify).toHaveBeenCalledTimes(1)
    expect(vi.mocked(api.notify).mock.calls[0][0]).toMatchObject({ id: 'a:problem', body: 'Conflicts in 1 file after the stash' })
  })

  it('ignores a clean apply', async () => {
    windowFocused.set(false)
    mergeState.set(state({ merging: false, conflicts: [] }))
    notifyStashConflicts('a')
    await Promise.resolve()
    expect(api.notify).not.toHaveBeenCalled()
  })
})

describe('startNotifications', () => {
  it('routes notify:open and chat events', async () => {
    // vitest runs in node: a bare EventTarget stands in for the window.
    vi.stubGlobal('window', new EventTarget())
    vi.stubGlobal('document', { hasFocus: () => true })
    const handlers: Record<string, (p: unknown) => void> = {}
    const on = ((name: string, cb: (p: unknown) => void) => {
      handlers[name] = cb
      return () => delete handlers[name]
    }) as any
    const stop = startNotifications(on)
    handlers['notify:open']({ repoID: 'b', target: 'repo' })
    expect(get(selectedRepoId)).toBe('b')
    handlers['notify:open']({ repoID: 'a', target: 'repo', hash: 'h7' })
    expect(get(selectedRepoId)).toBe('a')
    expect(get(selectedHash)).toBe('h7')

    windowFocused.set(false)
    handlers['chat:confirm']({ repoID: 'a', runID: 'x', confirmID: 'c', tool: 'push', title: 'Push main', details: [] })
    await Promise.resolve()
    expect(vi.mocked(api.notify).mock.calls[0][0]).toMatchObject({ id: 'a:ai', body: 'The AI is waiting for you to confirm: Push main', target: 'chat' })

    window.dispatchEvent(new Event('focus'))
    expect(get(windowFocused)).toBe(true)
    window.dispatchEvent(new Event('blur'))
    expect(get(windowFocused)).toBe(false)

    stop()
    expect(Object.keys(handlers)).toEqual([])
    window.dispatchEvent(new Event('focus'))
    expect(get(windowFocused)).toBe(false)
    vi.unstubAllGlobals()
  })
})

describe('startNotifications focus', () => {
  it('reads the focus again when it starts', () => {
    vi.stubGlobal('window', new EventTarget())
    vi.stubGlobal('document', { hasFocus: () => true })
    windowFocused.set(false)
    const stop = startNotifications((() => () => {}) as any)
    expect(get(windowFocused)).toBe(true)
    stop()
    vi.unstubAllGlobals()
  })
})
