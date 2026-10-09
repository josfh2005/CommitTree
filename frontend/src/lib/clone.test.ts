import { describe, expect, it, beforeEach } from 'vitest'
import { get } from 'svelte/store'
import {
  cloneDialogOpen, cloneForm, cloneView, dirName, editName, editURL, emptyForm, joinPath, nameError,
  startClone, startCloneEvents, viewFromStatus,
} from './clone'
import type { CloneDone, CloneProgress, Repo } from './types'

describe('dirName', () => {
  it.each([
    ['https://github.com/org/repo.git', 'repo'],
    ['https://github.com/org/repo', 'repo'],
    ['https://github.com/org/repo.git/', 'repo'],
    ['git@github.com:org/repo.git', 'repo'],
    ['git@host:repo.git', 'repo'],
    ['ssh://git@host:2222/org/repo.git', 'repo'],
    ['/src/thing/', 'thing'],
    ['file:///src/thing.git', 'thing'],
    ['  https://h/o/spaced.git  ', 'spaced'],
    ['', ''],
  ])('%s → %s', (url, want) => expect(dirName(url)).toBe(want))
})

describe('nameError', () => {
  it('accepts plain names, spaces and non-ASCII', () => {
    expect(nameError('repo')).toBe('')
    expect(nameError('my repo ñ')).toBe('')
  })
  it('refuses empty, dots and slashes', () => {
    expect(nameError('  ')).toBe('Enter a folder name.')
    expect(nameError('.')).toBe('Choose another folder name.')
    expect(nameError('..')).toBe('Choose another folder name.')
    expect(nameError('a/b')).toBe("The folder name can't contain a slash.")
    expect(nameError('a\\b')).toBe("The folder name can't contain a slash.")
  })
})

describe('joinPath', () => {
  it('joins without doubling the separator', () => {
    expect(joinPath('/Users/me', 'repo')).toBe('/Users/me/repo')
    expect(joinPath('/Users/me/', 'repo')).toBe('/Users/me/repo')
    expect(joinPath('C:\\src', 'repo')).toBe('C:\\src\\repo')
  })
})

describe('form editing', () => {
  it('follows the URL until the name is edited by hand', () => {
    let f = editURL(emptyForm, 'https://h/o/one.git')
    expect(f.name).toBe('one')
    f = editName(f, 'mine')
    f = editURL(f, 'https://h/o/two.git')
    expect(f.name).toBe('mine')
  })
  it('follows the URL again once the name is cleared', () => {
    let f = editName(editURL(emptyForm, 'https://h/o/one.git'), '')
    f = editURL(f, 'https://h/o/two.git')
    expect(f.name).toBe('two')
  })
})

describe('viewFromStatus', () => {
  it('maps running and idle status', () => {
    const p: CloneProgress = { phase: 'Receiving objects', percent: 40, detail: '4/10' }
    expect(viewFromStatus({ running: true, url: 'u', dest: '/d', progress: p, lastError: '' }))
      .toEqual({ kind: 'running', url: 'u', dest: '/d', progress: p })
    expect(viewFromStatus({ running: false, url: '', dest: '', progress: null, lastError: 'boom' }))
      .toEqual({ kind: 'form', error: 'boom' })
  })
})

type Handler = (payload: any) => unknown
function fakeEvents() {
  const handlers = new Map<string, Handler>()
  const on = (name: string, fn: Handler) => { handlers.set(name, fn); return () => handlers.delete(name) }
  return { on, fire: async (name: string, payload: unknown) => { await handlers.get(name)?.(payload) } }
}

const repo = { id: 'r1', name: 'copy', path: '/p/copy' } as Repo

describe('startClone and events', () => {
  let toasts: { message: string; kind: string; action?: { label: string } }[]
  let added: Repo[]
  let ev: ReturnType<typeof fakeEvents>
  let stop: () => void

  beforeEach(() => {
    stop?.()
    toasts = []
    added = []
    ev = fakeEvents()
    cloneView.set({ kind: 'form', error: '' })
    cloneForm.set(editURL(emptyForm, 'https://h/o/copy.git'))
    cloneDialogOpen.set(true)
    stop = startCloneEvents({
      on: ev.on as any,
      added: async (r) => { added.push(r) },
      toast: (message, kind, action) => { toasts.push({ message, kind, action }) },
    })
  })

  it('shows running at once, then progress', async () => {
    let resolve!: () => void
    const run = () => new Promise<void>((r) => { resolve = r })
    const p = startClone('/p', run)
    expect(get(cloneView)).toEqual({ kind: 'running', url: 'https://h/o/copy.git', dest: '/p/copy', progress: null })
    await ev.fire('clone:progress', { phase: 'Receiving objects', percent: 10, detail: '1/10' })
    expect(get(cloneView)).toMatchObject({ kind: 'running', progress: { percent: 10 } })
    resolve()
    await p
  })

  it('does not get stuck on running when clone:done beats the CloneRepo promise', async () => {
    let resolve!: () => void
    const p = startClone('/p', () => new Promise<void>((r) => { resolve = r }))
    await ev.fire('clone:done', { repo, error: '', cancelled: false } satisfies CloneDone)
    resolve()
    await p
    expect(get(cloneView).kind).toBe('form')
    expect(get(cloneDialogOpen)).toBe(false)
    expect(added).toEqual([repo])
  })

  it('returns to the form with the backend refusal', async () => {
    await startClone('/p', () => Promise.reject('The destination already exists and isn\'t an empty folder.'))
    expect(get(cloneView)).toEqual({ kind: 'form', error: "The destination already exists and isn't an empty folder." })
  })

  it('on success with the dialog open: closes it, adds the repo, resets the form, no toast', async () => {
    await ev.fire('clone:done', { repo, error: '', cancelled: false })
    expect(get(cloneDialogOpen)).toBe(false)
    expect(added).toEqual([repo])
    expect(get(cloneForm)).toEqual(emptyForm)
    expect(toasts).toEqual([])
  })

  it('on success with the dialog closed: toasts "Cloned <name>"', async () => {
    cloneDialogOpen.set(false)
    await ev.fire('clone:done', { repo, error: '', cancelled: false })
    expect(toasts).toEqual([{ message: 'Cloned copy', kind: 'info', action: undefined }])
  })

  it('on failure: form with the error and the values kept; a toast with Show when closed', async () => {
    await ev.fire('clone:done', { repo: null, error: 'boom', cancelled: false })
    expect(get(cloneView)).toEqual({ kind: 'form', error: 'boom' })
    expect(get(cloneForm).url).toBe('https://h/o/copy.git')
    expect(toasts).toEqual([])
    cloneDialogOpen.set(false)
    await ev.fire('clone:done', { repo: null, error: 'boom', cancelled: false })
    expect(toasts[0]).toMatchObject({ message: 'boom', kind: 'error', action: { label: 'Show' } })
  })

  it('on cancel: form, no error, no toast', async () => {
    cloneView.set({ kind: 'running', url: 'u', dest: '/d', progress: null })
    await ev.fire('clone:done', { repo: null, error: '', cancelled: true })
    expect(get(cloneView)).toEqual({ kind: 'form', error: '' })
    expect(toasts).toEqual([])
  })
})
