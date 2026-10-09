# Clone a repository — design

Date: 2026-10-09. Status: approved in chat, awaiting written-spec review.

## Goal

From the sidebar's "Add repo", the user can clone a remote repository: give a
URL and a destination folder, watch git's progress, cancel, and end up with
the new repository added to the list and selected. Today "Add repo" only
opens a folder picker (`App.AddRepo`); no clone code exists.

## Decisions (from the brainstorm)

- **Credentials: use what is already set up.** Same rule as a Fetch the user
  starts: credential helpers, the macOS keychain, ssh-agent and Git Credential
  Manager work; nothing asks for a password inside the app. An authentication
  failure gets a clear message. Asking for credentials in the app (CommitTree
  as `GIT_ASKPASS`) is a separate, later feature for clone, fetch, pull and
  push alike.
- **Options: URL, parent folder, folder name — nothing else.** The default
  branch is cloned, with `--recurse-submodules`. No branch field, no shallow
  clone, no submodule switch (YAGNI).
- **Progress lives in the dialog.** The form turns into a progress view with
  Cancel and "Continue in background"; closing it does not stop the clone.
- **System git, streamed.** `git clone --progress` with stderr read as it
  arrives. Not go-git (it would ignore the user's git config, helpers and
  ssh), not the embedded terminal (no progress, no cancel, no hand-off).

## Behaviour

### Entry point

"Add repo" opens a small menu: **Open folder…** (today's behaviour,
unchanged) and **Clone…**. One clone runs at a time: while one is running,
**Clone…** reopens the dialog on its progress view.

### The form

- **URL** — required. Any form git accepts (`https://…`, `ssh://…`,
  `git@host:org/repo.git`, a local path or `file://`). A URL starting with
  `-` is refused.
- **Parent folder** — with a "Choose…" button (directory picker). Defaults to
  the last parent used (remembered per machine in the frontend's persisted
  store), else the home folder.
- **Folder name** — filled from the URL as the user types: the last path
  segment without a trailing `/` or `.git` (`https://h/org/repo.git` → `repo`,
  `git@h:org/repo.git` → `repo`, `/src/thing/` → `thing`). Once the user
  edits the name by hand, further URL edits no longer overwrite it.
- A preview line shows the full destination path.
- **Clone** is enabled when the URL and name are non-empty. The backend
  validates before starting:
  - the parent folder exists and is a directory;
  - the name is not empty, `.` or `..`, and contains no path separator;
  - the destination does not exist, or is an empty directory (git's own
    rule).
  A refused field shows its error under it; nothing is run.

### Progress

The dialog shows the current phase and a bar: determinate when git reports a
percentage, indeterminate otherwise. Phases come from git's own lines —
`remote: Counting objects`, `remote: Compressing objects`, `Receiving
objects`, `Resolving deltas`, `Updating files`, and `Cloning into '<sub>'…`
for each submodule — with git's detail (counts, size, speed) as a small
second line. There is no overall percentage; the bar restarts per phase.

**Cancel** stops git the way Ctrl+C would. **Continue in background** closes
the dialog; the clone keeps running.

### Time limit

A clone has no fixed timeout (a large repository can take far longer than
the five minutes allowed to fetch and push). Instead a **stall watchdog**
ends it as a timeout when git writes nothing for five minutes.

### Outcome

- **Success:** the destination is added through the same path as Open
  folder (`store.Add`), and selected. If the dialog was closed, a toast says
  "Cloned <name>".
- **Failure:** the dialog returns to the form with the values kept and the
  error above the buttons. If the dialog was closed, a toast carries the
  error and reopening the dialog shows the form with it. Messages:
  - authentication failed (`ops.IsAuthError`): "Authentication failed. Set
    up a credential helper or an SSH key for this host, then try again.";
  - `Host key verification failed`: "The host's SSH key isn't trusted yet.
    Connect once from a terminal (ssh -T <host>) to accept it.";
  - repository not found / does not appear to be a git repository:
    "Repository not found at <url>.";
  - timeout: "Clone stalled: git sent nothing for 5 minutes.";
  - anything else: git's stderr, credentials masked (`cmdlog.MaskOutput`).
- **Cancel:** back to the form, no error.
- **Clean-up:** git removes what it created when it fails or is
  interrupted. As a safety net, if the destination did **not** exist before
  the clone and exists after a failure or cancel, the app removes it. A
  destination that existed before (an empty folder) is never removed.

### Command log

The clone goes through the command recorder like any git command (URL
credentials redacted). The Commands panel filters by repository, so the
clone does not appear there; its view is the dialog (and later the status
bar).

## Architecture

### `internal/gitcmd`

`RunStream(ctx, dir, env, stall, onLine, args...)`: a sibling of `RunEnv`
sharing its environment (`GIT_TERMINAL_PROMPT=0`, `LC_ALL=C`, extra env
last), process group, interrupt-on-cancel, `WaitDelay`, `begin`/`record`
and error mapping (`ErrCancelled`, `ErrTimeout`). Differences: no fixed
timeout; stderr is split on both `\r` and `\n` and each non-empty piece is
passed to `onLine` as it arrives (and still kept for the record and the
error); each piece resets the stall timer, and a stall ends the command with
`ErrTimeout`. The code both share moves into a helper rather than being
copied.

### `internal/clone` (new)

- `ParseProgress(line string) (Progress, bool)` —
  `Progress{Phase string; Percent int /* -1 when none */; Detail string}`;
  false for lines that are not progress.
- `Validate(parent, name string) (dest string, err error)` — typed errors
  `ErrParentMissing`, `ErrBadName`, `ErrDestNotEmpty`.
- `Run(ctx, url, dest string, stall time.Duration, onProgress func(Progress)) error`
  — refuses a URL starting with `-`; records whether `dest` existed; runs
  `git clone --progress --recurse-submodules -- <url> <dest>` from the
  parent folder through `RunStream`; on error, removes `dest` only if it did
  not exist before.
- `Explain(err error, url string) string` — the user-facing messages above.

### `internal/app/clone.go`

- `CloneRepo(url, parent, name string) error` — validates synchronously
  (returns the typed error), refuses while a clone is running
  (`ErrCloneRunning`), then runs the clone in a goroutine under a
  cancellable context derived from `a.ctx`.
- `CancelClone()` — cancels the running clone, if any.
- `CloneStatus() CloneState` — `{Running, URL, Dest, Progress, LastError}`,
  so a reopened dialog shows the right view.
- `PickCloneParent(start string) (string, error)` — directory picker.
- `DefaultCloneParent() string` — the home folder.
- Events: `clone:progress` (`Progress`), throttled to at most ~10 per
  second; `clone:done` (`{Repo *repos.Repo, Error string, Cancelled bool}`).
  On success the repo is added with `a.store.Add` before `clone:done`.

### Frontend

- `Sidebar.svelte`: "Add repo" opens a `ContextMenu` with Open folder… /
  Clone….
- `CloneDialog.svelte` (hosted like the other dialogs): form view and
  progress view.
- `lib/clone.ts` (pure, tested): `dirName(url)`, form validation, and a store
  that follows `clone:progress` / `clone:done` independently of the dialog
  being open; on `clone:done` it selects the repo or raises the toast when
  the dialog is closed. The last parent is a persisted store entry.

## Testing

- Go, `internal/clone`: `ParseProgress` table tests with real git output
  (including `remote:` lines, `, done.` lines and submodule lines);
  `Validate` cases; integration against a local bare repository (`file://`,
  via `testrepo`): a successful clone with a submodule, a non-empty
  destination refused, a cancel leaves no folder, a missing URL leaves no
  folder, a pre-existing empty destination survives a failure, a URL
  starting with `-` refused, and the stall watchdog
  (`GIT_SSH_COMMAND="sleep 30"` with a short stall) ending as a timeout.
- Go, `internal/gitcmd`: `RunStream` splits on `\r` and `\n`, records the
  command, maps cancel and stall.
- Go, `internal/app`: one clone at a time; the repo is added and
  `clone:done` emitted.
- vitest: `dirName` (https, ssh, scp-like, local paths, `.git`, trailing
  slash, empty), the store's transitions, form validation, the name stops
  following the URL after a manual edit.

## Docs

Same commit as the code: a "Cloning a repository" section in
`docs/spec/01-repositories-and-sidebar.md`, and `RunStream` with the stall
watchdog in `docs/spec/07-conventions-and-constraints.md`.

## Out of scope

Asking for credentials in the app (next, as its own feature); branch,
depth and submodule options; several clones at once; showing the clone in
the Commands panel; the status bar (queued after this feature).
