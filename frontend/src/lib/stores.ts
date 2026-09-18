import { derived, get, writable, type Writable } from 'svelte/store'
import { api } from './api'
import { emptyFilters, type Filters, type MergeState, type Refs, type Repo } from './types'

function persisted<T>(key: string, initial: T): Writable<T> {
  let start = initial
  try {
    const raw = localStorage.getItem(key)
    if (raw !== null) start = JSON.parse(raw)
  } catch {
    // Storage unavailable: fall back to the default.
  }
  const store = writable<T>(start)
  store.subscribe((value) => {
    try {
      localStorage.setItem(key, JSON.stringify(value))
    } catch {
      // Ignore: persistence is a convenience.
    }
  })
  return store
}

export const sidebarWidth = persisted('sidebarWidth', 280)
export const chatWidth = persisted('chatWidth', 340)
export const detailsHeight = persisted('detailsHeight', 280)
export const chatOpen = persisted('chatOpen', true)
export const selectedRepoId = persisted('selectedRepoId', '')
/** Ids of the repos whose refs are unfolded in the sidebar. */
export const expandedRepos = persisted<string[]>('expandedRepos', [])

export const repos = writable<Repo[]>([])
export const refs = writable<Refs | null>(null)
export const filters = writable<Filters>(emptyFilters())
export const selectedHash = writable('')
export const jumpTo = writable('')
export const logVersion = writable(0)
export const busy = writable('')
export const settingsOpen = writable(false)
export const mergeState = writable<MergeState | null>(null)

export const selectedRepo = derived([repos, selectedRepoId], ([$repos, $id]) => $repos.find((r) => r.id === $id) ?? null)

export async function loadRepos() {
  repos.set(await api.listRepos())
}

export async function loadRefs() {
  const repo = get(selectedRepo)
  if (!repo || repo.missing) {
    refs.set(null)
    return
  }
  try {
    refs.set(await api.getRefs(repo.id))
  } catch {
    refs.set(null)
  }
}

export async function loadMergeState() {
  const repo = get(selectedRepo)
  if (!repo || repo.missing) {
    mergeState.set(null)
    return
  }
  try {
    const state = await api.getMergeState(repo.id)
    // A slower answer for a repository the user has already left must not
    // overwrite the one now on screen.
    if (get(selectedRepoId) !== repo.id) return
    mergeState.set(state)
  } catch {
    if (get(selectedRepoId) === repo.id) mergeState.set(null)
  }
}

export async function refreshRepo() {
  await loadRepos()
  await loadRefs()
  await loadMergeState()
  logVersion.update((v) => v + 1)
}

export function selectRepo(id: string) {
  if (get(selectedRepoId) !== id) {
    filters.set(emptyFilters())
    selectedHash.set('')
  }
  selectedRepoId.set(id)
  // Selecting a folded repo unfolds it; folding it later keeps it selected.
  expandedRepos.update((ids) => (ids.includes(id) ? ids : [...ids, id]))
  loadRefs()
  loadMergeState()
}

export function toggleRepoExpanded(id: string) {
  expandedRepos.update((ids) => (ids.includes(id) ? ids.filter((x) => x !== id) : [...ids, id]))
}
