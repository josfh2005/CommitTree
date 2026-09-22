import { derived, get, writable, type Writable } from 'svelte/store'
import { api } from './api'
import { emptyFilters, type AISettings, type AheadBehind, type Filters, type GitSettings, type MergeState, type Refs, type Repo, type StashEntry, type WorktreeState } from './types'

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
export const worktreeState = writable<WorktreeState | null>(null)
/** The AI settings the commit box needs (auto-generation mode, task
 *  provider) without hitting the API on every render. Loaded at startup and
 *  refreshed after Settings saves — see SettingsDialog's save(). */
export const aiSettings = writable<AISettings | null>(null)
/** Which view the main pane shows: the log (with commit details / the merge
 *  view below it) or the Changes view. A merge in progress always wins over
 *  'changes' — see selectMainView below. */
export const mainView = writable<'log' | 'changes'>('log')

export const remoteInfo = writable<AheadBehind | null>(null)
export const stashEntries = writable<StashEntry[]>([])
export const gitSettings = writable<GitSettings | null>(null)
// The index a conflicted stash pop still owes a drop for, or -1.
export const owedStashDrop = writable<number>(-1)
// Set by the conflict view's "Done" for a stash conflict, which has no
// git-level abort: the files stay as they are and the view stops owning the
// screen. Cleared below whenever the conflict's kind changes or it goes
// away, so it can never hide a *different* conflict later.
export const stashConflictDismissed = writable<boolean>(false)

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
    // A dismissal belongs to one stash conflict only. Anything else — a new
    // kind, or nothing in progress — brings the view back.
    if (state?.kind !== 'stash') stashConflictDismissed.set(false)
  } catch {
    if (get(selectedRepoId) === repo.id) mergeState.set(null)
  }
}

export async function loadWorktreeState() {
  const repo = get(selectedRepo)
  if (!repo || repo.missing) {
    worktreeState.set(null)
    return
  }
  try {
    const state = await api.getWorktreeState(repo.id)
    // A slower answer for a repository the user has already left must not
    // overwrite the one now on screen.
    if (get(selectedRepoId) !== repo.id) return
    worktreeState.set(state)
  } catch {
    if (get(selectedRepoId) === repo.id) worktreeState.set(null)
  }
}

export async function loadRemoteInfo() {
  const repo = get(selectedRepo)
  if (!repo || repo.missing) {
    remoteInfo.set(null)
    return
  }
  try {
    const info = await api.getRemoteInfo(repo.id)
    if (get(selectedRepoId) !== repo.id) return
    remoteInfo.set(info)
  } catch {
    remoteInfo.set(null)
  }
}

export async function loadOwedStashDrop() {
  const repo = get(selectedRepo)
  if (!repo || repo.missing) {
    owedStashDrop.set(-1)
    return
  }
  try {
    const owed = await api.owedStashDrop(repo.id)
    if (get(selectedRepoId) !== repo.id) return
    owedStashDrop.set(owed)
  } catch {
    owedStashDrop.set(-1)
  }
}

export async function loadStashEntries() {
  const repo = get(selectedRepo)
  if (!repo || repo.missing) {
    stashEntries.set([])
    return
  }
  try {
    const entries = await api.getStashEntries(repo.id)
    if (get(selectedRepoId) !== repo.id) return
    stashEntries.set(entries)
  } catch {
    stashEntries.set([])
  }
}

export async function loadGitSettings() {
  try {
    gitSettings.set(await api.getGitSettings())
  } catch {
    // Left as whatever was last loaded — the toolbar has no strategy
    // picker of its own, so nothing else depends on this succeeding.
  }
}

export async function loadAISettings() {
  try {
    aiSettings.set(await api.getAISettings())
  } catch {
    // Left as whatever was last loaded (or null) — the commit box treats a
    // null store as "don't auto-generate" rather than erroring.
  }
}

export async function refreshRepo() {
  await loadRepos()
  await loadRefs()
  await loadMergeState()
  await loadWorktreeState()
  await loadRemoteInfo()
  await loadStashEntries()
  await loadOwedStashDrop()
  logVersion.update((v) => v + 1)
}

export function selectRepo(id: string) {
  if (get(selectedRepoId) !== id) {
    filters.set(emptyFilters())
    selectedHash.set('')
    mainView.set('log')
    stashConflictDismissed.set(false)
  }
  selectedRepoId.set(id)
  // Selecting a folded repo unfolds it; folding it later keeps it selected.
  expandedRepos.update((ids) => (ids.includes(id) ? ids : [...ids, id]))
  loadRefs()
  loadMergeState()
  loadWorktreeState()
  loadRemoteInfo()
  loadStashEntries()
  loadOwedStashDrop()
}

export function toggleRepoExpanded(id: string) {
  expandedRepos.update((ids) => (ids.includes(id) ? ids.filter((x) => x !== id) : [...ids, id]))
}
