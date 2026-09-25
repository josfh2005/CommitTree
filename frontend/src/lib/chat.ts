import { providerShortLabel } from './providers'
import type { AIMessage, ChatConfirmEvent, ChatDeltaEvent, ChatDoneEvent, ChatErrorEvent, ChatNoticeEvent, ChatStartEvent, ChatSuggestionsEvent, ChatToolEvent, ChatToolResultEvent } from './types'

export interface ChatToolUse {
  name: string
  args: Record<string, unknown> | null
  summary?: string
  confirm?: { id: string; title: string; details: string[]; state: ConfirmState }
}

// 'approved'/'rejecting' are transitional: set the instant the user clicks,
// before ConfirmChatAction returns, so the card stays disabled and doesn't
// flash back to its buttons before chat:tool_result resolves it.
export type ConfirmState = 'pending' | 'approved' | 'rejecting' | 'done' | 'rejected' | 'failed'

export interface ChatItem {
  role: 'user' | 'assistant'
  text: string
  tools: ChatToolUse[]
  stopped?: boolean
  error?: { message: string; code: string }
  notices?: string[]
  // What produced an assistant answer; absent on answers stored before it
  // was recorded.
  provider?: string
  model?: string
  // When the answer finished (RFC 3339); absent while it runs and on
  // answers stored before it was recorded.
  at?: string
}

export interface ChatState {
  repoID: string
  runID: string | null
  items: ChatItem[]
  // The run whose answer finished last, and the replies suggested for it.
  lastRunID?: string | null
  suggestions?: string[]
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
    if (!last.provider && m.provider) {
      last.provider = m.provider
      last.model = m.model
    }
    if (m.at) last.at = m.at
    for (const call of m.toolCalls ?? []) last.tools.push({ name: call.name, args: call.args })
    if (m.stopped) last.stopped = true
  }
  return { repoID, runID: null, items }
}

export function startRun(state: ChatState, text: string, runID: string): ChatState {
  return {
    ...state,
    runID,
    suggestions: undefined,
    items: [...state.items, { role: 'user', text, tools: [] }, { role: 'assistant', text: '', tools: [] }],
  }
}

// CHAT_EVENTS are every event applyEvent understands. The panel subscribes
// to this list, so a new event added to the reducer reaches the UI instead of
// being silently dropped.
export const CHAT_EVENTS = ['chat:start', 'chat:delta', 'chat:tool', 'chat:tool_result', 'chat:confirm', 'chat:notice', 'chat:done', 'chat:error', 'chat:suggestions'] as const

type Payload = ChatStartEvent | ChatDeltaEvent | ChatToolEvent | ChatToolResultEvent | ChatConfirmEvent | ChatNoticeEvent | ChatErrorEvent | ChatDoneEvent | ChatSuggestionsEvent

export function confirmState(summary: string): 'done' | 'rejected' | 'failed' {
  if (summary.startsWith('done')) return 'done'
  if (summary.startsWith('rejected')) return 'rejected'
  return 'failed'
}

/** Puts a pending write confirmation on screen: on the tool the model is
 *  waiting on, or — after the panel re-mounted and reloaded a history that
 *  does not include the in-flight answer yet — on a fresh assistant item. */
export function withPendingConfirm(state: ChatState, ev: ChatConfirmEvent): ChatState {
  if (ev.repoID !== state.repoID) return state
  const items = state.items.slice()
  let last = items[items.length - 1]
  if (!last || last.role !== 'assistant') {
    last = { role: 'assistant', text: '', tools: [] }
    items.push(last)
  } else {
    last = { ...last, tools: last.tools.slice() }
    items[items.length - 1] = last
  }
  if (last.tools.some((t) => t.confirm?.id === ev.confirmID)) return state.runID === ev.runID ? state : { ...state, runID: ev.runID }
  const confirm = { id: ev.confirmID, title: ev.title, details: ev.details ?? [], state: 'pending' as const }
  const i = last.tools.findIndex((t) => t.name === ev.tool && t.summary === undefined && !t.confirm)
  if (i >= 0) last.tools[i] = { ...last.tools[i], confirm }
  else last.tools.push({ name: ev.tool, args: null, confirm })
  return { ...state, runID: ev.runID, items }
}

// withConfirmDecision reflects the user's click immediately, before
// ConfirmChatAction returns: the card leaves 'pending' so it can't be
// double-clicked, and shows a transitional state until chat:tool_result
// resolves it. Passing back 'pending' reverts it — used when the confirm
// call itself failed, so the buttons come back.
export function withConfirmDecision(state: ChatState, confirmID: string, next: 'approved' | 'rejecting' | 'pending'): ChatState {
  const items = state.items.slice()
  for (let i = items.length - 1; i >= 0; i--) {
    const item = items[i]
    const idx = item.tools.findIndex((t) => t.confirm?.id === confirmID)
    if (idx < 0) continue
    const tool = item.tools[idx]
    if (!tool.confirm) return state
    const tools = item.tools.slice()
    tools[idx] = { ...tool, confirm: { ...tool.confirm, state: next } }
    items[i] = { ...item, tools }
    return { ...state, items }
  }
  return state
}

const stripPrefix = (s: string, prefix: string) => (s.startsWith(prefix) ? s.slice(prefix.length) : s)

// confirmResultText is the single line a decided card shows, with the tool
// result's own "done: "/"error: " prefix removed so the outcome isn't stated
// twice ("Approved and done · done: pushed …" duplicated "done").
export function confirmResultText(tool: ChatToolUse): string {
  const confirm = tool.confirm
  if (!confirm) return ''
  const summary = tool.summary ?? ''
  switch (confirm.state) {
    case 'done': {
      const rest = stripPrefix(summary, 'done: ')
      return rest ? `Approved and done · ${rest}` : 'Approved and done'
    }
    case 'rejected':
      return 'Rejected'
    case 'failed': {
      const rest = stripPrefix(summary, 'error: ')
      return rest ? `Failed: ${rest}` : 'Failed'
    }
    default:
      return ''
  }
}

export function applyEvent(state: ChatState, name: string, payload: Payload): ChatState {
  if (payload.repoID !== state.repoID) return state
  // A run can also start from the log ("Explain"); the panel's own send has
  // already added the pair, so its start event only names what answers,
  // which only the backend knows.
  if (name === 'chat:start') {
    const p = payload as ChatStartEvent
    const next = state.runID === p.runID ? state : startRun(state, p.text, p.runID)
    const last = next.items[next.items.length - 1]
    if (last.provider === p.provider && last.model === p.model) return next
    return { ...next, items: [...next.items.slice(0, -1), { ...last, provider: p.provider, model: p.model }] }
  }
  // chat:confirm is handled before the runID guard: it can arrive after the
  // panel re-mounted and reloaded history with no in-flight run (runID
  // null), and withPendingConfirm itself checks repoID and adopts ev.runID
  // — dropping it here would strand the run with no card and no Stop.
  if (name === 'chat:confirm') return withPendingConfirm(state, payload as ChatConfirmEvent)
  // Suggestions arrive seconds after chat:done; they only belong to the
  // answer that finished last, and only while nothing else is running.
  if (name === 'chat:suggestions') {
    const p = payload as ChatSuggestionsEvent
    if (state.runID !== null || p.runID !== state.lastRunID) return state
    return { ...state, suggestions: p.replies }
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
      if (i >= 0) {
        const tool = last.tools[i]
        last.tools[i] = { ...tool, summary: p.summary, ...(tool.confirm ? { confirm: { ...tool.confirm, state: confirmState(p.summary) } } : {}) }
      }
      return { ...state, items }
    }
    case 'chat:notice': {
      const p = payload as ChatNoticeEvent
      last.notices = [...(last.notices ?? []), p.text]
      return { ...state, items }
    }
    case 'chat:done': {
      const at = (payload as ChatDoneEvent).at
      if (at) last.at = at
      return { ...state, runID: null, lastRunID: state.runID, items }
    }
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

/** answeredBy is the line under an answer naming what produced it,
 *  "Anthropic · claude-opus-5", or "" when that is not known. */
export function answeredBy(item: { provider?: string; model?: string }): string {
  if (!item.provider || !item.model) return ''
  return `${providerShortLabel(item.provider)} · ${item.model}`
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
