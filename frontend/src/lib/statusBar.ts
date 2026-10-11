import { formatElapsed, NOT_RUNNING_MESSAGE } from './cmdlog'
import type { CancelTarget, Task, TaskKind, TaskOutcome, TaskResult } from './tasks'
import type { Repo } from './types'
import { errorMessage } from './ui'

/** Elapsed time once a task has run a full second; '' before. */
export function elapsedText(start: number, now: number): string {
  return now - start >= 1000 ? formatElapsed(now - start) : ''
}

export function outcomeIcon(o: TaskOutcome): string {
  return o === 'ok' ? '✓' : o === 'failed' ? '✗' : '⊘'
}

/** What the live region announces: the shown task (label and repository),
 *  or — when nothing runs, or only an Auto task does — the last result
 *  (icon and text). No progress, elapsed time or "N min ago": those would
 *  be re-announced as they tick. */
export function announcement(shown: Pick<Task, 'origin' | 'label' | 'repoName'> | null, last: Pick<TaskResult, 'outcome' | 'text' | 'repoName'> | null): string {
  if (shown && shown.origin !== 'auto') return shown.repoName ? `${shown.label} ${shown.repoName}` : shown.label
  if (last) return `${outcomeIcon(last.outcome)} ${last.text}${last.repoName ? ` ${last.repoName}` : ''}`
  return ''
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

export interface CancelDeps {
  cancelPull: () => Promise<void>
  cancelCommand: (repoId: string, entryId: number) => Promise<void>
  toast: (message: string, kind: 'error') => void
}

/** Asks the backend to stop a model download or a command; returns whether
 *  the "Cancelling…" state should stay (until the task ends). It does not
 *  when nothing was cancelled: the command had already ended on its own
 *  (silent) or the cancel failed (an error toast). */
export async function cancelTask(target: Exclude<CancelTarget, { kind: 'clone' }>, deps: CancelDeps): Promise<boolean> {
  try {
    if (target.kind === 'model') await deps.cancelPull()
    else await deps.cancelCommand(target.repoId, target.entryId)
    return true
  } catch (e) {
    const message = errorMessage(e)
    if (!message.includes(NOT_RUNNING_MESSAGE)) deps.toast(message, 'error')
    return false
  }
}
