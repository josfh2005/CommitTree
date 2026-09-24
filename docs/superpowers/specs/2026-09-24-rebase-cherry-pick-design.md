# git-ui — Rebase and cherry-pick — Design

Date: 2026-09-24
Status: Draft — design approved in conversation 2026-09-24, awaiting spec review
Builds on: `docs/spec/04-conflicts.md`, `docs/spec/02-log-and-history.md`,
`docs/spec/01-repositories-and-sidebar.md`, `docs/spec/06-ai.md`, the merge
agent design (`2026-09-17-merge-agent-design.md`), the chat write tools design
(`2026-09-23-chat-write-tools-design.md`).

## Goal

Sub-project 4 promised merge, rebase and cherry-pick with an AI conflict
resolver. Merge is done. For rebase and cherry-pick, the Conflicts view
already *recognises* one in progress (header, step count, Continue, Abort),
but only if it was started from a terminal: git-ui has no way to start
either, and "Resolve with AI" refuses anything but a merge because a run ties
itself to `MERGE_HEAD`.

This design adds:

1. **Starting** a plain rebase (current branch onto a branch or a commit) and
   a single-commit cherry-pick from the UI.
2. **The AI resolver for rebase and cherry-pick** conflicts, one step at a
   time, by generalising what a run ties itself to.
3. **Named sides** in the Conflicts view and in what the resolver reads, so
   nobody picks the wrong side when a rebase swaps "ours" and "theirs".
4. **Skip this commit** for rebase and cherry-pick, so an emptied step does
   not leave the user stuck.
5. A **`cherry_pick` chat write tool**, confirmed by an approval card.

## Decisions

Decided by the owner during brainstorming:

- **Plain rebase only.** Interactive rebase (reorder, squash, fixup, drop,
  reword) is a separate, later sub-project.
- **Single-commit cherry-pick** from a log row's context menu. No
  multi-select in the log, no ranges.
- **The AI resolver is in scope** for rebase and cherry-pick, one step at a
  time. It never continues, skips or aborts.
- **Uncommitted changes refuse the operation** with "Commit or stash your
  changes first". No autostash, no stash-and-continue button.
- **Rebase confirmation warns, never blocks**, when commits to be replayed
  are already on the branch's upstream (the user will need a force-push,
  which git-ui does not offer), and when merge commits in the range will be
  flattened.
- **Rebase is offered from both** the sidebar branch menu and the log row
  menu.
- **Cherry-pick asks for confirmation** too.
- **The chat may propose a cherry-pick** (approval card); rebase stays on the
  chat's "not offered" list because it rewrites history.
- **Operation identity is a fingerprint** `kind:commit`, replacing the
  resolver's `MERGE_HEAD` check.
- **Start logic lives in `internal/merge`**, next to `merge.Start`, since that
  package already owns Status/Continue/Abort for every conflicted kind.

## User flows

### Starting a rebase

Entry points:

- Sidebar branch row menu: **"Rebase `<head>` onto `<branch>`"**, directly
  below "Merge `<branch>` into `<head>`". Works for local and remote-tracking
  branches.
- Log row menu: **"Rebase `<head>` onto this commit"**.

The entry is disabled, with the reason as a tooltip (same pattern as Merge and
Reset), when:

| Reason | Tooltip |
|---|---|
| Target is the current branch | "This is the current branch" |
| `HEAD` already contains the target (target is an ancestor of `HEAD`) | "`<head>` already contains `<target>`" |
| Head is detached | "No branch is checked out" |
| A write is running | "Another operation is running" |
| Any conflicted operation is in progress (any kind `merge.Status` reports) | "Finish the `<kind>` in progress first" |

Uncommitted changes do **not** disable the entry (checking the working tree on
every menu open is not worth it); they are refused on click — see below.

Clicking opens a confirmation built from `RebasePreview`:

> Rebase **feature** onto **main**? 3 commits will be replayed on top of `main`.
>
> ⚠ 2 of these commits are already on `origin/feature`. After rebasing you'll
> need to force-push, which git-ui doesn't do.
>
> ⚠ 1 merge commit in this range will be flattened.

Each warning appears only when its count is non-zero. The upstream warning
needs the branch to have an upstream; with none, it is skipped. Confirming
runs the rebase.

### Starting a cherry-pick

Entry point: log row menu, **"Cherry-pick onto `<head>`"**.

Disabled, with a tooltip, when: the commit is already contained in `HEAD`
(ancestor), it is a merge commit ("Cherry-picking a merge commit isn't
supported"), the head is detached, a write is running, or any conflicted
operation is in progress.

Confirmation: "Cherry-pick `a1b2c3` *fix login* onto **feature**?"

### Uncommitted changes

Both operations check, before running git, whether the working tree has any
**tracked** change, staged or unstaged. If it does, they refuse with
"Commit or stash your changes first" and git is never invoked. Untracked
files do not block; if one would be overwritten, git refuses on its own and
that error is shown as-is.

### Outcomes

| Outcome | What the user sees |
|---|---|
| Finished cleanly | A short notice: "Rebased feature onto main — 3 commits" / "Cherry-picked a1b2c3 onto feature". Log and sidebar refresh. |
| Nothing to do (rebase) | "Already up to date". |
| Nothing to apply (cherry-pick) | "Nothing to apply: those changes are already on `feature`". git stops an emptied cherry-pick with `CHERRY_PICK_HEAD` and no unmerged paths; the app runs `cherry-pick --skip` itself so the user is never left in that state. |
| Conflicts | The Conflicts view takes over (it already does for these kinds), with "Resolve with AI" available. |
| Error | git's stderr, and no half-started operation left behind (see Error handling). |

## The Conflicts view

### Named sides

"Take ours / Take theirs" on manual files becomes "Take `<name>`", with the
name of what each side actually is:

| Kind | git's "ours" | git's "theirs" | Menu reads |
|---|---|---|---|
| Merge | current branch | branch being merged in | Take `feature` / Take `main` |
| Rebase | branch being rebased onto (onto) | the commit being replayed | Take `main` / Take `a1b2c3 fix login` |
| Cherry-pick | current branch | the picked commit | Take `feature` / Take `a1b2c3 fix login` |

A branch side is named by its branch; with none (detached, or an anonymous
onto), by its short hash. A commit side is "short hash + subject" truncated
to fit. The confirmation for taking a side uses the same names. Revert, am
and stash conflicts keep "ours / theirs" (out of scope).

The labels come from the backend (`State` gains `OursLabel`, `TheirsLabel`),
so the view and the resolver say the same thing.

### Skip this commit

For rebase and cherry-pick only, a secondary action **"Skip this commit"**
sits next to Continue and Abort. It asks for confirmation ("`a1b2c3` *fix
login* will not be applied. Its changes are dropped from the result.") and
runs `rebase --skip` / `cherry-pick --skip`. Like Continue and Abort, it stops
any AI resolver run first, and the view re-reads the repository afterwards.

It is always available during those two kinds (not only once settled),
because skipping is how a user gets out of a step they don't want at all.

Continue failing because the step came out empty (git: "No changes — did you
forget to use 'git add'?" / "The previous cherry-pick is now empty") shows
that message with a "Skip this commit" button in it.

### Resolve with AI

"Resolve with AI" is offered for merge, rebase and cherry-pick. Revert, am
and stash still don't get it.

## The AI resolver

### Tying a run to its operation

`merge.Fingerprint(dir)` returns `"<kind>:<commit>"`, or `""` when nothing is
in progress:

| Kind | Commit |
|---|---|
| Merge | `MERGE_HEAD` |
| Cherry-pick | `CHERRY_PICK_HEAD` |
| Rebase | `REBASE_HEAD`; if absent, `HEAD` (it moves with every replayed step) |
| Anything else | not offered; `ResolveConflictsWithAI` refuses |

`ResolveConflictsWithAI` stores the fingerprint at start instead of
`mergeHead`, and `runMergeTool` compares it on every call. The refusal text
becomes "The operation this run was started for is no longer in progress;
stop." Because a rebase's `REBASE_HEAD` changes each step, a run started for
step 2 cannot touch step 3, even if it were somehow still running after
Continue (Continue, Skip and Abort still stop it first).

### What the model reads

`mergetools.Run` takes a `Sides{Ours, Theirs string}` (descriptions, not
just names) from the app layer. `read_conflict` prints:

- Merge: `--- feature (the branch you are merging into) ---` /
  `--- main (the branch being merged) ---`
- Rebase: `--- main (the base being rebased onto) ---` /
  `--- a1b2c3 "fix login" (your commit being replayed) ---`
- Cherry-pick: `--- feature (the current branch) ---` /
  `--- a1b2c3 "fix login" (the commit being cherry-picked) ---`

The resolver's system prompt gains one paragraph per kind stating the goal:
for merge, combine both intents; for rebase and cherry-pick, preserve the
intent of the commit being applied on top of the target's current code. The
prompt remains user-editable through the existing prompts mechanism; the
per-kind paragraph is filled in by the app, not by the editable text.

Unchanged: the resolver never continues, skips or aborts; afterwards the view
reports what git still sees as unresolved. A rebase with several conflicting
steps needs one "Resolve with AI" per step.

## The `cherry_pick` chat write tool

- `cherry_pick(commit)`: resolves the commit, runs the same refusals as the
  UI (detached head, merge commit, already contained, uncommitted changes,
  operation in progress), then asks for approval with a card titled
  "Cherry-pick `a1b2c3` *fix login* onto **feature**".
- Result strings: applied / nothing to apply / "stopped on conflicts in N
  files; resolve them in the Conflicts view", same pattern as the merge
  tool.
- Rebase stays on the chat's "not offered" list; asked for one, the assistant
  explains where to do it in the UI.

## Architecture

### `internal/merge`

| Function | Behaviour |
|---|---|
| `Rebase(ctx, dir, onto) (Result, error)` | Refuses when any operation is in progress (`ErrOperationInProgress`) or tracked changes exist (`ErrDirtyWorktree`). Runs `git -c merge.conflictStyle=zdiff3 rebase <onto>`. Outcomes: `Rebased` (HEAD moved), `UpToDate` (HEAD unchanged), `Conflicted` (a rebase directory exists and there are unmerged paths). Any other failure: if a rebase directory was left behind, runs `rebase --abort` before returning the original error. |
| `CherryPick(ctx, dir, hash) (Result, error)` | Same refusals, plus `ErrMergeCommit` for a commit with more than one parent. Runs `git -c merge.conflictStyle=zdiff3 cherry-pick <hash>`. Outcomes: `Picked`, `Conflicted` (`CHERRY_PICK_HEAD` + unmerged paths), `NothingToApply` (`CHERRY_PICK_HEAD` without unmerged paths → runs `cherry-pick --skip`). Any other failure with `CHERRY_PICK_HEAD` left: `cherry-pick --abort`, then the error. |
| `RebasePreview(ctx, dir, onto) (Preview, error)` | `Commits` = `rev-list --count onto..HEAD`; `Merges` = same with `--merges`; `Published` = commits in `onto..HEAD` also reachable from `@{upstream}` (0 when there is no upstream); `Upstream` = its name. |
| `Skip(ctx, dir) error` | `rebase --skip` or `cherry-pick --skip` depending on `Status`; any other kind is an error. |
| `Fingerprint(ctx, dir) string` | As above. |
| `Status` | Gains `OursLabel`, `TheirsLabel` (short names for the view) and `OursDescription`, `TheirsDescription` (for the resolver). |

Every non-interactive git call here runs with `GIT_EDITOR=true`, as Continue
already does. `Result.Outcome` gains `Rebased`, `Picked`, `NothingToApply`.
The in-progress refusal uses `Status` (any kind), not just `MERGE_HEAD`.

### `internal/app/merge.go`

- New bindings: `RebaseOnto(repoID, onto)`, `RebasePreview(repoID, onto)`,
  `CherryPick(repoID, hash)`, `SkipStep(repoID)`. Writes go through
  `a.write` (the repository write lock) and emit the same change events as
  a merge.
- `ResolveConflictsWithAI` accepts merge, rebase and cherry-pick and stores
  the fingerprint; `runMergeTool` compares fingerprints and passes `Sides`.
- `SkipStep` stops the resolver run first, like Continue and Abort.

### `internal/ai`

- `mergetools.Run` takes `Sides`; tool text uses them.
- The resolver prompt gets the per-kind paragraph.
- `writetools` gains `cherry_pick`; its description and the "not offered"
  list are updated.

### Frontend

- Branch menu: "Rebase `<head>` onto `<branch>`" with disabled reasons.
- Log row menu: "Rebase `<head>` onto this commit", "Cherry-pick onto
  `<head>`" with disabled reasons. The row already knows whether it is a merge
  commit (its parent count). "Contained in HEAD" is asked when the menu opens,
  through a new read binding `IsAncestorOfHead(repoID, rev)`
  (`merge-base --is-ancestor`); the branch menu uses the same binding.
- Confirmation dialogs for both (rebase built from `RebasePreview`).
- Conflicts view: "Skip this commit", named "Take `<name>`" actions, "Resolve
  with AI" shown for three kinds, Skip offered inside the empty-step error.

## Error handling

- **Refusals before git runs** (dirty tree, operation in progress, merge
  commit, detached head) return typed errors the UI words as above.
- **git failing without a conflict** (unknown ref, untracked file in the way,
  hook failure) never leaves a sequencer state behind: Rebase and CherryPick
  abort what they started before returning the error.
- **Continue on an emptied step** surfaces git's message with a Skip button.
- **Resolver on a finished or changed operation**: every tool call is
  refused by the fingerprint check, and the run reports it.
- **Write lock**: all of the above run under the per-repository write lock,
  so a resolver edit, a Skip and a Continue never interleave.

## Testing

Real temporary repositories, as the existing merge tests do:

- Rebase: clean (commits replayed, HEAD moved), up to date, conflicted
  (Status reports rebase, step count), refused on tracked changes, refused
  while a cherry-pick is in progress, an unknown ref leaves no rebase
  directory.
- RebasePreview: commit count, merge count, published count with and without
  an upstream.
- Cherry-pick: clean, conflicted, emptied → `NothingToApply` with no
  `CHERRY_PICK_HEAD` left, merge commit refused, refused on tracked changes.
- Skip: rebase step skipped (next step or finished), cherry-pick skipped,
  error for a merge.
- Fingerprint: per kind; changes between two conflicting rebase steps.
- Status labels: ours/theirs names and descriptions for merge, rebase,
  cherry-pick.
- App: a resolver run's tool call is refused after the fingerprint changes;
  resolver accepted for rebase and cherry-pick, refused for revert and stash.
- mergetools: `read_conflict` prints the given sides.
- writetools: `cherry_pick` refusals, approval card title, conflict result.
- Frontend: menu entries and disabled reasons, confirmation dialogs, Skip
  and named take actions, following the existing component test patterns.

## Docs

Updated in the same commits as the behaviour (see the spec-in-sync rule):

- `docs/spec/04-conflicts.md`: resolver for three kinds and the fingerprint
  rule (replacing Rule 7), named sides, Skip, the empty-step case.
- `docs/spec/02-log-and-history.md`: the two new log row actions and their
  refusals.
- `docs/spec/01-repositories-and-sidebar.md`: the new branch menu row in the
  actions table; the write-lock list includes rebase and cherry-pick.
- `docs/spec/06-ai.md`: `cherry_pick` tool, rebase still not offered,
  per-kind resolver prompt and labels, resolver offered for three kinds.

## Out of scope

- Interactive rebase (its own sub-project).
- Cherry-picking several commits or a range; multi-select in the log.
- Autostash or stash-and-continue.
- Force-push.
- Cherry-picking merge commits (`-m`).
- The AI resolver for revert, `am` and stash conflicts.
- Running the resolver across several rebase steps automatically.
