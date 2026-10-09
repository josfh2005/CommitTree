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

export interface CloneForm { url: string; name: string; nameEdited: boolean }
export const emptyForm: CloneForm = { url: '', name: '', nameEdited: false }

export const cloneView = writable<CloneView>({ kind: 'form', error: '' })
/** The form's values, kept while the dialog is closed so a clone that fails
 *  in the background reopens with them. */
export const cloneForm = writable<CloneForm>(emptyForm)
export const cloneDialogOpen = writable(false)

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

/** Starts the clone in cloneForm under parent. The view turns to running
 *  before the backend is called: a tiny clone's clone:done can arrive
 *  before run resolves, and must not be overwritten by it. */
export async function startClone(parent: string, run: (url: string, parent: string, name: string) => Promise<void>) {
  const f = get(cloneForm)
  const url = f.url.trim()
  const name = f.name.trim()
  cloneView.set({ kind: 'running', url, dest: joinPath(parent, name), progress: null })
  try {
    await run(url, parent, name)
  } catch (e) {
    cloneView.set({ kind: 'form', error: errorMessage(e) })
  }
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
    cloneView.set({ kind: 'form', error: d.error })
    if (d.repo) {
      cloneDialogOpen.set(false)
      cloneForm.set(emptyForm)
      await deps.added(d.repo)
      if (!open) deps.toast(`Cloned ${d.repo.name}`, 'info')
    } else if (d.error && !open) {
      deps.toast(d.error, 'error', { label: 'Show', run: () => cloneDialogOpen.set(true) })
    }
  })
  return () => {
    offProgress()
    offDone()
  }
}
