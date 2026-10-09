/** Path completion for a typed folder field: the dropdown's state and keys,
 *  and a debounced lookup that ignores stale answers. Pure, no DOM. */

export interface PathList {
  items: string[]
  /** Index of the highlighted suggestion; -1 when none (the typed text stands). */
  selected: number
  open: boolean
}

export const closedList: PathList = { items: [], selected: -1, open: false }

/** The list for a lookup's answer: open when there is something to offer. */
export function openList(items: string[]): PathList {
  return items.length === 0 ? closedList : { items, selected: -1, open: true }
}

export const closeList = (): PathList => closedList

/** Moves the highlight by delta, wrapping at both ends. From "none", down
 *  goes to the first suggestion and up to the last. */
export function moveSelection(s: PathList, delta: 1 | -1): PathList {
  if (!s.open || s.items.length === 0) return s
  const n = s.items.length
  const next = s.selected < 0 ? (delta > 0 ? 0 : n - 1) : (s.selected + delta + n) % n
  return { ...s, selected: next }
}

/** The field's new value after picking a suggestion: the path plus a "/" so
 *  the next level can be completed straight away. */
export function applyPick(item: string): string {
  return item.endsWith('/') ? item : item + '/'
}

/** A suggestion split into the folder it lies in and its own name. */
export function splitPath(item: string): { dir: string; name: string } {
  const i = item.lastIndexOf('/')
  return { dir: item.slice(0, i + 1), name: item.slice(i + 1) }
}

export interface KeyResult {
  state: PathList
  /** The key was used here: the caller prevents its default (and, for Esc, stops it reaching the dialog). */
  handled: boolean
  /** A suggestion to put in the field (before applyPick), or null. */
  pick: string | null
}

/** What a key does in the open dropdown. value is the field's current text.
 *  Nothing happens while the list is closed, so Esc then closes the dialog
 *  and Enter submits the form.
 *  - ArrowDown / ArrowUp move the highlight.
 *  - Tab and Enter complete the highlighted suggestion. With none highlighted,
 *    Enter is left alone (it submits) and Tab completes the first suggestion
 *    only while a name is being typed (the text does not end in "/"); after
 *    a "/", and with Shift, Tab moves focus as usual.
 *  - Esc closes the list. */
export function handleKey(s: PathList, key: string, shift: boolean, value: string): KeyResult {
  const unhandled: KeyResult = { state: s, handled: false, pick: null }
  if (!s.open || s.items.length === 0) return unhandled
  switch (key) {
    case 'ArrowDown':
      return { state: moveSelection(s, 1), handled: true, pick: null }
    case 'ArrowUp':
      return { state: moveSelection(s, -1), handled: true, pick: null }
    case 'Escape':
      return { state: closedList, handled: true, pick: null }
    case 'Enter':
      return s.selected >= 0 ? { state: s, handled: true, pick: s.items[s.selected] } : unhandled
    case 'Tab':
      if (shift) return unhandled
      if (s.selected >= 0) return { state: s, handled: true, pick: s.items[s.selected] }
      return value.endsWith('/') ? unhandled : { state: s, handled: true, pick: s.items[0] }
    default:
      return unhandled
  }
}

export interface Completer {
  /** Looks up value after a pause in typing. */
  request: (value: string) => void
  /** Looks up value now (after a pick). */
  now: (value: string) => void
  /** Drops the pending and in-flight lookups: their answers are ignored. */
  cancel: () => void
}

/** Debounced lookups where only the latest counts: an answer arriving after
 *  a newer request, or after cancel(), is dropped. A failed lookup answers
 *  with no suggestions. */
export function createCompleter(
  fetch: (value: string) => Promise<string[]>,
  apply: (items: string[]) => void,
  delay = 150,
): Completer {
  let seq = 0
  let timer: ReturnType<typeof setTimeout> | null = null
  const clear = () => {
    if (timer !== null) clearTimeout(timer)
    timer = null
  }
  const run = async (value: string) => {
    const mine = ++seq
    let items: string[]
    try {
      items = (await fetch(value)) ?? []
    } catch {
      items = []
    }
    if (mine === seq) apply(items)
  }
  return {
    request(value) {
      clear()
      seq++
      timer = setTimeout(() => {
        timer = null
        void run(value)
      }, delay)
    },
    now(value) {
      clear()
      void run(value)
    },
    cancel() {
      clear()
      seq++
    },
  }
}
