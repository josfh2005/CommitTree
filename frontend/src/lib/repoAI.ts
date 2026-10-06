import { providerShortLabel } from './providers'
import type { ProviderName, RepoAIInfo, RepoAIOverride, RepoAIState } from './types'

export const ACTIONS = ['chat', 'commit-message', 'resolve-conflicts', 'explain-commit', 'explain-lines', 'suggest-replies'] as const
export type Action = (typeof ACTIONS)[number]

const LABELS: Record<Action, string> = {
  chat: 'Chat',
  'commit-message': 'Commit message',
  'resolve-conflicts': 'Resolve conflicts',
  'explain-commit': 'Explain commit',
  'explain-lines': 'Explain lines',
  'suggest-replies': 'Suggested replies',
}

export const actionLabel = (a: string) => LABELS[a as Action] ?? a

export const globalLabel = (provider: ProviderName, model: string) => `Global (${model} · ${providerShortLabel(provider)})`

/** withOverride applies patch to o: a provider set back to Global ('')
 *  clears its model, and blank instruction texts are dropped. */
export function withOverride(o: RepoAIOverride, patch: Partial<RepoAIOverride>): RepoAIOverride {
  const next: RepoAIOverride = { ...o, ...patch }
  if (patch.chatProvider === '') next.chatModel = ''
  if (patch.taskProvider === '') next.taskModel = ''
  if (next.instructions) {
    next.instructions = Object.fromEntries(Object.entries(next.instructions).filter(([, v]) => v.trim() !== ''))
  }
  return next
}

const STATES: Record<RepoAIState, string> = {
  none: '',
  approved: 'Approved',
  pending: 'Not approved',
  ignored: 'Ignored',
  changed: 'Changed since you approved',
}
export const stateText = (s: RepoAIState) => STATES[s]

export const usesRepoChatModel = (info: Pick<RepoAIInfo, 'overrides'> | null) => !!info?.overrides.chatProvider
