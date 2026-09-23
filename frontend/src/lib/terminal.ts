import { writable } from 'svelte/store'

/** One shell tab. Tabs belong to a repository; only the selected
 *  repository's tabs are shown, the rest keep running in the background. */
export interface TermTab {
  id: string
  repoId: string
  label: string
  /** null while the shell runs. */
  exitCode: number | null
}

export interface TermState {
  tabs: TermTab[]
  /** Last number handed out per repository — never reused while the app runs. */
  counters: Record<string, number>
  /** Active tab per repository. */
  active: Record<string, string>
}

export const emptyTermState = (): TermState => ({ tabs: [], counters: {}, active: {} })

export function addTab(s: TermState, repoId: string, id: string, shell: string): TermState {
  const n = (s.counters[repoId] ?? 0) + 1
  return {
    tabs: [...s.tabs, { id, repoId, label: `${shell} ${n}`, exitCode: null }],
    counters: { ...s.counters, [repoId]: n },
    active: { ...s.active, [repoId]: id },
  }
}

export function removeTab(s: TermState, id: string): TermState {
  const tab = s.tabs.find((t) => t.id === id)
  if (!tab) return s
  const siblings = tabsFor(s, tab.repoId)
  const at = siblings.findIndex((t) => t.id === id)
  const tabs = s.tabs.filter((t) => t.id !== id)
  const active = { ...s.active }
  if (active[tab.repoId] === id) {
    const next = siblings[at - 1] ?? siblings[at + 1]
    if (next) active[tab.repoId] = next.id
    else delete active[tab.repoId]
  }
  return { ...s, tabs, active }
}

export function markExited(s: TermState, id: string, code: number): TermState {
  if (!s.tabs.some((t) => t.id === id)) return s
  return { ...s, tabs: s.tabs.map((t) => (t.id === id ? { ...t, exitCode: code } : t)) }
}

export function removeRepoTabs(s: TermState, repoId: string): TermState {
  const active = { ...s.active }
  delete active[repoId]
  return { ...s, tabs: s.tabs.filter((t) => t.repoId !== repoId), active }
}

export function tabsFor(s: TermState, repoId: string): TermTab[] {
  return s.tabs.filter((t) => t.repoId === repoId)
}

export function setActive(s: TermState, repoId: string, id: string): TermState {
  return { ...s, active: { ...s.active, [repoId]: id } }
}

export function tabTitle(t: TermTab): string {
  return t.exitCode === null ? t.label : `${t.label} — exited (${t.exitCode})`
}

/** What a settled terminal command should refresh. Selecting a repository
 *  already reloads everything about it, so a background repository only
 *  needs the sidebar list (its branch label) refreshed. */
export function settledAction(selectedRepoId: string, repo: string): 'check' | 'list' {
  return repo === selectedRepoId ? 'check' : 'list'
}

export const terminalState = writable<TermState>(emptyTermState())
