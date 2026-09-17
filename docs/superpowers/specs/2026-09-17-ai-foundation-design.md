# git-ui — Sub-project 3: AI Foundation — Design

Date: 2026-09-17
Status: Draft for review
Builds on: `2026-09-16-git-ui-design.md` (sub-project 1, merged to `main`)

## Goal

Make git-ui AI-first with local models: a working repo chat in the right-hand
panel (Ollama, with read-only git tools), an "Explain" action on commits
(Apple Intelligence on-device or Ollama), an AI settings dialog that detects
Ollama and downloads models, and a new app icon.

## Scope decisions

- Sub-project 2 (working tree) is not built yet, so **commit message
  generation is out of scope** here; it lands with sub-project 2 on top of this
  provider layer.
- Providers in this sub-project: **Ollama** and **Apple Intelligence**.
  Anthropic and OpenAI come later behind the same interface.
- **Chat uses Ollama only**, with tools. **Apple Intelligence is used only for
  one-shot tasks** (Explain) with pre-built context, because of its small
  context window and to avoid tool bridging through the Swift helper.
- One saved conversation per repo, with a "New chat" button.
- Everything runs locally; no secret redaction needed in this sub-project.

## Non-goals

Commit message generation, Anthropic/OpenAI, Keychain storage (no API keys
yet), multiple conversations per repo, write operations by the AI, AI in
merge/rebase, streaming Apple Intelligence tool calls.

## Architecture

Approach: one Go `Provider` interface; each provider uses its native protocol.
Ollama over its native HTTP API; Apple Intelligence through a bundled Swift
helper executable. Streaming output reaches the frontend as Wails events.

### Backend packages (`internal/ai/...`)

| Package | Responsibility |
|---|---|
| `ai` | Shared types: `Role`, `Message{Role, Content, ToolCalls, ToolName, Stopped}`, `ToolCall{ID, Name, Args}`, `ToolSpec{Name, Description, Parameters}`, `Request{Model, System, Messages, Tools}`, `Chunk{Delta, ToolCalls, Done, Err}`, and `Provider` interface: `Chat(ctx, Request) (<-chan Chunk, error)`. |
| `ai/ollama` | Native client: `Chat` (`POST /api/chat`, `stream:true`, tools), `Ping`, `ListModels` (`GET /api/tags`), `Pull(ctx, name, progress func(done, total int64))` (`POST /api/pull`, streaming). Detects "does not support tools" errors as `ErrNoToolSupport`. |
| `ai/apple` | Runs the `git-ui-apple` helper: `Status(ctx) (Availability, error)` and `Respond(ctx, instructions, prompt) (<-chan Chunk, error)`. Locates the helper next to the app binary, then at `helpers/apple/.build/release/git-ui-apple`. Missing helper → unavailable with reason `helperNotFound`. |
| `ai/tools` | Read-only git tools built on `gitlog` and `refs`: `search_log`, `show_commit`, `diff_commit_file`, `list_refs`, `file_history`. `Specs() []ai.ToolSpec` and `Run(ctx, dir, call ai.ToolCall) string`. Output capped at 8,000 characters with a `[truncated]` marker. Errors are returned as result text. |
| `ai/agent` | Chat loop: build system prompt, trim history, call provider, stream deltas, execute tool calls, repeat up to 8 steps. Emits events through an `Emitter` interface. |
| `ai/tasks` | One-shot tasks: `ExplainCommit(ctx, provider, dir, hash, budget)` builds context and streams the answer. |
| `ai/settings` | Load/save `git-ui/ai.json`; defaults. |
| `ai/chatstore` | Load/save/clear `git-ui/chats/<repoID>.json`. |
| `ai/prompts` | One Markdown prompt ("skill") per AI action, embedded as defaults and overridable by user files; renders `{{repo}}`, `{{path}}`, `{{branch}}`, `{{date}}`. |

### Swift helper (`helpers/apple/`)

Swift package with executable `git-ui-apple` using `FoundationModels`:

- `git-ui-apple status` → one JSON line:
  `{"available":true}` or
  `{"available":false,"reason":"deviceNotEligible|appleIntelligenceNotEnabled|modelNotReady|unknown"}`
  from `SystemLanguageModel.default.availability`.
- `git-ui-apple respond` → reads `{"instructions":"...","prompt":"..."}` from
  stdin; streams one JSON object per line: `{"delta":"..."}` (incremental
  text, not cumulative), then `{"done":true}`; on failure `{"error":"..."}`
  and exit code 1.

Go runs it with a 60 s timeout; cancelling kills the process.

### App API additions (`internal/app`)

- `AIStatus() AIStatus` — `{ollama: {running, url, models[], chatModelInstalled}, apple: {available, reason}}`; Ollama ping timeout 2 s.
- `GetAISettings() settings.Settings`, `SaveAISettings(settings.Settings) error`.
- `PullModel(name string) error` (streams `model:progress {name, completed, total, status}`, ends with `model:done {name, error?}`), `CancelPull() error`. One pull at a time.
- `GetChat(repoID string) ([]ai.Message, error)`.
- `SendChat(repoID, text, runID string) error` — the frontend generates `runID` (so no early event is missed); returns immediately and streams events. Returns `ErrChatBusy` if a run is active for that repo.
- `StopChat(repoID string) error`, `ClearChat(repoID string) error`.
- `ExplainCommit(repoID, hash, provider, runID string) error` — `provider` is `""` (use settings), `"apple"` or `"ollama"`; streams `explain:*` events.
- `ListPrompts() []prompts.Info`, `OpenPromptsFolder() error`, `ResetPrompt(name string) error`.

### Frontend

- `ChatPanel.svelte` becomes functional: message list with simple Markdown, streaming text, tool chips (e.g. "searched log: NEXO-1250"), input (Enter sends, Shift+Enter newline), Stop button while running, "New chat".
- `SettingsDialog.svelte` (opened from sidebar Settings).
- `CommitDetails.svelte`: "✨ Explain" button; explanation streams below the message.
- `lib/chat.ts`: pure reducer applying chat events to state; `lib/markdown.ts`: minimal safe Markdown (escape HTML first; bold, inline code, code blocks, lists, short hashes as links that jump to the commit).

## Settings

`os.UserConfigDir()/git-ui/ai.json`:

```json
{
  "ollamaURL": "http://localhost:11434",
  "chatModel": "qwen2.5:7b",
  "taskProvider": "apple",
  "taskModel": "qwen2.5:7b"
}
```

- `taskProvider` is `"apple"` or `"ollama"`; `taskModel` is used only for Ollama.
- Defaults on first run: `ollamaURL` as above, `chatModel` `qwen2.5:7b`,
  `taskProvider` `apple` if the helper reports available, else `ollama`.

## Settings dialog

- **Ollama:** status (🟢 running / 🔴 not responding with "Open Ollama or install it from ollama.com"), editable URL with "Test", installed models list, chat model selector. When the chat model isn't installed: "Download qwen2.5:7b (~4.7 GB)" button with progress bar and Cancel, plus a free-text model name field. If the URL host is not `localhost`/`127.0.0.1`, show "Diffs will be sent over the network to this host."
- **Tasks:** radio Apple Intelligence (with availability reason) / Ollama (model selector).
- Footer note: "Everything is processed on this Mac."

## Chat flow

1. Frontend generates a `runID` and calls `SendChat(repoID, text, runID)`. Go appends the user message to the stored conversation and starts the run.
2. System prompt: the `chat` prompt rendered with repo name, path, current branch (`refs.CurrentLabel`) and today's date (ISO). Default rules: read-only access; use tools instead of guessing; cite short hashes; convert relative dates; answer in the user's language; be concise.
3. History sent to the model: last 40 messages; tool results older than the last 10 messages replaced by `"[earlier tool result omitted]"`. The stored file keeps everything.
4. Agent loop, max 8 steps:
   - Call `provider.Chat` with tools; forward text as `chat:delta {repoID, runID, text}`.
   - For each requested tool call: emit `chat:tool {repoID, runID, name, args}`, run it, emit `chat:tool_result {repoID, runID, name, summary}` (first line, max 120 chars), append assistant tool-call message and tool message.
   - No tool calls → stop.
   - 8 steps reached → append note "Step limit reached." to the assistant message.
5. Save conversation; emit `chat:done {repoID, runID}`. On failure emit `chat:error {repoID, runID, message, code}` where `code` ∈ `ollama_down | model_missing | no_tool_support | other`.
6. `StopChat` cancels the run context; partial assistant text is saved with `Stopped: true`; emit `chat:done`.

All events carry `repoID` and `runID`; the frontend ignores other repos' runs.

## Tools

| Tool | Args | Returns |
|---|---|---|
| `search_log` | `text?`, `author?`, `since?`, `until?`, `branch?`, `path?`, `limit` (default 20, max 50) | One line per commit: short hash, date (YYYY-MM-DD), author, subject, refs |
| `show_commit` | `rev` | Subject, body, author, date, parents (short), changed files with status |
| `diff_commit_file` | `rev`, `path` | Patch vs first parent, max 300 lines |
| `list_refs` | — | Current branch, local branches, remote branches (max 100), tags (max 100) |
| `file_history` | `path`, `limit` (default 15, max 30) | Same line format as `search_log`, path-filtered |

- `rev` is resolved with `gitlog.ResolveCommit`; unresolved or starting with `-` → error text returned to the model.
- All output capped at 8,000 characters.

## Explain commit

- Context: subject, body, file list, and the full diff against the first parent truncated to 6,000 characters (Ollama) or 3,000 characters (Apple).
- Instructions: the `explain-commit` prompt. Default: explain in 3 to 6 short bullet points what changed and the likely reason; answer in Spanish if the commit message is Spanish, otherwise English.
- Streams `explain:delta {runID, text}`, `explain:done {runID}`, `explain:error {runID, message}`. Not persisted.
- If Apple returns an error, the UI shows it with a "Try with Ollama" button that reruns with Ollama.

## Prompts (skills)

- One Markdown file per AI action. Defaults are embedded in the binary (`internal/ai/prompts/defaults/<name>.md`): `chat.md`, `explain-commit.md`. Later actions (commit message, PR description, conflict resolution, release notes) add a file each.
- User overrides live in `os.UserConfigDir()/git-ui/prompts/<name>.md`; when present they replace the default.
- Variables replaced at render time: `{{repo}}`, `{{path}}`, `{{branch}}`, `{{date}}`.
- `ListPrompts` reports each prompt and whether the user file exists and differs from the default (`customized`).
- `OpenPromptsFolder` creates the folder, writes a copy of each default whose user file is missing, and opens the folder in Finder.
- `ResetPrompt(name)` overwrites the user file with the default.
- Settings dialog: a Prompts section listing each prompt with a "customized" badge and "Restore default", plus "Open prompts folder".

## Error handling

| Situation | UI |
|---|---|
| Ollama not responding | Chat shows "Ollama is not running" block with Retry and Settings link; input disabled |
| Chat model not installed | "Model X is not installed" with Download button |
| Model without tool support | "This model doesn't support tools; use qwen2.5 or llama3.1" |
| Step limit | Partial answer + "Step limit reached." |
| Tool error | Returned to the model as the tool result; not shown as a chat error |
| Apple safety/context error | Explain shows the message + "Try with Ollama" |
| Apple helper missing | Tasks settings shows Apple unavailable: "helper not found" |
| Second SendChat while running | `ErrChatBusy`; UI already disables input while running |

## App icon

- Concept: branch lines (2–3 colored lanes that fork and merge, lane palette from the graph) plus the lowercase letters **"ai"**, bold and prominent, on a rounded macOS-style square that reads in light and dark Docks.
- Produce 3 SVG variants in `assets/icon-variants/`: (a) branch lines behind "ai"; (b) "ai" sitting at a merge dot; (c) branch lines forming a stylized "ai". Render to PNG and let the user choose.
- The chosen SVG becomes `assets/icon.svg` and is rendered to `build/appicon.png` (1024×1024); `wails build` generates the `.icns`.

## Build

- `Makefile`:
  - `make helper`: `swift build -c release --package-path helpers/apple`.
  - `make build`: `make helper`, `wails build`, copy `helpers/apple/.build/release/git-ui-apple` to `build/bin/git-ui.app/Contents/MacOS/`, then `codesign --force --deep -s - build/bin/git-ui.app`.
  - `make dev`: `make helper`, then `wails dev`.
- README updated with the new targets and the Ollama requirement.

## Testing

- **ai/ollama:** `httptest` server with recorded NDJSON: text streaming, tool calls, tool-unsupported error, list models, pull progress + cancel, connection refused.
- **ai/agent:** scripted fake provider: event order (delta → tool → tool_result → delta → done), 8-step limit, cancellation saves `Stopped`, tool error fed back to the model, history trimming (40 messages, old tool results omitted).
- **ai/tools:** `testrepo` repos: each tool's output, caps and truncation marker, `-` rev rejection, unresolved rev message.
- **ai/apple:** fake helper shell script: status available/unavailable, streaming, error line, timeout, missing binary.
- **ai/settings, ai/chatstore:** round-trip, defaults, clear.
- **ai/tasks:** context truncation differs for Apple (3,000) vs Ollama (6,000).
- **Frontend (Vitest):** `lib/chat.ts` reducer (delta, tool, tool_result, done, error, other-repo events ignored), `lib/markdown.ts` (escaping, bold, code, lists, hash links), progress formatting.
- **Swift:** `swift build` must succeed; manual `git-ui-apple status` and short `respond`.
- **Manual:** download `qwen2.5:7b` from Settings; ask "¿qué cambió esta semana en release/escala-release-20?" in `e2-funnel-puppeteer` and see tool chips; Explain with Apple and with Ollama; Stop mid-answer; relaunch and see saved chat; icon shows in Dock.
