import { get, writable, type Writable } from 'svelte/store'
import { api } from './api'
import type { CommitStatus, HostingInfo, HostingLimit, HostName, HostState, WatchEvent } from './types'
import { errorMessage, toast } from './ui'

/** Visible-row requests wait this long for more rows to be added to them. */
export const DEBOUNCE_MS = 500
/** How often the visible `running` commits are asked again. */
export const POLL_MS = 30_000

/** What the app knows of each repository's code host, by repository ID. */
export const hostingInfo: Writable<Record<string, HostingInfo>> = writable({})
/** The statuses shown in the log: repository ID, then commit hash. */
export const commitStatuses: Writable<Record<string, Record<string, CommitStatus>>> = writable({})
/** The hosts whose request limit is in force now, by host. */
export const hostingLimits: Writable<Record<string, HostingLimit>> = writable({})

const FINAL: HostState[] = ['success', 'failed', 'stopped']
const isFinal = (s: HostState) => FINAL.includes(s)

/** True when the repository is on a supported host and its account is connected. */
export function isHosted(info: HostingInfo | undefined): boolean {
  return !!info?.supported && info.account === 'connected'
}

/** Only https links are ever opened. */
export function isHttps(url: string): boolean {
  try {
    return new URL(url).protocol === 'https:'
  } catch {
    return false
  }
}

const inflightInfo = new Map<string, Promise<HostingInfo>>()

/** Reads the repository's host info from the backend (it never reaches the network) and stores it. */
export function loadHostingInfo(repoID: string): Promise<HostingInfo> {
  const running = inflightInfo.get(repoID)
  if (running) return running
  const p = api
    .hostingInfo(repoID)
    .then((info) => {
      hostingInfo.update((m) => ({ ...m, [repoID]: info }))
      return info
    })
    .finally(() => inflightInfo.delete(repoID))
  inflightInfo.set(repoID, p)
  return p
}

async function infoFor(repoID: string): Promise<HostingInfo> {
  return get(hostingInfo)[repoID] ?? loadHostingInfo(repoID)
}

// The hashes waiting for the debounce, by repository; the last set the log
// asked for, by repository; and the repository the log shows now.
let pending = new Map<string, Set<string>>()
const lastVisible = new Map<string, string[]>()
// The checked-out commit of each repository, which the status bar shows
// whether or not the log has it on screen.
const heads = new Map<string, string>()
let current = ''
let timer: ReturnType<typeof setTimeout> | undefined
const reported = new Set<string>()
// Commits whose state a watch event changed, so their checks are not known.
const unchecked = new Set<string>()

function enqueue(repoID: string, hashes: string[]) {
  const set = pending.get(repoID) ?? new Set<string>()
  hashes.forEach((h) => set.add(h))
  pending.set(repoID, set)
  if (timer !== undefined) clearTimeout(timer)
  timer = setTimeout(flush, DEBOUNCE_MS)
}

function flush() {
  timer = undefined
  const batch = pending
  pending = new Map()
  for (const [repoID, set] of batch) void fetchStatuses(repoID, [...set])
}

/** Asks for the statuses of the visible commits (and a margin) of a repository,
 *  after 500 ms without another call. The answer lands under `repoID` only. */
export function requestStatuses(repoID: string, hashes: string[]): void {
  lastVisible.set(repoID, hashes)
  current = repoID
  enqueue(repoID, hashes)
}

/** Asks for the status of a repository's HEAD commit for the status bar. It
 *  joins the log's set in refreshes and polling but never replaces it. */
export function requestHeadStatus(repoID: string, hash: string): void {
  if (!repoID || !hash) return
  heads.set(repoID, hash)
  // The poll follows the repository shown, with or without the log mounted.
  current = repoID
  enqueue(repoID, [hash])
}

/** What a repository's refreshes and polling cover: the log's set and HEAD. */
function watched(repoID: string): string[] {
  const head = heads.get(repoID)
  return [...(lastVisible.get(repoID) ?? []), ...(head ? [head] : [])]
}

async function fetchStatuses(repoID: string, hashes: string[]) {
  try {
    const info = await infoFor(repoID)
    if (!isHosted(info)) return
    const known = get(commitStatuses)[repoID] ?? {}
    // A final state a watch event put in has no checks yet: it is asked once more.
    const ask = hashes.filter((h) => !known[h] || !isFinal(known[h].state) || unchecked.has(`${repoID}|${h}`))
    if (!ask.length) return
    const got = await api.commitStatuses(repoID, ask)
    // repoID was captured when the request was made, so a repository switched
    // to meanwhile never receives this answer.
    commitStatuses.update((m) => ({ ...m, [repoID]: { ...m[repoID], ...got } }))
    ask.forEach((h) => unchecked.delete(`${repoID}|${h}`))
  } catch (e) {
    // "No access to …" is the one error worth saying, once per repository and text.
    const message = errorMessage(e)
    const key = `${repoID}|${message}`
    if (message.startsWith('No access to') && !reported.has(key)) {
      reported.add(key)
      toast(message, 'error')
    }
  }
}

/** Forgets the repository's unfinished statuses and asks for the last visible set again. */
export function refreshStatuses(repoID: string): void {
  const last = watched(repoID)
  if (!repoID || !last.length) return
  commitStatuses.update((m) => {
    const kept: Record<string, CommitStatus> = {}
    for (const [h, st] of Object.entries(m[repoID] ?? {})) if (isFinal(st.state)) kept[h] = st
    return { ...m, [repoID]: kept }
  })
  // The account may have changed since the info was read.
  loadHostingInfo(repoID)
    .catch(() => undefined)
    .then(() => enqueue(repoID, last))
}

/** Every 30 s asks again for the visible commits still `running`. Returns the stop function. */
export function startHostingPolling(): () => void {
  const id = setInterval(() => {
    if (!current) return
    const statuses = get(commitStatuses)[current] ?? {}
    const running = watched(current).filter((h) => statuses[h]?.state === 'running')
    if (running.length) enqueue(current, running)
  }, POLL_MS)
  return () => clearInterval(id)
}

/** Keeps `hosting:limit` payloads by host; a null `until` ends the limit. */
export function applyLimit(l: HostingLimit): void {
  hostingLimits.update((m) => {
    const next = { ...m }
    if (l.until === null) delete next[l.host as HostName]
    else next[l.host] = l
    return next
  })
}

/** The notice shown while a host's request limit is in force. */
export function hostingLimitText(l: HostingLimit): string {
  const name = l.host === 'github' ? 'GitHub' : 'Bitbucket'
  const until = l.until ? new Date(l.until) : null
  if (!l.known || !until || Number.isNaN(until.getTime())) return `${name}: request limit reached, retrying shortly`
  const hh = String(until.getHours()).padStart(2, '0')
  const mm = String(until.getMinutes()).padStart(2, '0')
  return `${name}: request limit reached, retrying at ${hh}:${mm}`
}

/** Puts the state of a watched push into the log's statuses. */
export function applyWatchEvent(e: WatchEvent): void {
  commitStatuses.update((m) => {
    const prev = m[e.repoID]?.[e.hash]
    // Checks of another state would contradict it; the next ask brings them.
    const same = !!prev && prev.state === e.state
    if (!same) unchecked.add(`${e.repoID}|${e.hash}`)
    const st: CommitStatus = { hash: e.hash, state: e.state, checks: same ? prev.checks : null }
    return { ...m, [e.repoID]: { ...m[e.repoID], [e.hash]: st } }
  })
}

// ---- Words ----------------------------------------------------------------

const VERB: Record<HostState, string> = { success: 'passed', failed: 'failed', running: 'running', stopped: 'stopped', none: '' }

/** The state as a word: "passed", "failed", "running" or "stopped". */
export function stateText(s: HostState): string {
  return VERB[s]
}

/** "Pipeline" for Pipelines, "Check" for Checks. */
function singular(noun: string): string {
  return noun.endsWith('s') ? noun.slice(0, -1) : noun
}

/** A duration as "42 s", "3 min" or "1 h 5 min". */
export function formatDuration(sec: number): string {
  if (sec < 60) return `${Math.max(0, Math.round(sec))} s`
  const min = Math.round(sec / 60)
  if (min < 60) return `${min} min`
  const h = Math.floor(min / 60)
  return min % 60 ? `${h} h ${min % 60} min` : `${h} h`
}

function ago(iso: string, now: Date): string {
  const sec = Math.round((now.getTime() - new Date(iso).getTime()) / 1000)
  if (!Number.isFinite(sec) || sec < 60) return 'just now'
  if (sec < 3600) return `${Math.floor(sec / 60)} min ago`
  if (sec < 86400) return `${Math.floor(sec / 3600)} h ago`
  return `${Math.floor(sec / 86400)} d ago`
}

/** The text under a log row's status icon. */
export function statusTooltip(noun: string, st: CommitStatus, now: Date): string {
  if (st.state === 'none') return ''
  const checks = st.checks ?? []
  if (checks.length <= 1) {
    const c = checks[0]
    if (st.state === 'running') {
      const since = c && (c.started || c.created)
      return `${singular(noun)} running${since ? ` · started ${ago(since, now)}` : ''}`
    }
    const duration = c && c.durationSec > 0 ? ` · ${formatDuration(c.durationSec)}` : ''
    return `${c?.name || singular(noun)} ${VERB[st.state]}${duration}`
  }
  const word = noun.toLowerCase()
  const count = (s: HostState) => checks.filter((c) => c.state === s).length
  if (st.state === 'success') return `${checks.length} ${word} passed`
  return `${count(st.state)} of ${checks.length} ${word} ${VERB[st.state]}`
}

/** The status bar's words for HEAD: "Pipeline passed", "Checks failed"; nothing for none. */
export function headStatusText(noun: string, s: HostState): string {
  if (s === 'none') return ''
  return `${noun === 'Checks' ? 'Checks' : 'Pipeline'} ${VERB[s]}`
}

export interface StateIcon {
  name: string
  tone: 'ok' | 'bad' | 'busy' | 'muted'
}

/** The icon for a state; none draws nothing. */
export function stateIcon(s: HostState): StateIcon | null {
  switch (s) {
    case 'success': return { name: 'check', tone: 'ok' }
    case 'failed': return { name: 'x', tone: 'bad' }
    case 'running': return { name: 'refresh', tone: 'busy' }
    case 'stopped': return { name: 'stop', tone: 'muted' }
    default: return null
  }
}
