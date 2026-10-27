import { writable } from 'svelte/store'
import { formatBytes, percent } from './format'
import { errorMessage } from './ui'
import type { UpdateInfo, UpdateProgress } from './types'

export type CheckTrigger = 'start' | 'focus' | 'manual'

/** A check on window focus happens at most this often (6 h). */
export const FOCUS_SPACING_MS = 6 * 60 * 60 * 1000

/** Whether a check runs now. Start follows the setting; focus also waits
 *  FOCUS_SPACING_MS since the last check; Check now always runs. */
export function shouldCheck(trigger: CheckTrigger, auto: boolean, lastCheck: number | null, now: number): boolean {
  if (trigger === 'manual') return true
  if (!auto) return false
  if (trigger === 'start') return true
  return lastCheck === null || now - lastCheck >= FOCUS_SPACING_MS
}

/** Whether the card pops up for a check's result. `shown` holds the versions
 *  shown in this run: start always shows, focus shows a version once,
 *  Check now shows (and so counts as that version's showing). */
export function shouldShowCard(trigger: CheckTrigger, info: UpdateInfo, shown: readonly string[]): boolean {
  if (!info.newer) return false
  if (trigger === 'focus') return !shown.includes(info.latest)
  return true
}

/** The reasons a restart should be confirmed first, in a fixed order. The
 *  caller passes the running tasks the status bar shows. */
export function restartBlockers(tasks: { label: string }[], chatRunRepo: string, commitDraft: string): string[] {
  const reasons = tasks.map((t) => `${t.label} is running`)
  if (chatRunRepo !== '') reasons.push('The chat is answering')
  if (commitDraft.trim() !== '') reasons.push('The commit message has unsaved text')
  return reasons
}

/** The card's line while downloading: a percentage, or the bytes received
 *  when the server did not say the total (total < 0). */
export function progressText(done: number, total: number): string {
  if (total >= 0) return `Downloading ${percent(done, total)} %`
  return done > 0 ? `Downloading ${formatBytes(done)}` : 'Downloading…'
}

/** The last check's result; null before one succeeded. The status-bar pill
 *  shows while it is newer, whatever happened to the card. */
export const updateInfo = writable<UpdateInfo | null>(null)
export const updateCardOpen = writable(false)
/** What Check now answered (Settings shows it); '' until one ran. */
export const manualCheckResult = writable('')

export interface UpdatePhase {
  phase: 'idle' | 'download' | 'install' | 'ready' | 'error'
  done: number
  total: number
  error: string
}
const IDLE: UpdatePhase = { phase: 'idle', done: 0, total: 0, error: '' }
export const updatePhase = writable<UpdatePhase>({ ...IDLE })

let phaseNow: UpdatePhase = IDLE
updatePhase.subscribe((p) => (phaseNow = p))

/** An update:progress event; one arriving after the install ended is ignored. */
export function setUpdateProgress(p: UpdateProgress) {
  if (phaseNow.phase !== 'download' && phaseNow.phase !== 'install') return
  updatePhase.set({ phase: p.phase, done: p.done, total: p.total, error: '' })
}

/** The update checker. `lastCheck` and the versions already shown live in
 *  memory only, so every run of the app starts fresh. */
export function createUpdateController(deps: { check: () => Promise<UpdateInfo>; now: () => number; auto: () => boolean }) {
  let lastCheck: number | null = null
  const shown: string[] = []

  async function run(trigger: CheckTrigger): Promise<void> {
    if (!shouldCheck(trigger, deps.auto(), lastCheck, deps.now())) return
    lastCheck = deps.now() // before the wait, and also when the check fails
    let info: UpdateInfo
    try {
      info = await deps.check()
    } catch (e) {
      if (trigger === 'manual') manualCheckResult.set(errorMessage(e))
      return
    }
    updateInfo.set(info)
    if (trigger === 'manual') manualCheckResult.set(info.newer ? `${info.latest} is available` : "You're up to date")
    if (shouldShowCard(trigger, info, shown)) {
      if (!shown.includes(info.latest)) shown.push(info.latest)
      updateCardOpen.set(true)
    }
  }
  return { run }
}

const isCancelled = (e: unknown) => /context canceled/i.test(errorMessage(e))

/** "Update and restart": download and unpack, ask first when a restart would
 *  interrupt something, then swap and relaunch. A cancelled download is not an
 *  error. Declining the question puts the card back to idle (the update stays
 *  staged in the app, but the next click downloads again). A click while a download,
 *  the install or the question is going on does nothing. */
export async function startUpdate(deps: {
  prepare: () => Promise<void>
  apply: () => Promise<void>
  confirm: (reasons: string[]) => Promise<boolean>
  blockers: () => string[]
}): Promise<void> {
  if (phaseNow.phase === 'download' || phaseNow.phase === 'install' || phaseNow.phase === 'ready') return
  updatePhase.set({ phase: 'download', done: 0, total: 0, error: '' })
  try {
    await deps.prepare()
  } catch (e) {
    updatePhase.set(isCancelled(e) ? { ...IDLE } : { ...IDLE, phase: 'error', error: errorMessage(e) })
    return
  }
  updatePhase.set({ ...IDLE, phase: 'ready' })
  const reasons = deps.blockers()
  if (reasons.length > 0 && !(await deps.confirm(reasons))) {
    updatePhase.set({ ...IDLE })
    return
  }
  updatePhase.set({ ...IDLE, phase: 'install' })
  try {
    await deps.apply()
  } catch (e) {
    updatePhase.set({ ...IDLE, phase: 'error', error: errorMessage(e) })
  }
}
