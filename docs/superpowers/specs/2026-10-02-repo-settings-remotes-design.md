# Repository settings: remotes, and a grouped repository menu

Date: 2026-10-02. Requested by the owner after the notifications minors.
Behaviour goes to a new `docs/spec/12-repository-settings.md` (listed in
`docs/spec/README.md`) and the menu to `docs/spec/01-repositories-and-sidebar.md`.

## Goal

Let the user manage a repository's remotes without a terminal, from a
per-repository settings dialog that later per-repository options (e.g. the
agent per repo) can join; and make the
repository row's context menu easier to scan by grouping it with separators.

Out of scope: renaming a remote, a separate push URL, AI chat tools for
remotes, settings for linked worktrees and submodules.

## 1. Context menu separators and groups

- `MenuItem` (`frontend/src/lib/ui.ts`) becomes a union with a separator
  entry `{ separator: true }`. `ContextMenu.svelte` draws it as a 1 px line
  (`var(--border)`, 4 px vertical margin) that is not focusable or
  clickable; the menu's vertical clamp counts a separator as 9 px instead of
  30. Leading, trailing and doubled separators are dropped by a pure helper
  `tidySeparators(items)` so callers can build groups conditionally.
- Repository row (`RepoRow.svelte`), main repository:
  1. Locate… (only when missing)
  2. Fetch · Pull · Push
  3. Show in Finder / Show in folder · Open in Terminal
  4. Repository settings… (disabled when missing, tooltip "The repository's
     folder is missing")
  5. Move to group… (not for a worktree shown nested) · Remove from list…
- Linked worktree: Fetch · Pull · Push / Finder · Terminal / Remove worktree….
  No Repository settings…: its remotes are its main repository's.
- The groups are built by a pure function `repoMenuGroups(...)` in a new
  `frontend/src/lib/repoMenu.ts`, returning labelled ids, so the order is
  testable; `RepoRow.svelte` maps them to actions.

## 2. Repository settings dialog

- Store `repoSettings = writable<{ repoID: string } | null>(null)`;
  `RepoSettingsDialog.svelte`, mounted next to `SettingsDialog`, styled like
  it (same modal, tab strip, Close button, Escape closes). Title
  "<repository name> settings". Tabs: **Remotes** only for now; the tab strip
  shows even with one tab so later tabs need no layout change.
- Remotes tab:
  - One row per remote, sorted by name: name (bold), fetch URL (ellipsized,
    full URL in the tooltip), and buttons **Test**, **Edit**, **Remove…**.
  - **Test** runs the connection test (below); while it runs the button
    reads "Testing…" and is disabled; then a line under the row shows
    "Connected" (`--ok`) or the message (`--danger`). The line clears on the
    next Test or edit of that row.
  - **Edit** turns the URL into a text input with **Save** and **Cancel**;
    Enter saves, Escape cancels (Escape in the input does not close the
    dialog). Save with an empty or unchanged URL is disabled.
  - **Remove…** asks: title "Remove remote <name>?", body "Its remote
    branches leave the log, and branches that track it lose their
    upstream.", danger button "Remove".
  - Empty list: "No remotes yet."
  - **Add remote** under the list opens an inline form: Name (prefilled
    `origin` when the repository has no remote, else empty) and URL, with
    **Add** and **Cancel**. Add is disabled while either is empty after
    trimming, or the name contains whitespace, or the name already exists
    ("A remote named <name> already exists" under the field). Other
    problems are git's: its error's first line shows under the form.
  - Every write disables the tab's buttons while it runs (the app's `busy`
    store, as other operations); a git error is shown in the dialog, not as a
    toast. After every successful write the dialog reloads the list and
    calls `refreshRepo()` when the repository is the selected one.
- The dialog closes itself if its repository disappears from the list.

## 3. Go (`internal/app/remotes.go`, `internal/ops/remotes.go`)

- `ops.Remote { Name, FetchURL, PushURL string }` (json `name`, `fetchURL`,
  `pushURL`).
- `ops.ListRemotes(ctx, dir) ([]Remote, error)`: parses `git remote -v`
  (sorted by name; `[]` not nil).
- `ops.AddRemote(ctx, dir, name, url)`: `git remote add -- <name> <url>`.
- `ops.SetRemoteURL(ctx, dir, name, url)`: `git remote set-url -- <name> <url>`.
- `ops.RemoveRemote(ctx, dir, name)`: `git remote remove -- <name>`.
- `ops.TestRemote(ctx, dir, name) (RemoteTest, error)` with
  `RemoteTest { OK bool; Message string }`: `git ls-remote --heads -- <name>`
  with `NoPromptEnv` and a 30 s timeout. OK → Message "Connected". Failure →
  Message "Authentication failed" when `IsAuthError`, "Timed out" on
  timeout, else the error's first line. The error return is only for a
  failure to run at all (bad repository).
- App methods `ListRemotes(id)`, `AddRemote(id, name, url)`,
  `SetRemoteURL(id, name, url)`, `RemoveRemote(id, name)` (the three writes
  through `a.write`, so they lock, show busy and are logged as user
  commands), `TestRemote(id, name)` (a read; not under the write lock).
- Name and URL are trimmed in Go; an empty one returns an error.
- `RemoveRemote` and `SetRemoteURL` clear that remote's background-fetch
  pause: new `autoPause.forget(key, remote string)`. (`AddRemote` needs
  nothing: a new remote is not paused.)

## 4. Spec docs and tests

- `docs/spec/12-repository-settings.md`: the dialog and the Remotes tab, as
  above, in the style of the other spec files.
- `docs/spec/01-repositories-and-sidebar.md`: the repository and worktree
  menus with their groups.
- Go tests (`internal/ops/remotes_test.go`, `internal/app/remotes_test.go`):
  list/add/set-url/remove on a test repository; add of an existing name
  fails with git's error; Test against a bare repository → Connected;
  against the 401 server used by `autopause_test.go` → "Authentication
  failed"; against a missing path → not OK, message non-empty; remove and
  set-url clear the remote's pause.
- vitest: `tidySeparators`, `repoMenuGroups` (main, missing, nested
  worktree, linked worktree), the add-form validation function
  `remoteFormError(name, url, existing)`.

## Process

Each behaviour change commits with its docs/spec update. Native execution,
Opus final review, merge `--no-ff`, rebuild and reopen the app.
