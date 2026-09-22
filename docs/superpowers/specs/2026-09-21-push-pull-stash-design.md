# git-ui — Sub-project 2b: Push, pull and stash — Design

Date: 2026-09-21
Status: Design
Builds on: `2026-09-21-working-tree-design.md` (sub-project 2a: staging and
commit, merged to `main`) and `2026-09-17-merge-agent-design.md` (merge view
and conflict resolution, merged to `main`).

## Goal

Push and pull with a remote, including handling an unset upstream and a
rebase-strategy pull that conflicts, and full stash support (push, apply,
pop, drop, preview). Together with 2a this closes the loop: an ordinary
day's work — edit, stage, commit, sync with the remote, stash something
half-finished — no longer needs a terminal at any point.

## Scope decisions

- **Split from sub-project 2.** This is 2b, following 2a (file-level
  staging and commit).
- **Push and Pull live in a toolbar**, always visible, not buried in the
  branch context menu. Fetch is a separate, third button.
- **Pull strategy is a three-way app setting**: `auto` (default) reads
  git's own resolved `pull.rebase` (repo config, then global, then merge if
  neither is set) with no app-level override; `merge`/`rebase` force the
  app's choice regardless of git config.
- **A rebase-pull that conflicts reuses the existing merge view**,
  generalized to also detect a rebase in progress, rather than a second,
  parallel conflict UI. See "Generalizing the conflict view" below.
- **First push of a branch with no upstream** runs `git push -u origin
  <branch>` — the conventional remote name, no prompt for which remote.
- **Force-push is out of scope**, deferred like hunk-level staging was in
  2a.
- **Stash is full CRUD**: push (message + include-untracked), list, apply,
  pop, drop, and a read-only diff preview — in its own sidebar section,
  the same shape as Tags/Branches.
- **A stash apply/pop that conflicts also reuses the merge view.** Unlike
  merge and rebase, git leaves no persistent marker for a stash conflict
  (no `MERGE_HEAD`, no `rebase-merge` directory) — see below.
- **Out of scope:** force-push, general interactive rebase (reorder,
  squash, edit — only the rebase a rebase-pull triggers is in scope),
  multiple-remote UI beyond the `origin` convention for auto-set-upstream,
  stash's `--patch`/hunk selection.

## Architecture

### `internal/ops` (extend — Fetch and Pull already exist here)

`ops.Fetch` and `ops.Pull` were already built (`internal/ops/ops.go`), ahead
of any UI calling them: `Fetch` runs `git fetch --all --prune` under
`gitcmd.NetworkTimeout` unchanged. `Pull` today is `git pull --ff-only`,
returning `ErrNotFastForward` on any divergence — this is the part 2b
replaces, since a real pull must merge or rebase, not just refuse. There is
no `Push` anywhere yet.

```go
type AheadBehind struct {
    Ahead  int `json:"ahead"`
    Behind int `json:"behind"`
}

// Outcome is Pull's own enum, distinct from merge.Outcome: a pull can
// additionally be Rebased, which a plain merge never is.
type Outcome int

const (
    UpToDate Outcome = iota
    Merged
    Rebased
    Conflicted
)

// Result mirrors merge.Result's shape for a pull's outcome.
type Result struct {
    Outcome   Outcome  `json:"outcome"`
    Conflicts []string `json:"conflicts"` // set only when Conflicted
}

func Push(ctx context.Context, dir string) error
func Pull(ctx context.Context, dir, strategy string) (Result, error) // strategy replaces the old ff-only-only signature
func Counts(ctx context.Context, dir string) (AheadBehind, error)
```

- `ErrNotFastForward` and its test (`TestPullRefusesDivergedHistory`) are
  removed — a diverged upstream is now Pull's normal job, resolved by merge
  or rebase, not a refusal.
- `Push` checks `@{upstream}` the same way `worktree.Preview` already does
  (the symbolic form, never an abbreviated remote-tracking name a local
  branch could shadow). No upstream → `git push -u origin -- <branch>`.
  Upstream present → `git push --`.
- `Pull("auto", ...)` runs plain `git pull --` — no `-c` override, so git's
  own resolved config decides. `Pull("merge", ...)` runs `git -c
  pull.rebase=false pull --`; `Pull("rebase", ...)` runs `git -c
  pull.rebase=true pull --` — the same one-shot `-c` pattern `merge.Start`
  already uses for `merge.conflictStyle`.
- `Counts` runs `git rev-list --left-right --count HEAD...@{upstream}` and
  parses the two numbers; no upstream yields a zero `AheadBehind` and no
  error, the same convention `worktree.Preview` uses for `Upstream`.
- Push and pull already run (and Push will run) under `gitcmd.NetworkTimeout`
  (5 minutes); `Counts` uses `gitcmd.ReadTimeout` like other reads.
- `git rebase --continue` (below) can prompt an editor if a replayed commit
  becomes empty or needs its message changed; that call sets `GIT_EDITOR`
  to a no-op, the same spirit as `GIT_TERMINAL_PROMPT=0` already disabling
  credential prompts.

### Generalizing the conflict view (`internal/merge`)

Rather than a parallel package duplicating `state.go`'s unmerged/manual/
text-vs-binary detection (~300 lines), `merge.State` gains a `Kind` field
and `Status`'s detection widens to recognise three sources of conflict:

```go
type Kind string

const (
    KindMerge      Kind = "merge"
    KindRebase     Kind = "rebase"
    KindCherryPick Kind = "cherry-pick"
    KindRevert     Kind = "revert"
    KindAM         Kind = "am"
    KindStash      Kind = "stash"
)

type State struct {
    Kind      Kind     `json:"kind"`
    Merging   bool     `json:"merging"` // kept for existing callers; true for any Kind
    From      string   `json:"from"`
    Into      string   `json:"into"`
    Conflicts []string `json:"conflicts"`
    Manual    []string `json:"manual"`
    Staged    []string `json:"staged"`
    Unstaged  []string `json:"unstaged"`
    // Step and Total are set only for KindRebase: "commit 2 of 5".
    Step  int    `json:"step,omitempty"`
    Total int    `json:"total,omitempty"`
    Subject string `json:"subject,omitempty"` // the replayed commit's subject
}
```

Detection runs in this order, and the order is load-bearing (the plan audit
of 2026-09-21 found the original three-value version misreporting a
conflicted cherry-pick as a stash conflict, which made Continue and Abort
silent no-ops):

- `CHERRY_PICK_HEAD` present → `KindCherryPick`; `REVERT_HEAD` →
  `KindRevert`. Both come first: they leave unmerged entries and no
  `MERGE_HEAD`, exactly like a conflicted stash.
- `MERGE_HEAD` present → `KindMerge`, exactly today's detection.
- `.git/rebase-merge`, or `.git/rebase-apply` without an `applying` file →
  `KindRebase`. Step and total come from `rebase-merge/msgnum` and
  `rebase-merge/end`; the subject comes from `git log -1 --format=%s
  REBASE_HEAD`.
- `.git/rebase-apply` **with** an `applying` file → `KindAM`. `git am` shares
  that directory with the old rebase backend, and running `rebase --abort`
  on it destroys the mailbox.
- None of the above, but unmerged index entries exist → `KindStash`. This is
  the only markerless case, which is why it must be last.
- `Stage`, `Unstage` and `Take` are unchanged — they already work purely
  off `Status`'s `Staged`/`Unstaged`/`Conflicts` lists, never off `Kind`.
- A new `Continue(ctx, dir)` replaces the direct call to `merge.Commit` at
  the call site: `KindMerge` → `merge.Commit`; every other kind but
  `KindStash` → `git <kind> --continue` (`rebase`, `cherry-pick`, `revert`,
  `am`) run with `GIT_EDITOR=true` **in the environment** — `-c
  core.editor=true` is not enough, because git resolves `GIT_EDITOR` first
  and a value inherited from the user's shell would open a real editor the
  app cannot close. The caller re-runs `Status` afterward — if still
  `KindRebase`, the next commit's conflicts are ready; if clean, the
  rebase finished. `KindStash` has no "continue"; finishing just means no
  unmerged entries remain, after which the app drops the stash if it was a
  `Pop` (see below).
- `Abort(ctx, dir)` becomes kind-aware too: `KindMerge` → `git merge
  --abort` (as today); every other kind but `KindStash` → `git <kind>
  --abort`; `KindStash` has
  nothing to abort at the git level — the frontend's "Abort" for a stash
  conflict is really "leave it, resolve later" and does nothing.
- The package keeps its name, `merge` — renaming to something like
  `conflict` would touch every import (`MergeView.svelte`, the app layer,
  the agent tools) for a label; not worth the churn for 2b.

### `internal/stash` (new)

```go
type Entry struct {
    Index   int    `json:"index"`
    Message string `json:"message"`
    Branch  string `json:"branch"`
    Hash    string `json:"hash"`
}

func List(ctx context.Context, dir string) ([]Entry, error)
func Push(ctx context.Context, dir, message string, includeUntracked bool) error
func Apply(ctx context.Context, dir string, index int) error
func Pop(ctx context.Context, dir string, index int) error
func Drop(ctx context.Context, dir string, index int) error
func Diff(ctx context.Context, dir string, index int) (string, error)
```

- `List` parses `git reflog show --format=%gd%x00%s%x00%H refs/stash`, after
  checking `refs/stash` exists at all (a repository that never stashed has no
  such ref and `reflog show` fails). **Not** `git stash list --format=…`:
  `stash list` ignores `--format`, `--pretty` and `-z` entirely — verified
  against git 2.54, it always prints `stash@{0}: On main: wip` — so the
  hash can never be parsed out of it. The placeholders below are what
  `reflog show` gives (`%gd` gives the
  `stash@{N}` ref, decomposed to `Index`; `%s` is stash's own subject line,
  which already encodes the branch it was taken from — parsed the way
  `mergeFrom` already parses git-generated text elsewhere).
- `Push` returns `ErrNothingToStash` when git prints "No local changes to
  save" (it exits 0 in that case, so an unchecked call is a silent no-op the
  UI would report as success). Otherwise it runs `git stash push -m <message>` (`--include-untracked` when
  requested), refusing (button disabled, mirroring Commit's
  nothing-staged rule) when the worktree is clean.
- `Apply`/`Pop` run `git stash apply stash@{N}` / `git stash pop
  stash@{N}`. On conflict, `merge.Status` picks it up as `KindStash` on
  the next call, exactly as it does for a mid-merge or mid-rebase
  repository — no separate signal is needed because "unmerged entries
  exist" is the detection.
- `Pop`'s conflict case needs the app to remember which stash index a
  drop is still owed once resolved, because git itself does not persist
  that intent anywhere (see "Risks"). This is tracked in-memory on the
  `App` per repo ID, alongside the existing per-repo write lock — not
  written to disk. If the app restarts mid-resolution, the leftover
  stash entry has to be dropped manually from the stash list; the
  worktree state itself is never at risk, only the tidiness of dropping
  the entry.
- `Diff` reuses `worktree`'s cap/binary-detection helpers against `git
  stash show -p stash@{N}`.

### App layer (`internal/app`)

- `Fetch(id)` is unchanged. `Pull(id)` (already exists, calling the old
  ff-only `ops.Pull`) is updated to call the new `ops.Pull(ctx, dir,
  strategy)`, reading the strategy from the new settings package once per
  call. `Push(id)` and `GetRemoteInfo(id)` (ahead/behind counts) are new.
- `GetStashEntries(id)`, `StashPush(id, message, includeUntracked)`,
  `StashApply(id, index)`, `StashPop(id, index)`, `StashDrop(id, index)`,
  `GetStashDiff(id, index)`.
- `ContinueConflict(id)`, `AbortConflict(id)` replace the merge view's
  direct commit/abort calls, dispatching on `merge.State.Kind`.
- Every mutation runs under the existing per-repo write lock — fetch,
  push and pull hold it for the network call's duration, same as a slow
  commit hook already can under `HookTimeout`. Success emits the existing
  `EventWorktreeChanged` (refs, ahead/behind counts and the stash list are
  all refreshed by the one event, same as 2a lumped staged/unstaged/
  untracked together) plus the existing log-refresh path, since a pull or
  a rebase continue can move `HEAD`.

### `internal/gitsettings` (new)

A general git-behaviour settings package, sibling to `ai/settings` but its
own file (`git.json`) — `ai/settings` stays AI-config only. Named
`gitsettings`, not `settings`, so a file that needs both never needs an
import alias for either.

```go
type Settings struct {
    PullStrategy string `json:"pullStrategy"` // auto | merge | rebase
}
```

A new "Git" section in the existing Settings dialog exposes `PullStrategy`
as a three-way choice, the same control shape 2a used for `commitMessage`.

## Frontend

- **Toolbar (new component):** Fetch, Pull, Push, each disabled while
  `$busy`; Pull and Push are additionally disabled while
  `merge.State.Merging` (any `Kind`) is true — Fetch stays available, since
  it never touches the worktree or the index. Push and
  Pull show an ahead/behind badge (`↑2 ↓1`) from `remote.Counts`. No
  per-click strategy chooser on Pull — the Settings dialog controls that,
  matching how `commitMessage`'s auto/manual already works without an
  inline picker.
- **Sidebar — Stash section**, same shape as Tags/Branches: collapsible,
  a count badge, each row showing message/branch/relative time. A hover
  action row (Apply, Pop, Drop); clicking a row opens its diff read-only
  in the main pane, the same way previewing a commit already does.
- **Changes view** gains a "Stash…" action beside Commit — a message
  field and an include-untracked checkbox — for stashing instead of
  committing.
- **`MergeView.svelte` generalization:** reads `merge.State.Kind` to swap
  the header text ("Merging `x` into `y`" / "Rebasing `x` onto `y`,
  commit 2 of 5" / "Resolving stashed changes") and the bottom action bar
  ("Commit merge" / "Continue rebase" + "Abort" / "Done" + a conditional
  "Drop stash" when the pending pop is still owed). `FileList`, the diff
  pane and the AI resolver are untouched.
- **Routing:** the existing "Changes row routes to the merge view during
  a merge" rule extends to a rebase or a stash conflict — same
  precedence, three trigger conditions instead of one.

## Errors

| Case | Behaviour |
|---|---|
| Push/pull/fetch network or auth failure | git's stderr in a toast, verbatim (`GIT_TERMINAL_PROMPT=0` already makes a missing credential fail fast rather than hang) |
| Push rejected (remote diverged, non-force) | git's rejection message verbatim — no app-invented "pull first" copy |
| Pull/rebase conflict | Routes into the shared conflict view instead of surfacing as an error |
| Stash apply/pop conflict | Routes into the shared conflict view, `Kind: "stash"` |
| Stash push with nothing to stash | Button disabled, mirroring Commit's disabled-when-nothing-staged rule |
| Repository mid-merge/rebase/stash-conflict | Fetch stays enabled; Push, Pull and Stash push stay disabled until resolved |

## Testing

- **Go, against real temp repositories:** a local+remote pair (a bare repo
  as the remote) for push/pull/fetch round-trips, including the no-
  upstream first push; a rebase-pull that conflicts, resolved via
  `Continue`, and one aborted; a stash push/apply/pop that conflicts,
  resolved, and confirmed dropped for the `Pop` case; `Counts`' ahead/
  behind arithmetic including the no-upstream zero case. The existing
  `TestFetchAndPullFastForward` and `TestPullRefusesDivergedHistory` in
  `internal/ops/ops_test.go` are rewritten: the first still exercises the
  fast-forward case (now via the default `auto` strategy), the second is
  replaced by tests that a diverged pull actually merges or rebases
  instead of refusing.
- **Vitest, on the pure parts:** pull-strategy resolution, toolbar
  disabled-states, stash-list parsing/grouping.
- **Manual, added to the pending list** (same as 2a): a real push/pull
  against an actual remote — temp repos exercise git's plumbing but not
  real auth/network failure modes.

## Risks

- **Generalizing `merge.State`** touches code the AI conflict resolver and
  agent tools already depend on. Mitigated by keeping `Stage`/`Unstage`/
  `Take`'s signatures and behaviour unchanged, and running the existing
  merge test suite unmodified as a regression check before adding the
  rebase and stash paths.
- **Stash-conflict "owed drop" tracking has no git-side persistence** — an
  app restart mid-resolution loses the reminder to drop the stash entry,
  though never the underlying changes. Accepted limitation, recorded here
  rather than over-building for an edge case, matching how 2a recorded
  its own residual minors.
- **`git rebase --continue` can prompt an editor.** Mitigated by a no-op
  `GIT_EDITOR` for that call only.

## Out of scope / later

- Force-push.
- General interactive rebase (reorder, squash, edit commits).
- Stash `--patch`/hunk selection.
- Multiple-remote UI beyond the `origin` convention for auto-set-upstream.
