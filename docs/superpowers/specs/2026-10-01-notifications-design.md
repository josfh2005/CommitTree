# Notifications (phase 1) — design

Date: 2026-10-01. Status: approved in chat, spec under review.

## Problem

CommitTree gives no sign of what happened while the user is looking
elsewhere. A long push, pull or rebase finishes or fails with the window
in the background; the chat waits on a write card or a decision card
nobody sees; a merge leaves conflicts in another repository. The user
only finds out by coming back and looking.

## Goal

Tell the user about the events that need them or that they were waiting
for, and nothing else: an OS notification when the window is not focused,
an in-app toast when it is focused but the event is out of sight.

## Phases

- **Phase 1 (this spec):** the notification system and the events that
  come from things the app already does — finished operations, the AI
  needing the user, problems.
- **Phase 2 (own spec, later):** a periodic background fetch and a
  "new commits on the remote" notification. It plugs into phase 1 as one
  more event and one more Settings checkbox. Nothing in phase 1 fetches on
  its own.

## Decisions

- **The frontend decides, Go delivers.** One module, `lib/notify.ts`,
  holds every rule (settings, threshold, focus, what is in sight). Go only
  sends OS notifications and reports clicks. The frontend already knows
  the user-level operation, the active repository and whether the chat is
  open; the command log only knows single git commands.
- **Operations count as user operations, not git commands.** A pull is
  one event, not a fetch plus a merge.
- **Successes notify only after ≥ 10 s.** Failures, conflicts and "AI
  needs you" always notify. The threshold is fixed, not a setting.
- **Focused + in sight = nothing new.** Errors keep their existing error
  toast, never doubled.
- **Settings: a master switch and one checkbox per category**, all on by
  default, stored client-side with `persisted(...)` like `highContrast`.
- **Permission is asked lazily**, the first time an OS notification would
  be sent, never at startup.
- **One notification per repository and category on screen.** The
  notification id is `<repoID>:<category>`; a new one replaces the old one
  where the OS allows it.

## Event catalogue

| Category | Event | Notifies when |
|---|---|---|
| Operations finished (`done`) | Fetch, Pull, Push, Merge, Rebase, Cherry-pick, init/update all submodules, git-flow start/finish | succeeded and took ≥ 10 s |
| | Chat answer finished | the AI turn took ≥ 10 s and left nothing pending |
| AI needs you (`ai`) | Write card awaiting confirmation | always, when it appears |
| | New decision card in the tray | always |
| Problems (`problem`) | One of the operations above failed (auth, hook, network…) | always |
| | Conflicts left after Merge, Pull, Rebase, Cherry-pick, Stash apply/pop | always |
| | Chat answer ended with an error | always |

Precedence inside one operation or turn: conflicts beat finished; a
failure beats finished; a pending card beats chat finished. At most one
notification per operation or turn.

Out of scope: stage, unstage, discard (instant), read commands, terminal
commands (a PTY gives no reliable end of command), `auto`-origin commands,
creating branches or tags.

Text: the title is the repository name; the body is one short sentence,
for example "Push finished · 14 s", "Pull failed: Authentication failed",
"Conflicts in 3 files after merging feature/x", "The AI wants to commit —
confirm it". Failure bodies use the first line of the error, truncated to
120 characters.

## Decision rules

`decide(event, context) → 'none' | 'toast' | 'system'` is a pure function
in `lib/notify.ts`. `context` holds the settings, whether the window is
focused, the active repository id and whether the chat panel is open.

1. Master switch off, or the event's category off → `none`.
2. Category `done` and duration < 10 000 ms → `none`.
3. Window not focused → `system`.
4. Window focused and the event is in sight → `none`. In sight means the
   event's repository is the active one and, for `ai` events and chat
   events, the chat panel is open.
5. Window focused and out of sight → `toast`.

Focus comes from `document.hasFocus()`, kept current by the window
focus/blur listeners `startFocusRefresh` already uses.

A `toast` decision shows an info toast with the body prefixed by the
repository name and a **View** button that selects the repository (and
opens the chat for `ai` and chat events). For a failure the existing
error toast from `run`/`runOp` is reused: it gains the repository name
and the View button when the repository is not the active one, and no
second toast is added.

A `system` decision calls Go's `Notify`. If that fails (no permission, not
available), the frontend falls back to the `toast` path so the user sees
it on return.

## Go delivery

New file `internal/app/notify.go`:

- `Notify(id, title, body, repoID, target string) error` — `target` is
  `repo` or `chat`. On the first call it checks authorization and, if not
  yet decided, requests it. Then `runtime.SendNotification` with
  `ID: id` and `Data: {repoID, target}`. Returns the reason on failure
  (denied, unavailable, missing bundle identifier).
- `NotificationStatus() (string, error)` — `allowed`, `denied`,
  `undecided` or `unavailable: <reason>`, for the Settings line.
- `Startup`: `InitializeNotifications` (an error is kept as the
  unavailable reason, not fatal) and `OnNotificationResponse`. A click
  shows and unminimises the window and emits `notify:open` with
  `{repoID, target}`.
- `Shutdown`: `CleanupNotifications`.
- The Wails runtime calls go through a small interface so tests use a
  fake, following the command log's pattern of never emitting before
  Startup or after shutdown.

Platform notes: on macOS notifications need the bundled app (a valid
bundle identifier); under `make dev` `Notify` may fail and the toast
fallback applies. Linux uses D-Bus. On Windows replacement by id is not
supported, so notifications stack.

## Frontend changes

- `lib/notify.ts` — `decide`, `notify(event)` (applies `decide`, then
  toast or `api.notify`), event types, message text.
- `lib/stores.ts` — `notifyEnabled`, `notifyDone`, `notifyAi`,
  `notifyProblem`, persisted booleans, default `true`.
- `lib/actions.ts` — `runOp(id, op, label, fn)` next to `run`: measures
  the duration, emits `done` or the failure, otherwise behaves like `run`.
  The catalogue operations switch to it; `run` stays for the rest. The
  places that already detect conflicts after merge, pull, rebase,
  cherry-pick and stash apply/pop emit `conflicts` with the file count.
- `lib/chat.ts` — emits `ai` when a pending write card or decision card
  arrives, and at the end of a turn emits chat finished (with duration) or
  chat failed, unless the turn left a pending card.
- `App.svelte` — listens to `notify:open`: `selectRepo`, and opens the
  chat when the target is `chat`.
- `SettingsDialog.svelte` — a **Notifications** section: "Show
  notifications" switch, the three category checkboxes (disabled while
  the switch is off) and the permission status line.

## Testing

- Vitest: `decide` over every combination of settings, threshold, focus,
  active repository and chat open; `runOp` success/failure above and below
  the threshold; conflict precedence; chat hooks (finished, failed,
  pending card wins).
- Go: `notify.go` with a fake runtime — authorization asked once, failure
  reasons returned, click emits `notify:open`, nothing emitted before
  Startup.
- Manual: OS notifications in the built app (`wails build`); toast
  fallback under `make dev`; click brings the window to the right repo.

## Docs

New `docs/spec/11-notifications.md`, linked from `docs/spec/README.md`;
short cross-references in `06-ai.md` (cards notify) and
`05-remote-and-stash.md` (long remote operations notify).
