import { describe, expect, it } from 'vitest'
import { commentTree, countLabel, emptyMessage, hostLabel, renderUntrusted, safeHref } from './prs'
import type { Comment, HostingInfo } from './types'

const info = (over: Partial<HostingInfo> = {}): HostingInfo => ({
  supported: true, host: 'bitbucket', owner: 'o', name: 'n', remote: 'origin', webURL: '', pullsURL: '',
  noun: 'Pipelines', account: 'connected', ...over,
})
const comment = (id: string, parent: string, created: string): Comment => ({
  id, parent, author: 'a', created, body: id, path: '', line: 0, verdict: '',
})

describe('emptyMessage', () => {
  it('says what each filter found nothing of', () => {
    expect(emptyMessage('open', info(), '')).toEqual({ text: 'No open pull requests' })
    expect(emptyMessage('mine', info(), '')).toEqual({ text: 'No open pull requests of yours' })
    expect(emptyMessage('review', info(), '')).toEqual({ text: 'No open pull requests waiting for your review' })
  })
  it('asks to connect an account, with the Accounts action', () => {
    expect(emptyMessage('open', info({ account: 'none' }), '')).toEqual({
      text: 'Connect your Bitbucket account to see pull requests',
      action: 'accounts',
    })
    expect(emptyMessage('open', info({ host: 'github', account: 'none' }), '').text).toBe('Connect your GitHub account to see pull requests')
  })
  it('names a rejected token', () => {
    expect(emptyMessage('open', info({ account: 'rejected' }), '').text).toBe('Bitbucket rejected the token')
    expect(emptyMessage('open', info({ host: 'github', account: 'rejected' }), '').text).toBe('GitHub rejected the token')
  })
  it('shows the error of a failed load', () => {
    expect(emptyMessage('open', info(), 'No access to o/n with this account').text).toBe('No access to o/n with this account')
  })
})

describe('hostLabel', () => {
  it('names the hosts', () => {
    expect(hostLabel('bitbucket')).toBe('Bitbucket')
    expect(hostLabel('github')).toBe('GitHub')
  })
})

describe('countLabel', () => {
  it('adds a plus when the host has more', () => {
    expect(countLabel({ n: 7, more: false })).toBe('7')
    expect(countLabel({ n: 100, more: true })).toBe('100+')
  })
})

describe('commentTree', () => {
  it('puts replies under their parent, in time order, with depth', () => {
    const tree = commentTree([
      comment('c', '', '2026-01-03'),
      comment('r2', 'a', '2026-01-05'),
      comment('a', '', '2026-01-01'),
      comment('r1', 'a', '2026-01-02'),
      comment('rr', 'r1', '2026-01-04'),
    ])
    expect(tree.map((t) => [t.comment.id, t.depth])).toEqual([
      ['a', 0], ['r1', 1], ['rr', 2], ['r2', 1], ['c', 0],
    ])
  })
  it('caps the depth at 4', () => {
    const chain = ['a', 'b', 'c', 'd', 'e', 'f', 'g'].map((id, i, all) => comment(id, i ? all[i - 1] : '', `2026-01-0${i + 1}`))
    expect(commentTree(chain).map((t) => t.depth)).toEqual([0, 1, 2, 3, 4, 4, 4])
  })
  it('keeps a reply whose parent is missing, and survives a cycle', () => {
    expect(commentTree([comment('x', 'gone', '2026-01-01')]).map((t) => [t.comment.id, t.depth])).toEqual([['x', 0]])
    const looped = commentTree([comment('p', 'q', '2026-01-01'), comment('q', 'p', '2026-01-02')])
    expect(looped.map((t) => t.comment.id).sort()).toEqual(['p', 'q'])
  })
  it('handles no comments', () => {
    expect(commentTree([])).toEqual([])
  })
})

describe('safeHref', () => {
  it('lets only https through, unchanged', () => {
    expect(safeHref('https://bitbucket.org/a')).toBe('https://bitbucket.org/a')
    expect(safeHref('http://x')).toBeNull()
    expect(safeHref('javascript:x')).toBeNull()
    expect(safeHref('data:text/html,x')).toBeNull()
    expect(safeHref('//evil.example')).toBeNull()
    expect(safeHref('')).toBeNull()
  })
})

describe('renderUntrusted', () => {
  it('escapes raw HTML', () => {
    const html = renderUntrusted('<img src=x onerror=alert(1)>')
    expect(html).not.toContain('<img')
    expect(html).toContain('&lt;img')
    expect(renderUntrusted('<script>alert(1)</script>')).not.toContain('<script')
  })
  it('strips a javascript: link to its text', () => {
    const html = renderUntrusted('[x](javascript:alert(1))')
    expect(html).not.toContain('javascript:')
    expect(html).not.toContain('<a')
    expect(html).toContain('x')
  })
  it('keeps an https link, escaped', () => {
    const html = renderUntrusted('see [the <b>docs</b>](https://example.com/a?x=1&y="2")')
    expect(html).toContain('<a href="https://example.com/a?x=1&amp;y=&quot;2&quot;"')
    expect(html).toContain('the &lt;b&gt;docs&lt;/b&gt;</a>')
    expect(html).not.toContain('<b>')
  })
  it('drops an http link and an image to their text', () => {
    expect(renderUntrusted('[a](http://x.example)')).not.toContain('<a')
    const img = renderUntrusted('![alt](https://x.example/p.png)')
    expect(img).not.toContain('<img')
  })
  it('leaves links in code alone', () => {
    const html = renderUntrusted('`[a](https://x.example)`\n\n```\n[b](https://y.example)\n```')
    expect(html).not.toContain('<a ')
  })
  it('does not let the text forge a link marker', () => {
    const html = renderUntrusted('0 [a](https://x.example)')
    expect(html.match(/<a /g)?.length).toBe(1)
  })
  it('does not turn commit-like words into dead links', () => {
    expect(renderUntrusted('fixed in 1a2b3c4d')).not.toContain('<a')
  })
})
