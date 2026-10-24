import { describe, expect, it } from 'vitest'
import { canSaveBitbucket, canSaveGitHub, githubSourceFor } from './accounts'
import type { HostingAccount } from './types'

const gh = (over: Partial<HostingAccount> = {}): HostingAccount => ({
  host: 'github', connected: false, rejected: false, name: '', email: '', source: '', ghAvailable: false, ...over,
})

describe('Accounts tab rules', () => {
  it('Bitbucket Save needs a non-blank email and token, and nothing running', () => {
    expect(canSaveBitbucket('me@x.io', 'tok', '')).toBe(true)
    expect(canSaveBitbucket('', 'tok', '')).toBe(false)
    expect(canSaveBitbucket('   ', 'tok', '')).toBe(false)
    expect(canSaveBitbucket('me@x.io', '  \t', '')).toBe(false)
    expect(canSaveBitbucket('me@x.io', 'tok', 'bitbucket')).toBe(false)
  })

  it('GitHub Save with the CLI needs no token; with a token it must not be blank', () => {
    expect(canSaveGitHub('gh', '', '')).toBe(true)
    expect(canSaveGitHub('token', '', '')).toBe(false)
    expect(canSaveGitHub('token', '   ', '')).toBe(false)
    expect(canSaveGitHub('token', 'ghp_x', '')).toBe(true)
    expect(canSaveGitHub('token', 'ghp_x', 'github')).toBe(false)
  })

  it('the GitHub choice follows the saved account, else the CLI when it exists', () => {
    expect(githubSourceFor(gh({ connected: true, source: 'token', ghAvailable: true }))).toBe('token')
    expect(githubSourceFor(gh({ connected: true, source: 'gh', ghAvailable: true }))).toBe('gh')
    expect(githubSourceFor(gh({ ghAvailable: true }))).toBe('gh')
    expect(githubSourceFor(gh({ ghAvailable: false }))).toBe('token')
  })

  it('never keeps the CLI choice when gh is gone', () => {
    expect(githubSourceFor(gh({ connected: true, source: 'gh', ghAvailable: false }))).toBe('token')
  })
})
