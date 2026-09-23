import type { Repo, Submodule } from './types'

const short = (hash: string) => hash.slice(0, 7)

export interface Marker {
  kind: 'moved' | 'modified' | 'untracked' | 'conflict'
  text: string
}

/** Status markers for a submodule row, in display order. `modified` wins
 *  over an `untracked`-only state (both can be true at once). */
export function markers(s: Submodule): Marker[] {
  const out: Marker[] = []
  if (s.moved) out.push({ kind: 'moved', text: `↕ ${short(s.checkedOut)}` })
  if (s.modified) out.push({ kind: 'modified', text: '●' })
  else if (s.untracked) out.push({ kind: 'untracked', text: '○' })
  if (s.conflict) out.push({ kind: 'conflict', text: '!' })
  return out
}

/** The dimmed-row label, when the submodule cannot be opened as-is.
 *  "not configured" wins over "not initialised" — a row missing from
 *  .gitmodules can't be initialised either. */
export function rowLabel(s: Submodule): '' | 'not initialised' | 'not configured' {
  if (!s.configured) return 'not configured'
  if (!s.initialised) return 'not initialised'
  return ''
}

/** Splits a slash-separated path into its directory (trailing slash kept,
 *  '' at the top) and its leaf name, for two-tone row rendering. */
export function splitPath(path: string): { dir: string; leaf: string } {
  const i = path.lastIndexOf('/')
  return i === -1 ? { dir: '', leaf: path } : { dir: path.slice(0, i + 1), leaf: path.slice(i + 1) }
}

/** Hover detail for a submodule row. */
export function tooltip(s: Submodule): string {
  const lines = [`Recorded: ${short(s.recorded)}`, `Checked out: ${short(s.checkedOut)}${s.branch ? ` on ${s.branch}` : ''}`, `URL: ${s.url}`]
  if (s.name !== s.path) lines.push(`Name: ${s.name}`)
  return lines.join('\n')
}

export interface SubmoduleCommit {
  dir: '>' | '<'
  subject: string
}

export interface SubmoduleDiff {
  path: string
  from: string
  to: string
  note: string
  commits: SubmoduleCommit[]
  content: string[]
}

const HEADER_RE = /^Submodule (.+) ([0-9a-f]+)\.\.\.?([0-9a-f]+)(?: \((.+)\))?:?$/
const CONTENT_RE = /^Submodule (.+) contains (modified|untracked) content$/
const COMMIT_RE = /^\s+([<>]) (.*)$/

/** Parses git's `--submodule=log` output for one submodule's diff hunk.
 *  Returns null when the text contains no `Submodule ` line, i.e. it isn't
 *  a submodule diff at all. */
export function parseSubmoduleDiff(diff: string): SubmoduleDiff | null {
  let result: SubmoduleDiff | null = null
  for (const line of diff.split('\n')) {
    const header = HEADER_RE.exec(line)
    if (header) {
      result = result ?? { path: header[1], from: '', to: '', note: '', commits: [], content: [] }
      result.path = header[1]
      result.from = header[2]
      result.to = header[3]
      result.note = header[4] ?? ''
      continue
    }
    const content = CONTENT_RE.exec(line)
    if (content) {
      result = result ?? { path: content[1], from: '', to: '', note: '', commits: [], content: [] }
      result.content.push(content[2])
      continue
    }
    if (!result) continue
    const commit = COMMIT_RE.exec(line)
    if (commit) result.commits.push({ dir: commit[1] as '<' | '>', subject: commit[2] })
  }
  return result
}

/**
 * Finds the `$repos` item id for the submodule at `path`, relative to
 * `repoId`'s own repository — by absolute path rather than parentId/subPath.
 * `subPath` is always relative to the TOP repository, so it can't be
 * compared against `path` directly once `repoId` is itself an opened
 * submodule (a nested submodule's `path` here is relative to ITS parent);
 * matching on the absolute filesystem path is correct in both cases.
 * Returns undefined when the submodule isn't an opened repository item.
 */
export function submoduleRepoId(list: Repo[], repoId: string, path: string): string | undefined {
  const repo = list.find((r) => r.id === repoId)
  if (!repo) return undefined
  const abs = `${repo.path}/${path}`
  return list.find((r) => r.path === abs)?.id
}

/** Confirmation text for "Update to recorded commit". */
export function updateMessage(s: Submodule): string {
  const base = `${s.path} will leave ${short(s.checkedOut)} and check out the recorded commit ${short(s.recorded)} on a detached HEAD.`
  return s.branch ? `${base} Branch ${s.branch} itself is not changed.` : base
}

/** Toast text after a write leaves submodules behind their recorded commit. */
export function movedMessage(n: number): string {
  return `${n} submodule${n === 1 ? '' : 's'} ${n === 1 ? 'is' : 'are'} not at the recorded commit`
}

/**
 * Repository selection after the repo list refreshes: null keeps whatever
 * is currently selected, '' clears it, and any other string is the id to
 * select instead. Only a submodule that vanished (removed, or its parent
 * repository closed) falls back to its parent when the parent is still
 * listed — a vanished worktree's selection is cleared outright, same as any
 * other removed repository (see docs/spec/01-repositories-and-sidebar.md's
 * Worktrees section).
 */
export function nextSelection(prevSelected: Repo | null, list: Repo[]): string | null {
  if (!prevSelected) return null
  if (list.some((r) => r.id === prevSelected.id)) return null
  if (prevSelected.submodule && prevSelected.parentId && list.some((r) => r.id === prevSelected.parentId)) {
    return prevSelected.parentId
  }
  return ''
}
