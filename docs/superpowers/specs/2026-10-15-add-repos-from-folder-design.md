# Add repositories from a folder — design

Date: 2026-10-15. Status: approved in conversation, awaiting written-spec review.

## Goal

Add many repositories in one step. The user picks a parent folder (for
example `~/projects`, holding a dozen services); CommitTree lists the git
repositories directly inside it and the user ticks which ones to add. Today
the only ways are **Open folder…** (one at a time) and **Clone…**.

Success: from an empty sidebar, ten repositories that live side by side in
one folder are added with one dialog, optionally straight into a group.

## Decisions (from the brainstorm)

- **Direct children only.** The scan looks at the parent's immediate
  subfolders; it never descends further. The parent itself is not offered,
  even when it is a repository (Open folder… covers that).
- **Group is chosen in the dialog**, default "No group": any existing group,
  or "New group…" with the parent folder's name suggested.
- Already-listed repositories are shown, disabled, labelled "Already added".
- No `git` call during the scan: it must be instant for a folder with
  hundreds of entries. `git rev-parse --show-toplevel` runs only when adding,
  as Open folder… does.

## User-visible behaviour

### Entry point

"Add repo" opens a menu with **Open folder…**, **Add from folder…** and
**Clone…**, in that order.

### The dialog

Title "Add repositories from a folder". From top to bottom:

1. **Parent folder** field with **Choose…** and **Scan** buttons. It
   behaves like the Clone dialog's parent field: typed or picked; trimmed; a
   leading `~` or `~/` expanded; a relative path refused with "Parent folder
   must be a full path (or start with ~)."; the same folder suggestions while
   typing. Initial value: the folder last scanned successfully (remembered
   across launches), else the home folder. The dialog does not scan on open.
   A scan starts on **Scan**, on Enter in the field (when no suggestion is
   highlighted), and right after Choose… returns a folder. **Scan** is
   disabled while the field is empty (after trimming) and reads "Scanning…"
   while a scan runs.
2. **Result list** (after a scan): one row per repository found, sorted by
   name (case-insensitive), each with a checkbox and the folder name; the
   full path is the row's tooltip. Rows of repositories not yet in the
   sidebar start ticked. Rows already in the sidebar are unticked, disabled
   and labelled "Already added". Above the list, **Select all** / **None**
   act on the enabled rows only. Below the list, when any were skipped: "N
   folders skipped (not git repositories)." (singular "1 folder skipped (not a
   git repository).").
3. **Group**: a select with "No group" (default), every existing group name
   (sorted), and "New group…". Choosing "New group…" shows a text field
   prefilled with the parent folder's name; an empty (after trimming) name
   disables Add.
4. Buttons: **Cancel** and **Add N repositories** ("Add 1 repository";
   disabled with "Add repositories" when nothing is ticked or no scan
   result is shown).

Editing the parent field after a scan clears the result list (it no longer
matches the field). A second scan replaces the list; only the latest scan's
answer counts.

Empty and error states, shown in place of the list:

- No repositories among the children: "No git repositories in this folder."
  (plus the skipped line when relevant).
- Parent does not exist: "That folder doesn't exist."
- Parent is not a folder: "That path isn't a folder."
- Parent cannot be read: "That folder can't be read: " followed by the
  system's reason.

### Adding

**Add** adds the ticked repositories in list order, with the chosen group,
in one store write. While it runs the buttons are disabled ("Adding…").
Then:

- All added: the dialog closes, the sidebar reloads, a toast says "Added N
  repositories." (or "Added 1 repository."). Selection does not change.
- Some failed (e.g. a folder deleted since the scan): the dialog closes,
  the sidebar reloads with the ones that were added, and an error toast says
  "Added N repositories; M couldn't be added: name (reason), …" (at most
  three named, then "and K more").
- Nothing new because every ticked repository was already listed: the
  dialog closes, toast "Nothing added: those repositories are already in the
  list."
- None added because all failed: the dialog stays open with the error
  inline: "No repository could be added: name (reason), …" (same naming
  rule).
- The store write itself fails: nothing is added (the batch is rolled back);
  the dialog stays open with the error inline.

A ticked repository that turns out to be listed already (resolved top-level
path equal to an existing entry, e.g. added from elsewhere in the meantime)
counts as neither added nor failed: it is left as it is, group untouched.
Two ticked rows that resolve to the same top-level are added once.

## What counts as a repository

A direct child of the parent is a candidate when all hold:

- its name does not start with `.`;
- it is a folder, or a symlink to a folder (followed with `os.Stat`);
- `<child>/.git` exists and is a **directory** (`os.Stat`).

A child whose `.git` is a file (a linked worktree or a submodule checkout)
is skipped: linked worktrees already appear under their main repository.
Every other non-hidden child (plain folder, file, broken symlink, child
without `.git`, unreadable child) counts towards "skipped". Hidden children
are neither listed nor counted.

"Already added" compares the candidate's path, symlinks resolved, against
every stored entry's path, symlinks resolved (the same `canonical()` the app
uses elsewhere), so `/tmp/x` and `/private/tmp/x` match.

## Architecture

### Go

**`internal/scanrepos`** (new, no git calls, no app state):

```go
type Candidate struct {
    Name string `json:"name"`
    Path string `json:"path"` // parent joined with the child's name
}
type Result struct {
    Parent  string      `json:"parent"`  // expanded, cleaned
    Repos   []Candidate `json:"repos"`   // sorted by name, case-insensitive; never nil
    Skipped int         `json:"skipped"`
}
var (
    ErrParentMissing  // "That folder doesn't exist."
    ErrParentNotDir   // "That path isn't a folder."
    ErrParentUnreadable // wraps the OS error: "That folder can't be read: …"
)
func Scan(parent string) (Result, error)
```

Parent expansion is shared with the Clone dialog: `clone.expandParent` is
exported as `clone.ExpandParent` and `scanrepos` calls it (same errors and
messages as today — "Parent folder must be a full path (or
start with ~)." and the home-unknown one). Clone's behaviour must not change.

**`repos.Store.AddMany(ctx, paths []string, group string) (added []Repo, failed []AddFailure, err error)`**

- `AddFailure{Path, Name, Reason string}`; `Name` is the path's base name.
- For each path: `toplevel` (git rev-parse); on error → failed, continue.
- Skips a top-level already stored or already added earlier in this batch.
- New entries get `uniqueID`, `Name = base(top)`, `Group = group` (group is
  trimmed; "" means ungrouped).
- One `save()` at the end, only if something was added. If it fails, the
  in-memory list is restored and `err` is returned (added and failed nil).
- `toplevel` runs outside the lock (as `Add` does today); the duplicate check
  and append happen under it.
- `Add` keeps its signature and documented behaviour (returns the existing
  entry when already stored) and shares the per-path logic with `AddMany`,
  so the two never disagree on what a duplicate is.

**`internal/app` bindings** (new file `addrepos.go`):

- `ScanReposIn(parent string) (ScanResult, error)` → `scanrepos.Scan` plus
  `Added bool` per candidate (canonical path comparison against the store).
  `ScanResult` mirrors `scanrepos.Result` with `Repos []ScanCandidate{Name,
  Path, Added}`.
- `AddRepos(paths []string, group string) (AddReposResult, error)` →
  `AddReposResult{Added []repos.Repo, Failed []repos.AddFailure}`, both
  never nil; the store-write error is returned as the binding's error.
- `PickReposParent(start string) (string, error)` → directory dialog titled
  "Add repositories from folder", starting at `start`; "" on cancel.
- `DefaultReposParent()` → home folder (reuse the clone helper behind
  `DefaultCloneParent`).

No write lock and no command-log entry: like Open folder…, adding only
touches `repos.json`.

### Frontend

- **`lib/addRepos.ts`** (pure, vitest): the dialog's view state and
  transitions — `fromScan(result)` (initial ticks), `toggle`, `selectAll`,
  `selectNone`, `ticked(state)` (paths in list order), `addLabel(n)`,
  `skippedLine(n)`, `summarize(result)` (toast kind + text, including the
  "at most three named" rule), `groupOptions(existing)`, and the
  "latest scan wins" token.
- **`FolderField.svelte`** (extracted from `CloneDialog.svelte`): the parent
  text field + Choose… + suggestions listbox using `lib/pathcomplete.ts`.
  Props: value (bindable), pick function, input id/label; emits `enter`
  when Enter is pressed with no highlighted suggestion, and `picked` after
  Choose… returns a folder. CloneDialog switches to it with no behaviour
  change (its existing tests and spec must still hold).
- **`AddReposDialog.svelte`**: the dialog above; mounted in `App.svelte`
  next to `CloneDialog`; open state in a store (`addReposDialogOpen`).
- **Remembered folder:** `persisted<string>('reposParent', '')` in
  `stores.ts`, set only after a scan succeeds.
- **`actions.ts`**: `openAddReposDialog()`; `addReposFromFolder(paths,
  group)` calls the binding, reloads repos, shows the toast.
- **Sidebar.svelte**: the menu gains "Add from folder…" between the other
  two.
- `api.ts` + regenerated `wailsjs` bindings.

## Error handling summary

| Situation | Result |
|---|---|
| Relative parent | inline field error, no scan |
| Parent missing / not a folder / unreadable | inline message in the list area |
| Child unreadable or broken symlink | counted as skipped |
| Folder vanished between scan and Add | that path in `failed`, others added |
| All ticked failed | dialog stays open, inline error listing them |
| `repos.json` write fails | batch rolled back, inline error, nothing added |
| Ticked repo already listed | silently left as is (group untouched) |

## Testing

- **`scanrepos`** (tmpdir): a repo (`.git` dir), a plain folder, a file, a
  hidden repo (`.hidden/.git`, not listed, not counted), a linked worktree
  (`.git` file, skipped), a symlink to a repo (listed), a broken symlink
  (skipped), sorting case-insensitively, `~` expansion, relative parent,
  missing parent, parent that is a file, empty parent folder (`Repos` is
  `[]`, not nil).
- **`repos.AddMany`**: adds several with the group; a path already stored
  (left unchanged, group untouched, not in added/failed); two paths with
  the same top-level (added once); a non-repo path (in failed, others
  added); a subfolder path records the top-level; write failure rolls back
  (e.g. store path in a read-only dir); `Add` still returns the existing
  entry for a duplicate.
- **app bindings**: `ScanReposIn` marks `Added` with a symlinked spelling of
  a stored path; `AddRepos` never returns nil slices.
- **`clone`**: existing tests still pass after the expansion helper moves.
- **vitest `addRepos.test.ts`**: initial ticks skip already-added rows,
  select all/none ignore disabled rows, labels and plurals, summarize with
  0/1/3/5 failures, group options, latest-scan-wins.
- Manual: real `~/projects`-like folder; Clone dialog's parent field still
  suggests and completes.

## Spec updates

`docs/spec/01-repositories-and-sidebar.md`: the "Add repo" menu sentence
lists the three items; new subsection "Adding repositories from a folder"
after "Cloning a repository" with the behaviour above (dialog, what counts
as a repository, already added, group, results and errors).

## Out of scope

- Recursive scanning or a depth setting.
- Adding the parent folder itself, bare repositories, or linked worktrees.
- Drag-and-drop of folders onto the sidebar.
- Watching the folder for new repositories later.
