import type { AIMessage, ChatDeltaEvent, ChatErrorEvent, ChatStartEvent, ChatToolEvent, ChatToolResultEvent } from './types'

export interface ChatToolUse {
  name: string
  args: Record<string, unknown> | null
  summary?: string
}

export interface ChatItem {
  role: 'user' | 'assistant'
  text: string
  tools: ChatToolUse[]
  stopped?: boolean
  error?: { message: string; code: string }
}

export interface ChatState {
  repoID: string
  runID: string | null
  items: ChatItem[]
}

export const emptyChat = (repoID: string): ChatState => ({ repoID, runID: null, items: [] })

// shouldReloadChat decides whether ChatPanel should reload state for repoID.
// It is false only when repoID is the same non-empty repo already loaded,
// so a repos-list refresh (which produces a new selectedRepo object with
// the same id) doesn't wipe an in-flight or already-loaded chat.
export function shouldReloadChat(state: ChatState, repoID: string): boolean {
  return repoID !== state.repoID || repoID === ''
}

const firstLine = (s: string) => s.split('\n')[0].slice(0, 120)

// fromMessages turns stored messages into display items: consecutive
// assistant/tool messages of one answer become a single assistant item.
export function fromMessages(repoID: string, messages: AIMessage[]): ChatState {
  const items: ChatItem[] = []
  for (const m of messages) {
    if (m.role === 'user') {
      items.push({ role: 'user', text: m.content, tools: [] })
      continue
    }
    let last = items[items.length - 1]
    if (m.role === 'tool') {
      const tool = last?.tools.find((t) => t.name === m.toolName && t.summary === undefined)
      if (tool) tool.summary = firstLine(m.content)
      continue
    }
    if (m.role !== 'assistant') continue
    if (!last || last.role !== 'assistant') {
      last = { role: 'assistant', text: '', tools: [] }
      items.push(last)
    }
    last.text += m.content
    for (const call of m.toolCalls ?? []) last.tools.push({ name: call.name, args: call.args })
    if (m.stopped) last.stopped = true
  }
  return { repoID, runID: null, items }
}

export function startRun(state: ChatState, text: string, runID: string): ChatState {
  return {
    ...state,
    runID,
    items: [...state.items, { role: 'user', text, tools: [] }, { role: 'assistant', text: '', tools: [] }],
  }
}

// CHAT_EVENTS are every event applyEvent understands. The panel subscribes
// to this list, so a new event added to the reducer reaches the UI instead of
// being silently dropped.
export const CHAT_EVENTS = ['chat:start', 'chat:delta', 'chat:tool', 'chat:tool_result', 'chat:done', 'chat:error'] as const

type Payload = ChatStartEvent | ChatDeltaEvent | ChatToolEvent | ChatToolResultEvent | ChatErrorEvent | { repoID: string; runID: string }

export function applyEvent(state: ChatState, name: string, payload: Payload): ChatState {
  if (payload.repoID !== state.repoID) return state
  // A run can also start from the log ("Explain"); the panel's own send has
  // already added the pair, so its start event changes nothing.
  if (name === 'chat:start') {
    if (state.runID === payload.runID) return state
    return startRun(state, (payload as ChatStartEvent).text, payload.runID)
  }
  if (payload.runID !== state.runID || state.runID === null) return state
  const items = state.items.slice()
  const last = { ...items[items.length - 1], tools: items[items.length - 1].tools.slice() }
  items[items.length - 1] = last
  switch (name) {
    case 'chat:delta':
      last.text += (payload as ChatDeltaEvent).text
      return { ...state, items }
    case 'chat:tool': {
      const p = payload as ChatToolEvent
      last.tools.push({ name: p.name, args: p.args })
      return { ...state, items }
    }
    case 'chat:tool_result': {
      const p = payload as ChatToolResultEvent
      const i = last.tools.findIndex((t) => t.name === p.name && t.summary === undefined)
      if (i >= 0) last.tools[i] = { ...last.tools[i], summary: p.summary }
      return { ...state, items }
    }
    case 'chat:done':
      return { ...state, runID: null, items }
    case 'chat:error': {
      const p = payload as ChatErrorEvent
      last.error = { message: p.message, code: p.code }
      return { ...state, runID: null, items }
    }
  }
  return state
}

export function toolLabel(tool: { name: string; args: Record<string, unknown> | null }): string {
  const value = Object.values(tool.args ?? {}).find((v) => v !== '' && v !== null && v !== undefined)
  return value === undefined ? tool.name : `${tool.name}: ${String(value)}`
}

export function errorText(error: { message: string; code: string }): string {
  switch (error.code) {
    case 'ollama_down':
      return 'Ollama is not running. Open Ollama and try again.'
    case 'model_missing':
      return 'The chat model is not installed. Download it in Settings.'
    case 'no_tool_support':
      return "This model doesn't support tools. Use qwen2.5 or llama3.1."
  }
  return error.message
}
