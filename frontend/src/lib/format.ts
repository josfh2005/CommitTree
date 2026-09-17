// The Until filter is otherwise exclusive of the chosen day (git's --until
// cuts off at 00:00:00 that day), so store end-of-day when a date is picked.
export function untilFilterValue(date: string): string {
  return date ? `${date} 23:59:59` : ''
}

// Strips the end-of-day suffix so a native date input displays the plain
// date the user picked.
export function untilDisplayValue(stored: string): string {
  return stored.split(' ')[0]
}

export function relativeDate(iso: string, now = new Date()): string {
  const date = new Date(iso)
  const seconds = Math.round((now.getTime() - date.getTime()) / 1000)
  if (seconds < 60) return 'just now'
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`
  if (seconds < 7 * 86400) return `${Math.floor(seconds / 86400)}d ago`
  const options: Intl.DateTimeFormatOptions = { month: 'short', day: 'numeric' }
  if (date.getFullYear() !== now.getFullYear()) options.year = 'numeric'
  return date.toLocaleDateString('en-US', options)
}
