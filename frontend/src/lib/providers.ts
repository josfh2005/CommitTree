import type { AIStatus, ProviderName } from './types'

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
