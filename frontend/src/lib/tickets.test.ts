import { get } from 'svelte/store'
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('./api', () => ({ api: { getJiraSettings: vi.fn() } }))

import { api } from './api'
import { jiraSettings, linkTickets, linkTicketsHtml, loadJira, ticketActive } from './tickets'
import { renderUntrusted } from './prs'

const jira = { site: 'https://escala.atlassian.net', keys: ['NEXO', 'SCL'] }
const keysOf = (text: string, j = jira) => linkTickets(text, j).flatMap((s) => ('key' in s ? [s.key] : []))

describe('ticketActive', () => {
  it('needs an https site and at least one key', () => {
    expect(ticketActive(jira)).toBe(true)
    expect(ticketActive({ site: '', keys: ['NEXO'] })).toBe(false)
    expect(ticketActive({ site: 'https://x.y', keys: [] })).toBe(false)
    expect(ticketActive({ site: 'http://x.y', keys: ['NEXO'] })).toBe(false)
    expect(ticketActive({ site: 'javascript:alert(1)', keys: ['NEXO'] })).toBe(false)
    expect(ticketActive(null)).toBe(false)
  })
})

describe('linkTickets', () => {
  it('changes nothing until the site and a key are set', () => {
    expect(linkTickets('NEXO-1 fix', null)).toEqual([{ text: 'NEXO-1 fix' }])
    expect(linkTickets('NEXO-1 fix', { site: '', keys: ['NEXO'] })).toEqual([{ text: 'NEXO-1 fix' }])
    expect(linkTickets('NEXO-1 fix', { site: 'https://a.b', keys: [] })).toEqual([{ text: 'NEXO-1 fix' }])
    expect(linkTickets('', jira)).toEqual([])
  })
  it('splits text around a ticket and links it to the browse page', () => {
    expect(linkTickets('NEXO-123: fix login', jira)).toEqual([
      { key: 'NEXO-123', url: 'https://escala.atlassian.net/browse/NEXO-123', label: 'NEXO-123' },
      { text: ': fix login' },
    ])
    expect(linkTickets('see (SCL-9) and NEXO-1.', jira)).toEqual([
      { text: 'see (' },
      { key: 'SCL-9', url: 'https://escala.atlassian.net/browse/SCL-9', label: 'SCL-9' },
      { text: ') and ' },
      { key: 'NEXO-1', url: 'https://escala.atlassian.net/browse/NEXO-1', label: 'NEXO-1' },
      { text: '.' },
    ])
  })
  it('matches a key written as its own word, also in a branch name', () => {
    expect(keysOf('feature/NEXO-123-login')).toEqual(['NEXO-123'])
    expect(keysOf('(NEXO-123)')).toEqual(['NEXO-123'])
    expect(keysOf('NEXO-123:')).toEqual(['NEXO-123'])
    expect(keysOf('NEXO-1,SCL-2')).toEqual(['NEXO-1', 'SCL-2'])
    expect(keysOf('NEXO-1 NEXO-1')).toEqual(['NEXO-1', 'NEXO-1'])
    expect(keysOf('NEXO-1\nSCL-2')).toEqual(['NEXO-1', 'SCL-2'])
  })
  it('does not match inside a longer token', () => {
    expect(keysOf('XNEXO-1')).toEqual([])
    expect(keysOf('NEXO-12a')).toEqual([])
    expect(keysOf('1NEXO-1')).toEqual([])
    expect(keysOf('-NEXO-1')).toEqual([])
    expect(keysOf('_NEXO-1')).toEqual([])
    expect(keysOf('NEXO-1_x')).toEqual([])
    expect(keysOf('NEXO-')).toEqual([])
    expect(keysOf('NEXO 12')).toEqual([])
    expect(keysOf('NEXOS-12')).toEqual([])
  })
  it('is case-insensitive on the key, shows what was written and links the upper-case key', () => {
    expect(linkTickets('nexo-123 and Scl-4', jira)).toEqual([
      { key: 'NEXO-123', url: 'https://escala.atlassian.net/browse/NEXO-123', label: 'nexo-123' },
      { text: ' and ' },
      { key: 'SCL-4', url: 'https://escala.atlassian.net/browse/SCL-4', label: 'Scl-4' },
    ])
  })
  it('leaves unlisted keys as plain text', () => {
    expect(linkTickets('UTF-8 and ABC-12', jira)).toEqual([{ text: 'UTF-8 and ABC-12' }])
  })
  it('ignores keys that are not plain letters and digits', () => {
    // Defence: a key from the store is never trusted to be a safe regex.
    expect(linkTickets('a-1', { site: 'https://a.b', keys: ['.*', 'a', '(x'] })).toEqual([{ text: 'a-1' }])
  })
  it('tolerates a trailing slash on the site', () => {
    expect(linkTickets('NEXO-1', { site: 'https://a.b/', keys: ['NEXO'] })).toEqual([{ key: 'NEXO-1', url: 'https://a.b/browse/NEXO-1', label: 'NEXO-1' }])
  })
})

describe('linkTicketsHtml', () => {
  const anchor = (k: string) => `<a href="https://escala.atlassian.net/browse/${k}" class="ticket">${k}</a>`
  it('is the identity until the integration is set', () => {
    expect(linkTicketsHtml('<p>NEXO-1</p>', null)).toBe('<p>NEXO-1</p>')
  })
  it('links a key in text, never inside a tag or an attribute', () => {
    expect(linkTicketsHtml('<p>fix NEXO-1</p>', jira)).toBe(`<p>fix ${anchor('NEXO-1')}</p>`)
    expect(linkTicketsHtml('<p title="NEXO-1">x</p>', jira)).toBe('<p title="NEXO-1">x</p>')
    expect(linkTicketsHtml('<span data-k="a NEXO-1 b">NEXO-2</span>', jira)).toBe(`<span data-k="a NEXO-1 b">${anchor('NEXO-2')}</span>`)
  })
  it('leaves existing anchors alone, also nested markup', () => {
    expect(linkTicketsHtml('<p><a href="https://x">NEXO-1</a> NEXO-2</p>', jira)).toBe(`<p><a href="https://x">NEXO-1</a> ${anchor('NEXO-2')}</p>`)
    expect(linkTicketsHtml('<a href="https://x"><strong>NEXO-1</strong></a>', jira)).toBe('<a href="https://x"><strong>NEXO-1</strong></a>')
  })
  it('leaves code, inline or fenced, as typed', () => {
    expect(linkTicketsHtml('<p><code>NEXO-1</code> NEXO-2</p>', jira)).toBe(`<p><code>NEXO-1</code> ${anchor('NEXO-2')}</p>`)
    expect(linkTicketsHtml('<pre><code>NEXO-1\nSCL-2</code></pre>', jira)).toBe('<pre><code>NEXO-1\nSCL-2</code></pre>')
  })
  it('shows the key as written and links the upper-case one', () => {
    expect(linkTicketsHtml('<p>feature/nexo-1-x</p>', jira)).toBe('<p>feature/<a href="https://escala.atlassian.net/browse/NEXO-1" class="ticket">nexo-1</a>-x</p>')
  })
  it('keeps escaped text escaped', () => {
    expect(linkTicketsHtml('<p>&lt;b&gt;NEXO-1&lt;/b&gt; &amp;NEXO-2</p>', jira)).toBe(`<p>&lt;b&gt;${anchor('NEXO-1')}&lt;/b&gt; &amp;${anchor('NEXO-2')}</p>`)
  })
  it('adds nothing but our own anchors to rendered markdown from a pull request', () => {
    const md = [
      'Fixes NEXO-1 and [NEXO-2](https://x.example/a) and `NEXO-3`.',
      '',
      '<a href="https://x">NEXO-4</a> <img src=x onerror=alert(1)> NEXO-5',
      '',
      '[click NEXO-6](javascript:alert(1))',
      '',
      '```',
      'NEXO-7',
      '```',
    ].join('\n')
    const out = linkTicketsHtml(renderUntrusted(md), jira)
    // Our anchors are exactly the ones for keys written in plain text.
    const ours = [...out.matchAll(/<a href="https:\/\/escala\.atlassian\.net\/browse\/(NEXO-\d+)" class="ticket">/g)].map((m) => m[1])
    expect(ours).toEqual(['NEXO-1', 'NEXO-4', 'NEXO-5', 'NEXO-6'])
    // The author's own anchor, code and fenced code are untouched, and nothing raw got through.
    expect(out).toContain('<a href="https://x.example/a">NEXO-2</a>')
    expect(out).toContain('<code>NEXO-3</code>')
    expect(out).toContain('<pre><code>NEXO-7</code></pre>')
    expect(out).not.toContain('<img')
    expect(out).not.toContain('onerror=alert(1)>')
    expect(out).not.toMatch(/href="javascript/)
    expect(out).toContain('&lt;a href=&quot;https://x&quot;&gt;')
  })
})

describe('jiraSettings store', () => {
  beforeEach(() => {
    vi.mocked(api.getJiraSettings).mockReset()
    jiraSettings.set(null)
  })
  it('loads once the backend answers', async () => {
    vi.mocked(api.getJiraSettings).mockResolvedValue(jira)
    await loadJira()
    expect(get(jiraSettings)).toEqual(jira)
  })
  it('stays off when the backend fails or answers null keys', async () => {
    vi.mocked(api.getJiraSettings).mockRejectedValue(new Error('boom'))
    await loadJira()
    expect(get(jiraSettings)).toBeNull()
    vi.mocked(api.getJiraSettings).mockResolvedValue({ site: 'https://a.b', keys: null as unknown as string[] })
    await loadJira()
    expect(get(jiraSettings)).toEqual({ site: 'https://a.b', keys: [] })
  })
})
