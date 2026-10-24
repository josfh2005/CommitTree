import { escapeHtml, renderMarkdown } from './markdown'
import type { Comment, HostCount, HostingInfo, PRFilter } from './types'

export type { PRFilter }

/** The host's name as the app says it. */
export function hostLabel(h: string): 'Bitbucket' | 'GitHub' {
  return h === 'github' ? 'GitHub' : 'Bitbucket'
}

const NONE: Record<PRFilter, string> = {
  open: 'No open pull requests',
  mine: 'No open pull requests of yours',
  review: 'No open pull requests waiting for your review',
}

/** What the list says in place of rows: no account, a rejected token, the
 *  error of the last load, or that the filter found nothing. `accounts` asks
 *  for a button that opens Settings on the Accounts tab. */
export function emptyMessage(f: PRFilter, info: HostingInfo, err: string): { text: string; action?: 'accounts' } {
  const host = hostLabel(info.host)
  if (info.account === 'none') return { text: `Connect your ${host} account to see pull requests`, action: 'accounts' }
  if (info.account === 'rejected') return { text: `${host} rejected the token` }
  if (err) return { text: err }
  return { text: NONE[f] }
}

/** The sidebar badge: the number, with a plus when the host has more. */
export function countLabel(c: HostCount): string {
  return c.more ? `${c.n}+` : String(c.n)
}

const MAX_DEPTH = 4

/** Comments in reading order: oldest first, each followed by its replies
 *  (oldest first). A reply to a comment that is not there reads as a first-level
 *  comment. Depth is capped at 4 so deep threads stay readable. */
export function commentTree(cs: Comment[]): { comment: Comment; depth: number }[] {
  const time = (c: Comment) => {
    const t = Date.parse(c.created)
    return Number.isNaN(t) ? 0 : t
  }
  const sorted = cs.map((c, i) => ({ c, i })).sort((a, b) => time(a.c) - time(b.c) || a.i - b.i).map((x) => x.c)
  const ids = new Set(sorted.map((c) => c.id))
  const children = new Map<string, Comment[]>()
  const roots: Comment[] = []
  for (const c of sorted) {
    if (c.parent && c.parent !== c.id && ids.has(c.parent)) children.set(c.parent, [...(children.get(c.parent) ?? []), c])
    else roots.push(c)
  }
  const out: { comment: Comment; depth: number }[] = []
  const seen = new Set<string>()
  const walk = (c: Comment, depth: number) => {
    if (seen.has(c.id)) return
    seen.add(c.id)
    out.push({ comment: c, depth: Math.min(depth, MAX_DEPTH) })
    for (const r of children.get(c.id) ?? []) walk(r, depth + 1)
  }
  roots.forEach((c) => walk(c, 0))
  // Comments that only point at each other have no root: they still show.
  sorted.forEach((c) => walk(c, 0))
  return out
}

/** The address when it is an https one, otherwise null. */
export function safeHref(url: string): string | null {
  try {
    return new URL(url).protocol === 'https:' ? url : null
  } catch {
    return null
  }
}

// [text](url) and ![alt](url); the address may hold one level of parentheses.
const LINK = /!?\[([^\]\n]+)\]\(((?:[^()\s]|\([^()\s]*\))+)\)/g
const OPEN = ''
const CLOSE = ''

/** Markdown from a pull request (a title's body, a description, a comment) as
 *  HTML that is safe to place in the page: renderMarkdown escapes everything,
 *  and links are rebuilt here so that only https ones are links (anything else
 *  shows as its text). Images are never loaded: an image reads as a link. The
 *  anchors have no target: the page opens them itself, and only https ones. */
export function renderUntrusted(md: string): string {
  const table: string[] = []
  // The markers carry a link through renderMarkdown; text cannot forge them.
  const source = md.split(OPEN).join('').split(CLOSE).join('')
  // Code, fenced as renderMarkdown fences it or inline, is left as typed.
  const linked = source
    .split('```')
    .map((segment, i) =>
      i % 2 === 1
        ? segment
        : segment
            .split(/(`[^`\n]+`)/g)
            .map((part, j) =>
              j % 2 === 1
                ? part
                : part.replace(LINK, (_m, text: string, url: string) => {
                    const href = safeHref(url)
                    const label = escapeHtml(text)
                    table.push(href ? `<a href="${escapeHtml(href)}">${label}</a>` : label)
                    return `${OPEN}${table.length - 1}${CLOSE}`
                  }),
            )
            .join(''),
    )
    .join('```')
  return renderMarkdown(linked)
    .replace(/<a href="#" data-hash="[0-9a-f]+">([0-9a-f]+)<\/a>/g, '$1')
    .replace(new RegExp(`${OPEN}(\\d+)${CLOSE}`, 'g'), (_m, i: string) => table[Number(i)] ?? '')
}
