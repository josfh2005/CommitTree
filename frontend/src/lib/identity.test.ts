import { describe, expect, it } from 'vitest'
import { isMine } from './identity'

const commit = (author: string, email: string) => ({ author, email })

describe('isMine', () => {
  it('matches by email, ignoring case', () => {
    expect(isMine(commit('Ana', 'Ana@X.com'), { name: 'Someone else', email: 'ana@x.com' })).toBe(true)
    expect(isMine(commit('Ana', 'bea@x.com'), { name: 'Ana', email: 'ana@x.com' })).toBe(false)
  })

  it('falls back to the name only when no email is configured', () => {
    expect(isMine(commit('Ana', 'ana@x.com'), { name: 'Ana', email: '' })).toBe(true)
    expect(isMine(commit('Bea', 'bea@x.com'), { name: 'Ana', email: '' })).toBe(false)
  })

  it('highlights nothing without an identity', () => {
    expect(isMine(commit('Ana', 'ana@x.com'), null)).toBe(false)
    expect(isMine(commit('', ''), { name: '', email: '' })).toBe(false)
  })
})
