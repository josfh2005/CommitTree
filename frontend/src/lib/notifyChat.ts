import { choiceState } from './chat'
import { firstLine, formatSeconds, type NotifyEvent } from './notifyRules'
import type { ChatConfirmEvent, ChatErrorEvent, ChatStartEvent, ChatToolResultEvent } from './types'

/** The chat runs being watched for notifications, by run id. A run that
 *  showed a decision card does not also notify that it finished. */
export interface ChatWatch {
  runs: Record<string, { repoID: string; start: number; decided: boolean }>
}

export const emptyWatch = (): ChatWatch => ({ runs: {} })

export const CHAT_WATCH_EVENTS = ['chat:start', 'chat:confirm', 'chat:tool_result', 'chat:done', 'chat:error'] as const

type Run = { repoID: string; runID: string }

export function watchChat(w: ChatWatch, name: string, payload: unknown, now: number): { watch: ChatWatch; event: NotifyEvent | null } {
  const p = payload as Run
  const without = () => {
    const runs = { ...w.runs }
    delete runs[p.runID]
    return { runs }
  }
  switch (name) {
    case 'chat:start': {
      const s = payload as ChatStartEvent
      return { watch: { runs: { ...w.runs, [s.runID]: { repoID: s.repoID, start: now, decided: false } } }, event: null }
    }
    case 'chat:confirm': {
      const c = payload as ChatConfirmEvent
      return { watch: w, event: { category: 'ai', repoID: c.repoID, target: 'chat', body: `The AI is waiting for you to confirm: ${c.title}` } }
    }
    case 'chat:tool_result': {
      // Only a card the tool accepted is on screen; a refused one is just
      // a failed call the model will retry.
      const t = payload as ChatToolResultEvent
      if (t.name !== 'propose_options' || choiceState(t) !== 'pending') return { watch: w, event: null }
      const run = w.runs[t.runID]
      const watch = run ? { runs: { ...w.runs, [t.runID]: { ...run, decided: true } } } : w
      return { watch, event: { category: 'ai', repoID: t.repoID, target: 'chat', body: 'The AI has a decision for you' } }
    }
    case 'chat:done': {
      const run = w.runs[p.runID]
      if (!run || run.decided) return { watch: without(), event: null }
      const ms = now - run.start
      return { watch: without(), event: { category: 'done', repoID: run.repoID, target: 'chat', body: `Chat answer finished · ${formatSeconds(ms)}`, durationMs: ms } }
    }
    case 'chat:error': {
      const e = payload as ChatErrorEvent
      return { watch: without(), event: { category: 'problem', repoID: e.repoID, target: 'chat', body: `Chat answer failed: ${firstLine(e.message)}` } }
    }
  }
  return { watch: w, event: null }
}
