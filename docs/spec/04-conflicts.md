# Conflicts

Conflicts is the single view that resolves every situation in which the
repository is stopped mid-operation with unmerged entries. It replaces the
Changes view and takes over the details pane below the log for as long as
one of these is in progress, since only it can advance or abandon the
operation; the rest of the application keeps working underneath it (the log
can still be browsed, other repositories are unaffected), but this
repository's own working tree and index are this view's business until it
is resolved, aborted, or — for the one kind that has no such thing —
dismissed.

## Concepts

- **Kind** — which of six git operations left the repository conflicted.
  Every kind but one is detected from a git-level marker; the last is
  inferred from having none.
- **Conflicted file** — a tracked path both sides changed, still holding
  conflict markers, that both a human and the AI resolver can edit line by
  line.
- **Manual file** — an unmerged path neither can edit region by region: one
  side deleted it while the other changed it, one side added it and the
  other didn't, it is a symlink or a submodule, or it fails a check that
  says the marker parser cannot be trusted on it. It can only be settled by
  taking one side's version whole, or by hand outside the application.
- **Settled** — a path with no conflict left, whether or not it has been
  staged yet for the commit or step that closes the operation.

## The six kinds, and why detection order matters

| Kind | What it is | How it is told apart |
|---|---|---|
| Merge | `git merge` stopped on conflicts | `MERGE_HEAD` exists |
| Rebase | `git rebase` stopped replaying a commit | a rebase bookkeeping directory exists and is not an `am` in disguise |
| Cherry-pick | `git cherry-pick` stopped on conflicts | `CHERRY_PICK_HEAD` exists |
| Revert | `git revert` stopped on conflicts | `REVERT_HEAD` exists |
| Applied mailbox patch | `git am` stopped applying a patch | the rebase-apply bookkeeping directory exists and carries an am-specific marker file |
| Stash conflict | `git stash apply` or `git stash pop` left conflicts | none of the above is present, but the index still has unmerged entries |

These are checked in a fixed order, and the order is load-bearing, not
incidental. The tempting shortcut — "no `MERGE_HEAD`, no rebase directory,
but there are unmerged entries, so it must be a stash conflict" — is wrong:
a conflicted cherry-pick or revert matches that description exactly, since
neither one leaves a rebase directory or a `MERGE_HEAD` behind. So the
sequencer pseudo-refs (`CHERRY_PICK_HEAD`, `REVERT_HEAD`) are checked
first, then `MERGE_HEAD`, then the two rebase bookkeeping directories (with
the mailbox-patch case told apart from an old-style rebase by a file only
`git am` writes into that same directory — the two use the same directory
name), and only once every one of those has failed to match does the
presence of unmerged entries fall through to the stash case. Getting this
order wrong would, for example, make Abort run `git rebase --abort` against
what is actually a `git am` in progress, discarding the mailbox patch
instead of the rebase that was never happening.

A repository matching none of the six — no pseudo-ref, no rebase directory,
no unmerged entries — is not conflicted at all, and the view does not
appear.

## The header

The header names the operation and, where one exists, what is being
combined and with what:

| Kind | Reads as |
|---|---|
| Merge | "Merging **‹branch/tag/hash›** into **‹current branch›**" |
| Rebase | "Rebasing **‹branch›** onto **‹branch or hash›**", plus "commit *N* of *M*: ‹subject›" when the step count is known |
| Cherry-pick | "Cherry-picking **‹short hash›** onto **‹current branch›**", plus the commit's subject |
| Revert | "Reverting **‹short hash›** on **‹current branch›**", plus the commit's subject |
| Applied patch | "Applying patch" plus the patch's subject line, when git left one to read |
| Stash conflict | "Resolving stashed changes" — no from/into, since a stash has neither |

The branch or commit being merged in is read from git's own merge message
when one exists (the quoted name inside it), falling back to the short hash
of the pseudo-ref when it does not. A rebase's destination is named by a
branch that still points at the target commit when one exists, and by its
short hash otherwise (an anonymous "onto" target, or one no branch names
any more). The step count ("commit *N* of *M*") is only available for the
newer rebase backend; the older one is shown with no step count rather than
a guessed one.

## The file list

Files are grouped into up to three sections, each omitted when empty:

1. **Conflicts** — every conflicted file, plus every manual file, together.
   A conflicted file shows a plain marker glyph; a manual file shows `!`.
2. **Unstaged** — settled files not yet staged for the commit or step that
   will close the operation. Shown with an `M` glyph.
3. **Staged** — settled files already staged. Shown with a check mark.

For a merge specifically, "settled" is narrower than "not currently
conflicted": a merge's Unstaged list only ever contains paths the incoming
side actually touches (what changed between the merge base and the branch
being merged in, plus the new name of anything the current side renamed
that the incoming side edited under its old name). A merge can begin with
unrelated uncommitted changes already sitting in the worktree, and those
are not the merge's to stage — they stay out of every section here and
remain visible only in the Changes view once the merge ends. Every other
kind has no such "incoming side" to filter by, so all of its settled paths
are listed.

Selecting a conflicted file shows its raw content, markers and all, with
marker lines picked out visually. Selecting a manual file shows an
explanatory placeholder instead of content, since there is nothing here to
edit line by line — it points at right-click for taking a side, or at
resolving it outside the application. Selecting a settled file shows the
diff the closing commit or step would carry for it: against the index if it
is staged, against HEAD if it is only in the worktree. Only a path that is
part of the operation currently in progress can be read this way; nothing
else is reachable through this view.

As with the Changes view, the selected file is tracked by path so it
survives moving between sections as it is worked on (Conflicts → Unstaged →
Staged, or Unstaged → Staged) — an edit through the AI resolver or an
action taken here re-diffs the same file in place rather than dropping the
selection.

## Per-file actions

| File status | Actions | Effect |
|---|---|---|
| Conflict (has markers) | Stage | Same staging operation as a settled file; refused while any marker remains in the file (see "The marker check" below). |
| Unstaged (settled, not staged) | Stage | Adds it to the index. |
| Staged | Unstage | Removes it from the index, keeping its content in the worktree. |
| Manual | Take ours / Take theirs (right-click) | Replaces the file wholly with one side's version — or deletes it, if that side deleted it — and stages the result. |

Taking a side asks for confirmation: it overwrites whatever is in the
worktree, including any hand edits, and unstaging afterwards does not bring
that content back. A manual file that is a submodule cannot be taken either
way through this view — checking out one side's commit for a submodule
does not by itself decide which commit the submodule should actually point
at, so this is left to be settled outside the application.

Unstaging a settled file is refused when it is not one this operation
actually touches — which, for a merge, means a path the incoming side never
changed; unstaging it would drop it out of every section here (and out of
the merge commit) with no way back into view except a terminal.

## The marker check

Staging a conflicted file re-reads it from disk and refuses if it still
contains any conflict marker line (`<<<<<<<`, `=======`, `|||||||`,
`>>>>>>>` at the start of a line). This is what stops a half-resolved file,
or one where an edit accidentally left a marker behind, from being staged
as if it were finished; the file stays in Conflicts until every marker is
gone. Emptying a conflicted region entirely (deleting a hunk's marker block
with nothing to replace it) counts as resolving it, so long as no marker
survives elsewhere in the file.

## Continue and abort

Two actions move the operation forward or throw it away, worded per kind:

| Kind | Continue | Abort |
|---|---|---|
| Merge | "Commit merge" — closes the merge with git's own generated merge message | "Abort merge" — `git merge --abort`, restoring the branch to before the merge started |
| Rebase | "Continue rebase" | "Abort rebase" |
| Cherry-pick | "Continue cherry-pick" | "Abort cherry-pick" |
| Revert | "Continue revert" | "Abort revert" |
| Applied patch | "Continue applying" | "Abort patch" |
| Stash conflict | none | none (see below) |

Continue is disabled while anything in Conflicts is still pending (any
conflicted or manual file left); it only becomes available once everything
is settled, staged or not — though for a merge specifically, committing
with settled files still unstaged is allowed after a warning, since it
means the merge commit keeps only the current branch's version of those
files and silently drops the incoming side's. For every kind but a merge,
"continue" runs that operation's own `--continue` (with no editor allowed
to open), which may immediately leave the next commit's conflicts behind
for this same view to show again — the view simply re-reads the repository
afterwards rather than assuming completion.

Abort asks for confirmation, since it throws away every resolution made so
far and returns the branch to where it was before the operation started;
the wording of the confirmation follows the kind, so it never claims to be
undoing "this merge" when it is actually a rebase or a cherry-pick.

Both continue and abort stop any AI resolver run first, so an agent cannot
go on editing a merge that the click just closed or discarded, and neither
can it go on to resolve the next merge's conflicts if a rebase's next step
immediately re-conflicts.

## The stash conflict, specially

A conflicted stash apply or pop has no git-level "continue" or "abort" at
all — stashing was never a sequencer operation with a state to unwind, only
an apply that left unmerged entries behind. This kind is offered two
different actions instead:

- **Done** — always available. It leaves every file exactly as it is,
  resolved or not, and simply stops this view owning the screen, handing
  control back to the Changes view. It does not require anything to be
  settled first, unlike Continue for the other five kinds.
- **Drop stash** — available only when a popped stash still has an entry
  waiting to be dropped. A `git stash pop` that conflicts deliberately
  leaves the stash entry in the list rather than losing it (git's own
  behaviour), and the application separately remembers, by the stash's
  commit hash rather than its position in the list, that this entry still
  owes a drop once its conflict is fully resolved by any means — staging,
  unstaging or taking a side through this view or through the Changes view
  is what settles it automatically, at which point the reminder is
  discharged and the entry is dropped without asking again. Drop stash
  offers to do that same drop by hand, immediately, before the conflict is
  necessarily fully resolved. A plain `git stash apply` conflict carries no
  such reminder, since apply never removes the stash entry to begin with.

Because Done can be clicked with the conflict still open, a stash conflict
is the one kind that can be "dismissed" rather than concluded. Dismissing
does not resolve anything at the git level — the unmerged entries are still
there — it only stops Conflicts from taking over the screen, so the user
can go back to reviewing and staging the same files through the Changes
view instead. The dismissal is remembered only for that one conflict: a
change in kind, or the conflict clearing entirely, forgets it, so a later,
different stash conflict is shown again rather than staying hidden by an
old dismissal. While dismissed, a control appears elsewhere in the
interface (outside this view) to bring Conflicts back on demand — the
dismissal would otherwise leave no way back to it once the files happen to
resolve on their own, or if the user changes their mind.

## The AI conflict resolver

"Resolve with AI" starts an agent that reads and edits conflicted files
directly — listing conflicts, reading one region's ancestor/ours/theirs
text, replacing a region's markers with resolved content, and staging a
file once it has no markers left — stopping short of continuing or
committing, which stays a manual decision. It shares the same run slot as
the chat panel, so a resolve run and an ordinary chat message can never
run at once, and it is itself stopped whenever the operation is continued
or aborted from this view.

The button is offered for a merge only, and refuses to run for any other
kind. This follows directly from how a run is tied to the merge it started
for: it identifies "its" merge by `MERGE_HEAD`'s commit, and every tool
call it makes is checked against that same commit still being current
before being allowed to touch anything — which stops a run from continuing
to edit a merge that has since been aborted or replaced by a different one,
and stops it from silently reaching into a rebase's or cherry-pick's
conflicts, which have no `MERGE_HEAD` for it to check against at all. A
rebase, cherry-pick or revert conflict has no equivalent pseudo-ref this
mechanism could be built on, so the resolver is not offered for them
in this view.

The resolver is unavailable outright when the application's AI features are
turned off, and refuses to start a second run while one is already active
for the repository. Once it finishes — successfully, on error, or stopped —
the view reports what git itself still sees as unresolved, rather than
trusting anything the model claimed while running: if nothing is left, it
says so and points at reviewing the staged files before committing; if
something remains, it lists exactly which files are still conflicted and
which still need a human, by name.

## Rules

1. Detection runs in a fixed order — cherry-pick, then revert, then merge,
   then rebase (with mailbox patches told apart from it), and only then the
   markerless stash case — because a conflicted cherry-pick or revert would
   otherwise be mistaken for a stash conflict, and a `git am` bookkeeping
   directory would otherwise be mistaken for a rebase.
2. A file is Manual, never Conflict, whenever a side is missing entirely
   (added on one side only, or deleted on one and changed on the other), or
   it is a symlink or submodule, or an attribute check says the marker
   parser cannot be trusted on it.
3. Staging a conflicted file is refused while any conflict marker line
   remains anywhere in it.
4. Continue is available only once nothing is left conflicted or manual; a
   merge's Continue additionally warns, but does not refuse, when settled
   files remain unstaged.
5. Abort and Continue are unavailable for a stash conflict; Done and,
   conditionally, Drop stash take their place.
6. A stash conflict's Done never changes the repository; it only stops this
   view owning the screen, and only for that one conflict.
7. The AI resolver is offered only for a merge, ties itself to that merge's
   commit, and refuses to keep acting once that commit is no longer the one
   in progress.
8. Only a path belonging to the operation currently in progress can be read
   or acted on through this view.

## Known divergences

None found: no older design document for this area was available to
compare against; the behaviour above was derived directly from the merge,
stash and app-layer Go packages and their tests, and from the conflict view
components and stores.
