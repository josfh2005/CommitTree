# git-ui — Running commands and Cancel in the Commands panel — Design

Date: 2026-09-27
Status: Approved design, not implemented
Builds on: `2026-09-27-command-log-design.md` (Commands panel, bottom dock).

## Goal

Today a row appears in the Commands panel only when its git command ends.
Show a write **while it runs** — so a slow push, pull, rebase or a commit
with slow hooks is visible as it happens — and let the user **cancel** it.
At the same time, the "Operation running: …" notice moves from the
terminal's header to the bottom dock, so it is seen whichever dock panel is
open.

## Decisions

- **Only writes appear while running.** A running read never gets a row,
  whatever **Show reads** says; reads still appear when they end, as today.
- **Every running write can be cancelled**, like Ctrl+C in a terminal:
  SIGINT to git's process group; if it is still alive 5 seconds later it is
  killed. A fast local write usually ends before anyone can click.
- **New outcome `cancelled`**, apart from `failed` and `timeout`.
- **"Operation running: `<label>`"** leaves the terminal's tab bar and
  becomes a thin strip at the top of the bottom dock, shown while the dock
  is open (Terminal, Commands or both) and an operation is running.
- **No live output** while a command runs (out of scope, as before).

## Backend

### Start and end (`internal/gitcmd`)

`SetRecorder` takes a `*gitcmd.Recorder{Begin, End}` (either may be nil;
nil removes the recorder):

- `Begin(Start) int64` is called just before git starts, with the caller's
  ctx, `dir`, args, start time and a `Cancel func()`. It returns an ID.
- `End(Record)` is called when git ends, as today; `Record` now carries
  that `ID`.
- Both calls are wrapped so a panic is recovered (a panicking `Begin`
  yields ID 0) and never changes the command's result.

### Cancelling (`internal/gitcmd`)

- `RunEnv` wraps the caller's ctx in `context.WithCancelCause` before its
  timeout; `Start.Cancel` cancels it with the new `gitcmd.ErrCancelled`.
- Stopping git — on cancel **and on timeout** — sends SIGINT to git's
  process group (git is started in its own group, so a hook and its
  children get it too, as with Ctrl+C in a terminal); git removes its lock
  files and exits. `WaitDelay` (5 s) then kills git if it is still alive.
  On Windows there is no SIGINT: the process is killed.
- A cancelled command's `*Error` wraps `ErrCancelled`, and its `Error()`
  text is always `git <args>: git command cancelled` — never stderr (a
  hook's own output, or a shell's "Killed by signal 2."), which says
  nothing about why the command actually stopped (a timeout still wraps
  `ErrTimeout` and keeps its own message). The caller sees an ordinary
  error: a UI action shows it in its toast; an approved AI write returns it
  as the tool's error and the chat skips the rest of that batch, as for any
  failed write.
- What a cancel leaves behind depends on the operation, and each checks for
  `ErrCancelled` before drawing its own conclusions from the state git left
  behind, rather than misreading a cancel as a conflict or a success:
  - `ops.Pull` and a plain `merge.Merge`/`merge.Start`: left exactly as git
    stopped it, mid-merge or mid-rebase, for the conflict banner. Without
    this check, `Pull` mistook a cancelled pull that happened to land on a
    real in-progress merge/rebase state for a conflict it caused, and
    reported `Result{Conflicted}` with a nil error.
  - `merge.Rebase` and `merge.CherryPick`: unchanged — they already run
    their own `--abort` on any non-conflict stop, cancel included, so the
    branch is restored the same as for a real failure. Only a genuine
    conflict is left for the banner.
  - `merge.Continue` (the banner's Continue): a cancel can land after the
    resolved step has committed but while the sequencer is still moving
    onto the next commit, which changes `Fingerprint` exactly the way a
    real "moved on to the next conflict" success does. Without the
    `ErrCancelled` check ahead of the fingerprint comparison, `Continue`
    read that as success and returned nil.
  - Stash apply/pop: not changed — a cancel that lands after git has
    already written conflicting entries is indistinguishable from a real
    stash conflict and is reported as one, same as today.
- Note: in `make dev` started from a terminal, a git in its own process
  group that tries to read the terminal (an SSH passphrase prompt) is
  stopped by the shell instead of prompting; the packaged app has no
  terminal, so nothing changes there.
- A helper a git command started without detaching (the credential cache
  daemon, above all) shares the process group and is stopped by a cancel or
  a timeout with everything else; it is not disabled, and starts again the
  next time a command needs it.

### Log (`internal/cmdlog`)

- `Outcome` gains `running` and `cancelled`.
- `(*Log).Begin(gitcmd.Start) (Entry, bool)` classifies the command and
  reserves the next ID. For a **write** it stores an entry with outcome
  `running` (and its cancel func) and returns it with `true`; for a read it
  stores nothing and returns `false`.
- `(*Log).Add(Record)` with a known ID replaces the running entry in place
  (same position, same ID) and forgets its cancel func; with an ID the log
  does not hold (a read, or a write whose entry was evicted) it appends a
  new entry with that ID; with ID 0 (no `Begin`) it allocates one.
- `(*Log).Cancel(repo string, id int64) error` calls the command's cancel
  func; `ErrNotFound` when that command is not running (or belongs to
  another repository).
- `Clear` keeps running entries.
- Origin is decided at `Begin` the same way as at the end (AI in the ctx,
  the AI-write mark for writes, else You/Auto) and kept by the final entry.

### App (`internal/app`)

- The recorder's `Begin` applies the AI-write mark like `End` does, calls
  `Log.Begin`, and emits the running entry as `cmdlog:entry` (after
  Startup only, as today). The finished entry is emitted with the same ID.
  One event name carries both; the frontend tells them apart by outcome.
- New binding `CancelCommand(repoID string, entryID int64) error`.
- `CommandLog` returns running entries too (a panel opened in the middle of
  a push shows it).

## Frontend

- `mergeEntries` replaces an entry whose ID it already holds, except that a
  finished entry is never replaced by a running one (a late event must not
  bring a finished row back to "running").
- A running row: a spinner instead of ✓/✗, the command, its badge, the
  start time, the elapsed time counting every second, and a **Cancel**
  button (disabled once clicked, until the entry ends). It does not expand.
- A cancelled row: **⊘**, muted colour, expanded text "Cancelled".
- A failed `CancelCommand` shows the panel's one-line error.
- `BottomDock` gets the "Operation running: `<label>`" strip at its top,
  shown only while `busy` is set; `TerminalPanel` no longer shows it.

## Testing

- Go, `internal/gitcmd`: `Begin` gets args and a working `Cancel`; `End`
  carries `Begin`'s ID; cancelling a commit whose `pre-commit` hook sleeps
  returns an error wrapping `ErrCancelled` in well under 5 s, creates no
  commit and leaves no `index.lock`; a panicking `Begin` does not break
  `Run`; a timeout is still `ErrTimeout`.
- Go, `internal/cmdlog`: a write's `Begin` → running entry → `Add` replaces
  it in place, one entry; a read's `Begin` stores nothing; `cancelled`
  outcome; `Cancel` calls the func and errors for unknown/finished IDs and
  for another repository; `Clear` keeps running entries.
- Go, `internal/app`: `CancelCommand` with an unknown ID errors; the
  running entry is emitted before the finished one with the same ID.
- Vitest: `mergeEntries` replace rules; elapsed-time formatting; outcome
  text for `cancelled`.
- Manual: a push to a slow/unreachable remote shows a running row, Cancel
  turns it into ⊘ and the toast reports it; the busy strip shows with only
  Commands open and with only Terminal open.

## Docs

- `docs/spec/09-command-log.md`: running rows, Cancel, the `cancelled`
  outcome.
- `docs/spec/08-terminal.md`: "Busy notice" moves to the bottom dock.

## Out of scope

- Live output of a running command.
- Showing running reads.
- Cancelling from anywhere other than the Commands panel (toolbar, toast).
