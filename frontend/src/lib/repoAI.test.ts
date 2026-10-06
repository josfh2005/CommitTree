import { describe, expect, it } from 'vitest'
import { ACTIONS, actionLabel, globalLabel, stateText, usesRepoChatModel, withOverride } from './repoAI'

describe('repoAI helpers', () => {
  it('lists the six actions with labels', () => {
    expect(ACTIONS).toEqual(['chat', 'commit-message', 'resolve-conflicts', 'explain-commit', 'explain-lines', 'suggest-replies'])
    expect(actionLabel('commit-message')).toBe('Commit message')
  })
  it('labels the inherited value', () => {
    expect(globalLabel('ollama', 'qwen2.5:7b')).toBe('Global (qwen2.5:7b · Ollama)')
  })
  it('clears a model when its provider goes back to global', () => {
    const o = withOverride({ chatProvider: 'anthropic', chatModel: 'claude-opus-5' }, { chatProvider: '' })
    expect(o.chatProvider).toBe('')
    expect(o.chatModel).toBe('')
  })
  it('drops blank instructions', () => {
    const o = withOverride({}, { instructions: { all: '  ', chat: 'Be brief.' } })
    expect(o.instructions).toEqual({ chat: 'Be brief.' })
  })
  it('describes each approval state', () => {
    expect(stateText('pending')).toBe('Not approved')
    expect(stateText('changed')).toBe('Changed since you approved')
    expect(stateText('approved')).toBe('Approved')
    expect(stateText('ignored')).toBe('Ignored')
  })
  it('knows when the chat model is the repository’s', () => {
    expect(usesRepoChatModel({ overrides: { chatProvider: 'ollama', chatModel: 'x' } } as any)).toBe(true)
    expect(usesRepoChatModel({ overrides: {} } as any)).toBe(false)
    expect(usesRepoChatModel(null)).toBe(false)
  })
})
