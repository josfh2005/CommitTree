import { modelHint, providerShortLabel } from './providers'
import type { AIStatus, ProviderName, RepoAIInfo, RepoAIOverride, RepoAIState } from './types'

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

/** showsInstructionsStrip: the chat shows its "review the repository's
 *  instructions" strip while shared instructions wait for an approval. */
export const showsInstructionsStrip = (info: Pick<RepoAIInfo, 'state'> | null) => info?.state === 'pending' || info?.state === 'changed'

export interface ModeOption { value: string; label: string }

// The same wording as Settings → AI models, minus its "(default)" suffix:
// next to a "Global (…)" option it would read as a second default.
export const COMMIT_MODES: ModeOption[] = [
  { value: 'auto-local', label: 'Automatic for local models' },
  { value: 'auto', label: 'Always automatic' },
  { value: 'manual', label: 'Only when I ask' },
]
export const REPLY_MODES: ModeOption[] = [
  { value: 'auto-local', label: 'Automatic for local models' },
  { value: 'auto', label: 'Always' },
  { value: 'off', label: 'Off' },
]

/** globalModeLabel is the "Global (…)" option of a mode select: the global
 *  value with the label the select itself uses for it. */
export const globalModeLabel = (modes: ModeOption[], value: string | undefined) =>
  `Global (${modes.find((m) => m.value === value)?.label ?? value ?? ''})`

/**
 * providerModels is what a provider's model select can offer: Ollama's
 * installed models, or a hosted provider's listed ones; hint says why the
 * list is empty (no key, Ollama stopped, listing failed), '' when it is not.
 * listed/listErrors hold what api.listModels returned or failed with.
 */
export function providerModels(
  p: ProviderName,
  status: AIStatus | null,
  listed: Partial<Record<ProviderName, string[]>>,
  listErrors: Partial<Record<ProviderName, string>> = {},
): { models: string[]; hint: string } {
  if (!status) return { models: listed[p] ?? [], hint: '' }
  if (p === 'ollama') {
    const models = status.ollama.models.map((m) => m.name)
    if (!status.ollama.running) return { models, hint: 'Ollama is not running' }
    return { models, hint: models.length ? '' : 'No models installed in Ollama' }
  }
  const hint = modelHint(p, status)
  if (hint) return { models: [], hint }
  return { models: listed[p] ?? [], hint: listErrors[p] ?? '' }
}

/**
 * chooseProvider is the patch for picking provider in one role's select:
 * Global ('') clears the model too, any other provider takes the first model
 * it lists. It is null when that provider lists none — a provider is only
 * ever saved together with a model.
 */
export function chooseProvider(role: 'chat' | 'task', provider: ProviderName | '', models: string[]): Partial<RepoAIOverride> | null {
  if (provider === '') return role === 'chat' ? { chatProvider: '', chatModel: '' } : { taskProvider: '', taskModel: '' }
  if (models.length === 0) return null
  return role === 'chat' ? { chatProvider: provider, chatModel: models[0] } : { taskProvider: provider, taskModel: models[0] }
}

/**
 * mergeDrafts is the text of the instruction boxes after a reload: what the
 * saved overrides say, except the box being typed in (focused), which keeps
 * its draft so a reload never wipes it.
 */
export function mergeDrafts(saved: Record<string, string> | undefined, current: Record<string, string>, focused: string): Record<string, string> {
  const next = { ...(saved ?? {}) }
  if (focused && focused in current) next[focused] = current[focused]
  return next
}
