import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  applyPick, closeList, createCompleter, handleKey, moveSelection, openList, splitPath, closedList,
} from './pathcomplete'

const list = (items: string[], selected = -1) => ({ items, selected, open: items.length > 0 })

describe('openList', () => {
  it('opens on suggestions with nothing selected, closes when empty', () => {
    expect(openList(['/a', '/b'])).toEqual({ items: ['/a', '/b'], selected: -1, open: true })
    expect(openList([])).toEqual(closedList)
  })
})

describe('moveSelection', () => {
  it('goes down from nothing to the first and wraps after the last', () => {
    let s = list(['/a', '/b', '/c'])
    s = moveSelection(s, 1)
    expect(s.selected).toBe(0)
    s = moveSelection(moveSelection(s, 1), 1)
    expect(s.selected).toBe(2)
    expect(moveSelection(s, 1).selected).toBe(0)
  })
  it('goes up from nothing to the last and wraps before the first', () => {
    const s = list(['/a', '/b', '/c'])
    expect(moveSelection(s, -1).selected).toBe(2)
    expect(moveSelection(list(s.items, 0), -1).selected).toBe(2)
  })
  it('does nothing on a closed list', () => {
    expect(moveSelection(closedList, 1)).toBe(closedList)
  })
})

describe('closeList', () => {
  it('empties and closes', () => expect(closeList()).toEqual(closedList))
})

describe('applyPick', () => {
  it('appends a slash so the user can keep descending', () => {
    expect(applyPick('/Users/me/src')).toBe('/Users/me/src/')
    expect(applyPick('~/src')).toBe('~/src/')
  })
  it('does not double a slash (the bare ~ suggestion is ~/)', () => {
    expect(applyPick('~/')).toBe('~/')
  })
})

describe('splitPath', () => {
  it('separates the folder part from the last segment', () => {
    expect(splitPath('/Users/me/src')).toEqual({ dir: '/Users/me/', name: 'src' })
    expect(splitPath('~/')).toEqual({ dir: '~/', name: '' })
    expect(splitPath('/src')).toEqual({ dir: '/', name: 'src' })
  })
})

describe('handleKey', () => {
  const items = ['/a/one', '/a/two']
  it('ignores every key while the list is closed (Esc then closes the dialog)', () => {
    for (const key of ['ArrowDown', 'ArrowUp', 'Tab', 'Enter', 'Escape']) {
      const r = handleKey(closedList, key, false, '/a/')
      expect(r.handled).toBe(false)
      expect(r.pick).toBeNull()
    }
  })
  it('moves the selection with the arrows and swallows the key', () => {
    const r = handleKey(list(items), 'ArrowDown', false, '/a/')
    expect(r).toMatchObject({ handled: true, pick: null })
    expect(r.state.selected).toBe(0)
    expect(handleKey(r.state, 'ArrowUp', false, '/a/').state.selected).toBe(1)
  })
  it('Tab and Enter complete the selected suggestion', () => {
    for (const key of ['Tab', 'Enter']) {
      const r = handleKey(list(items, 1), key, false, '/a/t')
      expect(r).toMatchObject({ handled: true, pick: '/a/two' })
    }
  })
  it('Enter with nothing selected is not handled, so the form submits', () => {
    const r = handleKey(list(items), 'Enter', false, '/a/')
    expect(r.handled).toBe(false)
    expect(r.pick).toBeNull()
  })
  it('Tab with nothing selected completes the first suggestion while a name is being typed', () => {
    const r = handleKey(list(items), 'Tab', false, '/a/o')
    expect(r).toMatchObject({ handled: true, pick: '/a/one' })
  })
  it('Tab with nothing selected after a slash, or Shift+Tab, just moves focus', () => {
    expect(handleKey(list(items), 'Tab', false, '/a/').handled).toBe(false)
    expect(handleKey(list(items), 'Tab', true, '/a/o').handled).toBe(false)
    expect(handleKey(list(items, 0), 'Tab', true, '/a/o').handled).toBe(false)
  })
  it('Esc closes the list only', () => {
    const r = handleKey(list(items, 0), 'Escape', false, '/a/')
    expect(r).toMatchObject({ handled: true, pick: null })
    expect(r.state).toEqual(closedList)
  })
  it('leaves other keys alone', () => {
    expect(handleKey(list(items), 'a', false, '/a/').handled).toBe(false)
  })
})

describe('createCompleter', () => {
  beforeEach(() => { vi.useFakeTimers() })
  afterEach(() => { vi.useRealTimers() })

  it('waits for a pause in typing, then asks once with the latest text', async () => {
    const fetch = vi.fn(async (v: string) => [v + 'x'])
    const got: string[][] = []
    const c = createCompleter(fetch, (items) => got.push(items), 150)
    c.request('/a')
    vi.advanceTimersByTime(100)
    c.request('/ab')
    vi.advanceTimersByTime(149)
    expect(fetch).not.toHaveBeenCalled()
    vi.advanceTimersByTime(1)
    await vi.runAllTimersAsync()
    expect(fetch).toHaveBeenCalledTimes(1)
    expect(fetch).toHaveBeenCalledWith('/ab')
    expect(got).toEqual([['/abx']])
  })

  it('now() asks immediately and cancels a pending debounced request', async () => {
    const fetch = vi.fn(async (v: string) => [v])
    const got: string[][] = []
    const c = createCompleter(fetch, (items) => got.push(items), 150)
    c.request('/a')
    c.now('/a/')
    await vi.runAllTimersAsync()
    expect(fetch).toHaveBeenCalledTimes(1)
    expect(got).toEqual([['/a/']])
  })

  it('a later request wins over an earlier answer arriving late', async () => {
    const resolvers: Record<string, (v: string[]) => void> = {}
    const fetch = (v: string) => new Promise<string[]>((r) => { resolvers[v] = r })
    const got: string[][] = []
    const c = createCompleter(fetch, (items) => got.push(items), 0)
    c.now('/a')
    c.now('/ab')
    resolvers['/ab'](['/ab-new'])
    await vi.runAllTimersAsync()
    resolvers['/a'](['/a-old'])
    await vi.runAllTimersAsync()
    expect(got).toEqual([['/ab-new']])
  })

  it('cancel() drops a pending timer and an answer in flight', async () => {
    let resolve: (v: string[]) => void = () => {}
    const fetch = vi.fn(() => new Promise<string[]>((r) => { resolve = r }))
    const got: string[][] = []
    const c = createCompleter(fetch, (items) => got.push(items), 150)
    c.request('/a')
    c.cancel()
    vi.advanceTimersByTime(500)
    expect(fetch).not.toHaveBeenCalled()
    c.now('/a')
    c.cancel()
    resolve(['/late'])
    await vi.runAllTimersAsync()
    expect(got).toEqual([])
  })

  it('a failed lookup gives no suggestions', async () => {
    const got: string[][] = []
    const c = createCompleter(async () => { throw new Error('x') }, (items) => got.push(items), 0)
    c.now('/a')
    await vi.runAllTimersAsync()
    expect(got).toEqual([[]])
  })
})
