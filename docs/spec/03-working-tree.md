# Working tree

The Changes view is where the user reviews everything not yet committed —
staged, unstaged and untracked files — inspects a diff for any one of them,
moves files between the index and the worktree, throws changes away, and
writes the commit that closes them. It is the ordinary, non-conflicted
counterpart to Conflicts: when a merge, rebase or other conflict is in
progress, Conflicts takes over the screen instead (see Conflicts).

This same view is also reachable from the commit log's "Uncommitted
changes" row (see Log and history): selecting that row opens it in the
log's own details pane instead of the sidebar. Both are the same
working-tree state — staging, unstaging or discarding a file in one is
reflected in the other immediately, and everything in this document applies
equally to either.

## Concepts

- **Staged** — a path with an entry in the index that differs from HEAD (or,
  in a repository with no commits yet, any entry at all).
- **Unstaged** — a tracked path whose worktree content differs from what is
  staged for it (or from HEAD, if nothing is staged).
- **Untracked** — a path git does not track at all.
- **Partially staged file** — a path that is both staged and unstaged at
  once: some of its changes are in the index, more have been made in the
  worktree since. Git reports this as one path with two status letters; the
  application lists it once under Staged and once under Unstaged.
- **Amend** — rewriting the last commit instead of creating a new one, by
  message alone or together with whatever is currently staged.

## The file list

The view groups changed paths into three sections, in this order: Staged,
Unstaged, Untracked. A section with no files is omitted; when every section
is empty the list shows an explicit empty state rather than nothing.

Each row shows a one-character status glyph and the path. The glyph is
git's own letter for the change: `M` modified, `A` added, `D` deleted, `R`
renamed, `T` type-changed, `?` untracked. A partially staged file therefore
appears twice, once per section, each occurrence carrying only that side's
letter (typically `A`/`M` staged and `M` unstaged, or similar). A row for a
submodule shows a package icon ahead of its status glyph.

Selecting a row loads that file's diff into the content pane. Selection is
tracked by path, not by list position: staging or unstaging a file moves it
into a different section, but if the user had it open, it stays open,
re-diffed against its new state. When a path exists in two sections at
once, the section the user actually opened is what is remembered, so
re-selecting after a change still resolves to the intended one of the two
rather than always the Staged copy.

Right-clicking a row offers Blame of the file as it stands in the working
tree, uncommitted lines shown as "Not committed yet"; it is disabled, with
a reason as its tooltip, for a deleted file, an untracked file (neither has
history to blame) and a submodule (submodules have no blame). The blame
view it opens follows the working tree while open, refreshing as files
change — see Blame in the log and history docs.

## The diff

The content pane shows the diff for whichever occurrence of the file was
selected:

- **Staged**: the diff between the index and HEAD (what the commit would
  contain).
- **Unstaged**: the diff between the worktree and the index (or HEAD, if
  nothing for that path is staged).
- **Untracked**: the file's entire content, shown as an addition against an
  empty file, since there is no tracked side to compare against.

A file's diff is only readable when its path is one the current status just
listed — a stale or invented path is refused. A large diff is truncated
rather than handed to the view whole, so a single very large file cannot
freeze the interface.

Diff lines are coloured by their leading character: `+` lines as additions,
`-` lines as deletions, `@@` hunk headers and the `diff`/`index`/`+++`/`---`
lines before the first hunk as metadata; everything else is plain. Inside a
hunk a line is read by its first character only, so an added `++ x` (shown
`+++ x`) is an addition, not metadata.

A changed path that is a submodule shows git's `--submodule=log` summary
instead of an ordinary diff: the old and new commit it points at, plus the
subjects of the commits between them — not a raw `Subproject commit` line.
Its status also reports, independently of the diff, whether the pointer
itself moved and whether the submodule's own working tree has modified or
untracked content.

The pane renders that summary as its own layout, not diff lines: the
submodule's path, the old and new commit it points at (7 characters each,
or a muted note — "new submodule", "submodule deleted", "commits not
present" — in place of the range when there is no ordinary range to show),
the list of commits between them (each prefixed `>` or `<` for arriving or
leaving, coloured the same as an addition or a deletion), and, when the
submodule's own working tree has modified or untracked content, a note
saying so plus an "Open submodule" link that selects it as a repository
(shown only once it is initialised). For an unstaged submodule whose
pointer has not moved — its own content is dirty but there is nothing to
stage or update here — that note is replaced with "Commit inside the
submodule first".

### Acting on part of a file

A file whose change is a plain text modification (status `M`: not added,
deleted, renamed, copied, type-changed, binary or a submodule) can be acted
on by hunk or by line from its diff. Every other file keeps only the
file-level actions.

- Each hunk header carries **Stage hunk** and **Discard hunk** for a file in
  Unstaged, or **Unstage hunk** for a file in Staged. Discarding from Staged
  is only offered for the whole file.
- A click on a `+` or `-` line selects it; Shift+click selects the change
  lines between the last clicked line and this one; Cmd+click (Ctrl+click)
  adds or removes one line. Context and header lines cannot be selected. A
  drag still selects text for copying. Change lines can also be reached with
  Tab; Enter or Space acts as a click, with the same Shift and Cmd (Ctrl)
  modifiers. Esc clears the selection, unless it is closing a menu or a
  dialog or the focus is elsewhere (the commit message, the chat). The
  selection survives a reload that brings back the same diff, and is cleared
  when the file, its section or its diff changes.
- With lines selected, a bar at the top of the diff shows the count and
  **Stage lines** / **Discard lines** (Unstaged) or **Unstage lines**
  (Staged). Staging some lines of a hunk leaves the others unstaged, and so
  on for the other two actions.
- The last hunk of a truncated diff has no actions, since it may be cut short.
- The action is refused, with "The file changed since it was shown —
  reloaded", when the file's diff changed after it was shown (an edit in an
  editor or a terminal); the diff then reloads and nothing is changed.
- A hunk or line discard is not confirmed. It shows "Discarded 1 hunk in
  `<file>`" (or "N lines") with **Undo**, which stays until dismissed. Undo
  puts the discarded lines back; only the last discard of the repository can
  be undone, and only while the app is open. If those lines changed since,
  Undo reports "Can't undo: the file changed since the discard" and changes
  nothing.
- All buttons are disabled while another write is running.

## Staging, unstaging and discarding

Every row offers the actions appropriate to its section, plus Discard,
which is available from either side:

| Section   | Actions available |
|-----------|--------------------|
| Staged    | Unstage, Discard |
| Unstaged  | Stage, Discard |
| Untracked | Stage, Discard |

A submodule row departs from this table. It never offers Discard — a
submodule's pointer is put right with Update, not thrown away, and its own
dirty content can only be dealt with from inside the submodule itself —
and Update to recorded commit takes Discard's place: Staged offers Unstage
+ Update, and Unstaged offers Stage + Update when the pointer has moved.
When the pointer has not moved and only the submodule's own content is
dirty, there is nothing stageable, so the row offers only Open submodule
(once initialised).

**Stage** adds an unstaged or untracked path to the index as it stands in
the worktree right now.

**Unstage** removes a path's entry from the index without touching the
worktree. A path that HEAD does not have (a new file, once unstaged) is not
simply forgotten: it is re-added to the index as an empty placeholder so it
keeps appearing as a changed path rather than disappearing into "no
changes". Unstaging is refused outright when the index holds the only copy
of a file's content — nothing in HEAD and nothing left in the worktree —
since it would otherwise be lost with no warning. A rename is unstaged as a
pair: both its old and new path are restored together, so the index does
not split into a deletion of the old name plus an untracked file at the new
one.

**Discard** throws a path's changes away and asks for confirmation first,
because it cannot be undone. What it does depends on whether the path has
ever been committed:

- A path with a HEAD copy (already tracked, or staged as a modification of
  an existing file) is restored from HEAD, in both the index and the
  worktree at once. This is true regardless of which of its two rows
  (Staged or Unstaged) triggered the discard: a partially staged file's
  staged and unstaged changes are thrown away together, not one row at a
  time.
- A path with no HEAD copy — an untracked file, or one staged as newly
  added — has nothing to restore to. Discard deletes it outright. The
  confirmation for this case says explicitly that the file is gone for
  good, rather than the ordinary "discard your changes" wording.
- A rename is discarded by restoring both its old and new path together.
  Restoring only the new path would leave it with no HEAD entry at that
  name, and git would delete it instead of reviving the old path's content.

Discard is the only destructive action in this view; every other action
(stage, unstage) is reversible by the opposite action.

## The commit box

Below the file list sits the commit box: a message field, an Amend toggle,
a "Write with AI" control, a Stash button, and the Commit (or Amend)
button.

**Commit** is enabled only when a message has been typed and, for an
ordinary commit, at least one path is staged. **Amend** may be ticked with
nothing staged at all — rewriting only the message is its most common use —
so with Amend on, the button enables as soon as there is a message and a
commit exists to rewrite. Amend itself is unavailable (its checkbox
disabled) in a repository with no commits yet, since there is nothing to
rewrite. Ticking Amend loads the last commit's message into the box, unless
the user has already typed something of their own, in which case their text
is left alone; unticking it restores whatever was in the box before Amend
was turned on, under the same condition. Amending a commit that the current
branch's upstream already has asks for confirmation first, since it means a
later push will need to be forced.

**Stash** is disabled when there is nothing changed at all — no staged,
unstaged or untracked file — mirroring git's own refusal to stash an empty
worktree.

**Write with AI** streams a generated commit message into the box from the
staged diff. It refuses when nothing is staged. Once a generation is
running, the message field is read-only and the button becomes a Stop
control; stopping only detaches the box from the stream (there is no way to
cancel the underlying request), so a slow provider keeps running after
Stop, its output simply unheard. Whatever the user had typed before
starting a generation is restored if the request fails outright. The
message can also be generated automatically, without the user asking,
depending on a setting: never, always, or only when the configured
provider is the local one — and always guarded by there being something
staged and the box not already containing typed or generated text.

## Nothing to commit

With no staged, unstaged or untracked files, the file list shows its empty
state, the diff pane shows a neutral placeholder, Stash is disabled, and
Commit is disabled unless Amend is on (which needs only a message, not
staged content).

## Reacting to change

The view does not poll. It refreshes when the application tells it the
working tree moved: after any stage, unstage, discard or commit made
through this view itself, and on a notification that the repository
changed underneath it (an operation elsewhere in the application, or the
window regaining focus after changes were made outside it, such as in a
terminal). On refresh, the previously open file is re-resolved by path (and
section, to break a tie for a partially staged file) rather than by list
position, so a file that merely moved from Unstaged to Staged stays open
and simply shows its new diff; a file that vanished entirely (fully
discarded, or its last change staged and committed) falls back to the first
file remaining, or to the empty state if none are left. Whenever a merge,
rebase or other conflict starts, this view stops being shown at all — see
Conflicts.

## Rules

1. A path that is both staged and unstaged (git's partially-staged case)
   appears once in each section, and stage/unstage/discard treat each
   occurrence as its own row, except Discard, which always affects the
   path's staged and unstaged content together, regardless of which row
   started it.
2. Discard deletes a path outright, with no way to undo it, exactly when
   the path has no HEAD copy — untracked, or staged as newly added.
   Otherwise Discard restores the path from HEAD.
3. A rename is always staged, unstaged and discarded as a pair of paths (old
   and new), never as the new path alone.
4. Unstaging is refused when it would leave a file's only remaining copy
   nowhere: not in HEAD, not in the index afterward, not in the worktree.
5. Commit requires a non-empty message and, unless amending, at least one
   staged path. Amend requires only a message and an existing commit to
   rewrite.
6. Stash requires at least one changed path of any kind (staged, unstaged
   or untracked); with none, it is disabled.
7. Generating a commit message requires at least one staged path and never
   overwrites text the user has typed or a message already produced.
8. Only a path the current status just reported can have its diff read or
   be staged, unstaged or discarded; an arbitrary path is refused.
9. A merge, rebase, cherry-pick, revert, applied patch or conflicted stash
   in progress replaces this view with Conflicts; the Changes view resumes
   once nothing is left in progress (or, for a stash conflict specifically,
   once the user dismisses it — see Conflicts).

## Known divergences

None found: the behaviour described here was verified directly against the
Go worktree package, the app-layer handlers, and the Svelte components and
stores that drive the Changes view, with no older design document
consulted for this area.
