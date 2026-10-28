import { othersAhead } from './push'
import type { Branch, PushRequest, Refs } from './types'

/** The rows of the Push dialog (docs/spec/05-remote-and-stash.md). Pure, so
 *  every rule is tested. `remote` is the remote branch's name on the selected
 *  remote; `typed` is true once the user wrote it, so changing the remote
 *  keeps it. */
export interface PushRow { local: string; remote: string; push: boolean; track: boolean; typed: boolean }

const remoteNames = (refs: Refs) => refs.remotes.map((r) => r.name)

/** "origin/feature/x" → origin + feature/x, preferring the longest remote
 *  name (as the Go side does); null when no remote prefixes it. */
export function splitUpstream(upstream: string, remotes: string[]): { remote: string; branch: string } | null {
  let best = ''
  for (const r of remotes) {
    if (upstream.startsWith(r + '/') && upstream.length > r.length + 1 && r.length > best.length) best = r
  }
  return best ? { remote: best, branch: upstream.slice(best.length + 1) } : null
}

/** The remote upstream of a local branch, or null (none, or another local branch). */
function upstreamOf(b: Branch, refs: Refs) {
  return b.upstream && !b.upstreamLocal ? splitUpstream(b.upstream, remoteNames(refs)) : null
}

/** The remote branch a local one shows while unticked: its upstream when that
 *  is on `remote`, else a branch of the same name there, else ''. */
export function counterpart(b: Branch, remote: string, refs: Refs): string {
  const up = upstreamOf(b, refs)
  if (up && up.remote === remote) return up.branch
  const on = refs.remotes.find((r) => r.name === remote)
  return on?.branches.some((rb) => rb.name === b.name) ? b.name : ''
}

/** The remote the dialog opens on: the first ticked branch's upstream
 *  remote, else origin, else the first remote, else ''. */
export function defaultRemote(refs: Refs, ticked: string[]): string {
  const first = ticked.length ? refs.local.find((b) => b.name === ticked[0]) : undefined
  const up = first && upstreamOf(first, refs)
  if (up) return up.remote
  return fallbackRemote(refs)
}

const fallbackRemote = (refs: Refs) =>
  refs.remotes.some((r) => r.name === 'origin') ? 'origin' : (refs.remotes[0]?.name ?? '')

/** Every local branch as a row; the ticked ones come with Track on and a
 *  remote branch (counterpart, else their own name). */
export function initialRows(refs: Refs, remote: string, ticked: string[]): PushRow[] {
  return refs.local.map((b) => {
    const on = ticked.includes(b.name)
    const other = counterpart(b, remote, refs)
    return { local: b.name, remote: on ? other || b.name : other, push: on, track: on, typed: false }
  })
}

/** Ticking fills a blank remote branch with the local name and turns Track
 *  on; unticking turns Track off and keeps the name. */
export function setTicked(row: PushRow, on: boolean): PushRow {
  if (on) return { ...row, push: true, track: true, remote: row.remote || row.local }
  return { ...row, push: false, track: false }
}

/** Select All: rows already ticked stay as they are (a user who unticked
 *  Track? keeps it off); unticking clears every row. */
export const selectAll = (rows: PushRow[], on: boolean): PushRow[] => rows.map((r) => (on && r.push ? r : setTicked(r, on)))

/** The remote changed: typed names stay, the rest follow the new remote
 *  (a ticked row left blank gets its local name). */
export function changeRemote(rows: PushRow[], refs: Refs, remote: string): PushRow[] {
  return rows.map((r) => {
    if (r.typed) return r
    const b = refs.local.find((x) => x.name === r.local)
    const other = b ? counterpart(b, remote, refs) : ''
    return { ...r, remote: other || (r.push ? r.local : '') }
  })
}

export const editRemote = (row: PushRow, name: string): PushRow => ({ ...row, remote: name.trim(), typed: true })

/** What OK sends: the ticked rows, in row order. */
export function pushRequest(remote: string, rows: PushRow[], tags: boolean, force: boolean): PushRequest {
  return {
    remote,
    branches: rows.filter((r) => r.push).map((r) => ({ local: r.local, remote: r.remote, track: r.track })),
    tags,
    force,
  }
}

export const canPush = (rows: PushRow[], tags: boolean): boolean => tags || rows.some((r) => r.push)

/** "Push main branches": the current branch when it is official (HEAD not
 *  detached, not tracking a local branch) plus the other main branches ahead.
 *  The remote is the one most of them track (ties: origin, then name order);
 *  only those on it are ticked, plus a current branch with no upstream. */
export function mainBranchesSelection(refs: Refs): { remote: string; ticked: string[] } {
  const current = !refs.detached ? refs.local.find((b) => b.current) : undefined
  const picked = [...(current?.official && !current.upstreamLocal ? [current] : []), ...othersAhead(refs)]
  const votes = new Map<string, number>()
  const remoteOf = new Map<string, string>()
  for (const b of picked) {
    const up = upstreamOf(b, refs)
    if (!up) continue
    remoteOf.set(b.name, up.remote)
    votes.set(up.remote, (votes.get(up.remote) ?? 0) + 1)
  }
  let remote = fallbackRemote(refs)
  if (votes.size > 0) {
    const top = Math.max(...votes.values())
    const tied = [...votes].filter(([, n]) => n === top).map(([r]) => r).sort()
    remote = tied.includes('origin') ? 'origin' : tied[0]
  }
  // A branch with no upstream can only be the current one (othersAhead needs an upstream).
  const ticked = picked.filter((b) => (remoteOf.get(b.name) ?? remote) === remote).map((b) => b.name)
  return { remote, ticked }
}
