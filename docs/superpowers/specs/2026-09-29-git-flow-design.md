# git-ui — git-flow: start and finish feature, release, hotfix and warmfix — Design

Date: 2026-09-29
Status: Approved design, not implemented
Builds on: `2026-09-17-merge-agent-design.md` (the Merge view and conflict
resolution) and `2026-09-25-repo-toolbar-design.md` (the toolbar).

## Goal

Work the git-flow way inside CommitTree, without SourceTree and without the
`git flow` command-line tool (not installed, and it knows nothing of
warmfix). The owner's work repositories were initialised by SourceTree, so
the feature reads and writes the same `gitflow.*` git config and stays
compatible with it: a branch started in one tool can be finished in the other.

Observed in those repositories (`e2-entity-storage-service`):
`gitflow.branch.master=master`, `gitflow.branch.develop=develop`, prefixes
`feature/ bugfix/ release/ hotfix/ support/`, an empty `versiontag`, and a
`gitflow.branch.<branch>.base` key per started branch. `warmfix/` branches
(sometimes spelled `Warmfix/`) start from a `release/*` branch and go back
into it.

## Scope decisions

- **Everything is local.** Start creates the branch locally; Finish merges
  locally. Nothing is pushed, published or tracked; the existing Push does that.
- **Four types**, each with a base and finish targets:

  | Type | Starts from | Finish merges into (in order) | Ends on |
  |---|---|---|---|
  | feature | develop | develop | develop |
  | release | develop | master, develop | develop |
  | hotfix | master | master, the releases ticked in the dialog, develop | develop |
  | warmfix | a local `release/*` (chosen) | that release | that release |

  A hotfix may also go into any local `release/*`: the Finish dialog lists
  one checkbox per local release, **unticked** by default.
- **No tags** on finish. Tags stay with the existing "New tag".
- **After finish** the local branch is deleted and the ending branch is
  checked out. A remote copy of the branch, if any, is left alone.
- **Merges are always `--no-ff`**, through the existing merge (zdiff3
  markers), with git's own message: `Merge branch 'hotfix/X' into master`.
- **Conflicts: finish is resumable by running it again.** Finish skips every
  target that already contains the branch, so after a conflict is resolved
  and committed in the Merge view, Finish picks up at the next target. No
  state is stored on disk; the repository itself is the state.
- **Up-to-date bases.** Start and Finish fetch first, then fast-forward (only
  fast-forward) every base or target that is behind its upstream. One that
  has diverged stops the operation before anything is merged.
- **UI lives in the toolbar only**: a "Flow" button with a menu. No sidebar
  or log context-menu entries.
- **Out of scope**: bugfix/ and support/ types (their config is preserved,
  not used), publish/track, tags, chat tools for flow.

## Backend: package `internal/gitflow`

### Config

`Read(ctx, dir) (Flow, error)`:

```go
type Flow struct {
    Initialized bool          // gitflow.branch.master and .develop are set
    Problem     string        // e.g. "develop does not exist"; "" when fine
    Master      string
    Develop     string
    Prefixes    Prefixes      // Feature, Release, Hotfix, Warmfix
    Current     *FlowBranch   // the checked-out branch, when it is a flow branch
    Branches    []FlowBranch  // every local flow branch
    Releases    []string      // local release/* branches
}
type FlowBranch struct {
    Name       string // full name, e.g. hotfix/NEXO-1400
    Type       string // feature | release | hotfix | warmfix
    Short      string // name without the prefix
    Base       string // from gitflow.branch.<name>.base, "" when missing
    InProgress bool   // already in some finish target but not all
}
```

- `prefix.warmfix` defaults to `warmfix/` when absent (SourceTree never
  writes it). Other prefixes default to git-flow's (`feature/`, `release/`,
  `hotfix/`).
- A branch's type is found by its prefix, compared **case-insensitively**, so
  `Warmfix/NEXO-39_2` is a warmfix.
- `InProgress` is true when some required finish target (hotfix: master
  and develop only, since releases are optional) has a merge commit whose
  second parent is the branch tip (`rev-list --merges --parents
  --ancestry-path tip..target`). Plain containment is not used: a branch
  just started from develop is contained in develop too. It is what the
  "In progress" group shows after an interrupted finish.
- `Problem` is set when the config names a master or develop branch that does
  not exist; the UI then offers only Init.

`Init(ctx, dir, cfg Config) error` writes `gitflow.branch.master`,
`gitflow.branch.develop` and the four prefixes, and creates develop from
master when develop does not exist. It never touches `bugfix`, `support`,
`versiontag` or `path.hooks`. Master must be an existing local branch.

### Keeping a base up to date

`syncBranch(ctx, dir, branch) (note string, err error)`: when `branch` has an
upstream and is behind it:
- ahead 0 → fast-forward: `merge --ff-only @{u}` when it is checked out,
  otherwise `fetch . <upstream>:<branch>`; note "develop fast-forwarded to
  origin/develop".
- ahead > 0 → `ErrDiverged` ("master has diverged from origin/master; pull
  it first").
No upstream, or not behind → nothing.

### Start

`Start(ctx, dir, typ, name, base string) (StartResult{Branch, Notes}, error)`:
1. Validate: initialised, known type, `check-ref-format --branch
   <prefix><name>`, branch does not exist yet. For warmfix, `base` must be a
   local `release/*`; for the others `base` is ignored and taken from the
   type.
2. `ops.Fetch`; a failure becomes the note "Fetch failed, used local
   branches: <reason>" and Start continues.
3. `syncBranch(base)`.
4. `switch -c <prefix><name> <base>` (uncommitted changes come along, as
   with any new branch), then `config gitflow.branch.<branch>.base <base>`.

### Plan and Finish

`Plan(ctx, dir, branch string, releases []string) (Plan, error)` is read-only
and returns the targets in order, each with `Done bool` (already contains the
branch), plus the ending branch. The Finish dialog shows it.

Target resolution:
- feature → [develop]; release → [master, develop];
- hotfix → [master, …releases (in the given order), develop];
- warmfix → [its `.base`]; with no `.base`, the dialog's chosen release;
  with neither, `ErrNoRelease` ("No release to finish warmfix/X into").

`Finish(ctx, dir, branch string, releases []string) (FinishResult, error)`:

```go
type FinishResult struct {
    Outcome   string   // "finished" | "conflicted"
    Target    string   // the target that conflicted
    Conflicts []string
    Merged    []string // targets merged by this call
    Notes     []string
}
```

1. Preconditions: no merge/rebase/cherry-pick in progress
   (`merge.Status`), no uncommitted changes to tracked files ("Commit or
   stash your changes first"; untracked files do not block).
2. `ops.Fetch` (failure → note, continue).
3. Resolve targets; for every target not yet done, `syncBranch(target)`
   **before any merge**, so divergence stops the finish with nothing changed.
4. For each target in order: skip when it already contains the branch;
   otherwise `ops.Checkout(target)` and `merge.Start(branch)`. `Conflicted`
   → return `{Outcome: conflicted, Target, Conflicts, Merged}` and stop;
   the repository is left on the target with the merge in progress.
5. All done: `ops.Checkout(ending)`, verify once more that every target
   contains the branch, `branch -D <branch>` (the verification is what makes
   the force safe; `-d` would wrongly compare against the branch's own
   upstream), and `config --unset gitflow.branch.<branch>.base`.

An error part-way (a hook rejecting a merge, a checkout failing) returns the
error together with `Merged`; running Finish again continues from there, as
after a conflict. A target checked out in another worktree surfaces as the
existing `ops.ErrCheckedOutElsewhere` ("develop is checked out in <path>").

### App methods (`internal/app/flow.go`)

All writes go through `a.write` (write lock, Command log):
`GetFlow(id) (Flow, error)`, `InitFlow(id, cfg) error`,
`StartFlow(id, typ, name, base) (StartResult, error)`,
`PlanFinish(id, branch, releases) (Plan, error)` (read-only),
`FinishFlow(id, branch, releases) (FinishResult, error)`.

## Frontend

### Toolbar

A `flow` item in the `refs` group, after Merge, labelled "Flow". Disabled
while busy or during any conflict, with the same reasons as Merge. Clicking
it:
- repository not initialised, or `Problem` set → the **Init** dialog;
- otherwise a **menu** anchored under the button:
  1. "Finish <type> <short>…" when `Current` is a flow branch;
  2. "Finish…" when not on a flow branch and local flow branches exist: a
     pick dialog with an "In progress" group first (`InProgress`), then the
     rest grouped by type;
  3. separator, then "Start feature…", "Start release…", "Start hotfix…",
     "Start warmfix…". Warmfix is disabled with "No local release branch"
     when `Releases` is empty.

Pure logic (menu items and reasons, dialog fields, messages) lives in
`lib/flow.ts` with tests; the toolbar item in `lib/toolbar.ts`.

### Dialogs

A new generic `form` dialog kind in DialogHost: a title, an optional static
message, and fields of kind `text`, `select` or `checkbox`; it resolves to the
values or null. Init and Finish need it; Start uses it too so the three look
alike.

- **Init**: Master (select of local branches; `master`, else `main`, else the
  current branch), Develop (`develop`), prefixes feature/release/hotfix/
  warmfix (prefilled from config or defaults). Button "Initialise".
- **Start**: "Name" with the prefix and base shown ("feature/ … from
  develop"); for warmfix a release select when there is more than one local
  release. Button "Start". Result: toast "Started feature/X from develop"
  plus any notes.
- **Finish**: the plan spelled out: "Merge hotfix/X into master (already
  there), develop. Then delete hotfix/X and stay on develop." For a hotfix,
  one unticked checkbox per local release, "Also merge into release/…",
  and ticking one re-plans. For a warmfix without `.base`, a release select.
  Button "Finish".

### After Finish

- `finished` → toast "Finished hotfix/X: merged into master, develop; branch
  deleted" plus notes; refresh refs, log and working tree.
- `conflicted` → the Merge view opens exactly as for a conflicted Merge. The
  frontend remembers the interrupted finish **in memory only**
  (`{repoId, branch, releases}`); the Merge view shows the line "Part of
  finishing hotfix/X (into develop)". When the merge is committed, a toast
  with the action "Continue finishing hotfix/X" runs Finish again without
  re-asking. Aborting the merge forgets it; the branch still appears under
  "In progress".
- Errors → an error toast with the message; when `Merged` is non-empty the
  toast says what was already merged.

## Errors

| Case | Behaviour |
|---|---|
| Invalid or existing branch name on Start | error before anything runs |
| Base or target diverged from upstream | stops before the first merge |
| Fetch fails | continues with local branches, note in the result |
| Tracked uncommitted changes on Finish | "Commit or stash your changes first" |
| Conflict or sequencer already in progress | blocked by the toolbar and by Finish |
| Target checked out in another worktree | `ErrCheckedOutElsewhere` message |
| Warmfix with no base and no release | "No release to finish warmfix/X into" |
| Error part-way | error plus what was merged; Finish again continues |
| Config names a missing master/develop | `Problem`; only Init is offered |

## Testing

- **Go, `internal/gitflow`** on `testrepo` repositories with a bare
  upstream: config read/write and defaults, case-insensitive `Warmfix/`,
  `InProgress`; Start per type (base, `.base` key, fast-forward from
  upstream, diverged → error, fetch failure → note); Plan; Finish per type
  (targets and order, skipping done targets, conflict → `conflicted` on the
  right target, finishing again after the commit deletes the branch and the
  `.base` key, hotfix with and without a release, warmfix into its release,
  dirty tree refused).
- **Vitest**: `lib/flow.ts` (menu, reasons, In progress, form values,
  messages), the toolbar `flow` item, the `form` dialog.
- **Disposable QA**: a Go test over copies of `/tmp` demo repositories given
  SourceTree-style config, running the whole flow through the App methods
  (start → commits → finish with a conflict → resolve → finish again);
  deleted afterwards.
- **Docs**: `docs/spec/10-git-flow.md`, plus the README index and the
  toolbar description, in the same commit as the change.
