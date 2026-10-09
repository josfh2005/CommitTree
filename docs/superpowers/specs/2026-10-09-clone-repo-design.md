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
- **Parent folder** — a text field the user can type in, with a "Choose…"
  button (directory picker) that fills it; see below. Defaults to
  the last parent used (remembered per machine in the frontend's persisted
  store), else the home folder.
- **Folder name** — filled from the URL as the user types: the last path
  segment without a trailing `/` or `.git` (`https://h/org/repo.git` → `repo`,
  `git@h:org/repo.git` → `repo`, `/src/thing/` → `thing`). Once the user
  edits the name by hand, further URL edits no longer overwrite it.
- A preview line shows the full destination path.
- **Clone** is enabled when the URL, the name and the (trimmed) parent folder
  are non-empty. The backend
  validates before starting:
  - the parent folder, as typed, is trimmed and a leading `~` (exactly `~`,
    or `~/…`) is expanded to the home folder (`~user` is not expanded); the
    result must be an absolute path, else the refusal is "Parent folder must
    be a full path (or start with ~).";
  - the parent folder exists and is a directory;
  - the name is not empty, `.` or `..`, and contains no path separator;
  - the destination does not exist, or is an empty directory (git's own
    rule).
  The name's own rules are checked as the user types and shown under the
  field; a backend refusal (parent missing, destination not empty, bad URL)
  is shown above the buttons. Nothing is run.
- The parent folder may be typed or picked with "Choose…". A typed `~` or
  relative path reaches the backend as typed; `Validate` expands the `~` and
  refuses a relative path, and the refusal shows like the others. The preview
  line shows the parent as typed (a `~` is not expanded there), and the
  remembered parent is the typed value, trimmed, kept only once the
  backend accepted the clone. If the home folder cannot be found, a `~` is
  refused with `ErrHomeUnknown` ("The home folder is unknown; type a full path."),
  which satisfies `errors.Is(err, ErrParentMissing)`, not the full-path message. The
  running view shows the destination the backend resolved, so a `~` appears
  expanded there.
- **Parent folder suggestions.** Typing in the parent field (not a value set
  by the remembered parent or by "Choose…") shows a dropdown of folders under
  the field. It is our own listbox (not a `<datalist>`): the input is a
  `role="combobox"` with `aria-expanded`, `aria-controls` and
  `aria-activedescendant`; the list is `role="listbox"` and each row
  `role="option"` with `aria-selected`.
  - *What is listed* (`clone.ListDirs`): the text up to its last `/` is the
    folder to read and the rest is the prefix (case-sensitive). Only folders
    (symlinks to folders count; files and dangling links do not). Names
    starting with `.` only when the prefix starts with `.`. Sorted by name,
    at most 50 (`MaxSuggestions`). Each suggestion is the whole path in the
    typed form, without a trailing `/`: a leading `~/` is expanded (like
    `Validate`) to read the folder but kept in the suggestion. The text is
    trimmed first.
  - *Nothing is listed* (an empty list, never an error) for a relative path,
    `~user`, a folder that does not exist, is a file or cannot be read, an
    empty text, and `~`/`~/` when the home folder is unknown. A bare `~`
    suggests `~/`, so Tab can start descending.
  - *Timing:* a lookup runs 150 ms after the last keystroke; a newer lookup
    (or closing the list) discards an answer still in flight, so the list
    never shows a stale answer. From the keystroke until the answer for the new
    text arrives the shown list is *stale*: nothing is highlighted and only
    Esc acts on it (Tab moves focus, Enter submits, the arrows do nothing), so
    a key never completes a suggestion made for older text. Picking a suggestion looks up the next level
    at once, without the pause.
  - *Keys:* Down/Up move the highlight, wrapping (from nothing: Down → first,
    Up → last). Tab/Enter complete the highlighted folder and add `/` (not
    doubled after `~/`); the field then lists that folder's children. Tab with
    nothing highlighted completes the first suggestion while a name is being
    typed (text not ending in `/`); after a `/`, or with Shift, it moves focus.
    Enter with nothing highlighted is not used, so it submits the form. Esc
    closes the list only (the event does not reach the dialog) and cancels any
    pending lookup so it cannot reopen; with the list closed Esc closes the
    dialog as before. Keys with Ctrl/Alt/Meta and IME-composition keys
    (`isComposing`, `keyCode` 229) are ignored. Mouse presses anywhere on the
    list, scrollbar included, keep the field focused. A click (on mousedown, so the field
    keeps focus) completes like Tab. The list closes on blur, on Choose… and on
    Clone, and when empty.

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
    "Repository not found at <url>." (only when the line names the
    repository being cloned; one that names another path or URL, a
    submodule's, shows git's own message);
  - timeout: "Clone stalled: git sent nothing for 5 minutes.";
  - anything else: git's stderr, credentials masked (`cmdlog.MaskOutput`).
- **Partly cloned:** once git has fetched the repository it keeps it on
  purpose when the checkout or a submodule then fails ("Clone succeeded, but
  checkout failed", "Failed to clone 'lib' a second time"), and exits
  non-zero. If the failure is not a cancel or a stall and the destination
  holds a repository with a valid `HEAD`, the folder is **kept**: the
  repository is added and selected as on success, and `clone:done` carries
  both the repository and the error "Cloned, but some submodules or files
  could not be checked out: <git's message, credentials masked, last lines>".
  The dialog closes, an error toast shows the message (also when the dialog
  was closed), and the status keeps it as the last error. `clone.Run`
  returns `ErrPartial` (wrapping git's error) for this case.
- **Cancel:** back to the form, no error. The Cancel button reads
  "Cancelling…" and is disabled until `clone:done` arrives.
- **Clean-up:** git removes what it created when it fails or is
  interrupted before the fetch is done. As a safety net, if the destination
  did **not** exist before the clone and exists after a cancel, a stall or a
  failure that left no repository with a valid `HEAD`, the app removes it.
  A destination that existed before (an empty folder) is never removed; a
  dangling symlink counts as existing (`Lstat`) and is refused, never
  removed. Quitting the app cancels a running clone.

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
error); any write to stdout or stderr resets the stall timer, and a stall ends the command with
`ErrTimeout`. The code both share moves into a helper rather than being
copied.

### `internal/clone` (new)

- `ParseProgress(line string) (Progress, bool)` —
  `Progress{Phase string; Percent int /* -1 when none */; Detail string}`;
  false for lines that are not progress.
- `Validate(parent, name string) (dest string, err error)` — typed errors
  `ErrParentNotAbsolute`, `ErrParentMissing`, `ErrBadName`, `ErrDestNotEmpty`.
  `parent` is the text the user typed: `Validate` trims it, expands a leading
  `~` and requires the result to be absolute before the other checks.
- `ListDirs(partial string) []string` — the parent folder suggestions above;
  never nil, no error.
- `Run(ctx, url, dest string, stall time.Duration, onProgress func(Progress)) error`
  — refuses a URL starting with `-`; records whether `dest` existed; runs
  `git clone --progress --recurse-submodules -- <url> <dest>` from the
  parent folder through `RunStream`; on a cancel, a stall or a failure
  that left no repository with a valid `HEAD`, removes `dest` only if it
  did not exist before (checked with `Lstat`); on any other failure with
  such a repository it keeps `dest` and returns `ErrPartial` wrapping git's
  error.
- `Explain(err error, url string) string` — the user-facing messages above.

### `internal/app/clone.go`

- `CloneRepo(url, parent, name string) error` — refuses while a clone is
  running (`ErrCloneRunning`, checked first), validates synchronously
  (returns the typed error), then runs the clone in a goroutine under a
  cancellable context derived from `a.ctx`.
- `CancelClone()` — cancels the running clone, if any.
- `CloneStatus() CloneState` — `{Running, URL, Dest, Progress, LastError}`,
  so a reopened dialog shows the right view; `URL` is the URL with its
  credentials redacted (`cmdlog.RedactArgs`), as the progress view shows it.
  `Shutdown` calls `CancelClone`.
- `PickCloneParent(start string) (string, error)` — directory picker.
- `DefaultCloneParent() string` — the home folder.
- `ListDirs(partial string) ([]string, error)` — `clone.ListDirs`, with a
  non-nil empty slice when there is nothing (the error is always nil).
- Events: `clone:progress` (`Progress`), throttled to at most ~10 per
  second, but a phase's 100% update is always sent; `clone:done`
  (`{Repo *repos.Repo, Error string, Cancelled bool}`). On success, and on a
  partly done clone (`Repo` and `Error` both set), the repo is added with
  `a.store.Add` before `clone:done`.

### Frontend

- `Sidebar.svelte`: "Add repo" opens a `ContextMenu` with Open folder… /
  Clone….
- `CloneDialog.svelte` (hosted like the other dialogs): form view and
  progress view.
- `lib/clone.ts` (pure, tested): `dirName(url)`, form validation, and a store
  that follows `clone:progress` / `clone:done` independently of the dialog
  being open; on `clone:done` it selects the repo or raises the toast when
  the dialog is closed. The last parent is the persisted `cloneParent` store in `lib/stores.ts`,
  saved only once the backend accepted the clone.
- `lib/pathcomplete.ts` (pure, tested): the suggestion list state, `handleKey`
  (what each key does), `applyPick`, and `createCompleter` (150 ms debounce,
  latest answer wins, cancel). `CloneDialog.svelte` renders the dropdown.

## Testing

- Go, `internal/clone`: `ParseProgress` table tests with real git output
  (including `remote:` lines, `, done.` lines and submodule lines);
  `Validate` cases; integration against a local bare repository (`file://`,
  via `testrepo`): a successful clone with a submodule, a non-empty
  destination refused, a cancel leaves no folder, a missing URL leaves no
  folder, a pre-existing empty destination survives a failure, a URL
  starting with `-` refused, and the stall watchdog
  (`GIT_SSH_COMMAND="sleep 30"` with a short stall) ending as a timeout.
- Go, `internal/clone`, `ListDirs`: prefix filter (case-sensitive), folders
  only (symlinks to folders in, to files and dangling out), hidden names only
  with a `.` prefix, sorted, the 50 limit, trailing `/` lists all children,
  relative / `~user` / missing / file → empty non-nil, `~/` keeps the typed
  form, bare `~`, trimmed text, unknown home. `internal/app`: the binding
  returns `[]`, not nil.
- Frontend, `lib/pathcomplete.test.ts`: wrap-around selection, each key in
  open, stale and closed states (Enter without selection is not handled, Esc closes
  only the list), `applyPick`, debounce, newest-answer-wins, cancel.
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
