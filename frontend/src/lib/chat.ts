import { providerShortLabel } from './providers'
import type { AIMessage, AIUsage, ChatUsageEvent, ChatChoiceEvent, ChatConfirmEvent, ChatDeltaEvent, ChatDoneEvent, ChatErrorEvent, ChatNoticeEvent, ChatStartEvent, ChatSuggestionsEvent, ChatToolEvent, ChatToolResultEvent } from './types'

export interface ChatToolUse {
  // The call's id; a decision card (propose_options) is answered by it.
  id?: string
  name: string
  args: Record<string, unknown> | null
  // How much of the answer's text came before this call, and when it came
  // (seq), so the call is drawn where it happened, not above all the text.
  at?: number
  seq?: number
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
  // Asides about the run, each placed like a tool call at the text length
  // it arrived at.
  notices?: ChatNotice[]
  // What produced an assistant answer; absent on answers stored before it
  // was recorded.
  provider?: string
  model?: string
  // When the answer finished (RFC 3339); absent while it runs and on
  // answers stored before it was recorded.
  at?: string
  // What the answer's model calls consumed; absent when none reported.
  usage?: TokenCount
}

// TokenCount is what the model calls of an answer (or a conversation)
// consumed. Calls without a reported usage are not in it.
export interface TokenCount { input: number; output: number; cacheRead: number; cacheWrite: number; calls: number }

function addCount(t: TokenCount | undefined, u: TokenCount): TokenCount {
  return {
    input: (t?.input ?? 0) + u.input,
    output: (t?.output ?? 0) + u.output,
    cacheRead: (t?.cacheRead ?? 0) + u.cacheRead,
    cacheWrite: (t?.cacheWrite ?? 0) + u.cacheWrite,
    calls: (t?.calls ?? 0) + u.calls,
  }
}

function addUsage(t: TokenCount | undefined, u: AIUsage): TokenCount {
  return addCount(t, { input: u.input, output: u.output, cacheRead: u.cacheRead ?? 0, cacheWrite: u.cacheWrite ?? 0, calls: 1 })
}

export interface ChatNotice {
  text: string
  at: number
  seq?: number
}

// seq orders calls and notices that arrive at the same point of the text.
let seqs = 0
const nextSeq = () => ++seqs

/** One piece of an assistant answer, in the order it happened. */
export type ChatPart = { kind: 'text'; text: string } | { kind: 'tool'; tool: ChatToolUse } | { kind: 'notice'; text: string }

/** parts lays an answer out in the order it happened: the text the model
 *  wrote, cut wherever a tool call or a notice came in between. */
export function parts(item: ChatItem): ChatPart[] {
  const marks = [
    ...item.tools.map((tool) => ({ at: tool.at ?? 0, seq: tool.seq ?? 0, part: { kind: 'tool', tool } as ChatPart })),
    ...(item.notices ?? []).map((n) => ({ at: n.at, seq: n.seq ?? 0, part: { kind: 'notice', text: n.text } as ChatPart })),
  ].sort((a, b) => a.at - b.at || a.seq - b.seq)
  const out: ChatPart[] = []
  let pos = 0
  const text = (end: number) => {
    const t = item.text.slice(pos, end)
    if (t.trim()) out.push({ kind: 'text', text: t })
    pos = end
  }
  for (const m of marks) {
    if (m.at > pos) text(Math.min(m.at, item.text.length))
    out.push(m.part)
  }
  text(item.text.length)
  return out
}

/** A message the application sent the resolver mid-answer to make it carry
 *  on (internal/app resolveNudge); shown as a notice, not as the user's. */
export const NUDGE_PREFIX = 'CommitTree: '
export const NUDGE_NOTICE = 'The model stopped with work left; CommitTree asked it to carry on.'

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
    let last = items[items.length - 1]
    if (m.role === 'user') {
      if (m.content.startsWith(NUDGE_PREFIX) && last?.role === 'assistant') {
        last.notices = [...(last.notices ?? []), { text: NUDGE_NOTICE, at: last.text.length, seq: nextSeq() }]
        continue
      }
      items.push({ role: 'user', text: m.content, tools: [] })
      continue
    }
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
    // One answer spans several messages; keep their texts apart.
    if (last.text && m.content && !last.text.endsWith('\n')) last.text += '\n\n'
    last.text += m.content
    if (!last.provider && m.provider) {
      last.provider = m.provider
      last.model = m.model
    }
    if (m.at) last.at = m.at
    if (m.usage) last.usage = addUsage(last.usage, m.usage)
    for (const call of m.toolCalls ?? []) last.tools.push({ id: call.id, name: call.name, args: call.args, at: last.text.length, seq: nextSeq() })
    if (m.stopped) last.stopped = true
  }
  return { repoID, runID: null, items }
}

export function startRun(state: ChatState, text: string, runID: string): ChatState {
  return {
    ...state,
    runID,
    // A new message makes any suggestion for the previous answer stale,
    // even one that arrives after this run failed to start.
    lastRunID: null,
    suggestions: undefined,
    items: [...state.items, { role: 'user', text, tools: [] }, { role: 'assistant', text: '', tools: [] }],
  }
}

// CHAT_EVENTS are every event applyEvent understands. The panel subscribes
// to this list, so a new event added to the reducer reaches the UI instead of
// being silently dropped.
export const CHAT_EVENTS = ['chat:start', 'chat:delta', 'chat:tool', 'chat:tool_result', 'chat:confirm', 'chat:notice', 'chat:done', 'chat:error', 'chat:suggestions', 'chat:choice', 'chat:usage'] as const

type Payload = ChatStartEvent | ChatDeltaEvent | ChatToolEvent | ChatToolResultEvent | ChatConfirmEvent | ChatNoticeEvent | ChatErrorEvent | ChatDoneEvent | ChatSuggestionsEvent | ChatChoiceEvent | ChatUsageEvent

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
  else last.tools.push({ name: ev.tool, args: null, confirm, at: last.text.length, seq: nextSeq() })
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
  // chat:choice answers a decision card, usually long after its run ended.
  if (name === 'chat:choice') {
    const p = payload as ChatChoiceEvent
    return withChoice(state, p.callID, p.summary)
  }
  // Suggestions arrive seconds after chat:done; they only belong to the
  // answer that finished last, and only while nothing else is running.
  if (name === 'chat:suggestions') {
    const p = payload as ChatSuggestionsEvent
    if (state.runID !== null || p.runID !== state.lastRunID) return state
    return { ...state, suggestions: p.replies }
  }
  if ((payload as Exclude<Payload, ChatChoiceEvent>).runID !== state.runID || state.runID === null) return state
  const items = state.items.slice()
  const last = { ...items[items.length - 1], tools: items[items.length - 1].tools.slice() }
  items[items.length - 1] = last
  switch (name) {
    case 'chat:delta':
      last.text += (payload as ChatDeltaEvent).text
      return { ...state, items }
    case 'chat:tool': {
      const p = payload as ChatToolEvent
      last.tools.push({ id: p.id, name: p.name, args: p.args, at: last.text.length, seq: nextSeq() })
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
      last.notices = [...(last.notices ?? []), { text: p.text, at: last.text.length, seq: nextSeq() }]
      return { ...state, items }
    }
    case 'chat:usage':
      last.usage = addUsage(last.usage, (payload as ChatUsageEvent).usage)
      return { ...state, items }
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

// WRITE_TOOLS mirrors writetools.Specs in internal/ai/writetools, plus the
// conflict agent's writes (internal/ai/mergetools): the tools that change
// the repository, shown with a pencil instead of the magnifier the read
// tools get.
const WRITE_TOOLS = new Set(['stage_files', 'unstage_files', 'commit', 'create_branch', 'checkout_branch', 'stash_push', 'fetch', 'push', 'pull', 'merge_branch', 'cherry_pick', 'resolve_hunk', 'stage_file'])

export const isWriteTool = (name: string): boolean => WRITE_TOOLS.has(name)

/** appliedText is what a resolve_hunk call wrote in place of the region,
 *  shown under its row: the model's reply can describe a different
 *  resolution than the one it sent. null for any other tool. */
export function appliedText(tool: { name: string; args: Record<string, unknown> | null }): string | null {
  if (tool.name !== 'resolve_hunk') return null
  const text = typeof tool.args?.resolved === 'string' ? tool.args.resolved : ''
  return text.trim() === '' ? '(region removed: nothing written in its place)' : text.replace(/\n$/, '')
}

export function toolLabel(tool: { name: string; args: Record<string, unknown> | null }): string {
  const value = Object.values(tool.args ?? {}).find((v) => v !== '' && v !== null && v !== undefined)
  return value === undefined ? tool.name : `${tool.name}: ${String(value)}`
}

/** conversationTokens totals the conversation's counted calls, or null when
 *  none is counted. missing: a finished answer has no count (stored before
 *  counts existed, or its provider gave none); the running one never is. */
export function conversationTokens(state: ChatState): { total: TokenCount; missing: boolean } | null {
  let total: TokenCount | undefined
  let missing = false
  state.items.forEach((item, i) => {
    if (item.role !== 'assistant') return
    if (item.usage) total = addCount(total, item.usage)
    else if (item.text && !(state.runID !== null && i === state.items.length - 1)) missing = true
  })
  return total ? { total, missing } : null
}

/** formatTokens: 999, 1.2k, 12k, 1.3M. */
export function formatTokens(n: number): string {
  const short = (v: number) => String(Math.round(v * 10) / 10)
  if (n < 1000) return String(n)
  // Under 999.95k, so it does not round up to "1000k".
  if (Math.round(n / 100) < 10_000) return `${short(n / 1000)}k`
  return `${short(n / 1_000_000)}M`
}

export function tokensText(t: TokenCount): string {
  return `${formatTokens(t.input)} in · ${formatTokens(t.output)} out`
}

export function tokensTitle(t: TokenCount): string {
  const parts = [`${t.calls} model call${t.calls === 1 ? '' : 's'}`]
  if (t.cacheRead) parts.push(`cache read ${formatTokens(t.cacheRead)}`)
  if (t.cacheWrite) parts.push(`cache written ${formatTokens(t.cacheWrite)}`)
  return parts.join(' · ')
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

/** nextChatRunRepo tracks which repository an AI run is working on, from
 *  the run's own events, so the Merge view can wait for it whether or not
 *  the chat panel is open. */
export function nextChatRunRepo(current: string, name: string, payload: { repoID?: string }): string {
  if (name === 'chat:start') return payload.repoID ?? current
  if ((name === 'chat:done' || name === 'chat:error') && payload.repoID === current) return ''
  return current
}

export interface DecisionCard { path: string; region: string; question: string; options: { label: string; text: string }[] }

/** The card of a propose_options call, or null when it is not one or its
 *  args are malformed (then the ordinary tool row shows). */
export function decisionCard(tool: Pick<ChatToolUse, 'name' | 'args'>): DecisionCard | null {
  if (tool.name !== 'propose_options' || !tool.args) return null
  const { path, region, question, options } = tool.args
  if (typeof path !== 'string' || typeof region !== 'string' || typeof question !== 'string' || !Array.isArray(options)) return null
  const opts: DecisionCard['options'] = []
  for (const o of options) {
    if (!o || typeof o !== 'object') return null
    const { label, text } = o as Record<string, unknown>
    if (typeof label !== 'string' || typeof text !== 'string') return null
    opts.push({ label, text })
  }
  return { path, region, question, options: opts }
}

export type ChoiceState = 'pending' | 'chosen' | 'settled'

/** What a decision card shows, read from its recorded tool result (the
 *  backend rewrites it when the user chooses) — or null when there is no
 *  card: the result has not arrived yet, or the tool refused the options. */
export function choiceState(tool: Pick<ChatToolUse, 'summary'>): ChoiceState | null {
  const s = tool.summary ?? ''
  if (s.startsWith('Shown to the user as a card')) return 'pending'
  if (s.startsWith('The user chose ')) return 'chosen'
  if (s.startsWith('Settled another way')) return 'settled'
  return null
}

/** Records a card's answer on its tool, found by call id in any item. */
export function withChoice(state: ChatState, callID: string, summary: string): ChatState {
  for (let i = state.items.length - 1; i >= 0; i--) {
    const idx = state.items[i].tools.findIndex((t) => t.id === callID)
    if (idx < 0) continue
    const items = state.items.slice()
    const tools = items[i].tools.slice()
    tools[idx] = { ...tools[idx], summary }
    items[i] = { ...items[i], tools }
    return { ...state, items }
  }
  return state
}
