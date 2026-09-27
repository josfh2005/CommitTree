/** Turns the high-contrast palette in theme.css on or off by marking the
 *  root element; theme.css keys its overrides on data-contrast="high". */
export function applyContrast(root: Pick<Element, 'setAttribute' | 'removeAttribute'>, high: boolean) {
  if (high) root.setAttribute('data-contrast', 'high')
  else root.removeAttribute('data-contrast')
}

function luminance(hex: string): number {
  const [r, g, b] = [1, 3, 5].map((i) => {
    const c = parseInt(hex.slice(i, i + 2), 16) / 255
    return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4
  })
  return 0.2126 * r + 0.7152 * g + 0.0722 * b
}

/** The WCAG contrast ratio between two #rrggbb colours, from 1 to 21. */
export function contrastRatio(a: string, b: string): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x)
  return (hi + 0.05) / (lo + 0.05)
}

/** Settings → Appearance → Theme. Auto follows the system. */
export type ThemePref = 'auto' | 'light' | 'dark'

export function isThemePref(v: unknown): v is ThemePref {
  return v === 'auto' || v === 'light' || v === 'dark'
}

/** The palette to draw with: an explicit choice wins over the system. */
export function resolveTheme(pref: ThemePref, systemDark: boolean): 'light' | 'dark' {
  return pref === 'auto' ? (systemDark ? 'dark' : 'light') : pref
}

/** Marks the root element; theme.css keys the dark palette on data-theme. */
export function applyTheme(root: Pick<Element, 'setAttribute'>, theme: 'light' | 'dark') {
  root.setAttribute('data-theme', theme)
}
