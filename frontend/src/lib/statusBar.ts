import { formatElapsed } from './cmdlog'
import type { TaskKind, TaskOutcome } from './tasks'
import type { Repo } from './types'

/** Elapsed time once a task has run a full second; '' before. */
export function elapsedText(start: number, now: number): string {
  return now - start >= 1000 ? formatElapsed(now - start) : ''
}

export function outcomeIcon(o: TaskOutcome): string {
  return o === 'ok' ? '✓' : o === 'failed' ? '✗' : '⊘'
}

export type OpenAction = { kind: 'clone' } | { kind: 'providers' } | { kind: 'commands'; repoId: string }

/** Where a click on a task or the last result leads; null when nowhere.
 *  cloneRunning: whether this is the clone still in progress. */
export function openAction(t: { kind: TaskKind; repoId: string }, cloneRunning: boolean, list: Repo[]): OpenAction | null {
  if (t.kind === 'clone') return cloneRunning ? { kind: 'clone' } : null
  if (t.kind === 'model') return { kind: 'providers' }
  const repo = list.find((r) => r.id === t.repoId)
  return repo && !repo.missing ? { kind: 'commands', repoId: repo.id } : null
}
