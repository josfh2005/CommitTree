import { get } from 'svelte/store'
import { beforeEach, describe, expect, it } from 'vitest'
import { modelDone, modelProgress, modelPull, pullPercent, startModelPull } from './modelPull'
import { EMPTY, taskState } from './tasks'

describe('model download as a status-bar task', () => {
  beforeEach(() => { taskState.set(EMPTY); modelPull.set(null) })

  it('is a task from before the backend call until model:done', async () => {
    await startModelPull('qwen', async () => { expect(get(taskState).tasks).toMatchObject([{ id: 'model', kind: 'model', label: 'Downloading qwen' }]) })
    modelProgress({ name: 'qwen', status: 'pulling', completed: 50, total: 200 })
    expect(get(taskState).tasks[0].progress).toBe('25 %')
    expect(get(modelPull)?.completed).toBe(50)
    modelDone({ name: 'qwen' })
    expect(get(modelPull)).toBeNull()
    expect(get(taskState).last).toMatchObject({ outcome: 'ok', text: 'Downloaded qwen' })
  })

  it('done before the call resolved leaves no task', async () => {
    await startModelPull('qwen', async () => { modelDone({ name: 'qwen', error: 'nope' }) })
    expect(get(taskState).tasks).toEqual([])
    expect(get(taskState).last?.outcome).toBe('failed')
  })

  it('a refused download ends failed and rethrows', async () => {
    await expect(startModelPull('qwen', async () => { throw new Error('ollama down') })).rejects.toThrow('ollama down')
    expect(get(taskState).tasks).toEqual([])
    expect(get(taskState).last).toMatchObject({ outcome: 'failed', message: 'ollama down' })
    expect(get(modelPull)).toBeNull()
  })

  it('cancelled', () => {
    taskState.set(EMPTY)
    void startModelPull('qwen', async () => {})
    modelDone({ name: 'qwen', error: 'canceled', canceled: true })
    expect(get(taskState).last?.outcome).toBe('cancelled')
  })

  it('pullPercent is empty until the size is known', () => {
    expect(pullPercent({ name: 'm', status: '', completed: 0, total: 0 })).toBe('')
    expect(pullPercent({ name: 'm', status: '', completed: 1, total: 3 })).toBe('33 %')
  })
})
