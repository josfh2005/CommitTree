/** coalesce wraps an async job so it never runs twice at once: a call
 *  while it runs gets one more run that starts when the current one ends,
 *  shared with every other call made meanwhile. Each caller so awaits a
 *  run that started after its call — it sees what it changed — and at most
 *  two runs happen back to back. */
export function coalesce(job: () => Promise<void>): () => Promise<void> {
  let current: Promise<void> | null = null
  let next: Promise<void> | null = null
  const run = (): Promise<void> => {
    if (!current) {
      current = job().finally(() => {
        current = null
      })
      return current
    }
    if (!next) {
      next = current
        .catch(() => {})
        .then(() => {
          next = null
          return run()
        })
    }
    return next
  }
  return run
}
