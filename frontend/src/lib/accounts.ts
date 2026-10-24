import type { HostingAccount, HostName } from './types'

export type GitHubSource = 'gh' | 'token'

/** Bitbucket Save needs a non-blank email and token; the backend refuses blanks too. */
export function canSaveBitbucket(email: string, token: string, busy: HostName | ''): boolean {
  return !!email.trim() && !!token.trim() && busy === ''
}

/** GitHub Save: the CLI needs nothing more; a token must not be blank. */
export function canSaveGitHub(source: GitHubSource, token: string, busy: HostName | ''): boolean {
  return (source === 'gh' || !!token.trim()) && busy === ''
}

/** The radio follows the saved account, else offers the CLI when it exists. Never the CLI when gh is gone. */
export function githubSourceFor(acc: HostingAccount): GitHubSource {
  const source: GitHubSource = acc.source === 'gh' || acc.source === 'token' ? acc.source : acc.ghAvailable ? 'gh' : 'token'
  return source === 'gh' && !acc.ghAvailable ? 'token' : source
}
