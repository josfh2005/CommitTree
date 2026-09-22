# git-ui — Embedded terminal — Design

Date: 2026-09-22
Status: Approved, not implemented
Builds on: `2026-09-16-git-ui-design.md` (layout, write lock, focus refresh)
and `2026-09-17-ai-foundation-design.md` (chat panel), both merged to `main`.

## Goal

A real interactive shell inside the app, split below the chat panel, so the
things git-ui does not cover can be done without leaving it. It must feel like
a real terminal — full pty, ANSI colour, resize, Ctrl-C, full-screen programs
such as vim — and the app's views must not go stale when a command typed there
changes the repository.

Out of scope: Windows (ConPTY), restoring tabs across restarts, a terminal
that is not tied to a repository, any AI access to the terminal.

## Decisions

| Question | Decision |
|---|---|
| Shell per what? | Each repository has its own set of tabs. Tabs of other repositories stay alive in the background. |
| Staleness | When output settles after the user pressed Enter, the app runs the same external-change check it runs on window focus. |
| Write lock | The terminal neither takes nor honours the per-repo write lock — the same as an external terminal. While an app operation runs on that repository, the terminal header says so. |
| Safety | No sandbox. The shell runs as the user with the app's environment. The AI never reads from or writes to the terminal. |
| Transport | Wails events for output, bound methods for input and resize. No local socket. |
| Dependencies | Go: `github.com/creack/pty`. Frontend: `@xterm/xterm`, `@xterm/addon-fit`. |

### Why the race on the write lock is accepted

`a.write` uses `TryLock` and returns `ErrBusy`; there is no queue. Making the
terminal take the lock between Enter and settle would let a pager or an editor
hold it indefinitely, and holding keystrokes back while an app operation runs
would freeze Ctrl-C during a long push. Git protects its own state with
`index.lock` and ref locks, so the realistic worst case of a race is a git
error (`index.lock exists`), not a corrupted repository — the same exposure an
external terminal already has. The future AI write tools
(`git-ui-ai-write-tools`) still enter through `a.write`: the lock guards the
app's own operations against each other, not the user's shell.

## Architecture

### `internal/terminal` (new, no Wails dependency)

```go
type Callbacks struct {
	OnData    func(tab string, data string) // batched, valid UTF-8
	OnSettled func(tab, repoID string)
	OnExit    func(tab string, code int)
}

type Manager struct { /* mu; map[tabID]*session */ }

func NewManager(cb Callbacks) *Manager
func (m *Manager) Open(repoID, dir string, cols, rows int) (tabID string, err error)
func (m *Manager) Write(tab, data string) error
func (m *Manager) Resize(tab string, cols, rows int) error
func (m *Manager) Close(tab string) error
func (m *Manager) CloseRepo(repoID string)
func (m *Manager) CloseAll()
```

A session is `$SHELL -l` started with `Dir = dir` on a pty, in its own process
group (`Setsid`). `$SHELL` empty → `/bin/zsh` on macOS, `/bin/sh` elsewhere.
Environment: the app's own (already repaired by `FixPath`) plus
`TERM=xterm-256color` and `COLORTERM=truecolor`.

- **Reader**: one goroutine reads the pty into a buffer; a 16 ms timer flushes
  it through `OnData`. A trailing incomplete UTF-8 sequence is held back for
  the next flush.
- **Settle detector**: a `Write` containing `\r` arms it; once armed, 400 ms
  with no output fires `OnSettled` once and disarms. No Enter, no settle.
- **Exit**: when the shell exits, `OnExit` reports its code. The tab stays in
  the map (so the frontend can keep its scrollback) until `Close`.
- **Close**: SIGHUP to the process group, then SIGKILL after 2 s if still
  alive; closes the pty. Closing a tab never asks for confirmation.

Tab IDs are opaque strings unique for the process lifetime.

### `internal/app/terminal.go`

Bound methods:

```go
func (a *App) TerminalOpen(repoID string, cols, rows int) (string, error)
func (a *App) TerminalWrite(tab, data string) error
func (a *App) TerminalResize(tab string, cols, rows int) error
func (a *App) TerminalClose(tab string) error
```

`TerminalOpen` fails for an unknown or missing repository. Events:

| Event | Payload |
|---|---|
| `terminal:data` | `{tab, data}` |
| `terminal:settled` | `{tab, repo}` |
| `terminal:exit` | `{tab, code}` |

`RemoveRepo` calls `CloseRepo`; app shutdown (`OnShutdown`) calls `CloseAll`.

## UI

- **Right column** holds the chat above and the terminal below, with a
  horizontal `Splitter` between them. The height is a persisted store
  (`terminalHeight`), minimum 120 px for each side. Chat and terminal open and
  close independently (`chatOpen`, `terminalOpen`); when only one is open it
  fills the column. The toolbar gets a terminal toggle beside the chat toggle;
  **Ctrl+`** toggles it too.
- **Tab bar**: `zsh 1`, `zsh 2`, … (basename of the shell plus a per-repo
  counter that never reuses a number while the app runs), `+` to open another
  shell at the repository root, `×` on each tab to close it. An exited tab
  reads `zsh 1 — exited (0)` and keeps its scrollback until closed.
- **Busy notice**: at the right of the tab bar, `Operation running: <label>`
  while an app operation runs on the selected repository. The label comes from
  the frontend's existing `run(label, …)` wrapper, which already names every
  write; the merge agent's run is covered by the chat's existing running
  state. No backend change.
- **Opening** the terminal on a repository with no tabs opens one. Closing the
  last tab leaves an "Open shell" button rather than closing the panel. On a
  missing repository the toggle is disabled.
- **Switching repositories**: every tab keeps a mounted but hidden xterm
  instance; only the selected repository's tabs are shown. Scrollback (5 000
  lines) lives in that instance and survives the switch. A tab is refitted
  when it becomes visible.
- **Resize**: a `ResizeObserver` on the panel drives `addon-fit`, which calls
  `TerminalResize`.
- Tabs are not restored on restart.

## Staleness

The body of `startFocusRefresh`'s focus handler is extracted into
`checkExternalChanges()` (reload merge and worktree state, compare the
fingerprint, `refreshRepo` on a change). Window focus keeps calling it.
`terminal:settled` calls it too when `repo` is the selected repository;
otherwise the repository is marked pending and checked when it is next
selected. Changes made by another program while the window stays focused are
still not seen until the next focus — unchanged from today.

## Safety

Stated in the specification as it is:

- The shell is not sandboxed. It runs as the user, with the user's login-shell
  profile and the app's environment, and can do anything Terminal.app can.
- It is not an app operation: it bypasses the write lock (see above) and the
  app does not inspect, filter or log what is typed.
- The AI has no access: terminal output is never added to a prompt or sent to
  a provider, and no tool can type into a terminal. Scrollback can hold
  secrets (tokens, `.env` contents), which is the reason. Widening this is a
  separate decision, to be made with the AI write tools.

## Error handling

- Shell fails to start → `TerminalOpen` returns the error; the frontend shows
  it as a toast and opens no tab.
- `Write`/`Resize` on an exited or unknown tab → error, ignored by the
  frontend.
- Removing a repository with running shells closes them without asking.

## Testing

- `internal/terminal`, against real `/bin/sh`: output of `echo` arrives via
  `OnData`; settle fires after Enter and not without it; `exit 3` reports 3;
  `Close` kills a background `sleep 100` child (process group); a multibyte
  character split across reads arrives whole; `CloseRepo` closes only that
  repository's tabs.
- `internal/app`: `TerminalOpen` on an unknown or missing repository fails;
  `RemoveRepo` closes that repository's tabs.
- Frontend (vitest), pure logic only: tab store (per-repo tabs, numbering,
  visible set, exited state) and the settled rule (selected → check now,
  other → pending until selected). xterm itself is not unit-tested.
- Manual: vim, htop, Ctrl-C on `sleep`, colours, resize, switching repos with
  a running process, and a `git commit` typed in the terminal updating the
  log without a focus change.

## Specification updates

- New `docs/spec/08-terminal.md` covering all of the above.
- `01-repositories-and-sidebar.md`: removing a repository closes its shells.
- `06-ai.md`: the AI has no access to the terminal.
- `07-conventions-and-constraints.md`: no sandbox; the terminal bypasses the
  write lock by design; new dependencies.
- `docs/spec/README.md`: index entry for `08`.
