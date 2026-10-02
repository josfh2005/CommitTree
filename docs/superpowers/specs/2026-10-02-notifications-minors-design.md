# Notifications — user-visible minors from phases 1 and 2

Date: 2026-10-02. Follows `2026-10-01-notifications-design.md`,
`2026-10-02-auto-fetch-design.md` and `2026-10-02-auto-fetch-minors-design.md`
(merged at bf1eeeb); behaviour in `docs/spec/05-remote-and-stash.md` and
`11-notifications.md`.

## Goal

Close the deferred minors a user would notice. Out of scope, still deferred:
pause keyed by directory (linked worktrees pause separately), `coalesce` may
run three times in a row, the Setsid test checks the session id only, and
the osxkeychain access dialog (documented, not a bug).

| # | Minor | Decision |
|---|---|---|
| 1 | Docs say a background fetch times out after 60 s; it is per remote | Fix the sentence |
| 2 | A pull that ends in conflicts does not unpause the repository | A conflicted pull resumes it |
| 3 | `authFailed` reaches the frontend unused; a paused remote is invisible | Amber dot + tooltip on the toolbar's Fetch |
| 4 | `Notify` holds its mutex while macOS shows the permission dialog | Release it around the request |
| 5 | A `propose_options` card the tool rejected still notifies | Notify on the tool's result, only when the card was shown |
| 6 | On Linux closing a notification opens the repository | Linux notifications get an **Open** button; only it opens |

## 1. Timeout sentence

`docs/spec/05-remote-and-stash.md`, "Background fetch": "It times out after
60 s" → "Each remote's fetch times out after 60 s". Docs only.

## 2. Conflicted pull resumes

A pull that stops on conflicts exits non-zero, so `noteRemoteUpdate` (which
needs exit code 0) leaves the pause in place, although its fetch reached the
remote. `App.Pull` (`internal/app/remote.go`, the only caller of `ops.Pull`,
used by the toolbar and the AI) calls `a.paused.resume(cmdlog.RepoKey(dir))`
when the result's outcome is `ops.Conflicted`.

Test (`internal/app`): pause a remote of a repository, pull into a conflict,
`paused` is false.

## 3. Paused remote visible

- Go: `func (a *App) AutoFetchPaused(id string) ([]string, error)` returns
  the repository's paused remotes, sorted; empty (not nil) when none. Backed
  by a new `autoPause.list(key) []string`.
- Frontend: `refreshRepo` also loads it into a per-repository store
  (`pausedRemotes`, `Record<repoID, string[]>`). `refreshRepo` already runs
  after every operation and after a background fetch that changed refs; it
  must also run after a background fetch that returned a non-empty
  `authFailed` (that fetch changed no refs). Resumes from the AI or git-flow
  are picked up by the refresh their operation already triggers.
- Toolbar: when the selected repository has paused remotes and Fetch is
  enabled, the Fetch button shows a small amber dot in its top-right
  corner (new colour token `--warning`, defined for light, dark and both
  high-contrast themes next to `--danger`) and its tooltip is
  "Background fetch paused for origin: authentication failed. Fetch to
  retry." — several remotes joined with ", ". A disabled Fetch keeps its
  busy tooltip; the dot stays.
- Pure helper `pausedTooltip(remotes: string[]): string` ('' when empty) in
  `lib/autoFetch.ts`.
- `AutoFetchResult.authFailed` is unchanged (Go pauses from it).

Tests: Go for `AutoFetchPaused`/`list`; vitest for `pausedTooltip` and for
the loop refreshing after an auth-failed fetch.

Spec: `05-remote-and-stash.md` toolbar table (Fetch row) and "Background
fetch" paragraph ("…until any fetch or pull succeeds…; meanwhile the
toolbar's Fetch shows an amber dot…"). A pull that stops on conflicts counts
as succeeding for this.

## 4. Permission request without the mutex

`Notify` sets `asked = true`, unlocks `notes.mu`, calls
`RequestAuthorization`, and locks again before sending. A `Notify` arriving
while the dialog is open sees `asked` and no permission and returns
`ErrNotificationsDenied`, so the frontend shows the toast; `NotificationStatus`
answers at once. `stopNotifications` during the dialog is safe: `Send` after
it fails and the frontend toasts.

Test: a fake notifier whose `RequestAuthorization` blocks on a channel; while
it is blocked a second `Notify` returns `ErrNotificationsDenied` and
`NotificationStatus` returns, then the first finishes and sends.

## 5. Card notification on the tool result

`watchChat` (`lib/notifyChat.ts`) stops reacting to `chat:tool`. On
`chat:tool_result` with `name === 'propose_options'` and a summary starting
"Shown to the user as a card" (Go `mergetools.CardShown`) it gives the `ai`
event "The AI has a decision for you" and marks the run `decided`. A rejected
card gives nothing and leaves `decided` false, so the run's "Chat answer
finished" can still notify. `CHAT_WATCH_EVENTS` swaps `chat:tool` for
`chat:tool_result`.

Tests: vitest — shown card notifies and marks decided; rejected card gives
nothing and `chat:done` then gives the finished event.

Spec: `11-notifications.md` table row "A new decision card" → "a decision
card shown to you".

## 6. Linux: only Open opens

Wails' Linux backend reports both a click on the body and a close by the
user as `DEFAULT_ACTION`, with nothing to tell them apart.

- `notifier` gains `RegisterCategory(runtime.NotificationCategory) error`
  and `SendWithActions(runtime.NotificationOptions) error`.
- On Linux (`runtime.GOOS`, held in `notifyState.goos` so tests can set it)
  `startNotifications` registers category `open` with one action
  `{ID: "open", Title: "Open"}`; `Notify` sends with
  `CategoryID: "open"` through `SendWithActions`. A registration error is
  ignored: the notification is then plain and cannot open anything, which
  is still better than opening on close.
- The response handler on Linux acts only on `ActionIdentifier == "open"`;
  elsewhere it keeps today's rule (anything but an error or a dismissal).

Tests: fake notifier with `goos = "linux"` — category registered, send goes
through `SendWithActions` with category `open`, a `DEFAULT_ACTION` response
emits nothing, an `open` response emits `notify:open`; with `goos =
"darwin"` behaviour is unchanged.

Spec: `11-notifications.md` replaces the "On Linux, closing a notification
also brings the window forward" paragraph with: "On Linux a notification
has an **Open** button, which is what opens it; clicking its text or closing
it does nothing, because the system reports both the same way."

## Process

Each behaviour change commits with its `docs/spec` update. Native execution,
Opus final review, merge `--no-ff` into main, rebuild and reopen the app.
