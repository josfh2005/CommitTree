# git-ui — Command log panel — Design

Date: 2026-09-27
Status: Approved design, not implemented
Builds on: `2026-09-22-embedded-terminal-design.md` (terminal panel, moved
here), `2026-09-25-repo-toolbar-design.md` (toolbar toggles) and the AI chat
write tools.

## Goal

Show, under the selected repository, every git command the app runs — from
the user's clicks, from the AI tools and from background refreshes — and how
each one ended. The user can see what the app really did, why something
failed, and copy a command to run it by hand.

At the same time the embedded terminal moves out of the chat column into the
same bottom area, so the two "under the hood" views live together and the
chat has the right column to itself.

## Decisions

- **Scope:** every command that goes through `gitcmd` is recorded. Reads are
  hidden by default; a **Show reads** checkbox reveals them. Commands typed
  in the embedded terminal are not recorded (they already print their own
  output there).
- **Origin badge per row:** You / AI / Auto.
- **Row:** ✓/✗, `git <args>`, origin badge, time, duration; a click expands
  exit code and stdout/stderr (each capped at 64 KB); a copy button copies the
  command.
- **Retention:** in memory only, last 500 commands per repository, a Clear
  button. Nothing is written to disk.
- **Secrets are redacted** before anything is stored.
- **Placement:** a bottom dock inside the main area, under the repository
  view. It holds the Terminal and the Commands panel side by side; each takes
  the full width when it is the only one open.
- **Capture approach:** one recorder hooked into `gitcmd.RunEnv` (the single
  choke point), events to the frontend. Rejected: passing an explicit runner
  to every package (large refactor, no user-visible gain) and logging around
  bindings in the frontend (misses the real commands, the AI's commands and
  exit codes).

## Backend

### Capture (`internal/gitcmd`)

`RunEnv` times each command and, after it ends, hands the result to a
package-level recorder set with `gitcmd.SetRecorder(func(Record))`. `Record`
carries the context, `dir`, args, start time, duration, exit code, outcome
(`ok` / `failed` / `timeout`), stdout and stderr. With no recorder set (tests,
other tools) nothing changes. The call is wrapped so a panic in the recorder
is recovered and never affects the git command.

### Origin

Carried in the context: `cmdlog.WithOrigin(ctx, cmdlog.OriginAI)`.

- The AI tool runner (`RunTool` in `internal/app/ai.go`) sets it on the ctx
  of every tool call, so the AI's reads and its `Prepare`/`Recheck` checks
  are AI.
- An approved AI write runs through the same `App` methods the UI uses
  (`executeWrite` → `StageFile`, `Push`, …), which take `a.ctx`, not the
  tool's ctx. `runWriteTool` therefore marks the repository (by path) as
  "AI writing" around `executeWrite`, and the recorder tags every *write*
  run in that repository meanwhile as AI; reads in that window keep their
  own origin. A user write in the same window would be tagged AI;
  accepted.
- Commands without an origin in the context are **You** when they are
  writes and **Auto** when they are reads. The app cannot tell a read
  caused by a click from one caused by a refresh, so all such reads are
  Auto.

### Read or write

Classified by subcommand, not by timeout class. Reads: `log`, `show`,
`status`, `diff`, `rev-parse`, `rev-list`, `for-each-ref`, `show-ref`,
`cat-file`, `blame`, `ls-files`, `ls-tree`, `merge-base`, `name-rev`,
`describe`, `symbolic-ref` (without a new value), `worktree list`,
`submodule status`, `stash list`, `config` with `--get`/`--get-all`/`--list`/
`--get-regexp`, `branch`/`tag` when only listing (`--list`, `-l`, `--format`,
no positional name), `check-ignore`, `check-attr`, `var`, `version`,
`check-ref-format`, `remote` when listing or `remote get-url` (`-v` is an
option, not a subcommand).
Everything else is a write, `fetch` included (it is always shown).

### Store (`internal/cmdlog`)

- `Entry{ID, Repo, Args, Origin, Kind, Start, DurationMs, ExitCode,
  Outcome, OutputTruncated, OutputDropped}`; stdout and stderr kept alongside, not in the
  entry, each truncated to 64 KB.
- Keyed by the command's `dir`, cleaned (`filepath.Clean`). Every App method
  runs git with its repository's path as `dir`, so a repository ID resolves
  to its log through that path; a submodule or worktree has its own path and
  so its own log. No lookup of known repositories is needed at record time.
- A ring of 500 per repository; adding the 501st drops the oldest. Guarded by
  a mutex (git runs concurrently from several goroutines).
- Output budget: at most 8 MB of stored output per repository. Past it, the
  output of the oldest entries is dropped (the entry stays, marked
  `OutputDropped`; its expanded row says "Output no longer kept").
- IDs are a process-wide increasing counter.

### Redaction (`internal/cmdlog/redact.go`)

Applied to args and output before storing:

- URLs `scheme://user:pass@host` → `scheme://user:***@host`; a bare
  `scheme://token@host` → `scheme://***@host`.
- `-c key=value` where the key matches `http.*.extraheader` or contains
  `token`, `password` or `secret` → `key=***`.
- Every secret found in the args is also masked wherever it appears in stdout
  and stderr.

### Events and bindings

- Event `cmdlog:entry` with the `Entry` (no output), emitted when a command
  ends, straight through the Wails runtime once `Startup` has run — not
  through the AI deps' `Emit`, which tests use to assert on chat events. There is no "running" row: a long push appears when it finishes.
- `CommandLog(repoID) (CommandLogView, error)` —
  `{repo, entries}`, entries newest first, for the initial load. `repo` is
  `cmdlog.RepoKey(dir)` — the same key `Entry.Repo` and `cmdlog:entry` events
  carry — not the frontend's own `Repo.path`, which a cleaned path need not
  match byte-for-byte (e.g. `--show-toplevel` on Windows can give
  `C:/x/repo` against `Clean`'s `C:\x\repo`). The frontend matches both the
  initial load and live events against this key. `entries` is never null.
- `CommandLogOutput(repoID string, id int64) (CommandOutput, error)` —
  `{stdout, stderr}` when a row is expanded; an error if the entry was
  dropped.
- `ClearCommandLog(repoID) error`.

## Frontend

### Layout

`App.svelte` today is `aside | main | section.side`, with the terminal under
the chat in `.side`.

- `.side` holds only the Chat; `chatWidth` unchanged.
- New `BottomDock.svelte` at the bottom of `main`, under the repository view,
  separated by a horizontal `Splitter`. One height, `dockHeight`, remembered;
  it takes over the stored `terminalHeight` value so nobody loses their
  setting. Minimum 120 px; the repository view keeps at least 200 px.
- Inside: `TerminalPanel` and `CommandLogPanel`. When both are open they share
  the width with a vertical `Splitter`; the Terminal's width is `dockSplit`
  (remembered), the Commands panel takes the rest. Minimum 240 px each.
- The Terminal moves as is: tabs, "Open shell", closing from its own header
  and theme all behave as before. It is still hidden, not unmounted, so its
  shells survive.
- The toolbar gains a **Commands** toggle next to Terminal and Chat, same
  toggle style. Shortcuts: ⌘J Terminal (unchanged), ⌘⇧J Commands (Ctrl on
  Linux). Neither can open with no repository selected or a missing one.

### Commands panel (`CommandLogPanel.svelte`, `lib/cmdlog.ts`)

- On open (and on repository change) loads `CommandLog(repoID)`, remembers
  the `repo` key it returned, and listens to `cmdlog:entry`, keeping only
  entries whose `repo` matches that key (not other repositories', and not
  matched against `Repo.path`). It keeps at most 500 entries, like the
  backend.
- Header: title "Commands", **Show reads** checkbox (off by default,
  remembered), **Clear**, close button.
- Rows newest first: ✓ or ✗ (error colour on failure, "timed out" for a
  timeout), `git <args>` in monospace with ellipsis (full command in the
  tooltip), origin badge, time HH:MM:SS, duration (`42 ms`, `1.3 s`).
- A click or Enter expands the row: exit code, stdout and stderr in `<pre>`
  with their own scroll, "output truncated" when cut; the output is fetched
  the first time. A copy button copies the redacted command line.
- ↑/↓ move between rows. Empty states: "No git commands yet"; when only
  reads are hidden, "Only reads so far — Show reads".
- Theme tokens only: light, dark and high contrast work.
- A failed binding call shows a one-line error in the panel.

## Testing

- Go, `internal/cmdlog`: ring eviction, per-repo separation, redaction of
  URLs / `-c` values / echoes in output, read/write classification table,
  origin from context and the You/Auto fallback, output truncation.
- Go, `internal/gitcmd`: the recorder receives a successful and a failing
  command with exit code and outcome; a panicking recorder does not break
  `Run`.
- Go, `internal/app`: a command run through the AI tool runner is recorded as
  AI; `CommandLogOutput` for an unknown ID errors.
- Vitest, `lib/cmdlog.ts`: filtering reads, ignoring other repos, the 500 cap,
  duration formatting.
- Manual: with `/tmp/git-ui-demo`, open Terminal and Commands together, resize
  both splitters, commit, fail a push, check reads and the AI badge.

## Docs

- New `docs/spec/09-command-log.md`; `docs/spec/README.md` lists it.
- `docs/spec/08-terminal.md` "Layout and visibility" rewritten for the bottom
  dock.
- Both in the same commits as the behaviour change.

## Out of scope

- A live "running" row and cancelling a command from the panel.
- Persisting the log across sessions or exporting it.
- Recording git commands typed in the embedded terminal.
- Filtering by origin (only Show reads).
