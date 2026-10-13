import { describe, expect, it, beforeEach } from 'vitest'
import { get } from 'svelte/store'
import {
  canSubmit, countdown, credentialRequest, notifyBody, onClosed, onRequest, secretLabel, title, tokenHint, waitingText,
  type CredentialRequest,
} from './credential'

const base: CredentialRequest = {
  id: '1', kind: 'https', repoId: 'r1', context: 'Push · repo', fromAI: false, host: 'github.com',
  remember: { name: 'osxkeychain' }, retry: false, waiting: 0, deadline: 60_000,
}

describe('credential', () => {
  beforeEach(() => credentialRequest.set(null))

  it('titles each kind', () => {
    expect(title(base)).toBe('Sign in to github.com')
    expect(title({ ...base, kind: 'ssh-passphrase' })).toBe('SSH key passphrase')
    expect(title({ ...base, kind: 'ssh-password', username: 'git', host: 'h' })).toBe('SSH password for git@h')
    expect(title({ ...base, kind: 'ssh-hostkey' })).toBe('Unknown SSH host')
    expect(title({ ...base, kind: 'ssh-other', host: '' })).toBe('The SSH server asks')
  })

  it('labels the secret field', () => {
    expect(secretLabel('https')).toBe('Token')
    expect(secretLabel('ssh-passphrase')).toBe('Passphrase')
    expect(secretLabel('ssh-password')).toBe('Password')
    expect(secretLabel('ssh-other')).toBe('Answer')
  })

  it('hints a token on the big forges only', () => {
    expect(tokenHint('github.com')).toBe('Use a personal access token, not your account password.')
    expect(tokenHint('gitlab.com')).not.toBeNull()
    expect(tokenHint('bitbucket.org')).not.toBeNull()
    expect(tokenHint('git.example.com')).toBeNull()
  })

  it('counts down only in the last 20 s', () => {
    expect(countdown(60_000, 0)).toBeNull()
    expect(countdown(60_000, 39_999)).toBeNull()
    expect(countdown(60_000, 40_500)).toBe('Closes in 20 s')
    expect(countdown(60_000, 59_100)).toBe('Closes in 1 s')
    expect(countdown(60_000, 61_000)).toBe('Closes in 0 s')
  })

  it('needs a username and a token for https, a secret otherwise, nothing for a host key', () => {
    const a = { username: 'me', secret: 't', remember: true, trust: false }
    expect(canSubmit(base, a)).toBe(true)
    expect(canSubmit(base, { ...a, username: '' })).toBe(false)
    expect(canSubmit(base, { ...a, secret: '' })).toBe(false)
    expect(canSubmit({ ...base, kind: 'ssh-passphrase' }, { ...a, username: '' })).toBe(true)
    expect(canSubmit({ ...base, kind: 'ssh-hostkey' }, { ...a, secret: '' })).toBe(true)
  })

  it('replaces and closes the shown request', () => {
    onRequest(base)
    onRequest({ ...base, waiting: 1 })
    expect(get(credentialRequest)?.waiting).toBe(1)
    onClosed('other')
    expect(get(credentialRequest)).not.toBeNull()
    onClosed('1')
    expect(get(credentialRequest)).toBeNull()
  })

  it('marks the waiting task', () => {
    expect(waitingText({ kind: 'op', repoId: 'r1' }, base)).toBe('Waiting for credentials…')
    expect(waitingText({ kind: 'op', repoId: 'r2' }, base)).toBe('')
    expect(waitingText({ kind: 'clone', repoId: '' }, { ...base, repoId: '', context: 'Clone · github.com/o/r' })).toBe('Waiting for credentials…')
    expect(waitingText({ kind: 'model', repoId: '' }, { ...base, repoId: '' })).toBe('')
    expect(waitingText({ kind: 'op', repoId: 'r1' }, null)).toBe('')
  })

  it('writes a notification body with no username', () => {
    expect(notifyBody({ ...base, username: 'me' })).toBe('Credentials needed for github.com · Push · repo')
    expect(notifyBody({ ...base, kind: 'ssh-passphrase', host: '' })).toBe('Credentials needed for SSH · Push · repo')
  })
})
