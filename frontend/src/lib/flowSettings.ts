import { derived, get, writable } from 'svelte/store'
import type { FlowConfig, FlowPrefixes, FlowSettings } from './types'
import { errorMessage } from './ui'

/** The Git-flow tab of Repository settings (docs/spec/12-repository-settings.md),
 *  apart from the component so its rules are tested. */

/** Roughly `git check-ref-format --branch`; Go checks with git itself. */
function validBranchName(n: string): boolean {
  if (n === '' || n === '@' || n.startsWith('-') || n.startsWith('/') || n.endsWith('/') || n.endsWith('.')) return false
  if (/[\s\x00-\x1f\x7f~^:?*[\\]/.test(n) || n.includes('..') || n.includes('@{') || n.includes('//')) return false
  return n.split('/').every((c) => !c.startsWith('.') && !c.endsWith('.lock'))
}

const PREFIXES: { key: keyof FlowPrefixes; label: string }[] = [
  { key: 'feature', label: 'Feature prefix' },
  { key: 'release', label: 'Release prefix' },
  { key: 'hotfix', label: 'Hotfix prefix' },
  { key: 'warmfix', label: 'Warmfix prefix' },
]

/** The first thing wrong with the form, '' when it can be saved. */
export function flowSettingsError(c: FlowConfig): string {
  const master = c.master.trim()
  const develop = c.develop.trim()
  for (const [label, name] of [['Production branch', master], ['Development branch', develop]]) {
    if (name === '') return `${label} is required`
    if (!validBranchName(name)) return `${label} is not a valid branch name`
  }
  // Case-insensitive, like the Go side: Main and main are one ref on macOS.
  if (master.toLowerCase() === develop.toLowerCase()) return 'Production and development must be different branches'
  const seen = new Map<string, string>()
  for (const { key, label } of PREFIXES) {
    const p = c.prefixes[key].trim()
    if (p === '') return `${label} is required`
    if (/\s/.test(p)) return `${label} has spaces`
    // A prefix is fine when a branch named with it would be.
    if (!validBranchName(p + 'x')) return `${label} is not valid`
    const other = seen.get(p.toLowerCase())
    if (other) return `${other} and ${label.toLowerCase()} must be different`
    seen.set(p.toLowerCase(), label)
  }
  return ''
}

function toConfig(s: FlowSettings): FlowConfig {
  return { master: s.master, develop: s.develop, prefixes: { ...s.prefixes } }
}

function trimmed(c: FlowConfig): FlowConfig {
  const { feature, release, hotfix, warmfix } = c.prefixes
  return {
    master: c.master.trim(), develop: c.develop.trim(),
    prefixes: { feature: feature.trim(), release: release.trim(), hotfix: hotfix.trim(), warmfix: warmfix.trim() },
  }
}

const sameConfig = (a: FlowConfig, b: FlowConfig) =>
  a.master === b.master && a.develop === b.develop && PREFIXES.every(({ key }) => a.prefixes[key] === b.prefixes[key])

export interface FlowSettingsDeps {
  get: (id: string) => Promise<FlowSettings>
  /** Rewrites the keys of a repository that is set up. */
  save: (id: string, cfg: FlowConfig) => Promise<void>
  /** "Initialize git-flow": the path a repository that is not set up takes. */
  init: (id: string, cfg: FlowConfig) => Promise<void>
  /** Runs after every write, failed or not (reload refs and sidebar). */
  afterWrite: (id: string) => Promise<void>
}

export interface FlowSettingsState {
  repoID: string
  settings: FlowSettings | null
  loadError: string
  /** The last save's error (or the form's, when it was not sent). */
  error: string
}

const emptyConfig = (): FlowConfig => ({ master: '', develop: '', prefixes: { feature: '', release: '', hotfix: '', warmfix: '' } })

export function flowSettingsPanel(d: FlowSettingsDeps) {
  const state = writable<FlowSettingsState>({ repoID: '', settings: null, loadError: '', error: '' })
  const form = writable<FlowConfig>(emptyConfig())
  /** Whether the form differs from what is stored. */
  const dirty = derived([state, form], ([$s, $f]) => !!$s.settings && !sameConfig(trimmed($f), toConfig($s.settings)))
  // Changes on every open, so an answer for an earlier one is dropped.
  let session = 0
  // The form as it was when the shown error came up: editing it clears the error.
  let errorFor = ''
  form.subscribe(($f) => {
    if (errorFor !== '' && JSON.stringify($f) !== errorFor) {
      errorFor = ''
      state.update((st) => (st.error === '' ? st : { ...st, error: '' }))
    }
  })

  async function load(): Promise<void> {
    const s = session
    const id = get(state).repoID
    try {
      const settings = await d.get(id)
      if (s !== session) return
      state.update((st) => ({ ...st, settings, loadError: '' }))
      form.set(toConfig(settings))
    } catch (e) {
      if (s !== session) return
      state.update((st) => ({ ...st, settings: null, loadError: errorMessage(e) }))
    }
  }

  function open(id: string): Promise<void> {
    session++
    state.set({ repoID: id, settings: null, loadError: '', error: '' })
    form.set(emptyConfig())
    return load()
  }

  /** Validates and writes; true when it was saved. */
  async function save(): Promise<boolean> {
    const { repoID: id, settings } = get(state)
    if (!settings) return false
    const cfg = trimmed(get(form))
    const bad = flowSettingsError(cfg)
    if (bad) {
      errorFor = JSON.stringify(get(form))
      state.update((st) => ({ ...st, error: bad }))
      return false
    }
    const s = session
    errorFor = ''
    state.update((st) => ({ ...st, error: '' }))
    let ok = true
    try {
      await (settings.initialized ? d.save(id, cfg) : d.init(id, cfg))
    } catch (e) {
      ok = false
      if (s === session) {
        errorFor = JSON.stringify(get(form))
        state.update((st) => ({ ...st, error: errorMessage(e) }))
      }
    }
    if (ok && s === session) await load()
    try {
      await d.afterWrite(id)
    } catch (e) {
      // The write itself went through; do not reject save() over the reload.
      if (ok && s === session) state.update((st) => ({ ...st, error: `Saved, but reloading failed: ${errorMessage(e)}` }))
    }
    return ok
  }

  return { state, form, dirty, open, save }
}
