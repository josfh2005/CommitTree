import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'
import { applyContrast, contrastRatio } from './theme'

describe('applyContrast', () => {
  it('sets and removes data-contrast on the root element', () => {
    const attrs = new Map<string, string>()
    const root = {
      setAttribute: (k: string, v: string) => void attrs.set(k, v),
      removeAttribute: (k: string) => void attrs.delete(k),
    }
    applyContrast(root, true)
    expect(attrs.get('data-contrast')).toBe('high')
    applyContrast(root, false)
    expect(attrs.has('data-contrast')).toBe(false)
  })
})

describe('contrastRatio', () => {
  it('matches the WCAG formula', () => {
    expect(contrastRatio('#000000', '#ffffff')).toBeCloseTo(21, 1)
    expect(contrastRatio('#ffffff', '#ffffff')).toBeCloseTo(1, 5)
  })
})

/** The custom properties declared in the first `selector { … }` block found
 *  at or after `from` in css. */
function block(css: string, selector: string, from = 0): Record<string, string> {
  const start = css.indexOf(selector + ' {', from)
  if (start < 0) throw new Error(`no block for ${selector}`)
  const body = css.slice(start, css.indexOf('}', start))
  return Object.fromEntries([...body.matchAll(/(--[\w-]+):\s*(#[0-9a-fA-F]{6})/g)].map((m) => [m[1], m[2]]))
}

describe('high contrast palettes in theme.css', () => {
  const css = readFileSync(new URL('../theme.css', import.meta.url), 'utf8')
  const selector = ":root[data-contrast='high']"
  const light = block(css, selector)
  const dark = block(css, selector, css.indexOf('@media (prefers-color-scheme: dark)', css.indexOf(selector)))

  for (const [name, palette] of [['light', light], ['dark', dark]] as const) {
    it(`${name}: text stays at least 4.5:1 on every background`, () => {
      const backgrounds = ['--bg', '--surface', '--sidebar', '--hover', '--active', '--selection']
      for (const fg of ['--text', '--muted', '--merge-text']) {
        for (const bg of backgrounds) {
          expect(contrastRatio(palette[fg], palette[bg]), `${fg} on ${bg}`).toBeGreaterThanOrEqual(4.5)
        }
      }
      for (const bg of ['--bg', '--surface', '--sidebar']) {
        expect(contrastRatio(palette['--faint'], palette[bg]), `--faint on ${bg}`).toBeGreaterThanOrEqual(4.5)
      }
    })

    it(`${name}: borders stand out from the surfaces they separate`, () => {
      for (const bg of ['--bg', '--surface', '--sidebar']) {
        expect(contrastRatio(palette['--border'], palette[bg]), `--border on ${bg}`).toBeGreaterThanOrEqual(1.9)
      }
    })
  }
})
