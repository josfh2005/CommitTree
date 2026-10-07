# Remote and stash

This area covers everything that leaves the local repository to talk to a
remote — fetching, pulling and pushing — and the stash, which is local but
shares the same toolbar and the same conflict machinery as a pull. Both are
about setting work aside from, or bringing it in from, somewhere other than
the current working tree.

## Concepts

- **Upstream**: the remote branch the current branch tracks. A branch can
  have no upstream, typically because it was created locally and never
  pushed.
- **Ahead/behind**: how many commits the current branch has that its upstream
  lacks (ahead), and vice versa (behind), computed by comparing the two
  histories.
- **Pull strategy**: how a pull integrates the upstream once fetched — follow
  the repository's own configuration, always merge, or always rebase. It is
  an application-level preference, stored once, not a per-repository or
  per-pull choice.
- **Conflict**: a merge, rebase or stash application that has left one or
  more files with unresolved regions. See Conflicts for how a conflict is
  represented and resolved; this document covers only what triggers one and
  what stays available while it lasts.
- **Stash**: a snapshot of the working tree (and, optionally, untracked
  files) saved under a message, without being committed. A repository can
  hold any number of stashes, ordered newest first.
- **Owed stash drop**: a bookkeeping note the application keeps when applying
  a stash by popping it goes wrong — see "Pop and the owed drop" below.

## Fetch, pull and push

The log view's header carries the repository toolbar: a ~60 px header with
the repository name and, under it, its path on the left, and on the right
labelled buttons (a 20 px outline icon with its name under it) in groups —
Commit, Stash · Fetch, Pull, Push · Branch, Merge · Terminal, Commands, Finder
("Folder" off macOS), Chat. Tag, rebase and cherry-pick are not on it: they
act on a selected commit and stay in context menus. A missing (moved or
deleted) repository shows no toolbar. Every disabled button's tooltip says
why it is disabled; an enabled one's says what it does.

| Button | Action | Disabled when (tooltip) |
|---|---|---|
| Commit | Selects the "Uncommitted changes" row, which opens the Changes view, and puts the cursor in the commit message box | another operation is running (the busy label, e.g. "Pushing…"); a merge, rebase or stash conflict is in progress ("Resolve the conflict first"); nothing is uncommitted ("Nothing to commit") |
| Stash | The "Stash changes" dialog | busy; a conflict is in progress; nothing is uncommitted ("Nothing to stash") |
| Fetch | Fetch, below; an amber dot while background fetches skip a remote that needs credentials, with the tooltip "Background fetch paused for <remotes>: authentication failed. Fetch to retry." | busy |
| Pull | Pull, below; the behind count as a badge | busy; a conflict is in progress |
| Push | Push, below (following Settings → General → Push); the current branch's ahead count as a badge | same as Pull |
| Branch | The "New branch" dialog, from HEAD (a detached HEAD's commit included) | busy |
| Merge | The branch picker below, then the usual merge confirmation | busy; a conflict is in progress; detached HEAD ("Check out a branch first"); no branch other than the current one, local or remote ("No other branches") |
| Terminal | A toggle: shows or hides the terminal panel, and looks pressed (soft background and border, no colour) while it is open | never |
| Commands | A toggle, like Terminal, for the Commands panel (see `09-command-log.md`) | never |
| Finder / Folder | Opens the repository folder | never |
| Chat | A toggle, like Terminal, for the chat panel | never |

The busy reason comes first, then the conflict, then the button's own rule.
Once every conflict of a merge, rebase, cherry-pick, revert or patch is
resolved but the operation is not yet finished, the conflict reason reads
"Finish the merge first" (rebase, cherry-pick, revert, patch) instead. All
buttons share one colour; a disabled one is drawn in the faint text colour.
"Uncommitted" counts staged, unstaged and untracked files, as the log's
uncommitted row does. A stash conflict dismissed with "Done" still counts as
a conflict here, and a "Resolve conflicts" button left of the toolbar is the
way back into it (see "Stash conflicts" below). Badges are hidden when their
count is zero.

In a narrow window the title and path ellipsize first; once the header is
narrower than 860 px the labels hide and the buttons become icon-only,
keeping their name in the tooltip. Below 480 px they tighten further, and if
even that does not fit (the window's minimum width with the side column
open) the toolbar scrolls sideways instead of spilling under the side column.

Fetch has no conflict-related restriction because it only updates the
repository's knowledge of the remote's refs; it never changes a branch, the
working tree or the index, so it stays available even while a conflict is
being resolved. Pull and Push are refused whenever a merge, rebase or stash
conflict currently owns the repository, for the same reason a commit is
refused then: the working tree and index are not in a state either operation
can safely act on. Both also refuse while any other write operation for the
repository is in progress — the application allows only one write at a time
per repository. A background fetch in progress is cancelled instead (see
"Background fetch").

### Merge branch picker

The toolbar's Merge opens a dialog titled "Merge into <current branch>": a
search field ("Search branches…", focused), a list and Cancel / Merge. The
list holds the branches on the commit selected in the log under "On selected
commit" (local and remote-tracking, so the branch picked in the log is at
hand), then the other local branches under "Local", then the other
remote-tracking ones as `remote/name` under "Remote"; no branch is listed
twice, the group is absent when no other branch is on the selected commit,
and the current branch and a remote's `HEAD` are left out. Typing filters the list by any part of the name, ignoring case
(`log` finds `feature/login`); with no match it reads "No branches match"
and Merge is disabled. The first item is selected when the dialog opens and
after every change to the search; ↑/↓ move the selection, Enter or a double
click confirms, Escape or a click outside cancels. Confirming runs the same
merge as the branch menu's "Merge `<branch>` into `<head>`": its
confirmation (including the choice offered for a local branch behind its
upstream), its "already up to date" message, its conflicts and submodule
warnings are unchanged.

A fetch, pull or push that takes 10 s or longer, fails or leaves conflicts
notifies — see [Notifications](11-notifications.md).

### Fetch

Fetch downloads every remote's refs and removes any remote-tracking branch
whose remote counterpart no longer exists. It reports only success or
failure; it never reports how much changed.

### Background fetch

When Settings → General → **Fetch in the background** is not Off (Every 5,
15 — the default —, 30 or 60 min), CommitTree runs `git fetch --prune
<remote>` for each remote of every repository in the sidebar that is not missing, one at a time:
first 30 s after the app starts, then every interval after the previous
round ends. A round is skipped while the computer is offline; a repository
with no remote, or with another write running, is skipped. Turning it Off
during a round lets the repository being fetched finish and fetches no
other.

A background fetch never asks for anything: askpass programs and Git
Credential Manager's dialogs are turned off for it, so a remote that needs a
password or a passphrase fails instead (helpers that answer on their own,
such as the macOS keychain or ssh-agent, still work). Such an authentication
failure stops background fetches of that remote — the repository's other
remotes are still fetched — until any fetch or pull of the repository
succeeds — a pull that stops on conflicts counts, since it reached the
remote — whether from the toolbar, the AI chat or a git-flow action, or the
app restarts. A repository whose remotes are all stopped this way is
skipped. Meanwhile the toolbar's Fetch shows an amber dot and names the
stopped remotes in its tooltip. The macOS keychain may ask once for access to a stored credential;
"Always Allow" ends that. No failure is notified;
the Commands panel has it. Each remote's fetch times out after 60 s. A
background fetch never shows the busy label, and when it changes the selected repository's remote branches, the
log and the ahead/behind badges refresh. New commits it brings to the
checked-out branch's upstream can notify (see `11-notifications.md`).

### Push

Settings → General → **Push** says what a click on Push (toolbar or the
repository row's menu) does: *Ask each time* (the default), *Current branch
only* or *All branches*. The toolbar button's tooltip follows it: "Push
`<branch>`" (just "Push" with a detached HEAD), "Push all branches" or "Push — asks current or all branches";
its badge always counts the current branch.

With *Ask each time*, Push pushes the current branch without asking when no
other local branch is ahead of its upstream (counted from the last fetch).
Otherwise a dialog "Push `<repository>`" offers *Current branch (`<name>`)*
(selected) and *All branches (N)* — N counts the current branch plus the
others ahead — with "Change the default in Settings → General." under it;
confirming reads "Push" or "Push N branches" ("Push 1 branch" for one),
Cancel pushes nothing. With a detached HEAD it offers only *All branches
(N)*. The repository row's menu also has **Push all branches**, which pushes
all branches whatever the setting says.

**Current branch** publishes the checked-out branch:

- If the branch already has an upstream, Push pushes to it as-is.
- If the branch has no upstream yet, Push sets one on the remote named
  `origin`, publishing under the branch's own name. This is a deliberate
  default rather than a prompt: a first push almost always means "publish
  this to the usual remote," and asking every time would slow down the
  common case.
- A detached HEAD has nothing to publish; Push refuses.

**All branches** is `App.PushAll`. It pushes the current branch (as above;
to its upstream even when that is gone, and up to date counts) plus every
other local branch whose upstream is on a remote, still exists, and is
ahead of it according to the last fetch. A branch that tracks another local
branch, or has no upstream, is never pushed this way; with a detached HEAD
only the other branches go. Each branch goes to its upstream by name
(`branch.<name>.pushRemote` and `push.default` are not consulted), with one
`git push --porcelain` per remote, remotes in name order. Nothing is forced,
no tag is pushed, and the push is not atomic: a branch the remote rejects
does not stop the others. Like Push it takes the repository's write lock and
has no conflict check of its own; the toolbar button and both menu items are
disabled while a conflict is in progress. The busy label reads "Pushing
branches…".

When nothing failed, a toast sums it up: "Pushed `<branch>`" or "Pushed N
branches", plus ", M already up to date" when some were; "Everything up to
date" when none moved; "Nothing to push" when no branch qualified. A push of
a repository that is not the selected one (from its row menu) prefixes the
toast with its name: "beta: Nothing to push". When any branch was not
pushed, a results dialog "Push results — `<repository>`" lists every branch
with its target, one row each, closed by a single OK: ✓ pushed, — up to
date, ✗ and the reason. It opens once the busy label is gone and the refs
have reloaded, so the toolbar is usable behind it. A branch the remote has
moved on from reads "The remote has commits you don't have — pull
`<branch>` first"; any other rejection shows git's reason. A remote that
cannot be reached (network, authentication, a missing remote) fails all of
its branches with git's message, and the other remotes are still pushed; a
push cancelled from the Commands panel reads "Cancelled". If the push cannot
start at all (the repository is missing or another write is running), an
error toast shows instead of the dialog.

### Pull

Pull first checks that the repository has no merge, rebase or other
resolution already in progress; if it does, Pull refuses outright rather than
attempting the fetch and integration. The reason is stricter than
convenience: if a pull ran anyway, the application could not tell a conflict
this pull caused apart from one that already existed, and would misreport
the outcome.

With that guard passed, Pull fetches and then integrates the upstream
according to the configured strategy:

| Strategy | Behaviour |
|---|---|
| Follow repository configuration | Integrates using whatever the repository's own settings resolve to, exactly as an unmodified pull typed at a terminal would. |
| Always merge | Integrates with a merge commit regardless of the repository's own configuration. |
| Always rebase | Replays the local commits on top of the upstream regardless of the repository's own configuration. |

The strategy is a single stored preference for the whole application, not
set per repository or per pull.

Once the integration finishes (or fails), Pull reports one of four outcomes:

| Outcome | Meaning |
|---|---|
| Already up to date | The branch tip did not move; there was nothing to integrate. |
| Merged | The branch tip moved to a new merge commit, or this was the first pull into a repository with no prior commits. |
| Rebased | The branch tip moved to a commit that is not reached by a plain merge from where it started — i.e. the local commits were replayed on top of the upstream. |
| Conflicted | The integration left the repository mid-merge or mid-rebase with one or more files needing attention. |

A conflicted outcome is not treated as a failure: Pull returns it as a result
with the list of conflicting paths, the same way a conflicted merge is
reported elsewhere (see Conflicts). Any other failure — network,
authentication, a rejected push, and so on — is returned as an error instead
of a result.

A successful pull can leave a submodule pointing behind its recorded
commit — git only moves a submodule's checkout along with the parent when
`submodule.recurse` is set, which CommitTree honours (because git does) but does
not set itself. When that happens, a toast reports how many submodules are
affected and offers an "Update all" action (see the Submodules section of
docs/spec/01-repositories-and-sidebar.md); there is no automatic update.

### Ahead/behind

The count is read fresh whenever asked, by comparing the current branch with
its upstream. A branch with no upstream, or any other failure reading the
comparison, reports zero ahead and zero behind rather than an error — the
toolbar simply shows no badges rather than surfacing a failure for a
perfectly normal state (a new local branch, a detached HEAD).

## Stash

The stash list appears in the sidebar, newest entry first — the same order
the repository's own stash history already gives. An empty stash is an empty
list, never an error or a missing section.

Each entry shows:

- Its message, exactly as it was given (or as originating tooling produced
  it, if the stash was created outside the application).
- The branch it was taken from.

### Creating a stash

Stashing prompts for an optional message and a checkbox for including
untracked files. Only the working tree's tracked changes are captured unless
the box is checked, in which case untracked files are captured too (ignored
files never are).

If there is nothing to stash — no staged, unstaged or untracked change to
capture — the operation is refused rather than silently creating an empty
entry. This mirrors the rule that disables committing with nothing staged;
the refusal exists as a backstop in the application layer even though the
button is normally already disabled for the same reason, covering the case
where the working tree changes between the button being enabled and the
click being handled.

### Applying, popping and dropping

Three actions act on a stash entry:

- **Apply**: reapplies the entry to the working tree and keeps it in the
  list.
- **Pop**: reapplies the entry and then removes it from the list, in one
  step.
- **Drop**: removes the entry from the list without reapplying it. This
  cannot be undone.

Applying a stash is offered through a confirmation dialog with a checkbox
labelled to delete the stash after applying it. Leaving the checkbox
unchecked performs a plain Apply; checking it performs a Pop instead — the
checkbox does not apply and then separately drop the entry, because Pop
already handles a conflicted outcome correctly (see below) and a separate
apply-then-drop would not. Popping directly (from the list's own action, not
through the Apply dialog) asks for a plain confirmation with no checkbox,
since Pop's own behaviour already implies deletion once resolved. Dropping
asks for a plain, explicit confirmation, since it discards the entry
outright and cannot be undone.

Both Apply and Pop can leave the repository conflicted, the same way a merge
can: reapplying a stash is itself a kind of merge between the working tree
and the snapshot. A conflicted Apply or Pop is not reported as a failure to
the caller — it is reported through the same conflict state a merge or
rebase reports, distinguished by its own kind so the conflict view can tailor
itself (see Conflicts). Unlike a merge or rebase conflict, a stash conflict
has no underlying abort command; the only way out of it is to resolve every
conflicting file (or otherwise let it settle) and finish, or dismiss the
conflict view without resolving it. Dismissing does not change any file — it
only lets the Changes view and the rest of the toolbar take over the screen
again; a "Resolve conflicts" button reappears in the toolbar as the way back
into the dismissed conflict.

### Pop and the owed drop

When Pop leaves a conflict behind, the stash entry is deliberately kept
rather than dropped — the user may still need it if the conflict cannot be
resolved. The application remembers, for that repository, that this entry's
drop is still owed once the conflict is resolved, and the conflict view
offers a "Drop stash" action for exactly that entry once it is available;
the action is present only when an owed drop exists for the repository, and
what it targets is decided at the moment it is offered, not fixed when the
conflict began.

The reminder is keyed by the stash's own identity (the commit it points to),
not by its position in the list. Positions shift: any stash pushed, applied-
and-dropped, or otherwise removed above the entry the reminder is waiting on
changes every position below it, in this repository and in any other
application session or terminal working against the same repository, since
the stash list is shared, not private to this application. Resolving the
reminder against identity rather than position means it always finds the
entry it meant, or reports there is nothing left to drop if that entry no
longer exists by any means (a hand-run drop, or another pop that consumed
it).

The reminder does not survive the application restarting: it exists only for
the running session, held in memory and nowhere else. If the application is
closed and reopened while a conflicted pop's drop is still owed, the
reminder is gone — the stash entry itself is untouched and still sits in the
list, but the application no longer offers the dedicated "Drop stash" action
for it. It can still be dropped by hand like any other entry once the
conflict is otherwise settled. This is a real gap rather than a documented
design choice: nothing in the codebase persists the reminder across a
restart, and nothing explains why not.

### Previewing a stash

Selecting an entry shows every file the stash touches, tracked and untracked
together, tracked changes listed first. Selecting a file shows its diff:
for a tracked file, the change against what the stash captured; for a file
that was only ever untracked, its whole content shown as newly added. A
renamed file's diff carries both its old and new path so the change reads as
a rename rather than as a deletion and an unrelated addition.

An empty stash (nothing captured) shows an explicit empty state rather than
an empty pane. Switching to a different stash entry always reloads its file
list from nothing; there is no continuity to preserve between two unrelated
stashes.

## Rules

1. Fetch never refuses because of a conflict; Pull and Push always do.
2. Only one write operation runs per repository at a time; any of Fetch,
   Pull, Push, or a stash action refuses rather than interleaving with
   another already running for the same repository — except a background
   fetch, which is cancelled (as Cancel in the Commands panel would) so the
   user's operation runs instead (a second operation started meanwhile is
   refused as usual). A background fetch itself never waits: it is skipped
   while another write runs.
3. Pull refuses outright, without attempting anything, when the repository
   already has an unresolved merge, rebase or stash conflict.
4. A push with no upstream sets one on the remote named `origin`, using the
   branch's own name, without asking.
5. Pull reports exactly one of: already up to date, merged, rebased,
   conflicted. A conflicted pull is a result, not an error.
6. Stashing with nothing to stash is refused, not silently accepted as an
   empty entry.
7. Applying a stash never deletes it; popping always removes it once there
   is no conflict left requiring it; dropping always removes it regardless
   of its contents.
8. A stash conflict has no underlying abort; it is left in place until
   resolved or dismissed, and dismissing changes no files.
9. The owed-drop reminder for a conflicted pop is identified by the stash's
   commit, never by its list position, so a shift in the stash list from any
   source cannot make it target the wrong entry.
10. The owed-drop reminder is in-memory only and does not survive an
    application restart.
11. The ahead/behind count, and any other read of a branch's upstream
    relationship, reports zero rather than an error when there is no
    upstream to compare against.

## Known divergences

None.
