import { get } from 'svelte/store'
import { afterEach, describe, expect, it, vi } from 'vitest'

vi.mock('./api', () => ({ api: {} }))

import { isLogOrder } from './logOrder'
import { autoFetchMinutes, expandedRepos, isAutoFetchMinutes, notifyRemote, blameBack, blamePrevious, blameStack, blameTarget, chatPreparing, closeBlame, expandedStashSections, explainIntoChat, expandedTagSections, mainView, openBlame, persisted, selectedHash, selectRepo, selectUncommitted, showCommitInLog, toggleCommit, toggleStashExpanded, toggleTagsExpanded, toggleUncommitted, uncommittedSelected } from './stores'

/** A minimal in-memory Storage, since these tests don't run in a DOM
 *  environment and so have no real localStorage to read from. */
function fakeStorage(initial: Record<string, string> = {}): Storage {
  const data = new Map(Object.entries(initial))
  return {
    getItem: (key) => (data.has(key) ? (data.get(key) as string) : null),
    setItem: (key, value) => void data.set(key, value),
    removeItem: (key) => void data.delete(key),
    clear: () => void data.clear(),
    key: (index) => Array.from(data.keys())[index] ?? null,
    get length() {
      return data.size
    },
  }
}

describe('persisted', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('reads a stored value through with no validator given', () => {
    vi.stubGlobal('localStorage', fakeStorage({ width: '340' }))
    expect(get(persisted('width', 280))).toBe(340)
  })

  it('falls back to the default when nothing is stored', () => {
    vi.stubGlobal('localStorage', fakeStorage())
    expect(get(persisted('width', 280))).toBe(280)
  })

  it('accepts a stored value that satisfies the validator', () => {
    vi.stubGlobal('localStorage', fakeStorage({ logOrder: '"date"' }))
    expect(get(persisted('logOrder', 'topo', isLogOrder))).toBe('date')
  })

  it('falls back to the default when the stored value fails the validator', () => {
    // A stale value from a since-removed option, or a corrupt entry, left
    // behind by an older version of the app.
    vi.stubGlobal('localStorage', fakeStorage({ logOrder: '"bogus"' }))
    expect(get(persisted('logOrder', 'topo', isLogOrder))).toBe('topo')
  })

  it('falls back to the default on unparsable JSON, validator or not', () => {
    vi.stubGlobal('localStorage', fakeStorage({ logOrder: '{not json' }))
    expect(get(persisted('logOrder', 'topo', isLogOrder))).toBe('topo')
  })

  it('falls back to the default when localStorage itself is unavailable', () => {
    vi.stubGlobal('localStorage', {
      getItem: () => {
        throw new Error('storage disabled')
      },
    })
    expect(get(persisted('logOrder', 'topo', isLogOrder))).toBe('topo')
  })
})

describe('uncommitted row selection', () => {
  afterEach(() => {
    selectedHash.set('')
    uncommittedSelected.set(false)
    mainView.set('log')
  })
  it('selecting the row clears the selected commit and shows the log', () => {
    selectedHash.set('abc')
    mainView.set('changes')
    selectUncommitted()
    expect(get(uncommittedSelected)).toBe(true)
    expect(get(selectedHash)).toBe('')
    expect(get(mainView)).toBe('log')
  })
  it('selecting any commit deselects the row, whoever sets it', () => {
    selectUncommitted()
    selectedHash.set('def')
    expect(get(uncommittedSelected)).toBe(false)
  })
  it('clearing the commit selection leaves the row selected', () => {
    selectUncommitted()
    selectedHash.set('')
    expect(get(uncommittedSelected)).toBe(true)
  })
})

describe('stash section expansion', () => {
  it('starts collapsed and toggles per repository', () => {
    expect(get(expandedStashSections)).toEqual([])
    toggleStashExpanded('a')
    expect(get(expandedStashSections)).toEqual(['a'])
    toggleStashExpanded('b')
    toggleStashExpanded('a')
    expect(get(expandedStashSections)).toEqual(['b'])
    toggleStashExpanded('b')
  })
})

describe('tags section expansion', () => {
  it('starts collapsed and toggles per repository', () => {
    expect(get(expandedTagSections)).toEqual([])
    toggleTagsExpanded('a')
    expect(get(expandedTagSections)).toEqual(['a'])
    toggleTagsExpanded('b')
    toggleTagsExpanded('a')
    expect(get(expandedTagSections)).toEqual(['b'])
    toggleTagsExpanded('b')
  })
})

describe('toggling the log selection', () => {
  afterEach(() => {
    selectedHash.set('')
    uncommittedSelected.set(false)
  })
  it('selects a commit, and clears it on a second click', () => {
    toggleCommit('a')
    expect(get(selectedHash)).toBe('a')
    toggleCommit('a')
    expect(get(selectedHash)).toBe('')
  })
  it('moves to another commit instead of clearing', () => {
    toggleCommit('a')
    toggleCommit('b')
    expect(get(selectedHash)).toBe('b')
  })
  it('toggles the uncommitted row the same way', () => {
    toggleCommit('a')
    toggleUncommitted()
    expect(get(uncommittedSelected)).toBe(true)
    expect(get(selectedHash)).toBe('')
    toggleUncommitted()
    expect(get(uncommittedSelected)).toBe(false)
  })
})

describe('loadRepos when a repository disappears', () => {
  it('clears a selection whose repository is no longer listed and drops its terminal tabs', async () => {
    const { api } = await import('./api')
    const { terminalState, addTab, emptyTermState } = await import('./terminal')
    const { loadRepos, selectedRepoId } = await import('./stores')
    const repo = (id: string) => ({ id, name: id, path: `/${id}`, missing: false, branch: 'main' })
    ;(api as Record<string, unknown>).listRepos = async () => [repo('main1'), repo('wt')]
    await loadRepos()
    selectedRepoId.set('wt')
    terminalState.set(addTab(emptyTermState(), 'wt', 't1', 'zsh'))

    ;(api as Record<string, unknown>).listRepos = async () => [repo('main1')]
    await loadRepos()
    expect(get(selectedRepoId)).toBe('')
    expect(get(terminalState).tabs).toEqual([])
  })
})

describe('explainIntoChat', () => {
  it('marks the repo as preparing until the call settles, then clears it', async () => {
    let finish!: () => void
    const call = explainIntoChat('r1', () => new Promise<void>((resolve) => (finish = resolve)))
    expect(get(chatPreparing)).toEqual(['r1'])
    finish()
    await call
    expect(get(chatPreparing)).toEqual([])
  })

  it('clears the mark when the call fails, and passes the error on', async () => {
    await expect(explainIntoChat('r1', () => Promise.reject(new Error('boom')))).rejects.toThrow('boom')
    expect(get(chatPreparing)).toEqual([])
  })
})

describe('blame navigation', () => {
  it('opens from the current view, walks back through previous revisions, then returns', () => {
    mainView.set('changes')
    openBlame('a.txt', '')
    expect(get(mainView)).toBe('blame')
    expect(get(blameTarget)).toEqual({ path: 'a.txt', rev: '', from: 'changes' })
    blamePrevious('old.txt', 'abc')
    expect(get(blameTarget)).toEqual({ path: 'old.txt', rev: 'abc', from: 'changes' })
    blameBack()
    expect(get(blameTarget)).toEqual({ path: 'a.txt', rev: '', from: 'changes' })
    blameBack()
    expect(get(blameTarget)).toBeNull()
    expect(get(mainView)).toBe('changes')
  })

  it('opening from the log returns to the log', () => {
    mainView.set('log')
    openBlame('a.txt', 'abc')
    blameBack()
    expect(get(mainView)).toBe('log')
  })

  it('showCommitInLog switches to the log even when the commit is already selected', () => {
    selectedHash.set('abc')
    openBlame('a.txt', 'abc')
    showCommitInLog('abc')
    expect(get(mainView)).toBe('log')
    expect(get(selectedHash)).toBe('abc')
  })

  it('selectRepo loads the repository without unfolding its sections', () => {
    expandedRepos.set([])
    selectRepo('folded-repo-id')
    expect(get(expandedRepos)).toEqual([])
  })

  it('selectRepo clears blame', () => {
    openBlame('a.txt', 'abc')
    selectRepo('some-other-repo-id')
    expect(get(blameTarget)).toBeNull()
    expect(get(blameStack)).toEqual([])
    expect(get(mainView)).toBe('log')
  })

  // Exercises the helper App.svelte calls when the selected repository goes
  // missing while blame is open, so a stale target does not reappear if the
  // repository comes back (see the `showBlame`/`closeBlame` reactive
  // statement in App.svelte).
  it('closeBlame leaves the log showing with no target or stack', () => {
    mainView.set('changes')
    openBlame('a.txt', '')
    blamePrevious('old.txt', 'abc')
    closeBlame()
    expect(get(mainView)).toBe('log')
    expect(get(blameTarget)).toBeNull()
    expect(get(blameStack)).toEqual([])
  })
})

describe('focusCommitBox', () => {
  it('is dropped when the uncommitted row loses the selection', async () => {
    const { focusCommitBox } = await import('./stores')
    selectUncommitted()
    focusCommitBox.set(true)
    selectedHash.set('abc')
    expect(get(focusCommitBox)).toBe(false)
    selectedHash.set('')
  })
})

describe('theme', () => {
  it('is the chosen theme, or the system one on auto', async () => {
    const { theme, themePref } = await import('./stores')
    themePref.set('dark')
    expect(get(theme)).toBe('dark')
    themePref.set('light')
    expect(get(theme)).toBe('light')
    themePref.set('auto') // no matchMedia under vitest: the system reads as light
    expect(get(theme)).toBe('light')
  })
})

describe('loadSideRefs', () => {
  it('keeps each repository\'s own refs and stash', async () => {
    const { api } = await import('./api')
    const { loadSideRefs, repos, sideRefs } = await import('./stores')
    repos.set([{ id: 'a', name: 'a', path: '/a' }, { id: 'b', name: 'b', path: '/b' }] as never)
    Object.assign(api, {
      getRefs: vi.fn(async (id: string) => ({ head: id === 'a' ? 'main' : 'develop' })),
      getStashEntries: vi.fn(async (id: string) => (id === 'a' ? [{ index: 0 }] : [])),
    })
    await loadSideRefs('a')
    await loadSideRefs('b')
    const got = get(sideRefs)
    expect(got.a.refs?.head).toBe('main')
    expect(got.a.stash).toHaveLength(1)
    expect(got.b.refs?.head).toBe('develop')
  })

  it('leaves a missing repository without refs', async () => {
    const { loadSideRefs, repos, sideRefs } = await import('./stores')
    repos.set([{ id: 'gone', name: 'gone', path: '/gone', missing: true }] as never)
    await loadSideRefs('gone')
    expect(get(sideRefs).gone).toEqual({ refs: null, stash: [] })
  })
})

describe('auto fetch settings', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('defaults to every 15 minutes with the remote notice on', () => {
    expect(get(autoFetchMinutes)).toBe(15)
    expect(get(notifyRemote)).toBe(true)
  })

  it('rejects a stored interval that is not one of the choices', () => {
    vi.stubGlobal('localStorage', fakeStorage({ autoFetchMinutes: '7' }))
    expect(get(persisted('autoFetchMinutes', 15, isAutoFetchMinutes))).toBe(15)
    vi.stubGlobal('localStorage', fakeStorage({ autoFetchMinutes: '30' }))
    expect(get(persisted('autoFetchMinutes', 15, isAutoFetchMinutes))).toBe(30)
  })
})
