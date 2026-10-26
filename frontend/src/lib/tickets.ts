import { writable } from 'svelte/store'
import { api } from './api'
import { escapeHtml } from './markdown'

/** The Jira site and project keys from Settings → Accounts. */
export interface JiraSettings {
  site: string
  keys: string[]
}

/** A piece of text, or a ticket that is a link: its upper-case key, the
 *  address, and the text as it was written (`nexo-1` in a branch name). */
export type TicketSegment = { text: string } | { key: string; url: string; label: string }

/** The saved Jira settings; null until loaded, or when they cannot be read. */
export const jiraSettings = writable<JiraSettings | null>(null)

/** Reads the settings from the backend into the store. A failure leaves ticket
 *  links off. */
export async function loadJira(): Promise<void> {
  try {
    const j = await api.getJiraSettings()
    jiraSettings.set({ site: j.site, keys: j.keys ?? [] })
  } catch {
    jiraSettings.set(null)
  }
}

const KEY = /^[A-Z][A-Z0-9]+$/

function httpsSite(site: string): string {
  try {
    return new URL(site).protocol === 'https:' ? site.replace(/\/+$/, '') : ''
  } catch {
    return ''
  }
}

/** True when ticket links are on: an https site and at least one key. */
export function ticketActive(j: JiraSettings | null | undefined): j is JiraSettings {
  return !!j && !!httpsSite(j.site) && j.keys.some((k) => KEY.test(k))
}

/** A listed key, a dash and digits, as its own word: the character before is
 *  not a letter, digit, `-` or `_`, and the one after is not a letter, digit
 *  or `_` (a `-` may follow, so `feature/NEXO-123-login` is a match). The
 *  character before is matched, not looked behind for, to run on every webview. */
function matcher(j: JiraSettings): RegExp {
  const keys = j.keys.filter((k) => KEY.test(k))
  return new RegExp(`(^|[^A-Za-z0-9_-])(${keys.join('|')})-(\\d+)(?![A-Za-z0-9_])`, 'gi')
}

/** The text split into plain text and ticket links. Text is never turned into
 *  markup here: a component renders each segment escaped. A key becomes a link
 *  only where it is literally written, in a listed project, and the link always
 *  carries the upper-case key. */
export function linkTickets(text: string, j: JiraSettings | null | undefined): TicketSegment[] {
  if (!text) return []
  if (!ticketActive(j)) return [{ text }]
  const site = httpsSite(j.site)
  const out: TicketSegment[] = []
  const re = matcher(j)
  let from = 0
  for (let m = re.exec(text); m; m = re.exec(text)) {
    const start = m.index + m[1].length
    if (start > from) out.push({ text: text.slice(from, start) })
    const key = `${m[2].toUpperCase()}-${m[3]}`
    out.push({ key, url: `${site}/browse/${key}`, label: m[2] + '-' + m[3] })
    from = m.index + m[0].length
  }
  if (from < text.length) out.push({ text: text.slice(from) })
  return out
}

/** The text of HTML that is already safe (renderUntrusted's output), with an
 *  anchor around each ticket key. Only text is touched: never a tag or an
 *  attribute, and nothing inside an existing anchor, inline code or a code
 *  block. The new anchors have no target; the page opens them itself. */
export function linkTicketsHtml(html: string, j: JiraSettings | null | undefined): string {
  if (!ticketActive(j)) return html
  let skip = { a: 0, code: 0, pre: 0 }
  return html
    .split(/(<[^>]*>)/)
    .map((part, i) => {
      if (i % 2 === 1) {
        const tag = /^<(\/?)([a-zA-Z][a-zA-Z0-9]*)/.exec(part)
        const name = tag?.[2].toLowerCase()
        if (tag && (name === 'a' || name === 'code' || name === 'pre')) {
          skip = { ...skip, [name]: Math.max(0, skip[name] + (tag[1] ? -1 : 1)) }
        }
        return part
      }
      if (skip.a || skip.code || skip.pre) return part
      return linkTickets(part, j)
        .map((s) => ('key' in s ? `<a href="${escapeHtml(s.url)}" class="ticket">${escapeHtml(s.label)}</a>` : s.text))
        .join('')
    })
    .join('')
}
