import { get } from 'svelte/store'
import { afterEach, describe, expect, it, vi } from 'vitest'

vi.mock('./api', () => ({ api: {} }))

import { isLogOrder } from './logOrder'
import { persisted } from './stores'

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
