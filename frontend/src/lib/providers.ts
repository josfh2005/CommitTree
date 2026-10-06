import type { AISettings, AIStatus, ProviderName } from './types'

export const PROVIDERS: { value: ProviderName; label: string }[] = [
  { value: 'ollama', label: 'Ollama (local)' },
  { value: 'openai', label: 'OpenAI' },
  { value: 'anthropic', label: 'Anthropic' },
]

/** providerShortLabel is the provider's name without qualifiers, for tight
 *  spots like the line under a chat answer; unknown values pass through. */
export function providerShortLabel(p: string): string {
  return p === 'ollama' ? 'Ollama' : (PROVIDERS.find((x) => x.value === p)?.label ?? p)
}

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

export type ChatBlocker = { kind: 'ollama_down' } | { kind: 'model_missing'; model: string } | { kind: 'no_key'; message: string }

/**
 * chatBlocker is why the chat panel can't take a message with chatProvider
 * as it stands, or null when it can. Ollama needs to be running with the
 * chat model installed; a hosted provider only needs its key — Ollama's
 * state doesn't matter to it.
 */
export function chatBlocker(chatProvider: ProviderName, status: AIStatus, chatModel?: string): ChatBlocker | null {
  if (!needsKey(chatProvider)) {
    if (!status.ollama.running) return { kind: 'ollama_down' }
    if (chatModel === undefined) {
      if (!status.ollama.chatModelInstalled) return { kind: 'model_missing', model: status.ollama.chatModel }
      return null
    }
    const installed = status.ollama.models.some((m) => m.name === chatModel || m.name === chatModel + ':latest')
    return installed ? null : { kind: 'model_missing', model: chatModel }
  }
  const found = status.providers.find((s) => s.provider === chatProvider)
  if (found?.hasKey) return null
  return { kind: 'no_key', message: found?.error || `Add an API key for ${providerShortLabel(chatProvider)} in Settings.` }
}

export interface ChatModelOption {
  provider: ProviderName
  label: string
  models: string[]
}

/**
 * chatModelOptions is what the chat's model picker offers: Ollama's
 * installed models when it is running, and each hosted provider that has a
 * key with the models listed for it (hostedModels, fetched by the caller).
 * A provider with nothing to pick is left out.
 */
export function chatModelOptions(status: AIStatus, hostedModels: Partial<Record<ProviderName, string[]>>): ChatModelOption[] {
  const out: ChatModelOption[] = []
  for (const { value } of PROVIDERS) {
    let models: string[] = []
    if (!needsKey(value)) {
      if (status.ollama.running) models = status.ollama.models.map((m) => m.name)
    } else if (status.providers.find((p) => p.provider === value)?.hasKey) {
      models = hostedModels[value] ?? []
    }
    if (models.length > 0) out.push({ provider: value, label: providerShortLabel(value), models })
  }
  return out
}
