import { describe, expect, it, beforeEach } from 'vitest'
import { get } from 'svelte/store'
import {
  cancelClone, cloneCancelling, cloneDialogOpen, cloneForm, cloneView, dirName, editName, editURL, emptyForm, joinPath, nameError, redactURL,
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

  it('returns whether the backend accepted the clone', async () => {
    expect(await startClone('/p', async () => {})).toBe(true)
    expect(await startClone('/p', () => Promise.reject('refused'))).toBe(false)
  })

  it('shows the destination the backend resolved once it accepted the clone', async () => {
    const status = async () => ({ running: true, url: '', dest: '/home/me/code/copy', progress: null, lastError: '' })
    await startClone('~/code', async () => {}, status)
    expect(get(cloneView)).toMatchObject({ kind: 'running', dest: '/home/me/code/copy' })
  })

  it('keeps the typed path when the clone already ended or the status fails', async () => {
    await startClone('~/code', async () => {}, async () => ({ running: false, url: '', dest: '', progress: null, lastError: '' }))
    expect(get(cloneView)).toMatchObject({ kind: 'running', dest: '~/code/copy' })
    await startClone('~/code', async () => {}, () => Promise.reject('no'))
    expect(get(cloneView)).toMatchObject({ kind: 'running', dest: '~/code/copy' })
  })

  it('does not overwrite a finished clone with the status', async () => {
    const status = async () => {
      await ev.fire('clone:done', { repo, error: '', cancelled: false } satisfies CloneDone)
      return { running: true, url: '', dest: '/x', progress: null, lastError: '' }
    }
    await startClone('/p', async () => {}, status)
    expect(get(cloneView).kind).toBe('form')
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

  it('cloned but not completely: the success path, and the error as an error toast', async () => {
    const error = 'Cloned, but some submodules or files could not be checked out: fatal: boom'
    await ev.fire('clone:done', { repo, error, cancelled: false })
    expect(get(cloneDialogOpen)).toBe(false)
    expect(added).toEqual([repo])
    expect(get(cloneForm)).toEqual(emptyForm)
    expect(toasts).toEqual([{ message: error, kind: 'error', action: undefined }])
    toasts.length = 0
    await ev.fire('clone:done', { repo, error, cancelled: false })
    expect(toasts).toEqual([{ message: error, kind: 'error', action: undefined }])
  })

  it('on cancel: form, no error, no toast', async () => {
    cloneView.set({ kind: 'running', url: 'u', dest: '/d', progress: null })
    await ev.fire('clone:done', { repo: null, error: '', cancelled: true })
    expect(get(cloneView)).toEqual({ kind: 'form', error: '' })
    expect(toasts).toEqual([])
  })
})

describe('redactURL', () => {
  it.each([
    ['https://user:tok3n@h/o/x.git', 'https://user:***@h/o/x.git'],
    ['https://tok3n@h/o/x.git', 'https://***@h/o/x.git'],
    ['ssh://git@h:2222/o/x.git', 'ssh://***@h:2222/o/x.git'],
    ['https://h/o/x.git', 'https://h/o/x.git'],
    ['git@h:o/x.git', 'git@h:o/x.git'],
    ['https://user:***@h/o/x.git', 'https://user:***@h/o/x.git'],
  ])('%s', (url, want) => expect(redactURL(url)).toBe(want))
})

describe('running view and cancelling', () => {
  it('startClone shows the URL without credentials but runs with the typed one', async () => {
    cloneForm.set(editURL(emptyForm, 'https://user:tok3n@h/o/copy.git'))
    let ran = ''
    await startClone('/p', async (url) => { ran = url })
    expect(ran).toBe('https://user:tok3n@h/o/copy.git')
    expect(get(cloneView)).toMatchObject({ kind: 'running', url: 'https://user:***@h/o/copy.git' })
  })

  it('cancelClone marks cancelling until clone:done; a failed cancel call undoes it', async () => {
    cloneCancelling.set(false)
    await cancelClone(async () => {})
    expect(get(cloneCancelling)).toBe(true)
    const ev = fakeEvents()
    const stop = startCloneEvents({ on: ev.on as any, added: async () => {}, toast: () => {} })
    await ev.fire('clone:done', { repo: null, error: '', cancelled: true })
    expect(get(cloneCancelling)).toBe(false)
    stop()
    await cancelClone(async () => { throw new Error('x') })
    expect(get(cloneCancelling)).toBe(false)
  })
})
