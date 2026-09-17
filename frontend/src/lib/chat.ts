import type { AIMessage, ChatDeltaEvent, ChatErrorEvent, ChatToolEvent, ChatToolResultEvent } from './types'

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

type Payload = ChatDeltaEvent | ChatToolEvent | ChatToolResultEvent | ChatErrorEvent | { repoID: string; runID: string }

export function applyEvent(state: ChatState, name: string, payload: Payload): ChatState {
  if (payload.repoID !== state.repoID || payload.runID !== state.runID || state.runID === null) return state
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
