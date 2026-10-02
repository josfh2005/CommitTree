# Background fetch — deferred minors from phase 2

Date: 2026-10-02. Follows `2026-10-02-auto-fetch-design.md` (merged at
8f7262e); behaviour in `docs/spec/05-remote-and-stash.md`,
`09-command-log.md`, `11-notifications.md`.

## Goal

Close the minors the phase 2 review deferred, without changing what the
feature is for: background fetches never prompt, never block a user write,
and never notify twice.

| # | Minor | Decision |
|---|---|---|
| 1 | A user write can get ErrBusy in nanosecond windows around a background fetch | New per-repository write lock that records its holder atomically |
| 2 | Off or stop mid-round lets the round finish (dev HMR can double loops) | The round checks before each repository and stops |
| 3 | ssh may ask for a passphrase on the tty under `make dev` | git runs in a session of its own (`Setsid`) |
| 3b | osxkeychain may show the macOS access dialog | **Not fixed**: it is the system asking once; "Always Allow" ends it. Documented |
| 4 | A background refresh may race an operation's refresh | `refreshRepo` coalesces concurrent calls |
| 5 | One auth-failing remote pauses every remote of the repository | Fetch remote by remote; pause per remote |
| 6 | A fetch or pull from the AI or git-flow does not unpause | Pause state moves to Go; any successful non-Auto fetch or pull of the repository clears it |
| 7 | The 401 test inherits the developer's global git config | The test ignores global and system config |

## 1. Write lock (`internal/app/writelock.go`)

Today the lock is a `sync.Mutex` per repository plus a `sync.Map` with the
running background fetch's cancel. Between `TryLock` and `Store` (and
between `Delete` and `Unlock`) a user write finds the mutex held and no
cancel, and gets ErrBusy instead of cancelling the fetch.

Replace both with one type whose state changes under its own mutex:

```go
type writeLock struct {
    mu      sync.Mutex
    cond    *sync.Cond // on mu
    holder  holder     // free, user, auto, or handoff
    cancel  func()     // the auto holder's cancel
    waiting bool       // a user write is waiting for the auto holder
}

func (l *writeLock) lockUser() error            // free → user; auto and nobody waiting → cancel, wait, take it; else ErrBusy
func (l *writeLock) tryLockAuto(cancel func()) bool // free → auto; else false
func (l *writeLock) unlock()                     // auto with a waiter → handoff to it; otherwise free
```

- `lockUser` on `auto` with `waiting == false`: sets `waiting`, calls
  `cancel`, then waits on `cond` until `holder == handoff`, and takes it as
  `user`. A second user write meanwhile sees `auto` with `waiting`, or
  `handoff`, and gets ErrBusy, as today: writes never queue.
- `unlock` from `auto` with `waiting` sets `handoff` (not `free`), so no
  other write can slip in between the background fetch ending and the
  waiter waking.
- `App.writes` holds `*writeLock` per id; `App.autoFetches` goes.
  `lockWrite` returns the lock's unlock func; `write` and `writeAll` use
  it (`writeAll` still releases what it took when one id fails).
- `AutoFetch` calls `tryLockAuto` with its cancel; false → `Skipped`.

## 2. Stopping mid-round (`frontend/src/lib/autoFetch.ts`)

`runRound(d, keepGoing)` checks `keepGoing()` before each repository and
returns when it is false. `startAutoFetch` passes
`() => !stopped && get(autoFetchMinutes) > 0`, where its stop function sets
`stopped`. The fetch already running finishes (60 s at most), but the round
does not move on. HMR in `make dev` calls the old loop's stop, so the old
round ends at its next repository instead of running beside the new one.

## 3. No controlling terminal (`internal/gitcmd/proc_unix.go`)

`startInGroup` sets `Setsid: true` instead of `Setpgid: true`. A new
session is also a new process group (its id is git's pid), so `interrupt`'s
`kill(-pid, SIGINT)` is unchanged. Without a controlling terminal, ssh
cannot read a passphrase or confirm a host key from the tty; with
`SSH_ASKPASS_REQUIRE=never` it fails, which counts as an auth failure.

This applies to every git command. The bundled app has no terminal, so
nothing changes there; under `make dev`, a passphrase-protected key needs
ssh-agent, as in the bundled app. Windows is untouched.

## 4. Coalesced refresh (`frontend/src/lib/stores.ts`)

`refreshRepo` keeps the promise of the refresh in flight. A call while one
runs does not start a second at once: it gets a promise for one more
refresh that starts when the current one ends; further calls meanwhile
share that same promise. Every caller still awaits a refresh that started
after its call, so it sees fresh state; at most two run back to back.

## 5 and 6. Pause per remote, kept in Go

### Go

`ops.AutoFetch(ctx, dir, paused func(remote string) bool)` fetches each
remote from `git remote` in order with `git fetch --prune <remote>` (same
no-prompt env and 60 s timeout each), skipping those `paused` reports.

- An auth failure of one remote is recorded in the new
  `AutoFetchResult.AuthFailed []string` and the next remote is fetched.
- Any other failure of one remote (network, timeout) is left to the
  Commands panel and the next remote is fetched.
- A cancel stops the loop and returns the error, as today.
- Every remote paused → `Skipped`.
- `RefsChanged` and `NewCommits` compare before the first fetch with after
  the last, as today.

`App` keeps `autoPaused`: a mutex and `map[repoKey]map[remote]bool`
(`repoKey` = `cmdlog.RepoKey(dir)`), in memory only.

- `App.AutoFetch` passes a `paused` func reading it, and adds
  `AuthFailed` to it.
- `recordGit` clears the repository's entry when a command ends with exit
  code 0, its origin is not Auto, and it is a `pull` or a `fetch` that is
  not `fetch .` (git-flow's local branch update). That covers the toolbar,
  the AI's fetch and pull tools (they call `App.Fetch` and `App.Pull`), and
  git-flow's fetch.

### Frontend

`pausedRepos`, `resumeAutoFetch` and the `pause` field of `outcome` go;
`actions.ts` no longer unpauses. An `AutoFetch` error (cancel, unreadable
repository) still does nothing.

### Commands panel

A background round now logs one `git fetch --prune <remote>` per remote
(Auto) instead of one `git fetch --all --prune`.

## 7. The 401 test

`TestAutoFetchNeverPromptsForCredentials` sets `GIT_CONFIG_GLOBAL=/dev/null`
and `GIT_CONFIG_NOSYSTEM=1` with `t.Setenv`, so a developer's credential
helper or URL rewrite cannot change its outcome.

## Docs (`docs/spec`, same commits as the behaviour)

- `05-remote-and-stash.md`, Background fetch: `git fetch --prune` per
  remote; an auth failure stops background fetches of that remote (others
  continue) until any fetch or pull of the repository by the user, the AI or
  git-flow succeeds, or the app restarts; Off stops a running round at the
  next repository; the macOS keychain may ask once for access.
- `09-command-log.md`: no change in wording beyond "the fetch" → "the
  fetches".
- `07-conventions-and-constraints.md`, "One write lock per repository":
  add the background fetch exception it lacks today — a user write
  arriving while a background fetch holds the lock cancels it and gets the
  lock next, before any other write; a second write meanwhile is busy.

## Testing

- `writeLock` (unit, `-race`): user/user → ErrBusy; auto then user →
  cancel called, user gets it after unlock; while a user waits, a second
  user gets ErrBusy, also right after the auto holder unlocks (handoff);
  `tryLockAuto` on a held lock → false.
- `ops.AutoFetch` with two remotes, one an `httptest` 401 and one a local
  bare repository: the good remote's commits arrive, `AuthFailed` names the
  bad one; with `paused` reporting the bad one, it is not fetched; both
  paused → `Skipped`.
- `App`: after an auth failure the next `AutoFetch` skips that remote; a
  successful `App.Fetch` clears it; a `fetch .` does not; an Auto fetch
  does not.
- `gitcmd`: a command runs with no controlling terminal (a git alias
  `!tty` or `ps -o tpgid` check, Unix only).
- Frontend: `runRound` stops when `keepGoing` turns false between
  repositories; `refreshRepo` called three times while one runs → two runs
  in total, every caller resolves after the second.

## Out of scope

Phase 1 minors (mutex held during the macOS permission prompt, rejected
propose_options still notifying, Linux close = click). Showing paused
remotes anywhere in the UI.
