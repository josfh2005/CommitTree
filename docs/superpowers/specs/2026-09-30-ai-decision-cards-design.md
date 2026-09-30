# AI decision cards — design

Date: 2026-09-30. Status: approved in chat, awaiting review of this document.

## Why

This is part B of region actions (part A:
`2026-09-30-region-actions-design.md`, on main at b3a4354). When the AI
resolver meets a region whose two sides genuinely contradict each other —
`apiTimeoutMs` 45000 on develop vs 60000 on the feature, or one side deleting
a function the other edited — it rightly leaves the region alone and explains
why in text. The user then has to find the region in the Merge view and
settle it there.

Instead, the AI presents the decision as a card in the chat: a short
question and 2–4 concrete options, each showing the exact text that would
be written. The user picks one (or types their own) and the app writes it
through the same region apply as part A. Nothing is committed.

Success: in the conflict lab (`scripts/conflict-lab`) with a capable model,
cases 7 and 9 end up as cards; picking an option in each resolves them, and
`check.py --after-cards` grades the run 11/11.

## Decisions (owner, 2026-09-30)

- **Not blocking.** The tool returns at once and the AI carries on with the
  other regions. The card stays in the chat — across reloads — and can be
  answered after the run ends. Nothing holds the repository while the user
  decides.
- **Regions only.** Files without markers (delete/modify of a whole file,
  binary) keep part A's Take buttons; no card for them.
- **The app keeps the options and applies them** (approach 1): the frontend
  sends the call id and the option index; the backend reads the text from
  the chat history, so what is written is exactly what was shown, and it
  records the choice in the history, so the card and the model both know
  what was picked.

## What the user gets

While or after the AI resolves, the chat shows, where the call happened:

    config/settings.json · region 3f2a9c1b
    develop wants 45 s, feature/checkout 60 s — which timeout?
     ○ 45000 (develop)
         "apiTimeoutMs": 45000,
     ● 60000 (feature/checkout)
         "apiTimeoutMs": 60000,
     ○ Other…
                                            [Apply]

- One row per option: its label, and under it the exact text in monospace.
  An empty text shows *(removes the region)*.
- **Other…** is always added by the app. Choosing it opens a text box
  pre-filled with the text of the option selected before it (the first by
  default); ⌘↵ applies, Esc goes back to the options.
- **Apply** writes the selected text in place of the region. When that was
  the file's last region the file is staged. A toast says what happened, as
  in part A ("config/settings.json resolved and staged", or "Region
  resolved"), and the Merge view reloads.
- While an AI run is in progress for the repository the card is shown but
  Apply is disabled, with "Available when the AI finishes".
- Once decided, the card collapses to one line: `Chose "60000
  (feature/checkout)" — written`. If the region was settled some other way
  first (Merge view buttons, Restart file then a new run, the file edited
  outside), a click on Apply turns the card into "Settled another way".
- Any other failure (the merge was committed or aborted, the repository is
  busy) shows the error toast and leaves the card as it was.

## Design

### `internal/ai/mergetools`: `propose_options`

A new tool in `Specs()`, so only the conflict resolver has it (the chat's
tools are unchanged).

- Arguments: `path` (string), `region` (string, a region id from
  `read_conflict`), `question` (string, one line: why this is the user's
  call), `options` (array of 2–4 objects `{label, text}`; `label` names the
  choice, e.g. "45000 (develop)"; `text` is the region's replacement, exactly
  as `resolve_hunk`'s `resolved` would be).
- Validation, in order, each failure returned as text for the model:
  the path passes the same `open()` check as `resolve_hunk` (a text conflict
  of the operation, not a symlink); the region id exists in the current
  file; 2–4 options; every label non-empty after trimming, and labels
  distinct; texts distinct; no text contains conflict markers; every text
  passes `checkResolution` against the region (no echoed context lines, no
  unbalanced brackets).
- It writes nothing and returns `changed = false`. Result: "Shown to the
  user as a card with N options; they will choose after you finish. Do not
  resolve this region yourself; carry on with the rest."

### `internal/ai/agent`

`Execute` gives every tool call without an `ID` one (`call_` + 8 random hex
chars) before it is emitted or stored. Ollama may omit ids; the card needs
one. Providers that already send an id are untouched.

`ToolEvent` (`chat:tool`) gains `ID string \`json:"id"\``.

### `internal/app`: `ChooseRegionOption`

`ChooseRegionOption(repoID, callID string, option int, text string)
(RegionResult, error)`:

1. Refused while an AI run holds the repository (`ErrChatBusy`, as
   `ResolveMergeRegion`).
2. Loads the chat history; finds the assistant message whose `ToolCalls`
   has `ID == callID` and `Name == "propose_options"`, and the tool message
   paired with it (the k-th `RoleTool` message after that assistant message
   for its k-th call — the positional pairing the provider adapters already
   use). Unknown id → error "no such choice".
3. When the paired tool message already records a decision (its content
   starts with `The user chose` or `Settled another way`) → error "already
   decided".
4. `option >= 0`: the text and label of `options[option]` from the call's
   stored args (out of range → error). `option == -1`: `text` as given,
   label "their own text".
5. Applies through the body of `ResolveMergeRegion` with choice `"text"`
   (extracted into a shared unexported helper): `writeMerge`,
   `merge.ResolveRegion`, stage when none are left.
6. On success, rewrites the paired tool message's content to
   `The user chose "<label>" for <path> (region <id>); it was written.`
   (with ` The file is resolved and staged.` when staged), saves the
   history, emits `chat:choice {repoID, callID, summary}` and
   `merge:changed`.
7. On `merge.ErrNoSuchRegion`: rewrites the tool message to `Settled
   another way: region <id> of <path> is no longer in conflict.`, saves,
   emits `chat:choice`, and returns no error (the card shows the new state;
   no toast).
8. Any other error: history untouched, error returned.

Saving the history is done under the same chat slot check as step 1, so
it cannot race a run appending to the same history.

### Prompt `resolve-conflicts.md`

"Do not ask the user questions" becomes: work on your own; when the two
changes genuinely contradict each other, do not pick a side — call
`propose_options` for that region with the exact text of each real
alternative (usually each side, and a combination when one makes sense),
then carry on. The final report names those regions as "left for you to
choose in the chat". The "if you cannot tell…" rule stays for regions where
even the options are unclear.

The nudge message ("If you are leaving a region for the user on purpose…")
also mentions `propose_options`.

### Frontend

- `lib/chat.ts`:
  - `ChatToolUse` gains `id?: string`, set from `chat:tool`'s `id` and, on
    reload, from `toolCalls[].id` in `fromMessages`.
  - `regionChoice(tool)` → `{path, region, question, options: {label,
    text}[]}` or `null` when the args are malformed (then the ordinary tool
    row is shown).
  - `choiceState(tool)` → `'pending' | 'chosen' | 'settled'` from the
    tool's `summary`: starts with `The user chose` → chosen; `Settled
    another way` → settled; anything else (including undefined) → pending.
  - The reducer handles `chat:choice` by setting that tool's `summary`
    (found by `id` in any item). It is handled before the runID guard, like
    `chat:confirm`, since it arrives after the run ended. Added to
    `CHAT_EVENTS`.
- `components/ChatPanel.svelte`: the card, styled like `.confirm`; Apply
  disabled while the panel's run is in progress for the repository.
- `lib/api.ts` + Wails bindings: `chooseRegionOption`.
- `lib/actions.ts`: `chooseRegionOption(repoID, callID, option, text)`
  following `resolveMergeRegion` (toast, merge reload).

## Testing

- Go, `mergetools`: `propose_options` refuses an unknown region, a path not
  in conflict, fewer than 2 / more than 4 options, empty or repeated labels,
  repeated texts, markers in a text, echoed context; accepts valid options
  and leaves the file untouched.
- Go, `agent`: a call without an id gets one, stored and emitted the same;
  a call with an id keeps it.
- Go, `app`: `ChooseRegionOption` writes the option's text, stages on the
  last region, rewrites the tool message and emits `chat:choice`; `-1` with
  text; region already settled → "Settled another way", no error; second
  choice refused; unknown call id refused; option out of range refused;
  refused while an AI run holds the repository.
- Frontend (vitest): `regionChoice`, `choiceState`, the reducer on
  `chat:choice` (also after the run ended), `fromMessages` carrying ids.
- Conflict lab: `check.py --after-cards` — cases 7 and 9 count as right
  when resolved to any side's text, and the grader checks the repository's
  chat history for a `propose_options` call on each of those two files
  (missing → that case scores 0). The history is the most recently modified
  file in the app's chats directory (`<UserConfigDir>/git-ui/chats`), or
  the file given with `--chat`. Without the flag grading is unchanged.
  Manual run with Sonnet: resolve with AI, pick in both cards, grade.

## Out of scope

- Files without markers; cards in the ordinary chat.
- Undoing a choice (Restart file covers it).
- Blocking the run on the user's answer.

## Docs

`docs/spec/04-conflicts.md` (the AI resolver section) and
`docs/spec/06-ai.md` (the conflict agent's tools) describe the tool and the
card, in the same commit as the behaviour. `scripts/conflict-lab/README.md`
documents `--after-cards`.
