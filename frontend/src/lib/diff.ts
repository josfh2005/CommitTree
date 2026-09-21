/**
 * lineClass classifies one line of diff-pane text for colouring. While a
 * merge conflict is unresolved the text is the raw conflict-marker block, not
 * a diff, so `resolved` (default true) switches between the two readings.
 * Shared by MergeView (which passes its own resolved flag) and ChangesView
 * (which always shows a real diff).
 */
export function lineClass(line: string, resolved = true): string {
  if (!resolved) {
    if (line.startsWith('<<<<<<<') || line.startsWith('>>>>>>>') || line.startsWith('|||||||') || line.startsWith('=======')) return 'marker'
    return ''
  }
  if (line.startsWith('+++') || line.startsWith('---') || line.startsWith('diff ') || line.startsWith('index ')) return 'meta'
  if (line.startsWith('@@')) return 'hunk'
  if (line.startsWith('+')) return 'add'
  if (line.startsWith('-')) return 'del'
  return ''
}
