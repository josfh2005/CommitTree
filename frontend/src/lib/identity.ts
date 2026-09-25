import type { Identity } from './types'

/** Whether a commit was authored by the repository's git user: by email,
 *  ignoring case, or by name when no email is configured. */
export function isMine(commit: { author: string; email: string }, me: Identity | null): boolean {
  if (!me) return false
  if (me.email) return commit.email.toLowerCase() === me.email.toLowerCase()
  return !!me.name && commit.author === me.name
}
