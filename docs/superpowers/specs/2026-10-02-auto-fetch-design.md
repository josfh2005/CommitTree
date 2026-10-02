# Notifications phase 2 — background fetch and "new commits on the remote"

Date: 2026-10-02. Builds on phase 1
(`2026-10-01-notifications-design.md`, behaviour in `docs/spec/11-notifications.md`).

## Goal

CommitTree fetches every repository in the background on a timer and tells
the user when the upstream of the checked-out branch gained commits they do
not have. It never prompts for credentials, never blocks a user operation,
and never repeats a notice for the same commits.

## Decisions

| Question | Decision |
|---|---|
| Interval | Configurable: Off / 5 / 15 / 30 / 60 min, default 15. First round 30 s after the app starts. |
| Which repositories | Every repository in the sidebar that is not missing, one at a time (never two background fetches at once). |
| What counts as new | Commits the upstream of the checked-out branch gained during this fetch that HEAD does not contain. |
| Failures | Silent. An authentication failure pauses that repository until a manual Fetch of it succeeds. Offline → the round is skipped. No notification for failures. |
| Battery | Not handled. |
| Who schedules | The frontend (timer + rules); Go runs the fetch and reports. Same split as phase 1. |

## Go — `AutoFetch(id string) (AutoFetchResult, error)`

```go
type AutoFetchResult struct {
    Skipped     bool   `json:"skipped"`     // no remote, or another write is running
    Branch      string `json:"branch"`      // checked-out branch, "" when detached
    Upstream    string `json:"upstream"`    // e.g. "origin/main", "" when none
    NewCommits  int    `json:"newCommits"`  // see below
    RefsChanged bool   `json:"refsChanged"` // any refs/remotes ref moved, appeared or vanished
}
```

1. No remote (`git remote` empty) → `Skipped`.
2. Takes the repository's write lock with `TryLock`; already held → `Skipped`.
   While held, the lock records a cancel function for this auto fetch
   (see "Lock" below).
3. Before the fetch: the upstream's tip (`rev-parse --verify -q @{upstream}`)
   and a snapshot of `for-each-ref refs/remotes` (name + hash).
4. `git fetch --all --prune` with origin **Auto** in the command log
   (`cmdlog.WithOrigin(ctx, cmdlog.OriginAuto)`), a 60 s timeout and the
   no-prompt environment below.
5. After: the upstream tip and the snapshot again. `RefsChanged` = snapshots
   differ. When the upstream existed before and after and its tip changed,
   `NewCommits = rev-list --count <new> ^<old> ^HEAD`; otherwise 0.

Because only commits that arrived during *this* fetch count, commits brought
by a manual Fetch are never announced later, and a restart does not repeat a
notice. A force-pushed upstream counts only the commits HEAD lacks. An
upstream removed by `--prune` yields 0.

### Never prompt

Extra environment for the auto fetch (appended last, via `gitcmd.RunEnv`):

- `GIT_TERMINAL_PROMPT=0` (already set for every command)
- `GIT_ASKPASS=false` and `SSH_ASKPASS=false` — any askpass invocation fails
  instead of opening a dialog (`false` exists on macOS and Linux; Windows is
  not built)
- `SSH_ASKPASS_REQUIRE=never`
- `GCM_INTERACTIVE=never` — Git Credential Manager fails instead of opening UI

The user's `core.sshCommand` and credential helpers are left alone: a helper
that answers silently (osxkeychain, ssh-agent, a key without passphrase)
still works; one that would need the user fails.

### Errors

The error is classified for the frontend: `auth` when stderr matches git's
authentication failures (`Authentication failed`, `could not read Username`,
`could not read Password`, `Permission denied (publickey`,
`Host key verification failed`, `terminal prompts disabled`); anything else
(network, timeout, cancelled) is `other`. The error string carries the class
as a prefix the frontend can test (`auto-fetch auth: …`); errors reach the
frontend as text, which already tells them apart by substring (e.g. "not fully
merged" in `actions.ts`).

### Lock

The per-repository write rule stays: one write at a time. What changes is
who waits:

- `write` / `writeAll` find the lock held by an **auto fetch** → they cancel
  it (the same cancel `gitcmd` uses for Cancel in the command log: git gets
  an interrupt and removes its lock files) and wait for the lock with a
  blocking `Lock`. The user's operation runs as soon as git exits (bounded by
  `WaitDelay`, 5 s).
- Held by anything else → `ErrBusy`, as today.
- An auto fetch never waits: it `TryLock`s and skips.

The cancelled auto fetch returns an `other` error, which the frontend
ignores. The background fetch never sets the toolbar's busy label.

## Frontend

### `lib/autoFetch.ts`

- A scheduler started once from the app shell. Reads `autoFetchMinutes`
  (store, persisted like phase 1's settings; 0 = Off). Changing it restarts
  the timer; Off stops it.
- One round: if `navigator.onLine` is false, skip. Otherwise, for each repo
  in the sidebar's order that is not missing and not paused, `await
  api.autoFetch(id)` — strictly serial. A round still running when the next
  tick comes skips that tick.
- `auth` error → add the repo to an in-memory `paused` set. A successful
  manual Fetch (in `fetchRemote`) or Pull of that repo removes it. Other
  errors are ignored.
- `refsChanged` and the repo is the selected one → refresh the log, refs and
  ahead/behind badges the same way `refreshRepo` does, without touching
  `busy`.
- `newCommits > 0` → `notify` with a `remote` event.

The pure part (what a result turns into: pause, refresh, event) lives in a
function in `notifyRules.ts` or `autoFetch.ts` that takes the result and
returns actions, so it is testable without timers.

### Notifications

- New category `remote` in `NotifyCategory` and `NotifyContext`, target
  `repo`, no duration. `decide` treats it like the others: system when not
  focused, nothing when the repo is selected in a focused window, toast
  otherwise.
- Body: `"3 new commits on origin/main"` (`"1 new commit on …"`). The title
  stays the repository name; View / click selects the repository.

### Settings → General

- **Fetch in the background**: select Off / Every 5 min / 15 / 30 / 60, above
  the Notifications block. Default 15.
- In Notifications, a fourth checkbox **New commits on the remote**, on by
  default; disabled with the hint "Turn on Fetch in the background first"
  when the interval is Off.

## Spec updates (same commit as the behaviour)

- `docs/spec/11-notifications.md`: the new event row, the category and
  setting, the dedup rule (only commits that arrived in that fetch).
- `docs/spec/05-remote-and-stash.md`: background fetch (interval, which repos,
  never prompts, pause on auth failure) and the amended one-write rule — a
  user write cancels a running background fetch instead of being refused.
- `docs/spec/09-command-log.md`: background fetches appear with the Auto
  badge; a cancelled one shows as cancelled.

## Testing

Go (testrepo with a bare remote):

- tip moved upstream → `NewCommits` = commits HEAD lacks; `RefsChanged`
- nothing new → 0, `RefsChanged` false
- the new commits already in HEAD (e.g. pushed from here) → 0
- detached HEAD / no upstream → 0, no error
- no remote → `Skipped`
- lock held by a user write → `Skipped`
- a user write while an auto fetch holds the lock cancels it and runs
- the command carries the no-prompt environment and Auto origin
- error classification: `auth` vs `other` from sample stderr

TypeScript (vitest):

- `decide` with `remote` across settings, focus and selection
- body pluralisation
- scheduler with fake timers: serial order, skips missing and paused repos,
  skips offline rounds, skips a tick while a round runs, Off stops it
- result handling: auth → paused; manual Fetch success → unpaused;
  `refsChanged` on the selected repo → refresh

## Out of scope

Battery awareness; per-repository intervals; announcing new remote branches
or other branches' upstreams; backoff for repositories that keep failing for
non-auth reasons; persisting the paused set across restarts.
