import { describe, expect, it } from 'vitest'
import { answeredBy, appliedText, pendingCards, trayIndexAfter, type PendingCard, conversationTokens, formatTokens, tokensText, tokensTitle, applyEvent, nextChatRunRepo, parts, isWriteTool, CHAT_EVENTS, confirmResultText, confirmState, emptyChat, errorText, fromMessages, shouldReloadChat, startRun, toolLabel, withConfirmDecision, withPendingConfirm, type ChatState } from './chat'
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
    expect(fromMessages('r1', messages)).toMatchObject({
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

  it('keeps which provider and model produced each answer', () => {
    const messages: AIMessage[] = [
      { role: 'user', content: 'old' },
      { role: 'assistant', content: 'stored before it was recorded' },
      { role: 'user', content: 'branches?' },
      { role: 'assistant', content: '', toolCalls: [{ id: 'c1', name: 'list_refs', args: {} }], provider: 'anthropic', model: 'claude-opus-5' },
      { role: 'tool', toolName: 'list_refs', content: 'main' },
      { role: 'assistant', content: 'On main.', provider: 'anthropic', model: 'claude-opus-5' },
    ]
    const items = fromMessages('r1', messages).items
    expect(items[1]).toEqual({ role: 'assistant', text: 'stored before it was recorded', tools: [] })
    expect(items[3]).toMatchObject({ provider: 'anthropic', model: 'claude-opus-5', text: 'On main.' })
  })

  it('keeps when each answer finished', () => {
    const items = fromMessages('r1', [
      { role: 'user', content: 'q' },
      { role: 'assistant', content: 'old answer' },
      { role: 'user', content: 'q2' },
      { role: 'assistant', content: 'new answer', at: '2026-09-25T10:00:00Z' },
    ]).items
    expect(items[1].at).toBeUndefined()
    expect(items[3].at).toBe('2026-09-25T10:00:00Z')
  })
})

describe('applyEvent', () => {
  const running = () => startRun(emptyChat('r1'), 'hola', 'run1')

  it('starts a run with a user and an empty assistant item', () => {
    expect(running()).toEqual({
      repoID: 'r1',
      runID: 'run1',
      lastRunID: null,
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
    expect(s.items[1]).toMatchObject({
      role: 'assistant',
      text: 'Found one.',
      tools: [{ name: 'search_log', args: { text: 'NEXO-1' }, summary: 'a1b2c3d fix' }],
    })
  })

  it('stamps the answer with the time chat:done carries', () => {
    const s = applyEvent(running(), 'chat:done', { repoID: 'r1', runID: 'run1', at: '2026-09-25T10:00:00Z' })
    expect(s.items[1].at).toBe('2026-09-25T10:00:00Z')
  })

  it('starts a run from a chat:start event (explain from the log)', () => {
    const idle = emptyChat('r1')
    const started = applyEvent(idle, 'chat:start', { repoID: 'r1', runID: 'run9', text: 'Explain commit a1b2c3d', provider: 'ollama', model: 'qwen2.5:7b' })

    expect(started.runID).toBe('run9')
    expect(started.items).toEqual([
      { role: 'user', text: 'Explain commit a1b2c3d', tools: [] },
      { role: 'assistant', text: '', tools: [], provider: 'ollama', model: 'qwen2.5:7b' },
    ])
    // The panel already echoed its own message, so its start event only
    // says what is answering.
    const withModel = applyEvent(running(), 'chat:start', { repoID: 'r1', runID: 'run1', text: 'hola', provider: 'openai', model: 'gpt-5' })
    expect(withModel.items).toEqual([
      { role: 'user', text: 'hola', tools: [] },
      { role: 'assistant', text: '', tools: [], provider: 'openai', model: 'gpt-5' },
    ])
    expect(applyEvent(withModel, 'chat:start', { repoID: 'r1', runID: 'run1', text: 'hola', provider: 'openai', model: 'gpt-5' })).toBe(withModel)
    // A start for another repo is ignored.
    expect(applyEvent(idle, 'chat:start', { repoID: 'r2', runID: 'run9', text: 'x', provider: 'ollama', model: 'm' })).toBe(idle)
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
    expect(s.items[1].notices).toMatchObject([{ text: 'The model wrote a tool call as text; CommitTree ran it.', at: 0 }])
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
  it('names what answered, with a short provider name', () => {
    expect(answeredBy({ provider: 'ollama', model: 'qwen2.5:7b' })).toBe('Ollama · qwen2.5:7b')
    expect(answeredBy({ provider: 'anthropic', model: 'claude-opus-5' })).toBe('Anthropic · claude-opus-5')
    expect(answeredBy({ provider: 'openai', model: 'gpt-5' })).toBe('OpenAI · gpt-5')
    expect(answeredBy({})).toBe('')
  })

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
    state = deliver(state, 'chat:start', { repoID: 'r1', runID: 'exp1', text: 'Explain commit abc1234: Fix login', provider: 'ollama', model: 'm' })
    state = deliver(state, 'chat:delta', { repoID: 'r1', runID: 'exp1', text: 'It repairs the session cookie.' })
    state = deliver(state, 'chat:done', { repoID: 'r1', runID: 'exp1' })

    expect(state.items).toEqual([
      { role: 'user', text: 'Explain commit abc1234: Fix login', tools: [] },
      { role: 'assistant', text: 'It repairs the session cookie.', tools: [], provider: 'ollama', model: 'm' },
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

describe('suggested replies', () => {
  const answered = () => applyEvent(startRun(emptyChat('r1'), 'hola', 'run1'), 'chat:done', { repoID: 'r1', runID: 'run1' })
  const offer = (s: ChatState, runID = 'run1', repoID = 'r1') => applyEvent(s, 'chat:suggestions', { repoID, runID, replies: ['ok dale', 'sí'] })

  it('keeps them for the answer that just finished', () => {
    expect(offer(answered()).suggestions).toEqual(['ok dale', 'sí'])
  })

  it('drops them for another run, another repo or while an answer runs', () => {
    expect(offer(answered(), 'old').suggestions ?? []).toEqual([])
    expect(offer(answered(), 'run1', 'r2').suggestions ?? []).toEqual([])
    expect(offer(startRun(emptyChat('r1'), 'hola', 'run1')).suggestions ?? []).toEqual([])
  })

  it('clears them when an answer starts, here or from the log', () => {
    const s = offer(answered())
    expect(startRun(s, 'ok dale', 'run2').suggestions ?? []).toEqual([])
    expect(applyEvent(s, 'chat:start', { repoID: 'r1', runID: 'exp', text: 'Explain', provider: 'ollama', model: 'm' }).suggestions ?? []).toEqual([])
  })

  it('drops late ones for the previous answer after a message the backend refused', () => {
    let s = startRun(answered(), 'otra', 'run2')
    s = applyEvent(s, 'chat:error', { repoID: 'r1', runID: 'run2', message: 'add an API key', code: 'other' })
    expect(offer(s).suggestions ?? []).toEqual([])
  })

  it('is an event the panel listens to', () => {
    expect(CHAT_EVENTS).toContain('chat:suggestions')
  })
})

describe('isWriteTool', () => {
  it('knows the chat write tools, mirroring writetools.Specs in Go', () => {
    for (const name of ['stage_files', 'unstage_files', 'commit', 'create_branch', 'checkout_branch', 'stash_push', 'fetch', 'push', 'pull', 'merge_branch', 'cherry_pick']) {
      expect(isWriteTool(name), name).toBe(true)
    }
    for (const name of ['search_log', 'working_tree_status', 'diff_working_file', 'blame_file']) {
      expect(isWriteTool(name), name).toBe(false)
    }
  })
})

describe('appliedText', () => {
  it('shows exactly what resolve_hunk wrote', () => {
    expect(appliedText({ name: 'resolve_hunk', args: { path: 'a.html', hunk: 0, resolved: '<menu></menu>\n<form></form>\n' } })).toBe('<menu></menu>\n<form></form>')
  })
  it('says when the region was removed', () => {
    expect(appliedText({ name: 'resolve_hunk', args: { path: 'a.html', hunk: 0, resolved: '' } })).toBe('(region removed: nothing written in its place)')
  })
  it('is null for other tools', () => {
    expect(appliedText({ name: 'read_conflict', args: { path: 'a.html', hunk: 0 } })).toBeNull()
  })
})

describe('parts', () => {
  const kinds = (ps: ReturnType<typeof parts>) => ps.map((p) => (p.kind === 'text' ? `text:${p.text.trim()}` : p.kind === 'tool' ? `tool:${p.tool.name}` : 'notice'))

  it('puts each call where it happened in a live answer', () => {
    let s = startRun(emptyChat('r1'), 'resolve', 'run1')
    s = applyEvent(s, 'chat:delta', { repoID: 'r1', runID: 'run1', text: 'Reading a.' })
    s = applyEvent(s, 'chat:tool', { repoID: 'r1', runID: 'run1', name: 'read_conflict', args: { path: 'a' } })
    s = applyEvent(s, 'chat:delta', { repoID: 'r1', runID: 'run1', text: 'Now b.' })
    s = applyEvent(s, 'chat:notice', { repoID: 'r1', runID: 'run1', text: 'carry on' })
    s = applyEvent(s, 'chat:tool', { repoID: 'r1', runID: 'run1', name: 'read_conflict', args: { path: 'b' } })
    expect(kinds(parts(s.items[1]))).toEqual(['text:Reading a.', 'tool:read_conflict', 'text:Now b.', 'notice', 'tool:read_conflict'])
  })

  it('keeps the order when loaded from history, and shows a nudge as a notice', () => {
    const s = fromMessages('r1', [
      { role: 'user', content: 'resolve' },
      { role: 'assistant', content: 'Reading a.', toolCalls: [{ id: '1', name: 'read_conflict', args: { path: 'a' } }] },
      { role: 'tool', toolName: 'read_conflict', content: 'a, conflict 0 of 1' },
      { role: 'assistant', content: 'Next I will do b.' },
      { role: 'user', content: 'CommitTree: you stopped, but git still reports…' },
      { role: 'assistant', content: '', toolCalls: [{ id: '2', name: 'read_conflict', args: { path: 'b' } }] },
      { role: 'tool', toolName: 'read_conflict', content: 'b, conflict 0 of 1' },
      { role: 'assistant', content: 'Done.' },
    ] as AIMessage[])
    expect(s.items).toHaveLength(2)
    expect(kinds(parts(s.items[1]))).toEqual(['text:Reading a.', 'tool:read_conflict', 'text:Next I will do b.', 'notice', 'tool:read_conflict', 'text:Done.'])
  })
})

describe('nextChatRunRepo', () => {
  it('follows a run from start to its end, whether or not the panel is open', () => {
    let repo = ''
    repo = nextChatRunRepo(repo, 'chat:start', { repoID: 'r1' })
    expect(repo).toBe('r1')
    repo = nextChatRunRepo(repo, 'chat:delta', { repoID: 'r1' })
    expect(repo).toBe('r1')
    expect(nextChatRunRepo(repo, 'chat:done', { repoID: 'r1' })).toBe('')
    expect(nextChatRunRepo(repo, 'chat:error', { repoID: 'r1' })).toBe('')
  })
  it('ignores the end of another repository’s run', () => {
    expect(nextChatRunRepo('r1', 'chat:done', { repoID: 'r2' })).toBe('r1')
  })
})

import { choiceState, decisionCard, withChoice } from './chat'

const cardArgs = {
  path: 'config/settings.json', region: '3f2a9c1b', question: 'Which timeout?',
  options: [{ label: '45000 (develop)', text: '  "apiTimeoutMs": 45000,\n' }, { label: 'remove', text: '' }],
}

describe('decisionCard', () => {
  it('reads a propose_options call', () => {
    expect(decisionCard({ name: 'propose_options', args: cardArgs })).toEqual(cardArgs)
  })
  it('is null for other tools and malformed args', () => {
    expect(decisionCard({ name: 'resolve_hunk', args: cardArgs })).toBeNull()
    expect(decisionCard({ name: 'propose_options', args: { ...cardArgs, options: 'x' } })).toBeNull()
    expect(decisionCard({ name: 'propose_options', args: { ...cardArgs, options: [{ label: 1, text: '' }] } })).toBeNull()
    expect(decisionCard({ name: 'propose_options', args: { ...cardArgs, region: undefined } })).toBeNull()
    expect(decisionCard({ name: 'propose_options', args: null })).toBeNull()
  })
})

describe('choiceState', () => {
  it('follows the recorded result', () => {
    expect(choiceState({ summary: 'Shown to the user as a card with 2 options; they will choose' })).toBe('pending')
    expect(choiceState({ summary: 'The user chose "remove" for x (region y); it was written.' })).toBe('chosen')
    expect(choiceState({ summary: 'Settled another way: region y of x is no longer in conflict.' })).toBe('settled')
  })
})

describe('choiceState without a card', () => {
  it('is null until the tool accepted the card, and when it refused it', () => {
    expect(choiceState({})).toBeNull()
    expect(choiceState({ summary: 'Not shown: a card has 2 to 4 options, not 5.' })).toBeNull()
    expect(choiceState({ summary: 'Option "x": Not applied: your resolution starts with …' })).toBeNull()
  })
})

describe('chat:choice', () => {
  const history: AIMessage[] = [
    { role: 'user', content: 'resolve' },
    { role: 'assistant', content: '', toolCalls: [{ id: 'c1', name: 'propose_options', args: cardArgs }] },
    { role: 'tool', toolName: 'propose_options', content: 'Shown to the user as a card with 2 options' },
  ]
  it('updates the card after the run ended', () => {
    const s = fromMessages('r', history)
    expect(s.items[1].tools[0].id).toBe('c1')
    const next = applyEvent(s, 'chat:choice', { repoID: 'r', callID: 'c1', summary: 'The user chose "remove" for x (region y); it was written.' })
    expect(choiceState(next.items[1].tools[0])).toBe('chosen')
    expect(choiceState(s.items[1].tools[0])).toBe('pending')
  })
  it('ignores another repository and unknown ids', () => {
    const s = fromMessages('r', history)
    expect(applyEvent(s, 'chat:choice', { repoID: 'other', callID: 'c1', summary: 'The user chose' })).toBe(s)
    expect(withChoice(s, 'nope', 'The user chose')).toBe(s)
  })
  it('is subscribed to', () => {
    expect(CHAT_EVENTS).toContain('chat:choice')
  })
  it('chat:tool carries the id', () => {
    const s = startRun(emptyChat('r'), 'resolve', 'run1')
    const next = applyEvent(s, 'chat:tool', { repoID: 'r', runID: 'run1', id: 'c9', name: 'propose_options', args: cardArgs })
    expect(next.items[1].tools[0].id).toBe('c9')
  })
})

describe('token counts', () => {
  const u = (input: number, output: number, cacheRead = 0) => ({ input, output, ...(cacheRead ? { cacheRead } : {}) })

  it('sums the usage of every model call folded into an answer', () => {
    const s = fromMessages('r', [
      { role: 'user', content: 'q' },
      { role: 'assistant', content: '', toolCalls: [{ id: 'c1', name: 'list_refs', args: {} }], usage: u(100, 10) },
      { role: 'tool', content: 'main', toolName: 'list_refs' },
      { role: 'assistant', content: 'On main.', usage: u(180, 25, 90) },
    ])
    expect(s.items[1].usage).toEqual({ input: 280, output: 35, cacheRead: 90, cacheWrite: 0, calls: 2 })
  })

  it('leaves an answer stored without usage uncounted', () => {
    const s = fromMessages('r', [{ role: 'user', content: 'q' }, { role: 'assistant', content: 'a' }])
    expect(s.items[1].usage).toBeUndefined()
    expect(conversationTokens(s)).toBeNull()
  })

  it('totals the conversation and flags answers without a count', () => {
    const s = fromMessages('r', [
      { role: 'user', content: 'old' },
      { role: 'assistant', content: 'old answer' },
      { role: 'user', content: 'q' },
      { role: 'assistant', content: 'a', usage: u(1000, 50) },
    ])
    expect(conversationTokens(s)).toEqual({ total: { input: 1000, output: 50, cacheRead: 0, cacheWrite: 0, calls: 1 }, missing: true })
  })

  it('adds live usage to the running answer and ignores other runs', () => {
    let s = startRun(emptyChat('r'), 'q', 'run1')
    s = applyEvent(s, 'chat:usage', { repoID: 'r', runID: 'run1', usage: u(100, 10) })
    s = applyEvent(s, 'chat:usage', { repoID: 'r', runID: 'run1', usage: u(150, 20, 100) })
    s = applyEvent(s, 'chat:usage', { repoID: 'r', runID: 'other', usage: u(9, 9) })
    s = applyEvent(s, 'chat:usage', { repoID: 'x', runID: 'run1', usage: u(9, 9) })
    expect(s.items[1].usage).toEqual({ input: 250, output: 30, cacheRead: 100, cacheWrite: 0, calls: 2 })
  })

  it('does not count the running answer as missing', () => {
    let s = startRun(emptyChat('r'), 'q', 'run1')
    s = applyEvent(s, 'chat:usage', { repoID: 'r', runID: 'run1', usage: u(100, 10) })
    s = { ...s, items: [...s.items, { role: 'user', text: 'q2', tools: [] }, { role: 'assistant', text: 'partial', tools: [] }] }
    expect(conversationTokens(s)?.missing).toBe(false)
  })

  it('formats token numbers', () => {
    expect(formatTokens(0)).toBe('0')
    expect(formatTokens(999)).toBe('999')
    expect(formatTokens(1000)).toBe('1k')
    expect(formatTokens(1234)).toBe('1.2k')
    expect(formatTokens(12000)).toBe('12k')
    expect(formatTokens(999_999)).toBe('1M')
    expect(formatTokens(1_250_000)).toBe('1.3M')
  })

  it('describes a count', () => {
    const t = { input: 8200, output: 640, cacheRead: 512, cacheWrite: 0, calls: 2 }
    expect(tokensText(t)).toBe('8.2k in · 640 out')
    expect(tokensTitle(t)).toBe('2 model calls · cache read 512')
    expect(tokensTitle({ ...t, calls: 1, cacheRead: 0 })).toBe('1 model call')
  })
})

describe('pending decision cards', () => {
  const shown = 'Shown to the user as a card with 2 options; the user will choose.'
  const args = (region: string) => ({ path: 'a.go', region, question: `q${region}`, options: [{ label: 'x', text: 'x' }, { label: 'y', text: '' }] })
  type Tool = { id?: string; name: string; args: Record<string, unknown>; summary?: string; at: number }
  const tool = (id: string | undefined, region: string, summary?: string): Tool =>
    ({ id, name: 'propose_options', args: args(region), summary, at: 0 })
  const stateWith = (...answers: Tool[][]): ChatState => ({
    repoID: 'r', runID: null,
    items: answers.flatMap((tools) => [{ role: 'user' as const, text: 'q', tools: [] }, { role: 'assistant' as const, text: 't', tools }]),
  })

  it('lists unanswered cards oldest first across answers', () => {
    const s = stateWith([tool('c1', '1', shown), tool('c2', '2', shown)], [tool('c3', '3', shown)])
    expect(pendingCards(s).map((p) => p.id)).toEqual(['c1', 'c2', 'c3'])
    expect(pendingCards(s)[0].card.question).toBe('q1')
  })

  it('leaves out answered, settled, refused, unfinished and id-less cards and other tools', () => {
    const s = stateWith([
      tool('c1', '1', 'The user chose "x" for a.go (region 1); it was written.'),
      tool('c2', '2', 'Settled another way: region 2 of a.go is no longer in conflict.'),
      tool('c3', '3', 'Not shown: a card has 2 to 4 options, not 5.'),
      tool('c4', '4', undefined),
      tool(undefined, '5', shown),
      { id: 'c6', name: 'list_conflicts', args: {}, summary: shown, at: 0 },
      tool('c7', '7', shown),
    ])
    expect(pendingCards(s).map((p) => p.id)).toEqual(['c7'])
  })

  it('drops a card once chat:choice records the answer', () => {
    const s = stateWith([tool('c1', '1', shown), tool('c2', '2', shown)])
    const after = applyEvent(s, 'chat:choice', { repoID: 'r', callID: 'c1', summary: 'The user chose "x" for a.go (region 1); it was written.' })
    expect(pendingCards(after).map((p) => p.id)).toEqual(['c2'])
  })

  const list = (...ids: string[]): PendingCard[] => ids.map((id) => ({ id, card: { path: 'a.go', region: id, question: id, options: [] } }))

  it('keeps the carousel on a sensible card', () => {
    // the shown card is still there (a card before it was answered)
    expect(trayIndexAfter('c', list('a', 'b', 'c'), list('b', 'c'))).toBe(1)
    // the shown card was answered: the next one takes its place
    expect(trayIndexAfter('b', list('a', 'b', 'c'), list('a', 'c'))).toBe(1)
    // it was the last: the previous one
    expect(trayIndexAfter('c', list('a', 'b', 'c'), list('a', 'b'))).toBe(1)
    // a new card arrives during the run: stay on the one being read
    expect(trayIndexAfter('a', list('a', 'b'), list('a', 'b', 'c'))).toBe(0)
    // nothing left
    expect(trayIndexAfter('a', list('a'), list())).toBe(-1)
    // nothing shown yet, or an id from another conversation
    expect(trayIndexAfter(null, list(), list('a', 'b'))).toBe(0)
    expect(trayIndexAfter('zz', list('a'), list('b', 'c'))).toBe(0)
  })
})
