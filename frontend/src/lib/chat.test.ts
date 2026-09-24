import { describe, expect, it } from 'vitest'
import { applyEvent, CHAT_EVENTS, confirmResultText, confirmState, emptyChat, errorText, fromMessages, shouldReloadChat, startRun, toolLabel, withConfirmDecision, withPendingConfirm, type ChatState } from './chat'
import type { AIMessage, ChatConfirmEvent } from './types'

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

  it('starts a run from a chat:start event (explain from the log)', () => {
    const idle = emptyChat('r1')
    const started = applyEvent(idle, 'chat:start', { repoID: 'r1', runID: 'run9', text: 'Explain commit a1b2c3d' })

    expect(started.runID).toBe('run9')
    expect(started.items).toEqual([
      { role: 'user', text: 'Explain commit a1b2c3d', tools: [] },
      { role: 'assistant', text: '', tools: [] },
    ])
    // The panel already echoed its own message, so its start event is a no-op.
    const running_ = running()
    expect(applyEvent(running_, 'chat:start', { repoID: 'r1', runID: 'run1', text: 'hola' })).toBe(running_)
    // A start for another repo is ignored.
    expect(applyEvent(idle, 'chat:start', { repoID: 'r2', runID: 'run9', text: 'x' })).toBe(idle)
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

describe('chat:notice', () => {
  it('is in CHAT_EVENTS, so the panel subscribes to it', () => {
    expect(CHAT_EVENTS).toContain('chat:notice')
  })

  it('appends the text to the running assistant item', () => {
    let s = startRun(emptyChat('r1'), 'hola', 'run1')
    s = applyEvent(s, 'chat:notice', { repoID: 'r1', runID: 'run1', text: 'The model wrote a tool call as text; CommitTree ran it.' })
    expect(s.items[1].notices).toEqual(['The model wrote a tool call as text; CommitTree ran it.'])
  })

  it('ignores a notice for another runID', () => {
    const s = startRun(emptyChat('r1'), 'hola', 'run1')
    const next = applyEvent(s, 'chat:notice', { repoID: 'r1', runID: 'old', text: 'ignored' })
    expect(next).toBe(s)
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

describe('CHAT_EVENTS', () => {
  // The panel subscribes to CHAT_EVENTS and ignores anything else, so an
  // event applyEvent handles but the list omits is dropped on the floor.
  // That is how "Explain in chat" lost its answer: chat:start was missing.
  it('carries a run started elsewhere, from start to answer', () => {
    const deliver = (state: ChatState, name: string, payload: Parameters<typeof applyEvent>[2]) =>
      (CHAT_EVENTS as readonly string[]).includes(name) ? applyEvent(state, name, payload) : state

    let state = emptyChat('r1')
    state = deliver(state, 'chat:start', { repoID: 'r1', runID: 'exp1', text: 'Explain commit abc1234: Fix login' })
    state = deliver(state, 'chat:delta', { repoID: 'r1', runID: 'exp1', text: 'It repairs the session cookie.' })
    state = deliver(state, 'chat:done', { repoID: 'r1', runID: 'exp1' })

    expect(state.items).toEqual([
      { role: 'user', text: 'Explain commit abc1234: Fix login', tools: [] },
      { role: 'assistant', text: 'It repairs the session cookie.', tools: [] },
    ])
    expect(state.runID).toBeNull()
  })
})

describe('write confirmations', () => {
  const confirm = (over: Partial<ChatConfirmEvent> = {}): ChatConfirmEvent => ({
    repoID: 'r', runID: 'run', confirmID: 'c1', tool: 'push', title: 'Push main to origin/main', details: ['a1b2c3d x'], ...over,
  })
  const running = () => {
    let s = startRun(emptyChat('r'), 'push it', 'run')
    s = applyEvent(s, 'chat:tool', { repoID: 'r', runID: 'run', name: 'push', args: {} })
    return s
  }

  it('attaches a pending card to the waiting tool', () => {
    const s = applyEvent(running(), 'chat:confirm', confirm())
    const tool = s.items[s.items.length - 1].tools[0]
    expect(tool.confirm).toEqual({ id: 'c1', title: 'Push main to origin/main', details: ['a1b2c3d x'], state: 'pending' })
  })

  it('resolves the card from the tool result', () => {
    let s = applyEvent(running(), 'chat:confirm', confirm())
    s = applyEvent(s, 'chat:tool_result', { repoID: 'r', runID: 'run', name: 'push', summary: 'rejected by the user' })
    expect(s.items[s.items.length - 1].tools[0].confirm?.state).toBe('rejected')
  })

  it('maps summaries to card states', () => {
    expect(confirmState('done: Push main to origin/main')).toBe('done')
    expect(confirmState('rejected by the user')).toBe('rejected')
    expect(confirmState('error: another operation is running')).toBe('failed')
  })

  it('restores a pending card into a reloaded conversation', () => {
    const reloaded = { repoID: 'r', runID: null, items: [{ role: 'user' as const, text: 'push it', tools: [] }] }
    const s = withPendingConfirm(reloaded, confirm())
    expect(s.runID).toBe('run')
    const last = s.items[s.items.length - 1]
    expect(last.role).toBe('assistant')
    expect(last.tools[0]).toMatchObject({ name: 'push', confirm: { id: 'c1', state: 'pending' } })
    expect(withPendingConfirm(s, confirm())).toEqual(s)
  })

  it('ignores a confirmation for another repository', () => {
    const s = running()
    expect(withPendingConfirm(s, confirm({ repoID: 'other' }))).toBe(s)
  })

  it('subscribes to chat:confirm', () => {
    expect(CHAT_EVENTS).toContain('chat:confirm')
  })

  it('attaches a card from chat:confirm even with no in-flight run (panel re-mounted)', () => {
    // fromMessages always sets runID null; a chat:confirm that arrives after
    // reload must still show the card and adopt the run, or the run hangs
    // with no card and no Stop (I1).
    const reloaded = { repoID: 'r', runID: null, items: [{ role: 'user' as const, text: 'push it', tools: [] }] }
    const s = applyEvent(reloaded, 'chat:confirm', confirm())
    expect(s.runID).toBe('run')
    const last = s.items[s.items.length - 1]
    expect(last.tools[0]).toMatchObject({ name: 'push', confirm: { id: 'c1', state: 'pending' } })
  })

  it('marks the card approved/rejecting immediately, and chat:tool_result still resolves it', () => {
    let s = applyEvent(running(), 'chat:confirm', confirm())
    s = withConfirmDecision(s, 'c1', 'approved')
    expect(s.items[s.items.length - 1].tools[0].confirm?.state).toBe('approved')
    s = applyEvent(s, 'chat:tool_result', { repoID: 'r', runID: 'run', name: 'push', summary: 'done: Push main to origin/main' })
    expect(s.items[s.items.length - 1].tools[0].confirm?.state).toBe('done')
  })

  it('reverts to pending when the confirm call itself failed', () => {
    let s = applyEvent(running(), 'chat:confirm', confirm())
    s = withConfirmDecision(s, 'c1', 'rejecting')
    s = withConfirmDecision(s, 'c1', 'pending')
    expect(s.items[s.items.length - 1].tools[0].confirm?.state).toBe('pending')
  })

  it('formats the decided outcome once, without repeating the tool summary prefix', () => {
    const doneTool = { name: 'push', args: null, summary: 'done: Push main to origin/main', confirm: { id: 'c1', title: 't', details: [], state: 'done' as const } }
    expect(confirmResultText(doneTool)).toBe('Approved and done · Push main to origin/main')

    const rejectedTool = { name: 'push', args: null, summary: 'rejected by the user', confirm: { id: 'c1', title: 't', details: [], state: 'rejected' as const } }
    expect(confirmResultText(rejectedTool)).toBe('Rejected')

    const failedTool = { name: 'push', args: null, summary: 'error: the repository changed', confirm: { id: 'c1', title: 't', details: [], state: 'failed' as const } }
    expect(confirmResultText(failedTool)).toBe('Failed: the repository changed')
  })
})
