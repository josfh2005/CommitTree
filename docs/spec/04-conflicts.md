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
edit line by line — it points at its Take buttons, or at resolving it
outside the application. Selecting a settled file shows the
diff the closing commit or step would carry for it: against the index if it
is staged, against HEAD if it is only in the worktree. Only a path that is
part of the operation currently in progress can be read this way; nothing
else is reachable through this view.

As with the Changes view, the selected file is tracked by path so it
survives moving between sections as it is worked on (Conflicts → Unstaged →
Staged, or Unstaged → Staged) — an edit through the AI resolver or an
action taken here re-diffs the same file in place rather than dropping the
selection.

## Region actions

Above each conflict region of a text file the pane shows a row of buttons
named after the two sides — "Take develop", "Take feature/checkout" (the
names the rest of the view uses for this operation) — plus "Both" (ours,
then theirs) and "Edit…". Hovering a Take button dims the lines it would
drop. Edit… turns the region into a text box holding both sides, applied
with Apply or ⌘↵ and dropped with Cancel or Esc. Each writes only that
region, found by its content id: if the region changed meanwhile (the AI,
another click), nothing is written and the file reloads. When a file's
last region is settled it is staged ("… resolved and staged").

"Restart file" puts a file of the operation that was in conflict — still
conflicted, or staged since — back as git first wrote it, markers
included and unstaged, after a confirmation; it is the undo for anything
resolved in it, by hand or by the AI. A file the merge settled cleanly
has no Restart.

A file without markers shows its "Take <side>" choices as buttons at the
top of the pane (the right-click menu still offers them). All of these are
disabled while another operation runs or an AI run is working on the
repository, and the app refuses them then as well.

## Per-file actions

| File status | Actions | Effect |
|---|---|---|
| Conflict (has markers) | Stage | Same staging operation as a settled file; refused while any marker remains in the file (see "The marker check" below). |
| Unstaged (settled, not staged) | Stage | Adds it to the index. |
| Staged | Unstage | Removes it from the index, keeping its content in the worktree. |
| Manual | Take `<side>` (buttons at the top of the pane, or right-click) | Replaces the file wholly with one side's version — or deletes it, if that side deleted it — and stages the result. |

The side is named for what it actually is, per kind: for a merge it is the
current branch / the branch merged in; for a rebase, the branch being
rebased onto / the commit being replayed (short hash plus subject); for a
cherry-pick, the current branch / the picked commit. Revert, an applied
patch and a stash conflict keep the plain "ours" / "theirs" labels. A
rebase names its sides this way because git itself swaps ours and theirs
for a rebase relative to a merge — the base being rebased onto is git's
"ours", and the commit being replayed is git's "theirs" — so the view
names each side for what it is rather than repeating git's swapped terms.

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

### Skip this commit

A rebase or a cherry-pick can also skip the step currently being replayed,
dropping its changes from the result entirely rather than committing them.
"Skip this commit" is always available for these two kinds (it needs
nothing settled first, unlike Continue), asks for confirmation naming what
is being dropped, stops any AI resolver run first the same way continue and
abort do, and then runs git's own `--skip` for that operation. Skip is not
offered for a merge, a revert, an applied patch or a stash conflict — none
of git's `--skip` supports the same "drop this step and move on" for them
the way rebase and cherry-pick do.

When Continue fails because the step it just tried to close came out empty
(the resolution left nothing to commit), the error offers Skip as a
follow-up: dropping the now-empty step is usually what was meant, though
the choice is still asked for rather than assumed.

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

The button is offered for a merge, a rebase and a cherry-pick, and the
resolver refuses to run for any other kind. While a run is working on the
repository the button is disabled (its tooltip says to stop the run from
the chat), as are the region and Take buttons and the commit/continue
button. Abort stays available as the way out: its tooltip and its
confirmation say the resolver is stopped first. A run is tied to the operation
it started for by a fingerprint: the kind plus the commit being combined —
`MERGE_HEAD` for a merge, `CHERRY_PICK_HEAD` for a cherry-pick, and for a
rebase the commit currently being replayed (`REBASE_HEAD`, or `HEAD` on the
older backend that has none). Every tool call re-checks that fingerprint
before touching anything, which stops a run from editing a merge that has
since been aborted or replaced, and — because a rebase's fingerprint changes
with every step — from reaching into the next step's conflicts after the
one it was started for has been continued or skipped. A rebase with several
conflicting steps therefore needs one run per step. Revert, applied-patch
and stash conflicts are not offered the resolver.

What the resolver reads names each side for what it is rather than "ours"
and "theirs", since a rebase swaps the two relative to a merge: for a rebase
the base side is the branch being rebased onto and the other is the commit
being replayed; for a cherry-pick, the current branch and the picked commit.
Its instructions say, per kind, which side is final (a rebase's base, a
cherry-pick's current branch) and whose intent is to be carried over.

The resolver is unavailable outright when the application's AI features are
turned off, and refuses to start a second run while one is already active
for the repository. Once it finishes — successfully, on error, or stopped —
the view reports what git itself still sees as unresolved, rather than
trusting anything the model claimed while running: if nothing is left, it
says so and points at reviewing the staged files before committing; if
something remains, it lists exactly which files are still conflicted and
which still need a human, by name.

### Decisions left to you

When the two sides of a region genuinely contradict each other — two values
for the same setting, one side deleting what the other edited — the resolver
does not pick one: it leaves the region as a card in the chat and carries on
with the rest. The card names the file and the region, asks a one-line
question, and lists two to four options, each with its label and, under it,
the exact text that would replace the region; an option that removes the
region says "(removes the region)". A last option, "Other…", is always
there: it opens a text box pre-filled with the text of the option selected
before it (the first by default), where ⌘↵ applies and Esc goes back to the
options.

Apply writes the selected text in place of the region, through the same
code as the region buttons, and stages the file when that was its last
region; a toast says so ("config/settings.json resolved and staged", or
"Region resolved"), and the Merge view reloads. Nothing is committed. While
an AI run is working on the repository the card is shown but Apply is
disabled, with "Available when the AI finishes" — the card is answered after
the run, not during it.

The card only appears once the tool has accepted the options: options it
refused (too few or too many, repeated, with conflict markers, or failing
the resolution checks) show as an ordinary tool row, and cannot be applied.
When every region still in conflict has a card, the resolver is not told to
carry on.

Once answered the card shrinks to one line saying what was chosen; if the
region was settled some other way first (the region buttons, or the file
edited outside), Apply turns the card into "Settled another way" instead.
The choice is recorded in the chat's history, in place of the tool's
result, so the card keeps its state after a reload; a later chat with the
model sees it while that result is still among the recent ones sent back. If
the operation was committed or aborted meanwhile, Apply shows git's error
and the card stays as it was. A card can be answered once. Files without
conflict markers never get a card; they keep their Take buttons.

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
7. The AI resolver is offered only for a merge, a rebase or a cherry-pick,
   ties itself to that operation's fingerprint (for a rebase, the step being
   replayed), and refuses to keep acting once the fingerprint changes.
8. Only a path belonging to the operation currently in progress can be read
   or acted on through this view.
9. Skip is offered only for a rebase or a cherry-pick, and stops any
   resolver run before it runs.

## Known divergences

None found: no older design document for this area was available to
compare against; the behaviour above was derived directly from the merge,
stash and app-layer Go packages and their tests, and from the conflict view
components and stores.

## Known limitations

With `rerere.autoupdate` turned on, git can fully stage a rebase or
cherry-pick step's recorded resolution by itself: no unmerged paths are
left, but the sequencer (`CHERRY_PICK_HEAD`, or the rebase directory) is
still there and the index differs from HEAD. The app does not recognise
this as a resolved step — it takes the "any other failure" branch, aborts
the operation, and reports git's "could not apply" error. Nothing is lost,
but the step cannot currently be continued from the app; it only affects
people who have `rerere.autoupdate` enabled.
