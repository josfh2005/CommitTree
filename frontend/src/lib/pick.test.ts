import { describe, expect, it } from 'vitest'
import { filterPick } from './pick'

const items = [{ key: 'a', label: 'feature/login' }, { key: 'b', label: 'fix/typo' }, { key: 'c', label: 'origin/develop' }]

describe('filterPick', () => {
  it('keeps everything for an empty query', () => {
    expect(filterPick(items, '  ')).toEqual(items)
  })
  it('matches any part of the label, ignoring case', () => {
    expect(filterPick(items, 'LOG').map((i) => i.key)).toEqual(['a'])
    expect(filterPick(items, 'o').map((i) => i.key)).toEqual(['a', 'b', 'c'])
  })
  it('gives nothing when nothing matches', () => {
    expect(filterPick(items, 'zzz')).toEqual([])
  })
})
