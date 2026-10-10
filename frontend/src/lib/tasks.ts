import { derived, get, writable } from 'svelte/store'
import { commandLine, outcomeText } from './cmdlog'
import { busy, repos, selectedRepoId } from './stores'
import type { CommandEntry, CommandOrigin } from './types'
import { errorMessage } from './ui'

/** What the status bar can show (docs/spec/13-status-bar.md). */
export type TaskKind = 'op' | 'clone' | 'fetch' | 'model' | 'git'
export type TaskOutcome = 'ok' | 'failed' | 'cancelled'

export interface Task {
  /** 'op:<n>', 'clone', 'model', 'fetch:<repoId>' or 'git:<entryId>'. */
  id: string
  kind: TaskKind
  origin: CommandOrigin
  label: string
  /** '' for a clone or a model download. */
  repoId: string
  repoName: string
  /** '' or e.g. 'Receiving 42 %'. */
  progress: string
  /** ms since epoch. */
  start: number
}

export interface TaskResult {
  kind: TaskKind
  outcome: TaskOutcome
  text: string
  /** Why it failed (the tooltip); '' otherwise. */
  message: string
  repoId: string
  repoName: string
  end: number
}

/** A git write that has started and not ended, from cmdlog:entry. */
export interface RunningWrite {
  id: number
  repoId: string
  origin: CommandOrigin
  args: string[]
  start: number
  /** True when an op or fetch of its repository was running as it started: it then never has a result of its own. */
  folded: boolean
}

export interface TaskState {
  tasks: Task[]
  writes: RunningWrite[]
  /** Ids of writes already ended, so a late running event can't bring one back. */
  ended: number[]
  last: TaskResult | null
}

export type CancelTarget = { kind: 'clone' } | { kind: 'model' } | { kind: 'command'; repoId: string; entryId: number }
export type EndTask = (error?: unknown) => void

export const EMPTY: TaskState = { tasks: [], writes: [], ended: [], last: null }
const ENDED_KEPT = 200
/** gitcmd.ErrCancelled's message. */
const CANCELLED_MESSAGE = 'git command cancelled'

/** Past tense of each busy label; a label missing here reads "<label> — done". */
const PAST: Record<string, string> = {
  'Adding remote…': 'Added remote',
  'Amending…': 'Amended',
  'Applying stash…': 'Applied stash',
  'Checking out…': 'Checked out',
  'Cherry-picking…': 'Cherry-picked',
  'Committing merge…': 'Committed merge',
  'Committing…': 'Committed',
  'Continuing…': 'Continued',
  'Creating branch…': 'Created branch',
  'Creating tag…': 'Created tag',
  'Deleting branch…': 'Deleted branch',
  'Deleting remote branch…': 'Deleted remote branch',
  'Deleting tag…': 'Deleted tag',
  'Discarding…': 'Discarded',
  'Dropping stash…': 'Dropped stash',
  'Fetching…': 'Fetched',
  'Initialising…': 'Initialised',
  'Merging…': 'Merged',
  'Popping stash…': 'Popped stash',
  'Pulling…': 'Pulled',
  'Pushing branches…': 'Pushed branches',
  'Pushing…': 'Pushed',
  'Rebasing…': 'Rebased',
  'Removing remote…': 'Removed remote',
  'Resetting…': 'Reset',
  'Resolving…': 'Resolved',
  'Restarting…': 'Restarted',
  'Restoring…': 'Restored',
  'Saving git-flow…': 'Saved git-flow',
  'Saving remote…': 'Saved remote',
  'Setting up git-flow…': 'Set up git-flow',
  'Skipping…': 'Skipped',
  'Staging…': 'Staged',
  'Starting…': 'Started',
  'Stashing…': 'Stashed',
  'Syncing URL…': 'Synced URL',
  'Taking ours…': 'Took ours',
  'Taking theirs…': 'Took theirs',
  'Unstaging…': 'Unstaged',
  'Updating…': 'Updated',
}

export function outcomeOf(error?: unknown): { outcome: TaskOutcome; message: string } {
  if (error === undefined || error === null) return { outcome: 'ok', message: '' }
  const message = errorMessage(error)
  return { outcome: message.includes(CANCELLED_MESSAGE) ? 'cancelled' : 'failed', message }
}

export function resultText(kind: TaskKind, label: string, outcome: TaskOutcome): string {
  if (kind === 'git') return label
  const base = label.replace(/…$/, '')
  if (outcome === 'failed') return `${base} failed`
  if (outcome === 'cancelled') return `${base} cancelled`
  if (kind === 'clone') return base.replace(/^Cloning /, 'Cloned ')
  if (kind === 'model') return base.replace(/^Downloading /, 'Downloaded ')
  return PAST[label] ?? `${base} — done`
}

export function addTask(s: TaskState, t: Task): TaskState {
  return { ...s, tasks: [...s.tasks.filter((x) => x.id !== t.id), t] }
}

export function updateTask(s: TaskState, id: string, patch: Partial<Pick<Task, 'progress' | 'label'>>): TaskState {
  if (!s.tasks.some((t) => t.id === id)) return s
  return { ...s, tasks: s.tasks.map((t) => (t.id === id ? { ...t, ...patch } : t)) }
}

/** endTask removes task id; a You/AI task becomes the last result. */
export function endTask(s: TaskState, id: string, outcome: TaskOutcome, message: string, now: number): TaskState {
  const t = s.tasks.find((x) => x.id === id)
  if (!t) return s
  const tasks = s.tasks.filter((x) => x.id !== id)
  if (t.origin === 'auto') return { ...s, tasks }
  return {
    ...s,
    tasks,
    last: { kind: t.kind, outcome, text: resultText(t.kind, t.label, outcome), message, repoId: t.repoId, repoName: t.repoName, end: now },
  }
}

/** An op or a fetch absorbs the git writes of its repository. */
const folds = (t: Task) => t.kind === 'op' || t.kind === 'fetch'
const foldedIn = (s: TaskState, repoId: string) => s.tasks.some((t) => folds(t) && t.repoId === repoId)

/** applyEntry follows one cmdlog:entry: a running write is tracked, its end
 *  drops it — and when it was shown on its own (a listed repository with no
 *  op or fetch around it, origin You/AI), becomes the last result. Reads
 *  and ends with no running event before them only mark the id ended. */
export function applyEntry(s: TaskState, e: CommandEntry, now: number, nameOf: (id: string) => string): TaskState {
  if (e.kind !== 'write') return s
  if (e.outcome === 'running') {
    if (s.ended.includes(e.id) || s.writes.some((w) => w.id === e.id)) return s
    return { ...s, writes: [...s.writes, { id: e.id, repoId: e.repoId, origin: e.origin, args: e.args, start: Date.parse(e.start), folded: foldedIn(s, e.repoId) }] }
  }
  const ended = [...s.ended, e.id].slice(-ENDED_KEPT)
  const w = s.writes.find((x) => x.id === e.id)
  if (!w) return { ...s, ended }
  const writes = s.writes.filter((x) => x.id !== e.id)
  if (w.repoId === '' || w.folded || foldedIn(s, w.repoId) || w.origin === 'auto') return { ...s, writes, ended }
  const outcome: TaskOutcome = e.outcome === 'ok' ? 'ok' : e.outcome === 'cancelled' ? 'cancelled' : 'failed'
  return {
    ...s,
    writes,
    ended,
    last: {
      kind: 'git', outcome, text: commandLine(w.args), message: outcome === 'failed' ? outcomeText(e) : '',
      repoId: w.repoId, repoName: nameOf(w.repoId), end: now,
    },
  }
}

const rank = (o: CommandOrigin) => (o === 'auto' ? 1 : 0)

/** Every running task, the one the bar shows first: You/AI before Auto, then
 *  the most recently started. */
export function visibleTasks(s: TaskState, nameOf: (id: string) => string): Task[] {
  const git: Task[] = s.writes
    .filter((w) => w.repoId !== '' && !foldedIn(s, w.repoId))
    .map((w) => ({
      id: `git:${w.id}`, kind: 'git', origin: w.origin, label: commandLine(w.args),
      repoId: w.repoId, repoName: nameOf(w.repoId), progress: '', start: w.start,
    }))
  return [...s.tasks, ...git].sort((a, b) => rank(a.origin) - rank(b.origin) || b.start - a.start)
}

/** What Cancel stops for t; null when nothing can be cancelled right now
 *  (an op whose repository runs no git write at this moment). */
export function cancelTarget(s: TaskState, t: Task): CancelTarget | null {
  if (t.kind === 'clone') return { kind: 'clone' }
  if (t.kind === 'model') return { kind: 'model' }
  if (t.kind === 'git') return { kind: 'command', repoId: t.repoId, entryId: Number(t.id.slice('git:'.length)) }
  if (t.repoId === '') return null
  const newest = s.writes
    .filter((w) => w.repoId === t.repoId)
    .sort((a, b) => b.start - a.start || b.id - a.id)[0]
  return newest ? { kind: 'command', repoId: t.repoId, entryId: newest.id } : null
}

export function relativeTime(ms: number): string {
  if (ms < 60_000) return 'just now'
  if (ms < 3_600_000) return `${Math.floor(ms / 60_000)} min ago`
  return `${Math.floor(ms / 3_600_000)} h ago`
}

// ---- The store ----

export const taskState = writable<TaskState>(EMPTY)

export function repoName(id: string): string {
  return get(repos).find((r) => r.id === id)?.name ?? ''
}

export const shownTasks = derived([taskState, repos], ([$s]) => visibleTasks($s, repoName))

export function hasTask(id: string): boolean {
  return get(taskState).tasks.some((t) => t.id === id)
}

/** beginTask registers t, started now, and returns its end: with no error
 *  it ends ok; with one, failed — or cancelled for a git cancel. Only the
 *  first call counts. */
export function beginTask(t: Omit<Task, 'start' | 'progress'> & { progress?: string }, now = Date.now()): EndTask {
  taskState.update((s) => addTask(s, { progress: '', ...t, start: now }))
  let done = false
  return (error) => {
    if (done) return
    done = true
    const { outcome, message } = outcomeOf(error)
    taskState.update((s) => endTask(s, t.id, outcome, message, Date.now()))
  }
}

let opSeq = 0

/** beginOp sets the toolbar's busy label and registers the operation as a
 *  You task of repoId (default: the repository selected now); its end
 *  clears the label and ends the task. */
export function beginOp(label: string, repoId: string = get(selectedRepoId)): EndTask {
  busy.set(label)
  const end = beginTask({ id: `op:${++opSeq}`, kind: 'op', origin: 'you', label, repoId, repoName: repoName(repoId) })
  return (error) => {
    busy.set('')
    end(error)
  }
}

export function finishTask(id: string, outcome: TaskOutcome, message = ''): void {
  taskState.update((s) => endTask(s, id, outcome, message, Date.now()))
}

export function setTaskProgress(id: string, progress: string): void {
  taskState.update((s) => updateTask(s, id, { progress }))
}

/** noteCommand is the app-wide cmdlog:entry listener. */
export function noteCommand(e: CommandEntry): void {
  taskState.update((s) => applyEntry(s, e, Date.now(), repoName))
}
