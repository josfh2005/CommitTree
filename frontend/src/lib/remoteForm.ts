/** The Remotes tab's add form (docs/spec/12-repository-settings.md):
 *  '' — can be added; '-' — incomplete, Add stays disabled with nothing
 *  shown; anything else — the reason, shown under the form. Git checks
 *  the rest when the remote is added. */
export function remoteFormError(name: string, url: string, existing: string[]): string {
  const n = name.trim()
  if (n === '' || url.trim() === '') return '-'
  if (/\s/.test(n)) return 'A remote name has no spaces'
  if (existing.includes(n)) return `A remote named ${n} already exists`
  return ''
}

export const defaultRemoteName = (existing: string[]) => (existing.length === 0 ? 'origin' : '')
