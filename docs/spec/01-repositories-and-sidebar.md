# Repositories and sidebar

The sidebar is the left-hand panel of the application and the only place a
user manages their list of repositories. From here they add a repository,
organise many of them into groups, pick which one is active, and — for the
active repository — browse and act on its branches, remotes, tags and
stashes. Every other area of the application acts on whichever repository is
selected here.

## Concepts

- **Repository entry**: one record in the user's list — an identifier, a
  display name, a filesystem path, and an optional group name. The list is
  the user's own; it is not discovered from disk.
- **Missing repository**: an entry whose path no longer contains a git
  working tree (the directory was moved, renamed or deleted).
- **Group**: a label shared by zero or more repository entries. A group is
  nothing but a name stored on each repository — there is no separate list
  of groups, so a group exists exactly as long as some repository carries
  its name, and two groups become one the moment a repository's name string
  matches another's.
- **Loose repository**: an entry with no group.
- **Selected repository**: the one repository whose log, changes, branches
  and stash are shown in the rest of the window. Exactly zero or one
  repository is selected at a time.
- **Write lock**: a repository-scoped lock. While one write is running
  against a given repository, every other write against that same
  repository is refused; writes on different repositories never block each
  other. Reads are never blocked by it.

## Repository list

The sidebar lists every repository the user has added, above an "Add repo"
control. Adding a repository opens a directory picker; the chosen directory
must be a git working tree (checked with `git rev-parse --show-toplevel`
from that directory) or the add is refused with an error. Given a
subdirectory of a working tree, the entry records the tree's top-level
directory, not the subdirectory that was picked. The entry's display name is
the top-level directory's own name.

Adding a path that already has an entry (comparing the resolved top-level
path) returns the existing entry rather than creating a duplicate; nothing
changes and the existing entry is selected. Every entry gets a stable
identifier derived from its path when it is first added; that identifier
never changes for the entry's lifetime, including across a relocation.

Removing a repository asks for confirmation, naming the repository and
stating that files on disk are left untouched. Removal only deletes the list
entry — no repository, missing or not, is ever deleted or modified on disk
by this action. If the removed repository was selected, selection is
cleared.

### Missing repositories

A repository is flagged missing purely by checking, on every list read,
whether its recorded path still contains a `.git` entry — nothing is cached.
An entry can flip between missing and present from one moment to the next if
the underlying directory reappears or vanishes. A missing repository:

- shows a "missing" badge in place of its current-branch label, and cannot
  be expanded to show branches, remotes, tags or stash (its row's expand
  control is disabled and greyed out);
- offers a "Locate…" action, in addition to the entry's other actions, that
  is not offered on a present repository;
- has every other row action (fetch, pull, push) refused;
- is skipped when the sidebar loads per-repository state (refs, merge
  state, worktree state, remote info, stash) for the selection — that state
  is simply cleared instead.

"Locate…" opens the same directory picker as adding a repository, and
re-points the existing entry at the chosen directory (again requiring it to
resolve to a git working tree's top level) without changing the entry's
identifier. Locating a repository forgets any commit-log paging state held
for its previous location, so browsing history afterwards starts fresh.

## Repository groups

A repository is assigned to a group from its row's context menu ("Move to
group…") or by dragging its row and dropping it onto a group's header or
body. Both paths call the same assignment and are no-ops when the drop or
choice would leave the repository in the group it is already in (including
dropping a loose repository on the loose area).

"Move to group…" offers every group name currently used by any repository,
plus "No group" (removes the repository from a group) and "New group…"
(prompts for a new name and assigns it). A drag can only target a group that
is already showing in the sidebar, or the loose area.

Dragging a repository onto the empty loose area is how a grouped repository
is put back to being loose; the loose area is always a valid drop target
even when it currently holds no rows, and shows a placeholder message for
the duration of any repository drag so it never disappears entirely.

### Renaming and merging groups

A group's context menu (opened from its header) offers "Rename group…",
prompting for a new name pre-filled with the current one. What happens next
depends on the name entered:

| Entered name | Result |
|---|---|
| Empty, whitespace-only, or unchanged after trimming | No-op: nothing is written. |
| Not currently used by any other group | Every repository in the old group is moved to the new name in one write. |
| Already used by a different, existing group | The two groups merge: every repository from both ends up under the entered name. The user is warned this cannot be undone and must confirm before it happens. |

A merge is not a distinct operation from a rename — because a group is only
ever a string on a repository, renaming a group to a name another group
already holds *is* merging them; there is no other way two groups become
one. Renaming a group that no longer has any repository in it (for instance
because they were all just moved out) is silently a no-op.

Whether a group is collapsed is remembered by name (see below); a rename or
merge carries the old name's collapsed state onto the new name so the
section does not visibly jump open or shut. When a merge combines a
collapsed group with an expanded one, the result is collapsed.

### Layout

Loose repositories are listed first, in the order they appear in the
underlying list, followed by one section per group, sorted alphabetically by
group name. A group section is a header row (its name and a count of the
repositories in it) that toggles the group between expanded and collapsed;
collapsing hides its repository rows. Groups start expanded; only collapsed
groups need to be remembered, and that memory is a plain list of collapsed
group names, held for the whole application rather than scoped to any one
repository.

## Selection

Clicking a repository row selects it. Selecting a repository that is already
folded also unfolds it, so its branches/remotes/tags/stash become visible;
folding it back afterwards does not deselect it. Switching the selected
repository resets the commit-log filters, clears the selected commit and any
stash preview, and returns the main view to the log.

Only the repository whose row is clicked is selected — clicking a row for a
branch, tag or stash entry that belongs to a repository other than the one
currently on screen selects that repository first (as if its row had been
clicked), then performs the row's own action.

## Per-repository sections

Once expanded, a repository's row is followed by its own sections, in this
fixed order: **Changes**, **Branches**, **Remotes**, **Tags**, **Stash**.
These render only once refs have loaded for the repository; if they fail to
load (for instance because the repository just went missing) they are
hidden entirely. The current branch (or `HEAD (<short-hash>)` when the head
is detached) is not part of this list; a detached head is shown as its own,
non-interactive row above the branches.

**Changes** is a single row that opens the Changes view for this repository;
it shows a count badge when the working tree has changes and no badge
otherwise. Clicking it while the log for this repository is already showing
opens Changes; clicking it again returns to the log. While a merge is in
progress, the row instead always returns to the log, because the merge view
takes over that space.

### Branches

Local branches whose name contains no `/` are listed loose, each showing a
checkmark next to the current branch. A "+" control next to the section
heading creates a new branch from `HEAD`. Local branches whose name does
contain a `/` are grouped by the first path segment (so `feature/x` and
`feature/y` fall under a `feature` group, and a name only one level deep
still gets a group of one); groups are listed after the loose branches,
sorted by name.

A branch group opens automatically the first time it is shown if it
contains the current branch or the branch the commit-log filter currently
points at; after that, its open/closed state is remembered by hand (a
click) and stops following either of those. Nested groups are only one level
deep — a name like `release/2026/spring` groups under `release`, and its
full remainder (`2026/spring`) is shown as the leaf text.

A branch row's double click checks it out (refused while the write lock is
held, or if it is already the current branch); its context menu offers:

| Action | Refused when |
|---|---|
| Check out | It is the current branch, or a write is already running. |
| Merge `<branch>` into `<head>` | It is the current branch, a write is running, the head is detached, or a merge is already in progress. |
| New branch from here… | Never. |
| New tag here… | Never. |
| Delete… (local) / Delete on remote… (remote-tracking) | It is the current branch. |

### Remotes

Shown only when the repository has at least one remote. Each remote is a
row that toggles open to show that remote's remote-tracking branches,
grouped exactly as local branches are grouped (loose names, then
`/`-prefixed groups, one level deep). The `origin/HEAD`
pointer some remotes carry is filtered out entirely and never shown as a
branch. The remotes section as a whole, and each individual remote's open
state, are UI-only and not remembered across the section collapsing and
reopening — they default to open every time the repository's refs are
freshly shown.

### Tags

The Tags section header always shows a count. It starts collapsed, and
whether it has been expanded is remembered per repository. Expanding it
with no tags shows "No tags" rather than an empty list. A "+" control
creates a new tag at `HEAD`. A tag row filters the commit log to that tag
when clicked; its context menu offers "New branch from here…" and a
(always available) "Delete…".

### Stash

The Stash section shows a count and, when expanded (it starts open and its
open/closed toggle is UI-only, not remembered), one row per stash entry,
newest first, labelled with the stash's message. "No stashed changes" is
shown in place of an empty list. Clicking an entry previews it (opens the
stash preview in the main view); double-clicking applies it immediately
without confirmation. Its context menu offers Apply, Pop and Drop, each
refused while a write is already running on this repository. Stash entries
are identified for selection purposes by the stash's own commit hash, not
its position in the list, because applying, popping or dropping any entry
shifts the position of every entry below it — including from another
running instance of the application working against the same repository,
since the stash is stored in the repository itself and is not private to
one window.

## What is refused while something else is running

The application holds one write lock per repository (never a single global
lock — two different repositories can be written to at the same time).
Checking out a branch, a remote branch or a commit, resetting the current
branch, creating or deleting a branch, creating or deleting a tag, and every
stash write (push, apply, pop, drop) and worktree/merge write share this
same per-repository lock. A second write attempted against a repository
while one is already running on it is refused outright, with no queueing —
the caller must retry once the first has finished. Fetch, pull and push from
a repository row's menu are likewise refused while a write is already
running against that repository, and pull and push are additionally refused
while a merge is already in progress. Reads (the repository list, refs, log,
stash list, diffs) are never subject to this lock and are always served,
including for a repository whose write lock is currently held.

## Rules

1. The repository list is user-curated, not auto-discovered; nothing is
   added to or removed from it except by an explicit add, remove, or
   relocate.
2. A repository's identifier is assigned once, from its path, and never
   changes afterwards, including across a relocation; adding the same path
   again always resolves to the existing entry and identifier.
3. A repository is "missing" purely on whether its recorded path currently
   contains a git working tree, re-evaluated on every read, never cached.
4. A missing repository can only be located or removed; every other action
   on it is refused, and it cannot be expanded.
5. A group is a name stored per repository, not a separate object; renaming
   a group to a name already in use by another group is indistinguishable
   from — and behaves exactly as — merging the two groups, and this cannot
   be undone.
6. Dropping or assigning a repository to the group it is already in (or the
   loose area, if it is already loose) does nothing.
7. Exactly one repository is selected at a time; selecting a different one
   resets that repository's log filters, selected commit, and stash
   preview, but never touches the previously selected repository's own
   remembered UI state (expanded/collapsed sections).
8. Branches and remote-tracking branches with a `/` in their name are
   grouped by their first path segment only, one level deep, even for a
   group that ends up with a single member.
9. Tags start collapsed per repository; branch groups, remotes, and the
   stash section each follow their own, independent default-open rule and
   are not all remembered the same way (see each section above).
10. Every write to a repository — branch, tag, stash, worktree or merge
    operations alike — is serialised through one lock per repository; a
    second concurrent write on the same repository is always refused, never
    queued.

## Known divergences

None: no earlier design document describing the sidebar or repository
groups was found in this repository to compare the built behaviour against.
