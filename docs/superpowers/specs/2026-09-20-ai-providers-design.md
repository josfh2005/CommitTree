# git-ui — Sub-project 3b: Hosted AI providers (OpenAI, Anthropic) — Design

Date: 2026-09-20
Status: Draft for review
Builds on: `2026-09-17-ai-foundation-design.md` (AI foundation) and
`2026-09-17-merge-agent-design.md` (conflict agent), both merged to `main`.

## Goal

Replace Apple Intelligence with two hosted providers that can hold multi-turn
conversations with tool calling, so the chat, the explain feature and the
conflict agent can run on a capable model. The user picks a provider and a
model per feature, types an API key into Settings, and the key is kept in the
operating system's own secret store on macOS, Linux and Windows.

The trial during the merge agent's walkthrough is the reason: a local
qwen2.5-coder drives the tools correctly but decides the content of similar
hunks wrongly, and Apple Intelligence cannot drive tools at all.

## Scope decisions

- **Apple Intelligence is removed outright**, not kept as a fallback. It is
  one-shot with no tool calling, and it pins the app to recent Apple silicon.
  `internal/ai/apple`, `helpers/apple/` and the codesign step that bundles the
  Swift helper all go.
- **Provider and model are chosen per feature**, as today: one pair for
  chat/agent, one for tasks (commit messages, explain). A cheap model can
  write commit messages while the agent runs on a capable one.
- **Keys are never written to `ai.json`.** They live in the OS store through
  `github.com/zalando/go-keyring`: Keychain on macOS, Secret Service on Linux,
  Credential Manager on Windows. No cgo.
- **Model lists come from each provider's API**, so they do not go stale.
- **The official SDK is used for each hosted provider**, not hand-rolled HTTP:
  `github.com/anthropics/anthropic-sdk-go` and `github.com/openai/openai-go`.
  Ollama keeps its existing dependency-free HTTP client.
- **Out of scope:** per-repo provider configuration (its own backlog item),
  spending limits, and usage reporting.

## Architecture

### Packages

```
internal/ai/anthropic   Provider + Responder over anthropic-sdk-go
internal/ai/openai      Provider + Responder over openai-go
internal/ai/ollama      unchanged
internal/ai/keys        Get/Set/Delete an API key per provider (go-keyring)
internal/ai/apple       DELETED
helpers/apple/          DELETED
```

The existing provider-neutral interfaces in `internal/ai/ai.go` are unchanged
and remain the only contract the app layer knows:

```go
type Provider interface {  // multi-turn with tool calling
    Chat(ctx context.Context, req Request) (<-chan Chunk, error)
}
type Responder interface { // one prompt under fixed instructions
    Respond(ctx context.Context, instructions, prompt string) (<-chan Chunk, error)
}
```

Both SDKs stream text deltas and tool calls, which is exactly what `ai.Chunk`
carries (`Delta`, `ToolCalls`, `Done`, `Err`). No interface change is needed,
and the agent loop in `internal/ai/agent` is untouched — including the
recovery of tool calls a model writes as text, which stays useful for any
provider.

### Anthropic client

- Model ids are plain strings; the default offered in Settings is
  `claude-opus-5`.
- Adaptive thinking (`thinking: {type: "adaptive"}`); no `budget_tokens`,
  which current models reject.
- Streaming always, with `max_tokens` around 64000, so long answers do not hit
  HTTP timeouts.
- Tool results are returned as `tool_result` blocks; every `tool_use` in one
  assistant message is answered in a single following user message, which is
  what the API expects for parallel calls.
- Model list: the Models API (`client.Models.List`).

### OpenAI client

- Streaming chat completions with tools; deltas and tool-call fragments are
  accumulated into `ai.Chunk` the way the Ollama client already does.
- Model list: `GET /v1/models`, filtered to chat models — the raw list
  includes embeddings and image models.

### Keys

```go
package keys
func Get(provider string) (string, error)
func Set(provider, key string) error
func Delete(provider string) error
func Available() error   // reports why the OS store is unusable, if it is
```

`Available` exists for the headless-Linux case: with no Secret Service, saving
must fail with a clear message rather than silently writing the key somewhere
readable. There is no plain-text fallback.

### Settings

`Settings` gains a chat provider and accepts the new names:

```go
type Settings struct {
    OllamaURL    string `json:"ollamaURL"`
    ChatProvider string `json:"chatProvider"` // ollama | openai | anthropic
    ChatModel    string `json:"chatModel"`
    TaskProvider string `json:"taskProvider"` // ollama | openai | anthropic
    TaskModel    string `json:"taskModel"`
}
```

Migration: a stored `"apple"` in either field loads as `"ollama"` and is
rewritten on the next save. `validate` rejects any other unknown name, as it
does today.

### App layer

- `providerFor(name)` returns the `ai.Provider` for a name, reading the key
  from `keys` for the hosted ones; `responderFor(name, model)` does the same
  for tasks. Both replace today's `switch` over `settings.ProviderApple` /
  `ProviderOllama` in `internal/app/ai.go`.
- `ListModels(provider)` replaces the Ollama-only listing. Results are cached
  per provider for the session; Settings has a refresh.
- `GetAIStatus` stops reporting Apple availability. Per provider it reports
  whether a key is stored and what the last check returned.
- `SetProviderKey(provider, key)` / `DeleteProviderKey(provider)` are new,
  and never return a stored key to the frontend.

### Frontend

- Settings gets a provider selector plus a model dropdown for chat/agent and
  for tasks.
- Each hosted provider has a key field: empty with Save, or masked
  (`sk-…abcd`) with Remove once stored. The full key is never sent back to the
  renderer after it is saved.
- With no key, the model dropdown shows "Add a key to see the models" instead
  of an empty list.
- The Ollama section (URL, model download) is unchanged.

### Errors

Provider failures are mapped to messages the chat can show, reusing the
existing `chat:error` path with its code:

| Cause | Message |
|---|---|
| No key stored | "Add an API key for <provider> in Settings." |
| 401 / invalid key | "<provider> rejected the API key." |
| 429 | "<provider> is rate limiting; try again in a moment." |
| Insufficient credit | "<provider> reports no available credit." |
| Network / timeout | The existing network message. |

## Testing

- **Go, per provider:** an `httptest` server replaying a recorded stream —
  text deltas, a tool call, then a rate-limit error. Both SDKs accept a custom
  base URL, so no network and no real key is involved.
- **Go, settings:** migration from `"apple"`, rejection of unknown names, and
  that no key ever reaches the JSON file.
- **Go, keys:** the package against a fake store, including the unavailable
  case.
- **Vitest:** Settings state — provider without a key, masked key, model list
  per provider, and the switch between providers.
- **Not automated:** the real OS keyring and real calls to each provider.
  Those need a key and are left as a manual check, as the Ollama path is
  today.

## Cost

Until now every feature ran locally and free. A hosted provider spends real
money per run, and the conflict agent is the heaviest user: many tool rounds
per conflicted file. Mitigations in this design: the model is chosen per
feature so tasks can stay on a cheap one; nothing calls a provider unless the
user asks; and the existing history trimming already bounds what each request
carries.

## Risks

- **A key in a crash log or a screenshot.** The key is never logged and never
  returned to the renderer after saving; errors carry the provider's message,
  not the request.
- **Linux without a secret store.** Saving fails with an explicit message;
  the user can still use Ollama.
- **SDK drift.** Two new dependencies with their own release cadence. The
  recorded-stream tests fail loudly if a response shape changes.
- **Sticker shock.** A mis-set model (an expensive one for commit messages)
  costs money quietly. The defaults offered in Settings favour a cheap task
  model.

## Out of scope / later

- Per-repo provider and model (backlog item of 2026-09-18).
- Spending limits or a token counter in the UI.
- Any provider beyond these three.
