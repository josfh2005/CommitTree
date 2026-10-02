import { describe, expect, it } from 'vitest'
import { defaultRemoteName, remoteFormError } from './remoteForm'

describe('remoteFormError', () => {
  it('is incomplete while a field is blank', () => {
    expect(remoteFormError('', 'u', [])).toBe('-')
    expect(remoteFormError('origin', '   ', [])).toBe('-')
  })
  it('refuses whitespace in the name and a duplicate', () => {
    expect(remoteFormError('my remote', 'u', [])).toBe('A remote name has no spaces')
    expect(remoteFormError(' origin ', 'u', ['origin'])).toBe('A remote named origin already exists')
  })
  it('accepts a new name and a URL', () => {
    expect(remoteFormError('upstream', 'git@host:x.git', ['origin'])).toBe('')
  })
})

describe('defaultRemoteName', () => {
  it('suggests origin only for the first remote', () => {
    expect(defaultRemoteName([])).toBe('origin')
    expect(defaultRemoteName(['origin'])).toBe('')
  })
})
