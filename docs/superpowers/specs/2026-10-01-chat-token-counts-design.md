# Chat token counts — design

Date: 2026-10-01. Status: approved in chat, spec under review.

## Goal

Show how many tokens the chat spends, so models can be compared by volume
(it came up on the conflict lab, where one run made 98 tool calls). Two
places:

- **Per answer:** the row under each answer (copy · time · provider · model)
  gains `8.2k in · 640 out`.
- **Per conversation:** in the composer bar, right after the model picker,
  `12.4k in · 1.1k out` for the repo's current conversation.

Out of scope: money cost, a per-app-session total, counts for suggested
replies and commit messages.

## Decisions

- **What counts.** Everything that writes into the repo's chat: typed chat
  turns (every model call of the agent loop), explanations (commit and lines)
  and the conflict resolver. Suggested replies and commit messages do not
  count; they are not part of the conversation.
- **"In" is everything the model read** on a call, cached tokens included,
  so the figure is comparable across providers. One agent turn re-sends the
  conversation on every tool round, so "in" grows with the rounds — that is
  the cost signal the user wants to see.
- **Cache detail and call count go in the tooltip**, not the visible text.
  No money cost.
- **Lifetime.** The conversation total is the sum of what is stored in the
  conversation, so it survives restarts and returns to nothing with "New
  chat".
- **Unknown is not zero.** A call whose provider gave no usage, an answer
  stored before counts existed, and a call cut short by Stop or an error have
  no count; they are left out of the sums and never shown as 0.

## Backend

### Neutral type (`internal/ai/ai.go`)

```go
type Usage struct {
    Input      int `json:"input"`                // everything read, cache included
    Output     int `json:"output"`
    CacheRead  int `json:"cacheRead,omitempty"`  // part of Input served from cache
    CacheWrite int `json:"cacheWrite,omitempty"` // part of Input written to cache
}
```

- `Chunk.Usage *Usage` — set only on the chunk with `Done: true`, nil when
  the provider reported nothing.
- `Message.Usage *Usage` (`json:"usage,omitempty"`) — on an assistant
  message, the usage of the model call that produced it.

### Providers

| Provider | Source | Mapping |
|---|---|---|
| Ollama | final NDJSON line (`done: true`) | Input = `prompt_eval_count`, Output = `eval_count` |
| OpenAI | trailing chunk with empty `choices` and `usage`, asked for with `stream_options: {include_usage: true}` | Input = `prompt_tokens`, Output = `completion_tokens`, CacheRead = `prompt_tokens_details.cached_tokens` |
| Anthropic | accumulated `message.Usage` | Input = `input_tokens + cache_read_input_tokens + cache_creation_input_tokens`, Output = `output_tokens`, CacheRead/CacheWrite from the two cache fields |

OpenAI today returns at the first `finish_reason`. It changes to: remember
the finish and its tool calls, keep reading until the stream ends (the usage
chunk comes after `finish_reason`), then send the single `Done` chunk with
the tool calls and the usage. If the stream ends without a usage chunk,
`Usage` stays nil. A stream error after `finish_reason` but before the end
still delivers the answer, without usage.

For Ollama, a final line with `prompt_eval_count` gives a usage; without it
(Ollama omits zero counts, e.g. a fully cached prompt) the usage is nil.
Ollama reuses the conversation's prefix from its cache, so its "in" is only
the prompt it processed, not everything the model read — a known
divergence from the "in" rule above.

### Agent loop (`internal/ai/agent`)

After each `Provider.Chat` call finishes, the `Done` chunk's usage is put on
that call's assistant message, and the agent emits:

```
chat:usage  UsageEvent{repoID, runID, usage}   // this call only
```

A call with nil usage emits nothing. The steps that already make an
assistant message per model call (tool rounds, the "carry on" nudge, the
recovered text tool call) each carry their own usage. `stampAnswer` keeps
the usage untouched.

### Explanations (`internal/app/ai.go`)

`streamIntoString` also returns the `Done` chunk's usage. `explainTask`
stores it on the explanation's assistant message and emits `chat:usage`
before `chat:done`. The conflict resolver already runs through the agent and
needs nothing extra.

## Frontend

### State (`frontend/src/lib/chat.ts`)

```ts
interface TokenCount {
  input: number; output: number;
  cacheRead: number; cacheWrite: number;
  calls: number;        // model calls with a count
}
```

- `ChatItem.usage?: TokenCount` on assistant items.
- `applyEvent('chat:usage')` adds the event's usage to the running answer
  (the last assistant item of that run), creating the count if absent.
- `fromMessages` sums the `usage` of every assistant message folded into an
  item. An item with no counted call has no `usage`.
- `conversationTokens(state)` sums every item's usage and returns
  `{ total, missing }`, or nothing when no item has a count. `missing` is
  true when a finished answer with text has no usage (an answer stored
  before counts existed, or a provider that gave none). The answer still
  running is never counted as missing. Stop and error inside an answer that
  has other counted calls do not make it missing.
- `formatTokens(n)`: `<1000` as is, then `1.2k`, then `1.2M` (one decimal,
  trailing `.0` dropped: `12k`, `3M`).
- Types in `lib/types.ts`: `AIUsage` on `AIMessage`, `ChatUsageEvent`, and
  `chat:usage` in `CHAT_EVENTS`.

### Views (`ChatPanel.svelte`)

- **Answer row:** after `answeredBy`, `8.2k in · 640 out` when the item has
  a usage. The tooltip reads "N model calls" plus "cache read X · cache
  written Y" when they are non-zero. The row only shows once the answer has
  finished, as today.
- **Composer bar:** a muted `12.4k in · 1.1k out` right after
  `<ModelPicker>`. It updates live while an answer runs and is hidden when
  the conversation has no count. The tooltip reads "This conversation: N
  model calls", the cache line when non-zero, and "Some answers have
  no count" when `missing`. It is plain text, not a control.

## Spec (`docs/spec/06-ai.md`)

The chat panel section gets the token counts in the answer-row paragraph and
a new paragraph after the model picker one. This goes in the same commit as
the behaviour.

## Testing

- Providers (`*_test.go`, httptest servers):
  - Ollama: the final line's counts come back on `Done`.
  - OpenAI: the usage chunk after `finish_reason` is read, tool calls still
    arrive, and a missing usage chunk gives nil.
  - Anthropic: input sums the cache fields.
- Agent (`agent_test.go`): a two-round tool loop stores a usage per
  assistant message, emits two `chat:usage` events, and nil usage emits
  none.
- App (`ai_test.go`): an explanation stores its usage and emits
  `chat:usage`.
- Frontend (`chat.test.ts`):
  - the reducer adds live usage to the running answer;
  - `fromMessages` sums and marks `missing`;
  - `conversationTokens` sums;
  - `formatTokens` boundaries.
- Manual: run `make dev`, ask with each configured provider, and check the
  row, the bar, live growth, New chat and a restart.
