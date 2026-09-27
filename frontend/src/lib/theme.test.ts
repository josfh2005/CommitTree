import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'
import { applyContrast, applyTheme, contrastRatio, isThemePref, resolveTheme } from './theme'

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

describe('resolveTheme', () => {
  it('follows the system on auto', () => {
    expect(resolveTheme('auto', true)).toBe('dark')
    expect(resolveTheme('auto', false)).toBe('light')
  })
  it('keeps an explicit choice whatever the system says', () => {
    expect(resolveTheme('light', true)).toBe('light')
    expect(resolveTheme('dark', false)).toBe('dark')
  })
})

describe('applyTheme', () => {
  it('marks the root with the resolved theme', () => {
    const attrs = new Map<string, string>()
    const root = { setAttribute: (k: string, v: string) => void attrs.set(k, v) }
    applyTheme(root, 'dark')
    expect(attrs.get('data-theme')).toBe('dark')
    applyTheme(root, 'light')
    expect(attrs.get('data-theme')).toBe('light')
  })
})

describe('isThemePref', () => {
  it('accepts only the three choices', () => {
    for (const v of ['auto', 'light', 'dark']) expect(isThemePref(v)).toBe(true)
    for (const v of ['system', '', null, 1]) expect(isThemePref(v)).toBe(false)
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
  const lightHigh = block(css, ":root[data-contrast='high']")
  const darkHigh = block(css, ":root[data-theme='dark'][data-contrast='high']")
  // High contrast sits on top of the normal palette of the same mode.
  const lightBase = block(css, ':root')
  const darkBase = block(css, ":root[data-theme='dark']")

  it('is chosen by data-theme, not by the system media query', () => {
    // The theme setting (auto/light/dark) resolves in main.ts; a media
    // query here would override an explicit Light on a dark system.
    expect(css).not.toContain('prefers-color-scheme')
  })
  const light = { ...lightBase, ...lightHigh }
  const dark = { ...lightBase, ...darkBase, ...darkHigh }
  const textTokens = ['--text', '--muted', '--faint', '--merge-text']

  it('changes only text colours, never backgrounds or borders', () => {
    expect(Object.keys(lightHigh).sort()).toEqual([...textTokens].sort())
    expect(Object.keys(darkHigh).sort()).toEqual([...textTokens].sort())
  })

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
  }
})
