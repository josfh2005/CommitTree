import { derived, get, writable, type Writable } from 'svelte/store'
import { api } from './api'
import type { BlameTarget } from './blame'
import { validSelectedStash, type SelectedStash } from './stash'
import { nextSelection } from './submodules'
import { emptyFilters, type AISettings, type AheadBehind, type Filters, type GitSettings, type Identity, type LogOrder, type MergeState, type Refs, type Repo, type StashEntry, type WorktreeState } from './types'
import { isLogOrder } from './logOrder'
import { removeRepoTabs, terminalState } from './terminal'

/** @param isValid For a value drawn from a constrained set (e.g. a union of
 *  string literals): a type guard checked against whatever JSON.parse
 *  returns, so a corrupt localStorage entry or one left by an older version
 *  of the app with a since-removed option falls back to `initial` instead
 *  of reaching the UI unvalidated. Omit it for values with no such
 *  constraint (free-form strings, arrays, numbers, booleans). */
export function persisted<T>(key: string, initial: T, isValid?: (value: unknown) => value is T): Writable<T> {
  let start = initial
  try {
    const raw = localStorage.getItem(key)
    if (raw !== null) {
      const parsed = JSON.parse(raw)
      start = !isValid || isValid(parsed) ? parsed : initial
    }
  } catch {
    // Storage unavailable or corrupt JSON: fall back to the default.
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
/** Settings → Appearance → High contrast; main.ts applies it to the page. */
export const highContrast = persisted('highContrast', false, (v): v is boolean => typeof v === 'boolean')

/** Repositories whose chat is taken by an explanation still being prepared
 *  (blame and diffs being read) before its question appears in the chat. */
export const chatPreparing = writable<string[]>([])

/** Runs an Explain … in chat call, marking the repository's chat as
 *  preparing until the call returns, so the chat shows Stop meanwhile. */
export async function explainIntoChat(repoID: string, call: () => Promise<void>): Promise<void> {
  chatPreparing.update((ids) => [...ids, repoID])
  try {
    await call()
  } finally {
    chatPreparing.update((ids) => {
      const i = ids.indexOf(repoID)
      return i < 0 ? ids : [...ids.slice(0, i), ...ids.slice(i + 1)]
    })
  }
}
export const terminalOpen = persisted('terminalOpen', false)
/** Height of the terminal under the chat, in pixels. */
export const terminalHeight = persisted('terminalHeight', 260)
export const selectedRepoId = persisted('selectedRepoId', '')
/** Ids of the repos whose refs are unfolded in the sidebar. */
export const expandedRepos = persisted<string[]>('expandedRepos', [])
/** Ids of the repos whose Tags section is expanded — it starts collapsed,
 *  so only the non-default (expanded) state needs remembering, same as
 *  expandedRepos above. Keyed by repo id so expanding it in one repository
 *  doesn't expand it in another. */
export const expandedTagSections = persisted<string[]>('expandedTagSections', [])
/** Ids of the repositories whose sidebar Stash section is expanded. Like
 *  Tags it starts collapsed, remembered per repository. */
export const expandedStashSections = persisted<string[]>('expandedStashSections', [])
/** Ids of the repositories whose sidebar Submodules section is expanded.
 *  Like Tags and Stash it starts collapsed, remembered per repository. */
export const expandedSubmoduleSections = persisted<string[]>('expandedSubmoduleSections', [])
/** Ids of the sidebar repo groups that are collapsed — groups start
 *  expanded, so only the non-default (collapsed) state needs remembering. */
export const collapsedRepoGroups = persisted<string[]>('collapsedRepoGroups', [])
/** How the log is ordered — a global preference, not per repository, so it
 *  is stored here alongside the other persisted UI preferences rather than
 *  in a per-repo backend setting (contrast gitSettings below, which is
 *  per-repository and backend-owned). */
export const logOrder = persisted<LogOrder>('logOrder', 'topo', isLogOrder)

export const repos = writable<Repo[]>([])
export const refs = writable<Refs | null>(null)
export const filters = writable<Filters>(emptyFilters())
export const selectedHash = writable('')
export const jumpTo = writable('')
export const logVersion = writable(0)
export const busy = writable('')
export const settingsOpen = writable(false)
/** Wails' Environment().platform, read once at startup (App.svelte). */
export const platform = writable('')
export const mergeState = writable<MergeState | null>(null)
export const worktreeState = writable<WorktreeState | null>(null)
/** The AI settings the commit box needs (auto-generation mode, task
 *  provider) without hitting the API on every render. Loaded at startup and
 *  refreshed after Settings saves — see SettingsDialog's save(). */
export const aiSettings = writable<AISettings | null>(null)
/** Which view the main pane shows: the log (with commit details / the merge
 *  view below it), the Changes view, a stash preview, or a file's blame. A
 *  merge in progress always wins over either — see conflictOwnsScreen in
 *  remote.ts, which App.svelte applies on top of this. */
export const mainView = writable<'log' | 'changes' | 'stash' | 'blame'>('log')

/** The file the Blame view shows, and the targets Blame previous revision
 *  left behind, newest last, for Back to return to. */
export const blameTarget = writable<BlameTarget | null>(null)
export const blameStack = writable<BlameTarget[]>([])
export const blameIgnoreWhitespace = persisted('blameIgnoreWhitespace', false)

/** Opens the Blame view on path at rev ('' = working tree); Back returns to
 *  the view it was opened from. */
export function openBlame(path: string, rev: string) {
  const from = get(mainView) === 'changes' ? 'changes' : 'log'
  blameStack.set([])
  blameTarget.set({ path, rev, from })
  mainView.set('blame')
}

export function blamePrevious(path: string, rev: string) {
  const current = get(blameTarget)
  if (!current) return
  blameStack.update((s) => [...s, current])
  blameTarget.set({ path, rev, from: current.from })
}

export function blameBack() {
  const stack = get(blameStack)
  if (stack.length) {
    blameTarget.set(stack[stack.length - 1])
    blameStack.set(stack.slice(0, -1))
    return
  }
  const from = get(blameTarget)?.from ?? 'log'
  blameTarget.set(null)
  mainView.set(from)
}

/** Clears the Blame view outright and leaves the log showing — used
 *  wherever a stale blame target must not reappear later: a repository
 *  switch (selectRepo), the repository list going empty (loadRepos), and
 *  the selected repository becoming missing while blame is open
 *  (App.svelte). */
export function closeBlame() {
  mainView.set('log')
  blameTarget.set(null)
  blameStack.set([])
}

/** Selects a commit in the log. mainView is set explicitly: when the blame
 *  was opened from this very commit, selectedHash does not change, so
 *  App.svelte's "a selection means the log" rule would not fire. */
export function showCommitInLog(hash: string) {
  selectedHash.set(hash)
  jumpTo.set(hash)
  mainView.set('log')
}
/** The log's synthetic "Uncommitted changes" row is selected, so the
 *  details pane shows the Changes view. Mutually exclusive with
 *  selectedHash: selectUncommitted clears the hash, and the subscription
 *  below clears this whenever anything selects a commit — graph clicks,
 *  jump arrows, context menus — without each of them knowing about it. */
export const uncommittedSelected = writable(false)
selectedHash.subscribe((hash) => {
  if (hash) uncommittedSelected.set(false)
})

export function selectUncommitted() {
  selectedHash.set('')
  uncommittedSelected.set(true)
  mainView.set('log')
}

/** A left click on a log row or graph dot: select the commit, or clear the
 *  selection (closing the details pane) when it is the one already
 *  selected. Right-click menus and jumps keep using selectedHash.set, which
 *  always selects. */
export function toggleCommit(hash: string) {
  selectedHash.set(get(selectedHash) === hash ? '' : hash)
}

/** The same toggle for the log's "Uncommitted changes" row. */
export function toggleUncommitted() {
  if (get(uncommittedSelected)) uncommittedSelected.set(false)
  else selectUncommitted()
}
/** Which stash the sidebar has selected for the preview pane, or null when
 *  none is. Identified by hash, not index — an index shifts whenever any
 *  entry below it is applied, popped or dropped, including from another
 *  session, since the stash stack is shared across worktrees. Revalidated
 *  against the current list in loadStashEntries, and cleared outright on a
 *  repository switch — see selectRepo and validSelectedStash. */
export const selectedStash = writable<SelectedStash | null>(null)

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
  // Captured before repos.set(list) below, so it reflects what was selected
  // against the OLD list — nextSelection decides what to do with it once the
  // new list is in. At startup `repos` is still empty, so this is always
  // null then — prevId (the persisted id alone) covers that case below.
  const prevSelected = get(selectedRepo)
  const prevId = get(selectedRepoId)
  const list = await api.listRepos()
  repos.set(list)
  // A detected worktree or submodule can vanish between two reads (removed
  // in a terminal, deinitialised, or by the tool that created it). Whatever
  // is no longer listed loses its terminal tabs, and a selection pointing at
  // it is cleared — except a vanished submodule, which falls back to its
  // still-listed parent — see nextSelection.
  const ids = new Set(list.map((r) => r.id))
  terminalState.update((s) => {
    let next = s
    for (const t of s.tabs) if (!ids.has(t.repoId)) next = removeRepoTabs(next, t.repoId)
    return next
  })
  // At startup there is no previous Repo object to run nextSelection's
  // parent-fallback through (a vanished submodule's parentId was never
  // fetched), so a stale persisted id that no longer exists is cleared
  // outright, same as any other removed repository's default case.
  const next = prevSelected ? nextSelection(prevSelected, list) : prevId && !ids.has(prevId) ? '' : null
  if (next === null) return
  if (next) {
    selectRepo(next)
    return
  }
  selectedRepoId.set('')
  selectedHash.set('')
  uncommittedSelected.set(false)
  closeBlame()
  refs.set(null)
  mergeState.set(null)
  worktreeState.set(null)
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

/** The selected repository's git user, whose commits the log highlights. */
export const identity = writable<Identity | null>(null)

export async function loadIdentity() {
  const repo = get(selectedRepo)
  if (!repo || repo.missing) {
    identity.set(null)
    return
  }
  try {
    const me = await api.getIdentity(repo.id)
    if (get(selectedRepoId) === repo.id) identity.set(me)
  } catch {
    if (get(selectedRepoId) === repo.id) identity.set(null)
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
    selectedStash.update((s) => validSelectedStash(s, entries))
  } catch {
    stashEntries.set([])
    selectedStash.set(null)
  }
}

/** selectStash opens the stash preview for entry — a sidebar click. Mirrors
 *  openChanges' cross-repo handling: the caller selects the target
 *  repository first when it differs from the one already selected. Stored
 *  by hash (see SelectedStash/validSelectedStash), not by index alone. */
export function selectStash(entry: StashEntry) {
  selectedStash.set({ index: entry.index, hash: entry.hash })
  mainView.set('stash')
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
    uncommittedSelected.set(false)
    closeBlame()
    stashConflictDismissed.set(false)
    selectedStash.set(null)
    worktreeState.set(null)
    identity.set(null)
  }
  selectedRepoId.set(id)
  // Selecting a folded repo unfolds it; folding it later keeps it selected.
  expandedRepos.update((ids) => (ids.includes(id) ? ids : [...ids, id]))
  loadRefs()
  loadMergeState()
  loadWorktreeState()
  loadIdentity()
  loadRemoteInfo()
  loadStashEntries()
  loadOwedStashDrop()
}

export function toggleRepoExpanded(id: string) {
  expandedRepos.update((ids) => (ids.includes(id) ? ids.filter((x) => x !== id) : [...ids, id]))
}

export function toggleTagsExpanded(repoId: string) {
  expandedTagSections.update((ids) => (ids.includes(repoId) ? ids.filter((x) => x !== repoId) : [...ids, repoId]))
}

export function toggleStashExpanded(repoId: string) {
  expandedStashSections.update((ids) => (ids.includes(repoId) ? ids.filter((x) => x !== repoId) : [...ids, repoId]))
}

export function toggleSubmodulesExpanded(repoId: string) {
  expandedSubmoduleSections.update((ids) => (ids.includes(repoId) ? ids.filter((x) => x !== repoId) : [...ids, repoId]))
}

export function toggleRepoGroupCollapsed(name: string) {
  collapsedRepoGroups.update((names) => (names.includes(name) ? names.filter((x) => x !== name) : [...names, name]))
}
