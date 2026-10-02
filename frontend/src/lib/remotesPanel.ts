import { get, writable } from 'svelte/store'
import type { RemoteConfig, RemoteTest } from './types'
import { errorMessage } from './ui'

/** The Remotes tab's data (docs/spec/12-repository-settings.md), apart from
 *  the component so its ordering rules are tested: a write's git error
 *  survives the reload that follows it, and a list or a Test that answers
 *  after the dialog moved on (another repository, or closed and reopened)
 *  is dropped. */
export interface RemotesDeps {
  list: (id: string) => Promise<RemoteConfig[]>
  test: (id: string, name: string) => Promise<RemoteTest>
  /** Runs after every write, whether it failed or not (refresh the app). */
  afterWrite: (id: string) => Promise<void>
}

export interface RemotesState {
  repoID: string
  remotes: RemoteConfig[]
  loadError: string
  /** The last write's error, shown at the top of the tab. */
  error: string
  tests: Record<string, RemoteTest | 'running'>
}

const empty = (repoID: string): RemotesState => ({ repoID, remotes: [], loadError: '', error: '', tests: {} })

export function remotesPanel(d: RemotesDeps) {
  const state = writable<RemotesState>(empty(''))
  // session changes on every open; loads count separately so only the
  // latest one lands.
  let session = 0
  let loads = 0

  async function reload() {
    const s = session
    const n = ++loads
    const id = get(state).repoID
    try {
      const remotes = await d.list(id)
      if (s === session && n === loads) state.update((st) => ({ ...st, remotes, loadError: '' }))
    } catch (e) {
      if (s === session && n === loads) state.update((st) => ({ ...st, remotes: [], loadError: errorMessage(e) }))
    }
  }

  function open(id: string): Promise<void> {
    session++
    state.set(empty(id))
    return reload()
  }

  /** Runs one git write; true when it succeeded. */
  async function write(fn: () => Promise<unknown>): Promise<boolean> {
    const s = session
    const id = get(state).repoID
    state.update((st) => ({ ...st, error: '' }))
    let ok = true
    try {
      await fn()
    } catch (e) {
      ok = false
      if (s === session) state.update((st) => ({ ...st, error: errorMessage(e) }))
    }
    if (s === session) await reload()
    await d.afterWrite(id)
    return ok
  }

  async function test(name: string) {
    const s = session
    const id = get(state).repoID
    state.update((st) => ({ ...st, tests: { ...st.tests, [name]: 'running' } }))
    let res: RemoteTest
    try {
      res = await d.test(id, name)
    } catch (e) {
      res = { ok: false, message: errorMessage(e) }
    }
    if (s === session) state.update((st) => ({ ...st, tests: { ...st.tests, [name]: res } }))
  }

  function clearTest(name: string) {
    state.update((st) => {
      const { [name]: _, ...tests } = st.tests
      return { ...st, tests }
    })
  }

  return { state, open, write, test, clearTest }
}
