import type { AISettings, AIStatus, ProviderName } from './types'

export const PROVIDERS: { value: ProviderName; label: string }[] = [
  { value: 'ollama', label: 'Ollama (local)' },
  { value: 'openai', label: 'OpenAI' },
  { value: 'anthropic', label: 'Anthropic' },
]

/** Hosted providers need an API key; Ollama runs on the user's machine. */
export function needsKey(p: ProviderName): boolean {
  return p === 'openai' || p === 'anthropic'
}

/**
 * modelHint is what to show instead of the model dropdown: why it is empty,
 * or "" when the models can be listed.
 */
export function modelHint(p: ProviderName, status: AIStatus): string {
  if (!needsKey(p)) return ''
  if (status.keyStore) return status.keyStore
  const found = status.providers.find((s) => s.provider === p)
  if (found?.error) return found.error
  return found?.hasKey ? '' : 'Add a key to see the models'
}

/**
 * modelForProvider picks the model to select after a provider's model list
 * changes (on a provider switch, or once its models finish loading): the
 * current value when the new list still has it, otherwise the list's first
 * entry, or "" when the list is empty.
 */
export function modelForProvider(current: string, models: string[]): string {
  if (models.includes(current)) return current
  return models[0] ?? ''
}

/**
 * usesOllama reports whether either feature (chat/agent or the commit-
 * explain task) is set to the local Ollama provider. The Settings dialog's
 * Ollama model dropdown and Download button only make sense — and only
 * write to a field that provider actually reads — when this is true.
 */
export function usesOllama(chatProvider: ProviderName, taskProvider: ProviderName): boolean {
  return chatProvider === 'ollama' || taskProvider === 'ollama'
}

/**
 * settingsHaveModels reports whether both selected models are non-empty.
 * syncModels() sets a model to "" when its provider's list no longer
 * contains it (e.g. right after removing the key of the provider currently
 * selected); saving that would fail with "model names must not be empty"
 * for what the user experiences as a successful key removal.
 */
export function settingsHaveModels(settings: AISettings): boolean {
  return settings.chatModel !== '' && settings.taskModel !== ''
}

/**
 * processingNotice is the one-line footer describing where repository
 * content goes: local disk only when both features use Ollama and its URL
 * is local, otherwise a note that the selected hosted provider(s) receive
 * it.
 */
export function processingNotice(chatProvider: ProviderName, taskProvider: ProviderName, ollamaRemote: boolean): string {
  if (!ollamaRemote && chatProvider === 'ollama' && taskProvider === 'ollama') {
    return 'Everything is processed on this Mac.'
  }
  return 'The selected providers receive the repository content being asked about.'
}
