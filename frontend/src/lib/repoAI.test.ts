import { describe, expect, it } from 'vitest'
import { ACTIONS, COMMIT_MODES, REPLY_MODES, actionLabel, chooseProvider, globalLabel, globalModeLabel, instructionsToSave, mergeDrafts, settingsRepoID, providerModels, showsInstructionsStrip, stateText, usesRepoChatModel, withOverride } from './repoAI'
import type { AIStatus } from './types'

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
  it('shows the instructions strip only while approval is owed', () => {
    for (const state of ['pending', 'changed'] as const) expect(showsInstructionsStrip({ state })).toBe(true)
    for (const state of ['none', 'approved', 'ignored'] as const) expect(showsInstructionsStrip({ state })).toBe(false)
    expect(showsInstructionsStrip(null)).toBe(false)
  })
})

const status = (over: Partial<AIStatus['ollama']> = {}, providers: AIStatus['providers'] = []): AIStatus =>
  ({ ollama: { running: true, url: '', chatModel: '', models: [{ name: 'qwen2.5:7b' }], chatModelInstalled: true, ...over }, providers }) as AIStatus

describe('Global (…) options', () => {
  it('shows the global mode with the label the select uses', () => {
    expect(globalModeLabel(COMMIT_MODES, 'manual')).toBe('Global (Only when I ask)')
    expect(globalModeLabel(REPLY_MODES, 'auto-local')).toBe('Global (Automatic for local models)')
    expect(globalModeLabel(REPLY_MODES, undefined)).toBe('Global ()')
  })
  it('passes an unknown mode through', () => {
    expect(globalModeLabel(COMMIT_MODES, 'later')).toBe('Global (later)')
  })
})

describe('override round trip', () => {
  it('picks the first model of the chosen provider', () => {
    expect(chooseProvider('chat', 'anthropic', ['claude-opus-5', 'claude-sonnet-5'])).toEqual({ chatProvider: 'anthropic', chatModel: 'claude-opus-5' })
    expect(chooseProvider('task', 'ollama', ['a'])).toEqual({ taskProvider: 'ollama', taskModel: 'a' })
  })
  it('refuses a provider that lists no model', () => {
    expect(chooseProvider('chat', 'openai', [])).toBeNull()
  })
  it('goes back to Global with the model cleared', () => {
    const patch = chooseProvider('task', '', [])
    expect(patch).toEqual({ taskProvider: '', taskModel: '' })
    expect(withOverride({ taskProvider: 'ollama', taskModel: 'a', aiOff: true }, patch!)).toEqual({ taskProvider: '', taskModel: '', aiOff: true })
  })
  it('keeps the other fields when one is overridden', () => {
    const o = withOverride({ commitMessage: 'manual' }, chooseProvider('chat', 'ollama', ['m'])!)
    expect(o).toEqual({ commitMessage: 'manual', chatProvider: 'ollama', chatModel: 'm' })
  })
})

describe('providerModels', () => {
  it('lists the installed Ollama models', () => {
    expect(providerModels('ollama', status(), {})).toEqual({ models: ['qwen2.5:7b'], hint: '' })
  })
  it('explains an Ollama that is stopped or empty', () => {
    expect(providerModels('ollama', status({ running: false }), {}).hint).toBe('Ollama is not running')
    expect(providerModels('ollama', status({ models: [] }), {}).hint).toBe('No models installed in Ollama')
  })
  it('asks for a key before listing a hosted provider', () => {
    expect(providerModels('openai', status(), {})).toEqual({ models: [], hint: 'Add a key to see the models' })
  })
  it('lists a hosted provider that has a key, and reports a failed listing', () => {
    const s = status({}, [{ provider: 'openai', hasKey: true }] as AIStatus['providers'])
    expect(providerModels('openai', s, { openai: ['gpt-x'] })).toEqual({ models: ['gpt-x'], hint: '' })
    expect(providerModels('openai', s, {}, { openai: 'bad key' })).toEqual({ models: [], hint: 'bad key' })
  })
})

describe('mergeDrafts', () => {
  it('takes the saved texts', () => {
    expect(mergeDrafts({ all: 'a' }, { all: 'old', chat: 'x' }, '')).toEqual({ all: 'a' })
  })
  it('keeps the draft of the box being typed in', () => {
    expect(mergeDrafts({ all: 'saved', chat: 'c' }, { all: 'typing…' }, 'all')).toEqual({ all: 'typing…', chat: 'c' })
  })
  it('ignores a focused box without a draft', () => {
    expect(mergeDrafts(undefined, {}, 'chat')).toEqual({})
  })
})

describe('instructionsToSave', () => {
  it('returns the stored map with the box changed, keeping the other boxes', () => {
    expect(instructionsToSave('chat', { chat: 'new' }, { all: 'a', chat: 'old' })).toEqual({ all: 'a', chat: 'new' })
    expect(instructionsToSave('all', { all: 'typed' }, undefined)).toEqual({ all: 'typed' })
  })
  it('returns null when the text is what is already stored', () => {
    expect(instructionsToSave('chat', { chat: 'same' }, { chat: 'same' })).toBeNull()
    expect(instructionsToSave('chat', {}, undefined)).toBeNull()
  })
  it('returns the map with an empty text when a stored box was cleared', () => {
    expect(instructionsToSave('chat', { chat: '' }, { chat: 'old' })).toEqual({ chat: '' })
  })
})

describe('settingsRepoID', () => {
  it('is the repository itself for a main repository', () => {
    expect(settingsRepoID({ id: 'a' })).toBe('a')
  })
  it('is the main repository for a linked worktree, detected or added', () => {
    expect(settingsRepoID({ id: 'wt', parentId: 'main' })).toBe('main')
  })
  it('keeps a submodule on its own entry', () => {
    expect(settingsRepoID({ id: 'sub', parentId: 'main', submodule: true })).toBe('sub')
  })
})
