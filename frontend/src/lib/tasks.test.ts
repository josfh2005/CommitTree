import { get } from 'svelte/store'
import { beforeEach, describe, expect, it } from 'vitest'
import {
  EMPTY, addTask, applyEntry, beginOp, beginTask, cancelTarget, endTask, finishTask, outcomeOf, relativeTime,
  resultText, setTaskProgress, taskState, visibleTasks, type Task, type TaskState,
} from './tasks'
import { busy, repos, selectedRepoId } from './stores'
import type { CommandEntry, Repo } from './types'

const names: Record<string, string> = { a: 'alpha', b: 'beta' }
const nameOf = (id: string) => names[id] ?? ''
const task = (over: Partial<Task> = {}): Task => ({
  id: 'op:1', kind: 'op', origin: 'you', label: 'Pushing…', repoId: 'a', repoName: 'alpha', progress: '', start: 1000, ...over,
})
const entry = (over: Partial<CommandEntry> = {}): CommandEntry => ({
  id: 7, repo: '/x/a', repoId: 'a', args: ['commit', '-m', 'x'], origin: 'ai', kind: 'write',
  start: new Date(5000).toISOString(), durationMs: 0, exitCode: 0, outcome: 'running',
  outputTruncated: false, outputDropped: false, ...over,
} as CommandEntry)

describe('outcomeOf', () => {
  it('is ok without an error, cancelled for a git cancel, failed otherwise', () => {
    expect(outcomeOf()).toEqual({ outcome: 'ok', message: '' })
    expect(outcomeOf(null)).toEqual({ outcome: 'ok', message: '' })
    expect(outcomeOf(new Error('git command cancelled'))).toEqual({ outcome: 'cancelled', message: 'git command cancelled' })
    expect(outcomeOf('push rejected')).toEqual({ outcome: 'failed', message: 'push rejected' })
  })
})

describe('resultText', () => {
  it('uses the past-tense table for operations', () => {
    expect(resultText('op', 'Pushing…', 'ok')).toBe('Pushed')
    expect(resultText('op', 'Deleting branch…', 'ok')).toBe('Deleted branch')
    expect(resultText('op', 'Cherry-picking…', 'ok')).toBe('Cherry-picked')
    expect(resultText('op', 'Committing merge…', 'ok')).toBe('Committed merge')
    expect(resultText('op', 'Initialising git-flow…', 'ok')).toBe('Initialised git-flow')
  })
  it('falls back to "<label> — done" for a label the table lacks', () => {
    expect(resultText('op', 'Frobbing…', 'ok')).toBe('Frobbing — done')
  })
  it('says failed / cancelled after the label without its ellipsis', () => {
    expect(resultText('op', 'Pulling…', 'failed')).toBe('Pulling failed')
    expect(resultText('op', 'Pushing…', 'cancelled')).toBe('Pushing cancelled')
  })
  it('names clones, model downloads and git commands', () => {
    expect(resultText('clone', 'Cloning foo', 'ok')).toBe('Cloned foo')
    expect(resultText('clone', 'Cloning foo', 'failed')).toBe('Cloning foo failed')
    expect(resultText('model', 'Downloading qwen', 'ok')).toBe('Downloaded qwen')
    expect(resultText('git', "git commit -m x", 'failed')).toBe('git commit -m x')
  })
})

describe('endTask', () => {
  it('removes the task and records a You/AI result', () => {
    const s = endTask(addTask(EMPTY, task()), 'op:1', 'ok', '', 9000)
    expect(s.tasks).toEqual([])
    expect(s.last).toEqual({ kind: 'op', outcome: 'ok', text: 'Pushed', message: '', repoId: 'a', repoName: 'alpha', end: 9000 })
  })
  it('never lets an Auto task set the last result, even a failed one', () => {
    const before: TaskState = { ...EMPTY, last: { kind: 'op', outcome: 'ok', text: 'Pushed', message: '', repoId: 'a', repoName: 'alpha', end: 1 } }
    const s = endTask(addTask(before, task({ id: 'fetch:a', kind: 'fetch', origin: 'auto', label: 'Fetching' })), 'fetch:a', 'failed', 'boom', 9000)
    expect(s.tasks).toEqual([])
    expect(s.last).toBe(before.last)
  })
  it('ignores an unknown id', () => {
    expect(endTask(EMPTY, 'nope', 'ok', '', 1)).toBe(EMPTY)
  })
})

describe('applyEntry', () => {
  it('tracks a running write and drops it on its end', () => {
    const s1 = applyEntry(EMPTY, entry(), 6000, nameOf)
    expect(s1.writes).toEqual([{ id: 7, repoId: 'a', origin: 'ai', args: ['commit', '-m', 'x'], start: 5000, folded: false }])
    const s2 = applyEntry(s1, entry({ outcome: 'ok' }), 7000, nameOf)
    expect(s2.writes).toEqual([])
    expect(s2.last).toEqual({ kind: 'git', outcome: 'ok', text: 'git commit -m x', message: '', repoId: 'a', repoName: 'alpha', end: 7000 })
  })
  it('ignores reads', () => {
    expect(applyEntry(EMPTY, entry({ kind: 'read' }), 1, nameOf)).toBe(EMPTY)
  })
  it('ignores a running event that arrives after its end', () => {
    const s1 = applyEntry(EMPTY, entry({ outcome: 'ok' }), 1, nameOf)
    expect(s1.last).toBeNull()
    const s2 = applyEntry(s1, entry(), 2, nameOf)
    expect(s2.writes).toEqual([])
  })
  it('folds a write ending inside an op of the same repository (no result of its own)', () => {
    const withOp = addTask(EMPTY, task())
    const s = applyEntry(applyEntry(withOp, entry({ origin: 'you' }), 1, nameOf), entry({ origin: 'you', outcome: 'failed', exitCode: 1 }), 2, nameOf)
    expect(s.last).toBeNull()
  })
  it('a write started inside an op stays folded even if the op ends first', () => {
    const op = task()
    let s = applyEntry(addTask(EMPTY, op), entry({ origin: 'you' }), 1, nameOf)
    s = endTask(s, 'op:1', 'ok', '', 2)
    expect(s.last?.text).toBe('Pushed')
    const last = s.last
    s = applyEntry(s, entry({ origin: 'you', outcome: 'ok' }), 3, nameOf)
    expect(s.last).toBe(last)
    expect(s.writes).toEqual([])
  })
  it('does not record an Auto write as the last result', () => {
    const s = applyEntry(applyEntry(EMPTY, entry({ origin: 'auto' }), 1, nameOf), entry({ origin: 'auto', outcome: 'ok' }), 2, nameOf)
    expect(s.last).toBeNull()
  })
  it('maps timeout to failed and keeps the reason', () => {
    const s = applyEntry(applyEntry(EMPTY, entry(), 1, nameOf), entry({ outcome: 'timeout' }), 2, nameOf)
    expect(s.last?.outcome).toBe('failed')
    expect(s.last?.message).toBe('Timed out')
  })
})

describe('visibleTasks', () => {
  it('shows a bare write as a git task, folds it into an op or fetch of its repository', () => {
    const s = applyEntry(EMPTY, entry(), 1, nameOf)
    expect(visibleTasks(s, nameOf).map((t) => [t.id, t.kind, t.label, t.repoName])).toEqual([['git:7', 'git', 'git commit -m x', 'alpha']])
    const withFetch = addTask(s, task({ id: 'fetch:a', kind: 'fetch', origin: 'auto', label: 'Fetching' }))
    expect(visibleTasks(withFetch, nameOf).map((t) => t.id)).toEqual(['fetch:a'])
  })
  it('never shows a write outside a listed repository', () => {
    expect(visibleTasks(applyEntry(EMPTY, entry({ repoId: '' }), 1, nameOf), nameOf)).toEqual([])
  })
  it('puts You/AI before Auto, then the most recent first', () => {
    let s = addTask(EMPTY, task({ id: 'fetch:b', kind: 'fetch', origin: 'auto', repoId: 'b', start: 9000 }))
    s = addTask(s, task({ id: 'op:1', start: 1000 }))
    s = addTask(s, task({ id: 'clone', kind: 'clone', repoId: '', start: 2000 }))
    expect(visibleTasks(s, nameOf).map((t) => t.id)).toEqual(['clone', 'op:1', 'fetch:b'])
  })
})

describe('cancelTarget', () => {
  it('cancels a clone, a model download, or a git task by its entry', () => {
    expect(cancelTarget(EMPTY, task({ id: 'clone', kind: 'clone' }))).toEqual({ kind: 'clone' })
    expect(cancelTarget(EMPTY, task({ id: 'model', kind: 'model' }))).toEqual({ kind: 'model' })
    expect(cancelTarget(EMPTY, task({ id: 'git:7', kind: 'git' }))).toEqual({ kind: 'command', repoId: 'a', entryId: 7 })
  })
  it('cancels the newest running write of an op repository, or nothing', () => {
    expect(cancelTarget(EMPTY, task())).toBeNull()
    let s = applyEntry(EMPTY, entry({ id: 3, start: new Date(1000).toISOString() }), 1, nameOf)
    s = applyEntry(s, entry({ id: 4, start: new Date(2000).toISOString() }), 1, nameOf)
    s = applyEntry(s, entry({ id: 5, repoId: 'b', start: new Date(3000).toISOString() }), 1, nameOf)
    expect(cancelTarget(s, task())).toEqual({ kind: 'command', repoId: 'a', entryId: 4 })
  })
  it('cancels nothing for an op or fetch without a repository, whatever else runs', () => {
    const s = applyEntry(EMPTY, entry({ repoId: '' }), 1, nameOf)
    expect(cancelTarget(s, task({ repoId: '', repoName: '' }))).toBeNull()
    expect(cancelTarget(s, task({ id: 'fetch:', kind: 'fetch', origin: 'auto', repoId: '', repoName: '' }))).toBeNull()
  })
})

describe('relativeTime', () => {
  it('says just now, then minutes, then hours', () => {
    expect(relativeTime(59_999)).toBe('just now')
    expect(relativeTime(60_000)).toBe('1 min ago')
    expect(relativeTime(59 * 60_000)).toBe('59 min ago')
    expect(relativeTime(2 * 3_600_000 + 5)).toBe('2 h ago')
  })
})

describe('store helpers', () => {
  beforeEach(() => {
    taskState.set(EMPTY)
    busy.set('')
    repos.set([{ id: 'a', name: 'alpha' }, { id: 'b', name: 'beta' }] as Repo[])
    selectedRepoId.set('a')
  })

  it('beginOp sets the busy label and a task, its end clears both and records the result', () => {
    const end = beginOp('Pulling…', 'b')
    expect(get(busy)).toBe('Pulling…')
    expect(get(taskState).tasks).toMatchObject([{ kind: 'op', origin: 'you', label: 'Pulling…', repoId: 'b', repoName: 'beta' }])
    end(new Error('git command cancelled'))
    expect(get(busy)).toBe('')
    expect(get(taskState).tasks).toEqual([])
    expect(get(taskState).last).toMatchObject({ outcome: 'cancelled', text: 'Pulling cancelled', repoName: 'beta' })
  })

  it('beginOp defaults to the repository selected at its start, even if the selection changes', () => {
    const end = beginOp('Pushing…')
    selectedRepoId.set('b')
    end()
    expect(get(taskState).last).toMatchObject({ text: 'Pushed', repoId: 'a', repoName: 'alpha' })
  })

  it('an end called twice records once', () => {
    const end = beginTask({ id: 'model', kind: 'model', origin: 'you', label: 'Downloading m', repoId: '', repoName: '' })
    end()
    taskState.update((s) => ({ ...s, last: null }))
    end(new Error('late'))
    expect(get(taskState).last).toBeNull()
  })

  it('setTaskProgress and finishTask act by id; unknown ids are no-ops', () => {
    beginTask({ id: 'clone', kind: 'clone', origin: 'you', label: 'Cloning foo', repoId: '', repoName: '' })
    setTaskProgress('clone', 'Receiving 42 %')
    setTaskProgress('nope', 'x')
    expect(get(taskState).tasks[0].progress).toBe('Receiving 42 %')
    finishTask('clone', 'ok')
    expect(get(taskState).last?.text).toBe('Cloned foo')
  })
})
