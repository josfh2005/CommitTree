
/** The rules behind notifications (docs/spec/11-notifications.md): pure, so
 *  every combination of settings, focus and what is on screen is testable. */

export type NotifyCategory = 'done' | 'ai' | 'problem' | 'remote'
export type NotifyTarget = 'repo' | 'chat'
export type Delivery = 'none' | 'toast' | 'system'

export interface NotifyEvent {
  category: NotifyCategory
  repoID: string
  /** One short sentence; the title is the repository name. */
  body: string
  /** What View or a click opens: the repository, or its chat. */
  target: NotifyTarget
  /** How long the operation or the turn took; `done` events need it. */
  durationMs?: number
  /** The caller already showed it as an error toast (a failed operation),
   *  so the toast path must not add a second one. */
  toasted?: boolean
}

export interface NotifyContext {
  enabled: boolean
  done: boolean
  ai: boolean
  problem: boolean
  remote: boolean
  focused: boolean
  activeRepoID: string
  chatOpen: boolean
}

export const DONE_THRESHOLD_MS = 10_000

export type OpKind = 'fetch' | 'pull' | 'push' | 'merge' | 'rebase' | 'cherry-pick' | 'submodules' | 'flow' | 'stash'

const OP_LABEL: Record<OpKind, string> = {
  fetch: 'Fetch', pull: 'Pull', push: 'Push', merge: 'Merge', rebase: 'Rebase',
  'cherry-pick': 'Cherry-pick', submodules: 'Submodule update', flow: 'git-flow', stash: 'Stash',
}

export function decide(e: NotifyEvent, c: NotifyContext): Delivery {
  if (!c.enabled || !c[e.category]) return 'none'
  if (e.category === 'done' && (e.durationMs ?? 0) < DONE_THRESHOLD_MS) return 'none'
  if (!c.focused) return 'system'
  const inSight = e.repoID === c.activeRepoID && (e.target !== 'chat' || c.chatOpen)
  return inSight ? 'none' : 'toast'
}

export function firstLine(text: string, max = 120): string {
  const line = text.split('\n').map((l) => l.trim()).find((l) => l !== '') ?? ''
  return line.slice(0, max)
}

export const formatSeconds = (ms: number) => `${Math.round(ms / 1000)} s`

export const doneBody = (op: OpKind, ms: number) => `${OP_LABEL[op]} finished · ${formatSeconds(ms)}`

export const failureBody = (op: OpKind, message: string) => `${OP_LABEL[op]} failed: ${firstLine(message)}`

export const conflictsBody = (op: OpKind, files: number) =>
  `Conflicts in ${files} file${files === 1 ? '' : 's'} after the ${OP_LABEL[op].toLowerCase()}`

export const remoteBody = (count: number, upstream: string) =>
  `${count} new commit${count === 1 ? '' : 's'} on ${upstream}`

export const notificationId = (e: NotifyEvent) => `${e.repoID}:${e.category}`

/** Settings' line for Go's NotificationStatus. */
export function statusText(status: string): string {
  if (status === 'allowed') return 'System notifications are allowed.'
  if (status === 'not allowed')
    return 'System notifications are not allowed yet — CommitTree asks the first time it needs one, or allow them in the system settings.'
  if (status.startsWith('unavailable: '))
    return `System notifications are unavailable (${status.slice('unavailable: '.length)}); in-app toasts are used instead.`
  return ''
}
