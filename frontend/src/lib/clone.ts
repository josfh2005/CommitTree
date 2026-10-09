import { get, writable } from 'svelte/store'
import type { EventsOn } from '../../wailsjs/runtime/runtime'
import type { CloneDone, CloneProgress, CloneStatus, Repo } from './types'
import { errorMessage, type Toast } from './ui'

/** The folder git would clone url into: its last path segment without a
 *  trailing slash or ".git" (https, ssh://, scp-like host:path, local). */
export function dirName(url: string): string {
  const s = url.trim().replace(/[/\\]+$/, '').replace(/\.git$/, '').replace(/[/\\]+$/, '')
  return s.slice(Math.max(s.lastIndexOf('/'), s.lastIndexOf('\\'), s.lastIndexOf(':')) + 1)
}

/** Why name can't be a clone's folder name; '' when it can. Mirrors
 *  clone.Validate's name rule so the form can say so as the user types. */
export function nameError(name: string): string {
  const n = name.trim()
  if (n === '') return 'Enter a folder name.'
  if (n === '.' || n === '..') return 'Choose another folder name.'
  if (/[/\\]/.test(n)) return "The folder name can't contain a slash."
  return ''
}

/** parent/name for the preview line, with parent's own separator. */
export function joinPath(parent: string, name: string): string {
  const sep = parent.includes('\\') && !parent.includes('/') ? '\\' : '/'
  return parent.replace(/[/\\]+$/, '') + sep + name
}

export type CloneView =
  | { kind: 'form'; error: string }
  | { kind: 'running'; url: string; dest: string; progress: CloneProgress | null }

/** url with any credentials hidden, as the backend shows it
 *  (cmdlog.RedactArgs): user:password@ → user:***@, a bare user@ → ***@. */
export function redactURL(url: string): string {
  return url.replace(/([A-Za-z][A-Za-z0-9+.-]*:\/\/)([^/@\s:]+)(:[^/@\s]*)?@/g, (_m, scheme, user, pass) =>
    pass ? `${scheme}${user}:***@` : `${scheme}***@`)
}

export interface CloneForm { url: string; name: string; nameEdited: boolean }
export const emptyForm: CloneForm = { url: '', name: '', nameEdited: false }

export const cloneView = writable<CloneView>({ kind: 'form', error: '' })
/** The form's values, kept while the dialog is closed so a clone that fails
 *  in the background reopens with them. */
export const cloneForm = writable<CloneForm>(emptyForm)
export const cloneDialogOpen = writable(false)
/** Cancel was asked for and clone:done has not arrived yet (git can take a
 *  few seconds to stop). */
export const cloneCancelling = writable(false)

export async function cancelClone(cancel: () => Promise<void>) {
  cloneCancelling.set(true)
  try {
    await cancel()
  } catch {
    cloneCancelling.set(false)
  }
}

export function editURL(f: CloneForm, url: string): CloneForm {
  return { ...f, url, name: f.nameEdited ? f.name : dirName(url) }
}

/** A hand-edited name stops following the URL; clearing it resumes. */
export function editName(f: CloneForm, name: string): CloneForm {
  return { ...f, name, nameEdited: name !== '' }
}

export function viewFromStatus(s: CloneStatus): CloneView {
  return s.running
    ? { kind: 'running', url: s.url, dest: s.dest, progress: s.progress }
    : { kind: 'form', error: s.lastError }
}

/** Starts the clone in cloneForm under parent (as typed: the backend may
 *  expand it) and returns whether the backend accepted it. The view turns
 *  to running before the backend is called: a tiny clone's clone:done can
 *  arrive before run resolves, and must not be overwritten by it. Once
 *  accepted, a still-running view shows the destination the backend
 *  resolved (status), not the typed path. */
export async function startClone(
  parent: string,
  run: (url: string, parent: string, name: string) => Promise<void>,
  status?: () => Promise<CloneStatus>,
): Promise<boolean> {
  const f = get(cloneForm)
  const url = f.url.trim()
  const name = f.name.trim()
  cloneCancelling.set(false)
  // The view is shown on screen: it never holds the URL's credentials.
  cloneView.set({ kind: 'running', url: redactURL(url), dest: joinPath(parent, name), progress: null })
  try {
    await run(url, parent, name)
  } catch (e) {
    cloneView.set({ kind: 'form', error: errorMessage(e) })
    return false
  }
  if (status) {
    try {
      const s = await status()
      if (s.running) cloneView.update((v) => (v.kind === 'running' ? { ...v, dest: s.dest } : v))
    } catch {
      // Keep the typed path.
    }
  }
  return true
}

export interface CloneDeps {
  on: typeof EventsOn
  /** Called with the new repository: reload the list and select it. */
  added: (repo: Repo) => Promise<void>
  toast: (message: string, kind: Toast['kind'], action?: Toast['action']) => void
}

/** Follows clone:progress and clone:done whether or not the dialog is
 *  open; returns the unsubscribe. */
export function startCloneEvents(deps: CloneDeps): () => void {
  const offProgress = deps.on('clone:progress', (p: CloneProgress) =>
    cloneView.update((v) => (v.kind === 'running' ? { ...v, progress: p } : v)))
  const offDone = deps.on('clone:done', async (d: CloneDone) => {
    const open = get(cloneDialogOpen)
    cloneCancelling.set(false)
    cloneView.set({ kind: 'form', error: d.error })
    if (d.repo) {
      cloneDialogOpen.set(false)
      cloneForm.set(emptyForm)
      await deps.added(d.repo)
      // Cloned, but not completely (a submodule or the checkout failed): the
      // dialog is gone, so the error is shown as a toast whatever it was.
      if (d.error) deps.toast(d.error, 'error')
      else if (!open) deps.toast(`Cloned ${d.repo.name}`, 'info')
    } else if (d.error && !open) {
      deps.toast(d.error, 'error', { label: 'Show', run: () => cloneDialogOpen.set(true) })
    }
  })
  return () => {
    offProgress()
    offDone()
  }
}
