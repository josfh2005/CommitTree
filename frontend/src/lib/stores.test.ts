import { get } from 'svelte/store'
import { afterEach, describe, expect, it, vi } from 'vitest'

vi.mock('./api', () => ({ api: {} }))

import { isLogOrder } from './logOrder'
import { expandedStashSections, mainView, persisted, selectedHash, selectUncommitted, toggleStashExpanded, uncommittedSelected } from './stores'

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
