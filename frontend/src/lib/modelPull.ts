import { writable } from 'svelte/store'
import { beginTask, finishTask, setTaskProgress } from './tasks'
import type { ModelDone, ModelProgress } from './types'
import { errorMessage } from './ui'

/** The Ollama download in progress, for Settings → Providers and the
 *  status bar; null when none. */
export const modelPull = writable<ModelProgress | null>(null)

export function pullPercent(p: ModelProgress): string {
  return p.total > 0 ? `${Math.floor((p.completed / p.total) * 100)} %` : ''
}

/** startModelPull registers the download before asking the backend, so a
 *  model:done that beats the call's own return still ends it. */
export async function startModelPull(name: string, pull: (name: string) => Promise<void>): Promise<void> {
  modelPull.set({ name, status: 'starting', completed: 0, total: 0 })
  beginTask({ id: 'model', kind: 'model', origin: 'you', label: `Downloading ${name}`, repoId: '', repoName: '' })
  try {
    await pull(name)
  } catch (e) {
    modelPull.set(null)
    finishTask('model', 'failed', errorMessage(e))
    throw e
  }
}

export function modelProgress(p: ModelProgress): void {
  modelPull.set(p)
  setTaskProgress('model', pullPercent(p))
}

export function modelDone(p: ModelDone): void {
  modelPull.set(null)
  finishTask('model', p.canceled ? 'cancelled' : p.error ? 'failed' : 'ok', p.error ?? '')
}
