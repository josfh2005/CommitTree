// "Add from folder…": the dialog's state and wording, kept pure for tests.
import { writable } from 'svelte/store'
import type { AddReposResult, Repo, ScanResult } from './types'

export const addReposDialogOpen = writable(false)

export interface ScanRow {
  name: string
  path: string
  /** Already in the sidebar: shown disabled, never ticked. */
  added: boolean
  ticked: boolean
}

export interface ScanView {
  parent: string
  rows: ScanRow[]
  skipped: number
}

export function fromScan(r: ScanResult): ScanView {
  return {
    parent: r.parent,
    skipped: r.skipped,
    rows: r.repos.map((c) => ({ name: c.name, path: c.path, added: c.added, ticked: !c.added })),
  }
}

const setTicks = (v: ScanView, f: (row: ScanRow) => boolean): ScanView => ({
  ...v,
  rows: v.rows.map((row) => (row.added ? row : { ...row, ticked: f(row) })),
})

export const toggle = (v: ScanView, path: string) => setTicks(v, (row) => (row.path === path ? !row.ticked : row.ticked))
export const selectAll = (v: ScanView) => setTicks(v, () => true)
export const selectNone = (v: ScanView) => setTicks(v, () => false)

/** The ticked paths, in list order. */
export function ticked(v: ScanView | null): string[] {
  return v ? v.rows.filter((r) => r.ticked && !r.added).map((r) => r.path) : []
}

export function addLabel(n: number): string {
  if (n === 0) return 'Add repositories'
  return n === 1 ? 'Add 1 repository' : `Add ${n} repositories`
}

export function skippedLine(n: number): string {
  if (n === 0) return ''
  return n === 1 ? '1 folder skipped (not a git repository).' : `${n} folders skipped (not git repositories).`
}

export const NEW_GROUP = '__new_group__'

/** Every group name in use, once each, sorted. */
export function groupOptions(repos: Repo[]): string[] {
  return [...new Set(repos.map((r) => r.group).filter((g): g is string => !!g))].sort((a, b) => a.localeCompare(b))
}

/** The group to add into: "" for none; null when "New group…" has no name yet. */
export function chosenGroup(choice: string, newName: string): string | null {
  if (choice !== NEW_GROUP) return choice
  const name = newName.trim()
  return name === '' ? null : name
}

/** The last segment of a folder path, as suggested for "New group…". */
export function folderName(path: string): string {
  const trimmed = path.replace(/\/+$/, '')
  return trimmed.slice(trimmed.lastIndexOf('/') + 1)
}

export interface AddSummary {
  kind: 'info' | 'error'
  text: string
  /** Nothing was added and something failed: show text in the dialog instead of a toast. */
  keepOpen: boolean
}

const repositories = (n: number) => (n === 1 ? '1 repository' : `${n} repositories`)

function failures(r: AddReposResult): string {
  const named = r.failed.slice(0, 3).map((f) => `${f.name} (${f.reason})`)
  const more = r.failed.length - named.length
  return named.join(', ') + (more > 0 ? `, and ${more} more` : '') + '.'
}

export function summarize(r: AddReposResult): AddSummary {
  const added = r.added.length
  if (r.failed.length === 0) {
    if (added === 0) return { kind: 'info', text: 'Nothing added: those repositories are already in the list.', keepOpen: false }
    return { kind: 'info', text: `Added ${repositories(added)}.`, keepOpen: false }
  }
  if (added === 0) return { kind: 'error', text: `No repository could be added: ${failures(r)}`, keepOpen: true }
  return { kind: 'error', text: `Added ${repositories(added)}; ${r.failed.length} couldn't be added: ${failures(r)}`, keepOpen: false }
}

/** Numbers requests so only the latest answer is used; invalidate() drops the one in flight. */
export interface Latest {
  next(): number
  isCurrent(n: number): boolean
  invalidate(): void
}

export function createLatest(): Latest {
  let current = 0
  return {
    next: () => ++current,
    isCurrent: (n) => n === current,
    invalidate: () => void ++current,
  }
}
