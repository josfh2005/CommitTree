import { get } from 'svelte/store'
import { afterEach, describe, expect, it, vi } from 'vitest'

vi.mock('./api', () => ({ api: {} }))

import { isLogOrder } from './logOrder'
import { blameBack, blamePrevious, blameStack, blameTarget, expandedStashSections, expandedTagSections, mainView, openBlame, persisted, selectedHash, selectRepo, selectUncommitted, showCommitInLog, toggleCommit, toggleStashExpanded, toggleTagsExpanded, toggleUncommitted, uncommittedSelected } from './stores'

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

  it('selectRepo clears blame', () => {
    openBlame('a.txt', 'abc')
    selectRepo('some-other-repo-id')
    expect(get(blameTarget)).toBeNull()
    expect(get(blameStack)).toEqual([])
    expect(get(mainView)).toBe('log')
  })
})
