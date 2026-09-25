# git-ui — Suggested replies in the AI chat — Design

Date: 2026-09-25
Status: Approved design, not implemented
Builds on: `2026-09-17-ai-foundation-design.md` (the chat and its events),
`2026-09-23-chat-write-tools-design.md` (every write confirmed by a card).

## Goal

Once an answer is on screen, offer the replies the user is most likely to
type next — "ok dale", "sí, eso quiero", "creemos la rama fix/login" — as
chips that send with one click. The owner answers most turns with a short
confirmation or the obvious next step.

## Decisions

- **A separate call with the task role.** After an answer finishes, the task
  provider and model (the cheap one) are asked for up to three replies as a
  JSON array. The chat's own answer, its streaming and its stored history are
  untouched. A failed or unreadable call shows nothing, with no error.
- **Wait 5 seconds first.** Generation starts 5 s after the answer finishes.
  A message sent (or any other answer started) in that time cancels it before
  any call is made, so a user who answers straight away spends nothing.
- **Three modes, like the commit message.** Settings → AI → "Suggested
  replies": *Automatic for local models* (default) — only when the task
  provider is Ollama; *Always*; *Off*. The task provider decides because it
  makes the call.
- **A chip sends straight away.** It sends its text exactly as if typed. It
  does not only fill the input.
- **No path around the write confirmation.** A chip is an ordinary user
  message. If it leads the model to a write ("sí, crea la rama X"), the model
  still has to call the write tool, and that still pauses on the Approve /
  Reject card. Suggestions never call tools and never act.
- **Ephemeral.** Suggestions are not stored; a reopened conversation has
  none, so revisiting an old chat costs no call.

## Behaviour

- Offered after an answer that finished normally — a typed message, an
  explanation or a conflict-resolution run. Not after a stopped answer or an
  error.
- Up to three pill buttons just above the message box, left-aligned,
  wrapping. Short (at most 60 characters), in the language the user writes in,
  in the user's voice.
- No loading indicator: they appear when ready.
- They disappear when any message is sent (a chip or typed), when another
  answer starts (e.g. "Explain" from the log), when the chat is cleared and
  when the repository changes. Typing in the box does not hide them.

## Architecture

### Settings — `internal/ai/settings`

`SuggestReplies string` (`json:"suggestReplies"`) with `SuggestAutoLocal =
"auto-local"`, `SuggestAuto = "auto"`, `SuggestOff = "off"`. Empty on load
becomes `auto-local`; any other value fails validation with `ErrInvalid`, as
`CommitMessage` does.

### Prompt — `internal/ai/prompts`

New name `SuggestReplies = "suggest-replies"` with a default file in
`defaults/`, listed and customisable like the others. It asks for a JSON array
of at most three short replies the user might send next, in the user's
language and voice (not questions from the assistant, not restating the
answer), and nothing else.

### Generation — `internal/ai/tasks`

`SuggestReplies(ctx, r ai.Responder, instructions string, history
[]ai.Message) ([]string, error)`:

- Context: the last 6 user/assistant messages with text, oldest first, as
  `User:` / `Assistant:` lines; tool messages and tool calls are left out;
  each message is cut to 1500 characters.
- Calls `r.Respond` and collects the text.
- Parsing (`ParseReplies`, pure): takes the text from the first `[` to the last
  `]` (so code fences and surrounding prose are tolerated), unmarshals it as
  `[]any`, keeps strings only, trims them, drops empty ones, duplicates
  (case-insensitive) and those over 60 characters, and returns at most three.
  No array, or an empty result, is an error.

### Orchestration — `internal/app`

- `aiState` gains `suggestCancel map[string]context.CancelFunc` (repo ID →
  pending suggestion) and `suggestDelay time.Duration` (5 s; tests set it
  small).
- `a.cancelSuggestions(repoID)` cancels and forgets the pending one. It is
  called wherever a run takes the repo's chat slot (`SendChat`,
  `explainTask`, `ResolveConflicts`) and in `ClearChat`.
- `a.suggestReplies(repoID, runID, cfg)` is called right after `chat:done` is
  emitted by those three runs, only when the run returned no error at all
  (`runErr == nil`) and it saved — a stopped run also emits `chat:done`, with
  `context.Canceled`, and must not get suggestions. It returns at once unless
  the mode allows it for `cfg.TaskProvider` (same rule as the commit message:
  `off` never, `auto` always, `auto-local` only for Ollama). Otherwise it
  registers a cancel func and starts a goroutine that waits `suggestDelay`
  (or returns on cancel), loads the stored history, builds the task
  responder and the `suggest-replies` prompt, calls `tasks.SuggestReplies`
  under a 30 s timeout, and — if not cancelled meanwhile and without error —
  emits `chat:suggestions`. It then removes its own entry from the map (only
  if it is still its own).
- The goroutine never takes the chat slot, so it never makes the chat busy.

Event: `chat:suggestions` → `SuggestionsEvent{RepoID, RunID string; Replies
[]string}`, constant `EventSuggestions` next to the other chat events in
`internal/ai/agent`.

### Frontend

- `chat.ts`: `ChatState` gains `lastRunID: string | null` (set on
  `chat:done`) and `suggestions: string[]`. `chat:suggestions` is handled
  before the runID guard and accepted only when `state.runID === null` and
  `payload.runID === state.lastRunID`; otherwise ignored. `startRun` (and so
  `chat:start` for a new run) clears them; `emptyChat` and `fromMessages`
  start with none. `CHAT_EVENTS` includes `chat:suggestions`.
- `ChatPanel.svelte`: the pills above the textarea, inside the composer; a
  click calls `send(text)` — `send()` is split so the typed path and the chip
  path share it. Hidden while an answer runs.
- `SettingsDialog.svelte`: "Suggested replies" select next to the commit
  message one, with the three modes.
- `types.ts` (`AISettings.suggestReplies`, `ChatSuggestionsEvent`) and the
  generated `wailsjs/go/models.ts`.

## Testing

- `tasks`: `ParseReplies` on a clean array, a fenced array, prose around it,
  more than three, too long, non-strings, duplicates, not JSON, empty.
  `SuggestReplies` with a fake responder: context leaves out tool messages
  and keeps the last 6.
- `settings`: default `auto-local` on an empty value, unknown value rejected.
- `app` (fake Ollama answering the suggestion prompt with an array):
  `auto-local` + Ollama emits `chat:suggestions` after `chat:done` with the
  run's ID; `off` emits nothing; `auto-local` with a hosted task provider
  emits nothing; a stopped answer emits nothing; a new `SendChat` within the
  delay cancels it (no event, no request to the model); `ClearChat` cancels.
- Vitest: reducer accepts matching suggestions, drops stale ones (other
  runID, run in progress), clears them on start/clear.
- `docs/spec/06-ai.md` describes the behaviour and the setting, in the same
  commit as the code.

## Out of scope

- Suggestions that act (a chip that runs a tool directly).
- Storing suggestions or regenerating them for a reopened conversation.
- Hiding chips while the user types, or a manual "suggest" button.
