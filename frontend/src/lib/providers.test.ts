import { describe, expect, it } from 'vitest'
import { modelForProvider, modelHint, needsKey, processingNotice, PROVIDERS, settingsHaveModels, usesOllama } from './providers'
import type { AISettings, AIStatus } from './types'

const status = (over: Partial<AIStatus> = {}): AIStatus => ({
  ollama: { running: true, url: '', chatModel: '', models: [], chatModelInstalled: true },
  providers: [
    { provider: 'openai', hasKey: false, keyHint: '' },
    { provider: 'anthropic', hasKey: true, keyHint: 'sk-…abcd' },
  ],
  ...over,
})

describe('providers', () => {
  it('lists the three providers, ollama first', () => {
    expect(PROVIDERS.map((p) => p.value)).toEqual(['ollama', 'openai', 'anthropic'])
  })

  it('knows which providers need a key', () => {
    expect(needsKey('ollama')).toBe(false)
    expect(needsKey('openai')).toBe(true)
  })

  it('asks for a key when the provider has none', () => {
    expect(modelHint('openai', status())).toBe('Add a key to see the models')
  })

  it('has no hint once the key is stored', () => {
    expect(modelHint('anthropic', status())).toBe('')
  })

  it('reports that keys cannot be saved at all', () => {
    expect(modelHint('openai', status({ keyStore: 'no usable secret store' }))).toBe('no usable secret store')
  })
})

describe('modelForProvider', () => {
  it('keeps the current model when the new list still has it', () => {
    expect(modelForProvider('gpt-4.1', ['gpt-4.1', 'gpt-4.1-mini'])).toBe('gpt-4.1')
  })

  it('falls back to the first model when the current one is gone', () => {
    expect(modelForProvider('qwen2.5:7b', ['gpt-4.1', 'gpt-4.1-mini'])).toBe('gpt-4.1')
  })

  it('falls back to "" when the list is empty', () => {
    expect(modelForProvider('qwen2.5:7b', [])).toBe('')
  })
})

describe('usesOllama', () => {
  it('is true when the chat provider is ollama', () => {
    expect(usesOllama('ollama', 'openai')).toBe(true)
  })

  it('is true when the task provider is ollama', () => {
    expect(usesOllama('anthropic', 'ollama')).toBe(true)
  })

  it('is false when neither feature uses ollama', () => {
    expect(usesOllama('openai', 'anthropic')).toBe(false)
  })
})

describe('settingsHaveModels', () => {
  const base = (over: Partial<AISettings> = {}): AISettings => ({
    ollamaURL: 'http://localhost:11434',
    chatProvider: 'anthropic',
    chatModel: 'claude-opus-5',
    taskProvider: 'anthropic',
    taskModel: 'claude-opus-5',
    ...over,
  })

  it('is true when both models are set', () => {
    expect(settingsHaveModels(base())).toBe(true)
  })

  it('is false once a model has been cleared, e.g. after a key removal', () => {
    expect(settingsHaveModels(base({ chatModel: '' }))).toBe(false)
    expect(settingsHaveModels(base({ taskModel: '' }))).toBe(false)
  })
})

describe('processingNotice', () => {
  it('says everything stays local when both features use a local ollama', () => {
    expect(processingNotice('ollama', 'ollama', false)).toBe('Everything is processed on this Mac.')
  })

  it('warns once a hosted provider is selected for either feature', () => {
    expect(processingNotice('openai', 'ollama', false)).toBe('The selected providers receive the repository content being asked about.')
    expect(processingNotice('ollama', 'anthropic', false)).toBe('The selected providers receive the repository content being asked about.')
  })

  it('warns when ollama itself is remote', () => {
    expect(processingNotice('ollama', 'ollama', true)).toBe('The selected providers receive the repository content being asked about.')
  })
})
