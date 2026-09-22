import { describe, expect, it } from 'vitest'
import { isLogOrder, LOG_ORDERS, parseLogOrder } from './logOrder'

describe('isLogOrder', () => {
  it('accepts the two known orders', () => {
    expect(isLogOrder('topo')).toBe(true)
    expect(isLogOrder('date')).toBe(true)
  })

  it('rejects anything else', () => {
    expect(isLogOrder('')).toBe(false)
    expect(isLogOrder('TOPO')).toBe(false)
    expect(isLogOrder(undefined)).toBe(false)
    expect(isLogOrder(null)).toBe(false)
    expect(isLogOrder(42)).toBe(false)
  })
})

describe('parseLogOrder', () => {
  it('passes known orders through', () => {
    expect(parseLogOrder('topo')).toBe('topo')
    expect(parseLogOrder('date')).toBe('date')
  })

  it('falls back to topo for anything unrecognized', () => {
    expect(parseLogOrder('')).toBe('topo')
    expect(parseLogOrder('bogus')).toBe('topo')
  })
})

describe('LOG_ORDERS', () => {
  it('lists exactly the two supported orders', () => {
    expect(LOG_ORDERS).toEqual(['topo', 'date'])
  })
})
