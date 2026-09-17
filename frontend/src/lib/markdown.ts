// A deliberately small Markdown renderer for model output. Everything is
// HTML-escaped first; only a few constructs are turned back into markup.

export function escapeHtml(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;')
}

const HASH = /\b([0-9a-f]{7,12})\b/g

function inline(escaped: string): string {
  return escaped
    .split(/(`[^`]+`)/g)
    .map((part, i) => {
      if (i % 2 === 1) return `<code>${part.slice(1, -1)}</code>`
      return part
        .replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>')
        .replace(HASH, (m) => (/[a-f]/.test(m) && /\d/.test(m) ? `<a href="#" data-hash="${m}">${m}</a>` : m))
    })
    .join('')
}

export function renderMarkdown(src: string): string {
  const out: string[] = []
  src.split('```').forEach((segment, index) => {
    if (index % 2 === 1) {
      const body = segment.replace(/^[^\n]*\n/, '').replace(/\n$/, '')
      out.push(`<pre><code>${escapeHtml(body)}</code></pre>`)
      return
    }
    let list: string[] = []
    let para: string[] = []
    const flushList = () => {
      if (list.length) out.push(`<ul>${list.map((l) => `<li>${l}</li>`).join('')}</ul>`)
      list = []
    }
    const flushPara = () => {
      if (para.length) out.push(`<p>${para.join('<br>')}</p>`)
      para = []
    }
    for (const raw of segment.split('\n')) {
      const line = raw.trimEnd()
      const item = line.match(/^\s*(?:[-*]|\d+\.)\s+(.*)$/)
      if (item) {
        flushPara()
        list.push(inline(escapeHtml(item[1])))
        continue
      }
      if (line.trim() === '') {
        flushList()
        flushPara()
        continue
      }
      flushList()
      const heading = line.match(/^#{1,6}\s+(.*)$/)
      para.push(heading ? `<strong>${inline(escapeHtml(heading[1]))}</strong>` : inline(escapeHtml(line)))
    }
    flushList()
    flushPara()
  })
  return out.join('')
}
