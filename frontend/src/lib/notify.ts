import { get, writable } from 'svelte/store'
import { api } from './api'
import { conflictsBody, decide, doneBody, failureBody, notificationId, type Delivery, type NotifyEvent, type NotifyTarget, type OpKind } from './notifyRules'
import { chatOpen, mergeState, notifyAi, notifyDone, notifyEnabled, notifyProblem, repos, selectRepo, selectedRepoId } from './stores'
import { errorMessage, toast } from './ui'
import { EventsOn } from '../../wailsjs/runtime/runtime'
import { CHAT_WATCH_EVENTS, emptyWatch, watchChat } from './notifyChat'

/** Whether the window has focus, kept live by startNotifications. */
export const windowFocused = writable(typeof document !== 'undefined' && document.hasFocus())

const repoName = (id: string) => get(repos).find((r) => r.id === id)?.name ?? 'CommitTree'

/** What View or a notification click opens; a repository removed since is ignored. */
export function openTarget(repoID: string, target: NotifyTarget) {
  if (!get(repos).some((r) => r.id === repoID)) return
  selectRepo(repoID)
  if (target === 'chat') chatOpen.set(true)
}

const viewAction = (repoID: string, target: NotifyTarget) => ({ label: 'View', run: () => openTarget(repoID, target) })

/** notify applies the rules and delivers: an OS notification, or a toast
 *  (also when the OS one cannot be shown). Resolves to what was decided. */
export async function notify(e: NotifyEvent): Promise<Delivery> {
  const d = decide(e, {
    enabled: get(notifyEnabled), done: get(notifyDone), ai: get(notifyAi), problem: get(notifyProblem),
    focused: get(windowFocused), activeRepoID: get(selectedRepoId), chatOpen: get(chatOpen),
  })
  if (d === 'system') {
    try {
      await api.notify({ id: notificationId(e), title: repoName(e.repoID), body: e.body, repoID: e.repoID, target: e.target })
      return d
    } catch {
      // Not allowed or not available: the toast below is seen on return.
    }
  }
  if (d !== 'none' && !e.toasted) toast(`${repoName(e.repoID)}: ${e.body}`, 'info', viewAction(e.repoID, e.target))
  return d
}

/** The error toast of a failed operation: as before for the selected
 *  repository; another one's carries its name and a View button. */
export function opError(id: string, e: unknown) {
  const message = errorMessage(e)
  if (id === get(selectedRepoId)) toast(message, 'error')
  else toast(`${repoName(id)}: ${message}`, 'error', viewAction(id, 'repo'))
}

/** track runs one user operation and notifies how it ended: conflicts,
 *  else finished (the 10 s threshold is decide's); a failure is notified
 *  and rethrown for the caller's opError. */
export async function track<T>(id: string, op: OpKind, fn: () => Promise<T>, conflictsOf?: (r: T) => number): Promise<T> {
  const start = Date.now()
  let result: T
  try {
    result = await fn()
  } catch (e) {
    void notify({ category: 'problem', repoID: id, target: 'repo', body: failureBody(op, errorMessage(e)), toasted: true })
    throw e
  }
  const files = conflictsOf ? conflictsOf(result) : 0
  if (files > 0) {
    void notify({ category: 'problem', repoID: id, target: 'repo', body: conflictsBody(op, files) })
  } else {
    const ms = Date.now() - start
    void notify({ category: 'done', repoID: id, target: 'repo', body: doneBody(op, ms), durationMs: ms })
  }
  return result
}

/** A stash apply/pop reports a conflict as success and leaves it in the
 *  merge state (refreshed for the selected repository only). */
export function notifyStashConflicts(id: string) {
  const s = get(mergeState)
  if (id !== get(selectedRepoId) || !s?.merging || s.kind !== 'stash' || s.conflicts.length === 0) return
  void notify({ category: 'problem', repoID: id, target: 'repo', body: conflictsBody('stash', s.conflicts.length) })
}

/** startNotifications keeps windowFocused live, opens what a clicked OS
 *  notification was about, and turns chat events into notifications —
 *  here, app-wide, since the chat panel is unmounted while closed. */
export function startNotifications(on: typeof EventsOn = EventsOn): () => void {
  const focus = () => windowFocused.set(true)
  const blur = () => windowFocused.set(false)
  // Read again here, not only at import: the webview may not have reported
  // focus yet when the module first loaded.
  windowFocused.set(document.hasFocus())
  window.addEventListener('focus', focus)
  window.addEventListener('blur', blur)
  const offOpen = on('notify:open', (p: { repoID: string; target: NotifyTarget }) => openTarget(p.repoID, p.target === 'chat' ? 'chat' : 'repo'))
  let watch = emptyWatch()
  const offChat = CHAT_WATCH_EVENTS.map((name) =>
    on(name, (payload: unknown) => {
      const r = watchChat(watch, name, payload, Date.now())
      watch = r.watch
      if (r.event) void notify(r.event)
    }),
  )
  return () => {
    window.removeEventListener('focus', focus)
    window.removeEventListener('blur', blur)
    offOpen()
    offChat.forEach((off) => off())
  }
}
