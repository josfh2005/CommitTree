import { describe, expect, it } from 'vitest'
import { escapeHtml, renderMarkdown } from './markdown'

describe('renderMarkdown', () => {
  it('escapes HTML', () => {
    expect(escapeHtml('<b>&"')).toBe('&lt;b&gt;&amp;&quot;')
    expect(renderMarkdown('<script>alert(1)</script>')).toBe('<p>&lt;script&gt;alert(1)&lt;/script&gt;</p>')
  })

  it('renders paragraphs, bold and inline code', () => {
    expect(renderMarkdown('Hello **world**\nline two\n\nuse `git log`')).toBe(
      '<p>Hello <strong>world</strong><br>line two</p><p>use <code>git log</code></p>',
    )
  })

  it('renders lists and headings', () => {
    expect(renderMarkdown('## Changes\n- one\n* two\n1. three')).toBe(
      '<p><strong>Changes</strong></p><ul><li>one</li><li>two</li><li>three</li></ul>',
    )
  })

  it('renders fenced code blocks, even unclosed while streaming', () => {
    expect(renderMarkdown('before\n```go\nx := 1 < 2\n```\nafter')).toBe(
      '<p>before</p><pre><code>x := 1 &lt; 2</code></pre><p>after</p>',
    )
    expect(renderMarkdown('```\npartial')).toBe('<pre><code>partial</code></pre>')
  })

  it('links commit hashes outside code', () => {
    expect(renderMarkdown('see a1b2c3d and `a1b2c3d`')).toBe(
      '<p>see <a href="#" data-hash="a1b2c3d">a1b2c3d</a> and <code>a1b2c3d</code></p>',
    )
    expect(renderMarkdown('year 2026091 and word decade1')).toContain('<a href="#" data-hash="decade1">')
    expect(renderMarkdown('number 1234567')).not.toContain('data-hash')
  })
})
