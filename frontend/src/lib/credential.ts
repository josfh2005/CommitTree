import { writable } from 'svelte/store'

/** The credential dialog's pure logic (docs/spec/05-remote-and-stash.md). */

export type CredentialKind = 'https' | 'ssh-passphrase' | 'ssh-password' | 'ssh-hostkey' | 'ssh-other'

/** credential:request — never carries an answer. */
export interface CredentialRequest {
  id: string
  kind: CredentialKind
  /** '' for a clone. */
  repoId: string
  /** e.g. 'Push · my-repo'. */
  context: string
  fromAI: boolean
  host: string
  path?: string
  username?: string
  /** ssh's own text, for 'ssh-other'. */
  prompt?: string
  keyPath?: string
  keyType?: string
  fingerprint?: string
  remember: { name: string } | null
  /** A passphrase or password asked again: the last one was wrong. */
  retry: boolean
  /** Requests queued behind this one. */
  waiting: number
  /** ms since epoch. */
  deadline: number
}

export interface CredentialAnswer {
  username: string
  secret: string
  remember: boolean
  trust: boolean
}

export const COUNTDOWN_FROM_MS = 20_000

/** The request on screen; null when none. */
export const credentialRequest = writable<CredentialRequest | null>(null)

export function onRequest(r: CredentialRequest) {
  credentialRequest.set(r)
}

export function onClosed(id: string) {
  credentialRequest.update((cur) => (cur?.id === id ? null : cur))
}

export function title(r: CredentialRequest): string {
  switch (r.kind) {
    case 'https': return `Sign in to ${r.host}`
    case 'ssh-passphrase': return 'SSH key passphrase'
    case 'ssh-password': return `SSH password for ${r.username}@${r.host}`
    case 'ssh-hostkey': return 'Unknown SSH host'
    default: return 'The SSH server asks'
  }
}

export function secretLabel(kind: CredentialKind): string {
  return { https: 'Token', 'ssh-passphrase': 'Passphrase', 'ssh-password': 'Password', 'ssh-hostkey': '', 'ssh-other': 'Answer' }[kind]
}

const TOKEN_HOSTS = new Set(['github.com', 'gitlab.com', 'bitbucket.org'])

export function tokenHint(host: string): string | null {
  return TOKEN_HOSTS.has(host.toLowerCase()) ? 'Use a personal access token, not your account password.' : null
}

export function countdown(deadline: number, now: number): string | null {
  const left = deadline - now
  if (left > COUNTDOWN_FROM_MS) return null
  return `Closes in ${Math.max(0, Math.ceil(left / 1000))} s`
}

export function canSubmit(r: CredentialRequest, a: CredentialAnswer): boolean {
  if (r.kind === 'ssh-hostkey') return true
  if (r.kind === 'https') return a.username.trim() !== '' && a.secret !== ''
  return a.secret !== ''
}

/** The status bar's text for a task waiting on the dialog, else ''. */
export function waitingText(t: { kind: string; repoId: string }, r: CredentialRequest | null): string {
  if (!r) return ''
  const mine = t.kind === 'clone' ? r.repoId === '' && r.context.startsWith('Clone') : t.repoId !== '' && t.repoId === r.repoId
  return mine ? 'Waiting for credentials…' : ''
}

/** The notification body when the window is not focused: host and context only. */
export function notifyBody(r: CredentialRequest): string {
  return `Credentials needed for ${r.host || 'SSH'} · ${r.context}`
}
