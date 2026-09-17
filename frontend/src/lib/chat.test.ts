import { describe, expect, it } from 'vitest'
import { applyEvent, emptyChat, errorText, fromMessages, shouldReloadChat, startRun, toolLabel } from './chat'
import type { AIMessage } from './types'

describe('fromMessages', () => {
  it('merges tool rounds into one assistant item', () => {
    const messages: AIMessage[] = [
      { role: 'user', content: 'branches?' },
      { role: 'assistant', content: '', toolCalls: [{ id: 'c1', name: 'list_refs', args: {} }] },
      { role: 'tool', toolName: 'list_refs', content: 'Current branch: main\nLocal branches: main' },
      { role: 'assistant', content: 'On main.' },
      { role: 'user', content: 'thanks' },
      { role: 'assistant', content: 'Wai', stopped: true },
    ]
    expect(fromMessages('r1', messages)).toEqual({
      repoID: 'r1',
      runID: null,
      items: [
        { role: 'user', text: 'branches?', tools: [] },
        { role: 'assistant', text: 'On main.', tools: [{ name: 'list_refs', args: {}, summary: 'Current branch: main' }] },
        { role: 'user', text: 'thanks', tools: [] },
        { role: 'assistant', text: 'Wai', tools: [], stopped: true },
      ],
    })
  })
})

describe('applyEvent', () => {
  const running = () => startRun(emptyChat('r1'), 'hola', 'run1')

  it('starts a run with a user and an empty assistant item', () => {
    expect(running()).toEqual({
      repoID: 'r1',
      runID: 'run1',
      items: [
        { role: 'user', text: 'hola', tools: [] },
        { role: 'assistant', text: '', tools: [] },
      ],
    })
  })

  it('applies tool, result, deltas and done', () => {
    let s = running()
    s = applyEvent(s, 'chat:tool', { repoID: 'r1', runID: 'run1', name: 'search_log', args: { text: 'NEXO-1' } })
    s = applyEvent(s, 'chat:tool_result', { repoID: 'r1', runID: 'run1', name: 'search_log', summary: 'a1b2c3d fix' })
    s = applyEvent(s, 'chat:delta', { repoID: 'r1', runID: 'run1', text: 'Found ' })
    s = applyEvent(s, 'chat:delta', { repoID: 'r1', runID: 'run1', text: 'one.' })
    s = applyEvent(s, 'chat:done', { repoID: 'r1', runID: 'run1' })
    expect(s.runID).toBeNull()
    expect(s.items[1]).toEqual({
      role: 'assistant',
      text: 'Found one.',
      tools: [{ name: 'search_log', args: { text: 'NEXO-1' }, summary: 'a1b2c3d fix' }],
    })
  })

  it('records errors and ends the run', () => {
    const s = applyEvent(running(), 'chat:error', { repoID: 'r1', runID: 'run1', message: 'down', code: 'ollama_down' })
    expect(s.runID).toBeNull()
    expect(s.items[1].error).toEqual({ message: 'down', code: 'ollama_down' })
  })

  it('ignores events from other repos or runs', () => {
    const s = running()
    expect(applyEvent(s, 'chat:delta', { repoID: 'r2', runID: 'run1', text: 'x' })).toBe(s)
    expect(applyEvent(s, 'chat:delta', { repoID: 'r1', runID: 'old', text: 'x' })).toBe(s)
    expect(applyEvent(emptyChat('r1'), 'chat:done', { repoID: 'r1', runID: 'run1' }).items).toEqual([])
  })
})

describe('shouldReloadChat', () => {
  it('is false when the repo id is unchanged, running or idle', () => {
    expect(shouldReloadChat(emptyChat('r1'), 'r1')).toBe(false)
    expect(shouldReloadChat(startRun(emptyChat('r1'), 'hola', 'run1'), 'r1')).toBe(false)
  })

  it('is true when the repo id changes', () => {
    expect(shouldReloadChat(emptyChat('r1'), 'r2')).toBe(true)
  })

  it('is true when the target repo id is empty', () => {
    expect(shouldReloadChat(emptyChat('r1'), '')).toBe(true)
  })
})

describe('labels', () => {
  it('shows the first non-empty argument', () => {
    expect(toolLabel({ name: 'search_log', args: { text: '', author: 'Ana' } })).toBe('search_log: Ana')
    expect(toolLabel({ name: 'list_refs', args: null })).toBe('list_refs')
  })

  it('explains known error codes', () => {
    expect(errorText({ code: 'no_tool_support', message: 'x' })).toContain('qwen2.5')
    expect(errorText({ code: 'other', message: 'boom' })).toBe('boom')
  })
})
