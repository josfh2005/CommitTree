import { describe, expect, it } from 'vitest'
import { modelHint, needsKey, PROVIDERS } from './providers'
import type { AIStatus } from './types'

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
