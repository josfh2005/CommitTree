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
control. "Add repo" opens a menu with **Open folder…** and **Clone…**. Open
folder… opens a directory picker; the chosen directory must be a git working
tree (checked with `git rev-parse --show-toplevel` from that directory) or the
add is refused with an error. Given a
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
cleared. Any embedded-terminal tabs open on the repository are closed along
with it, without a separate confirmation (see Terminal).

A repository row's context menu is grouped, with a line between groups:
Locate… (only while the folder is missing); Fetch, Pull, Push, Push main branches; the
show-in-file-manager action and Open in Terminal; Repository settings…
(disabled while the folder is missing; see `12-repository-settings.md`);
Move to group… and Remove from list…. A linked worktree's menu has Fetch,
Pull, Push, Push main branches; the same two open actions; and Remove worktree… — no Repository
settings…, since its remotes are its main repository's.

A repository row's context menu also offers to show the repository's
working tree in the platform's file manager, labelled in the platform's own
words: "Show in Finder" on macOS, "Show in Explorer" on Windows, "Open in
File Manager" elsewhere (it hands the directory to `xdg-open`). The file
manager is launched and not waited on. It is refused for a missing
repository. The same mechanism opens the prompts folder from the AI
settings, on every platform.

Next to it, "Open in Terminal" opens a new window of the system terminal in
the repository's working tree — the Terminal app on macOS, a Command Prompt
on Windows, `x-terminal-emulator` elsewhere — also launched and not waited
on, and refused for a missing repository. It is separate from the embedded
terminal (see Terminal).

### Cloning a repository

**Clone…** opens a dialog with the URL, the parent folder (typed or picked
with "Choose…"; the last one a clone started with, accepted by the backend, is
remembered, the home folder before that; a leading `~` means the home folder, and anything that is not a full
path is refused) and the folder name. The name follows the URL as it is typed —
its last path segment without `.git` — until the user edits it; clearing it
makes it follow again. While the parent folder is typed, a dropdown under the
field suggests folders: see "Parent folder suggestions" below. The name may not be empty, `.` or `..`, or contain a
slash, and the destination must not exist or must be an empty folder; a URL
that is empty or starts with `-` is refused.

Parent folder suggestions: while the user types in the parent field (not when
the value is set by the remembered parent or by "Choose…"), a dropdown under
the field lists the folders matching the text, looked up ~150 ms after the
last keystroke (only the newest answer is shown). The text up to its last `/`
is the folder to list and what follows is the prefix a name must start with
(case-sensitive). Only folders are listed (a symlink to a folder counts, files
do not), those whose name starts with `.` only when the prefix itself starts
with `.`, sorted by name, at most 50. A leading `~/` is expanded to look the
folder up but suggestions keep the typed `~/` form; a bare `~` suggests `~/`;
`~user`, relative paths, and folders that do not exist or cannot be read give
no suggestions (and no error). Surrounding whitespace is ignored. With no
suggestion the dropdown is not shown. Keys in the field: Down / Up move the
highlight (wrapping; from nothing, Down goes to the first and Up to the
last); Tab or Enter complete the highlighted folder and add a `/` so the next
level is listed straight away; Tab with nothing highlighted completes the first
suggestion while a name is being typed (the text does not end in `/`),
otherwise (and with Shift) it moves focus as usual; Enter with nothing
highlighted submits the form as usual; Esc closes the dropdown only, not the
dialog. Between a keystroke and the answer for the new text the list is stale:
nothing is highlighted and only Esc acts on it (Tab moves focus, Enter
submits). Esc also cancels a pending lookup. Clicking a suggestion completes it the same way. The dropdown also
closes when the field loses focus, when Choose… is used and on Clone.

**Clone** runs `git clone --progress --recurse-submodules -- <url> <dest>`
from the parent folder. Credentials come only from what is already set up
(credential helpers, the keychain, ssh-agent, Git Credential Manager); the
application never asks for a password. The dialog shows the resolved
destination (a typed `~` expanded) and git's current phase
(counting, compressing, receiving, resolving, updating files, each
submodule), a bar with the phase's percentage when git gives one, and git's
detail line. **Cancel** stops git as Ctrl+C would; **Continue in
background** closes the dialog without stopping the clone. One clone runs at
a time: **Clone…** during a clone reopens its progress.

There is no fixed time limit; the clone is stopped as stalled when git
writes nothing for five minutes. When it succeeds, the repository is added
as Open folder… would add it, and selected; with the dialog closed, a toast
says "Cloned <name>". When it fails, the dialog returns to the form with its
values and the reason — authentication, an untrusted SSH host key, a
repository not found, a stall, or git's own message, with credentials
hidden; with the dialog closed, an error toast offers **Show**. The progress
view shows the URL with its credentials hidden, and **Cancel** reads
"Cancelling…" until git has stopped. When git fetched the repository but
then failed on a submodule or the checkout, the folder is kept: the
repository is added and selected, and an error toast says "Cloned, but some
submodules or files could not be checked out" with git's reason. A
destination folder the clone created is otherwise removed after a failure or
cancel; an empty folder that existed before is left in place. Quitting the
app cancels a running clone. The clone is recorded like any
command but, belonging to no repository, does not appear in the Commands
panel.

### Worktrees

Every read of the repository list also asks git, for each present stored
repository that is a main working tree, which linked worktrees it has
(`git worktree list --porcelain -z`). Each linked worktree whose directory
exists, and that git does not report as prunable or bare, is listed as a
child of that repository, labelled with its directory's name and its
current branch (or `HEAD (<short hash>)` when detached). Nothing about
detected worktrees is stored: one removed elsewhere simply stops being
listed at the next read. The main working tree is the repository's own
entry and is never listed again as a child. A stored repository that is
itself a linked worktree of another listed repository is shown as that
repository's child instead of at the top level, keeping its own identifier,
and gets no children of its own (git would list its siblings from it).
Because worktrees come and go without touching the selected repository's
refs, the list is re-read every time the window regains focus (and after the
embedded terminal settles), not only when that repository changed. When a
worktree stops being listed, its terminal tabs are closed and, if it was
selected, the selection is cleared exactly as when a repository is removed.

When a local branch is checked out in another worktree, switching to it is
refused with "<branch> is checked out in another worktree (<path>)" rather
than git's own message.

A linked worktree's context menu offers "Remove worktree…", disabled while
another operation runs against the repository and, separately, when git
reports the worktree as locked (`git worktree lock`) — disabled with a
tooltip explaining that it is locked, since removal is refused either way
until it is unlocked. It never appears for the main repository itself or for
an ordinary list entry.

Choosing it first reads the worktree's state: its branch (or detached),
how many uncommitted changes it has, and whether its branch is merged into
the main working tree's HEAD — exactly what `git branch -d` would accept.
The confirmation names the worktree's folder and states that the folder is
deleted from disk; a worktree with uncommitted changes adds how many will
be lost ("It has N uncommitted changes that will be lost", singular for
one) and its button reads "Remove anyway" instead of "Remove" — removal
then passes `--force` to `git worktree remove`, which is otherwise refused
for a worktree with uncommitted changes. A non-detached worktree's
confirmation also offers a checkbox, "Also delete branch <name>", checked
by default only when the branch is already merged; a detached worktree gets
no checkbox, since it has no branch to offer.

Removal runs `git worktree remove` (with `--force` when there are
uncommitted changes) from the main repository's directory, under the main
repository's write lock, so it cannot interleave with a merge, rebase or
cherry-pick on that repository. When the branch checkbox was checked, `git
branch -d` (never `-D`) runs next, once the worktree itself is gone. If git
refuses because the branch turns out not to be merged after all, the
worktree stays removed — only the branch survives — and the app offers the
same force-delete confirmation it shows when deleting an ordinary branch
git refuses for the same reason; declining it simply leaves the branch in
place. The repository list is refreshed once removal finishes, the same way
every other write refreshes it.

### Submodules

Every read of the repository list also detects each present item's
submodules (recursively, into every initialised one), the same way it
detects worktrees: nothing about them is stored, and the count and
children are rebuilt from scratch at every read. Unlike a worktree, a
detected submodule is never a row in this list — it carries a
`submoduleCount` on the item that has it (a stored repository or a
detected worktree), and, for each of its own submodules that is
initialised, a child item nested under it the same way a worktree's
sub-worktree would be, keyed by its own path-derived identifier. An
uninitialised or unconfigured submodule still counts, but gets no item. Nor
does one whose absolute path is already a stored repository in its own
right (added separately, before or after it became a submodule of
another) — that path keeps its single, existing item rather than gaining a
second, duplicate one.
These child items exist so a submodule can be opened as its own repository
(see Per-repository sections); they are never sidebar rows themselves.

A submodule row's menu offers up to three writes. Each runs `git` in the
submodule's **direct parent** — the top repository for a first-level
submodule, or the absolute directory of the nearest submodule that
contains it for a nested one (e.g. `vendor/lib` for `vendor/lib/deps/zlib`)
— with the path passed to git relative to that direct parent, never the
top-relative path run from the top (git refuses a pathspec that reaches
into a different repository). Locking follows the same shape: the top
repository's write lock, the direct parent's write lock (only when it
differs from the top, i.e. for a nested submodule), and the submodule's own
write lock are all taken (all `TryLock`; any already held → busy) — see
"One write lock per repository" in Conventions and constraints.

- **Initialise** (only when not initialised) runs
  `git submodule update --init -- <path>`, cloning it if needed and
  checking it out at the commit the parent's index records.
- **Update to recorded commit** (only when initialised and moved) runs
  `git submodule update -- <path>`, moving it back to the recorded commit.
  It is refused, not forced, when doing so would overwrite local changes
  inside the submodule; the refusal reads "<path> has local changes that
  updating would overwrite. Commit or stash them inside the submodule
  first." — `<path>` is always the top-relative path, even though the
  command git ran used the path relative to the direct parent.
- **Sync URL** runs `git submodule sync -- <path>`, copying the current
  URL from `.gitmodules` into the submodule's own remote configuration.

The section header's menu offers **Initialise all**
(`git submodule update --init --recursive`) and **Update all**
(`git submodule update --recursive`, refused the same way as a single
Update when it would overwrite local changes anywhere underneath), each
touching every submodule under the repository at once, run once from the
top (recursion is git's own, so no per-submodule direct-parent splitting
applies here). When git's refusal does not name which submodule it
refused, the message reads "A submodule has local changes that updating
would overwrite. Commit or stash them inside the submodule first." instead
of naming one.

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
- has every other row action (fetch, pull, push, push main branches) refused;
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

Loose repositories are listed first, followed by one section per group.
The order button next to the "Repos" heading chooses how the rest is
ordered, and the choice is remembered:

- **By name** (the default): groups are sorted alphabetically by name, and
  within the loose area and every group repositories are sorted by display
  name, ignoring case and accents, with the path breaking ties — never by
  the order they were added. Dragging a repository only moves it into or
  out of a group (see Groups).
- **Manual (drag to arrange)**: repositories and groups keep the order the
  user arranges. Dropping a repository on another repository row places it
  just above that row — in the same area, or in that row's group, moving it
  there — with an accent line marking the spot; dropping it on a group's
  header or on the empty space of an area puts it at the end of that group
  or area. A group header can be dragged too: over the upper half of another
  group it lands above that group, over the lower half below it. A newly
  added repository goes to the end of the loose area. The first switch to
  manual order starts from the by-name order, so nothing moves; later
  switches back and forth keep the manual arrangement, which is stored with
  the repository list.

A repository's detected worktrees are rows directly under its own row (after
its expanded sections, when it is expanded), indented one level deeper and
marked "↳", sorted by name the same way. They follow their main repository
wherever it is, whatever their own group field says, and are visible whether
or not the main repository is expanded or selected. A worktree row selects
and expands exactly like a repository row. Its context menu offers only
Fetch, Pull, Push, Push main branches, the show-in-file-manager action, Open in Terminal and
Remove worktree…; a stored repository
shown nested (see Worktrees) keeps "Remove from list" and loses only "Move
to group…". A worktree row cannot be dragged into a group. A group section is a header row (its name and a count of the
repositories in it) that toggles the group between expanded and collapsed;
collapsing hides its repository rows. Groups start expanded; only collapsed
groups need to be remembered, and that memory is a plain list of collapsed
group names, held for the whole application rather than scoped to any one
repository.

## Selection

Clicking a repository row selects it and loads its log, without unfolding
its branches/remotes/tags/stash: those show or hide with the row's chevron,
or by double-clicking the row. Folding or unfolding never changes the
selection. Switching the selected
repository resets the commit-log filters, clears the selected commit and any
stash preview, and returns the main view to the log.

Only the repository whose row is clicked is selected — clicking a row for a
branch, tag or stash entry that belongs to a repository other than the one
currently on screen selects that repository first (as if its row had been
clicked), then performs the row's own action.

## Per-repository sections

Once expanded, a repository's row is followed by its own sections, in this
fixed order: **Branches**, **Remotes**, **Tags**, **Stash**, and, last and
only when the repository has at least one submodule, **Submodules**. Each
section heading starts exactly under the repository's name, whether the
repository is loose or inside a group; its rows sit one step further in.
Branches/Remotes/Tags/Stash render only once refs
have loaded for the repository; if they fail to load (for instance because
the repository just went missing) they are hidden entirely. Submodules does
not depend on refs: its header and count show as soon as the repository is
expanded, independently of whether refs loaded. The current branch (or
`HEAD (<short-hash>)` when the head is detached) is not part of this list; a
detached head is shown as its own, non-interactive row above the branches.

Every expanded repository shows its own branches, remotes, tags and stash,
never the selected repository's. The selected repository's sections follow
the live state every action refreshes; any other expanded repository reads
its own when it is expanded, and again whenever the repository list is
re-read (after an action, a fetch or a window focus) and when it stops
being the selected one. Until they have loaded its sections stay hidden.

An opened submodule (see Submodules below) shows this same set of sections
except Submodules itself — its own submodules, if any, are already part of
the top repository's flat list.

The selected repository's row stands out from every other: it has the
selection background with square corners, a straight accent-coloured bar
along its left edge, its name in bold and its branch label in the accent
colour.

The selected repository's row shows, after its branch, a small count of
its uncommitted changes (distinct changed paths) when the working tree has
any, and nothing otherwise; other repositories' rows show no count, since
only the selected repository's working tree is read. The changes themselves
are reached from the log's "Uncommitted changes" row or the toolbar's
**Commit** (see `02-log-and-history.md` and `05-remote-and-stash.md`); there
is no separate Changes row in the sidebar.

### Branches

A local branch that another worktree of the same repository has checked
out shows a "worktree" badge, and its tooltip reads "Checked out in
<directory name>". Checking it out (double-click or the menu) and deleting
it are disabled, since git would refuse both. That includes a worktree whose
directory has been deleted but that git has not pruned yet — git still holds
the branch for it — and the tooltip then adds that the directory is gone and
`git worktree prune` releases it.

A local branch with an upstream shows how far apart they are, as of the
last fetch, at the right of its row (before the "worktree" badge and the
funnel icon): `↑N` in red for commits the branch has that the upstream
lacks, `↓M` in blue for commits the upstream has that the branch lacks,
both when they have diverged. Nothing shows when both are zero, without an
upstream, or when the upstream is gone. The tooltip reads "N commits to push
to <upstream> · M commits to pull, as of the last fetch", leaving out a zero
part. The counts come with the branch list, so they refresh whenever it
does (after a fetch — background ones included —, pull, push, commit,
checkout…), in every expanded repository. Remote-tracking branches and
collapsed folders show none. The two colours keep a 4.5:1 contrast on the
sidebar in light, dark and both high-contrast themes.

Local branches whose name contains no `/` are listed loose. The current
branch shows a checkmark and its name in bold; in the selected repository it
also gets a soft accent-tinted background (unless it is the branch the log is
filtered by, which keeps that row's own highlight). An expanded repository
that is not selected shows its current branch without the tint, so only the
active repository's rows carry an accent background. The branch or tag the log is filtered by (from the
filter bar, or a tag row's click) is highlighted and carries a funnel icon
whose tooltip says the log is filtered to it; only the selected
repository's rows are marked, since the filter belongs to its log. A "+" control next to the section
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
held, or if it is already the current branch); its context menu offers the
entries below, in groups separated by a line: Check out; then Fetch, Pull and
Push (a local branch) or only Fetch (a remote-tracking branch); then Merge and
Rebase; then New branch and New tag; then Delete. Fetch, Pull and Push are
described in docs/spec/05-remote-and-stash.md ("Per-branch Fetch, Pull and
Push").

| Action | Refused when |
|---|---|
| Check out | It is the current branch, it is checked out in a linked worktree, or a write is already running. |
| Fetch `<remote>` | A write is already running. `<remote>` is the branch's upstream remote, a remote-tracking row's own remote, or, for a local branch with no upstream, `origin` (else the only remote; with neither it is disabled, "No remote"). |
| Pull `<branch>` (local branches) | A write is running, a merge, rebase or cherry-pick is in progress, the branch has no upstream ("No upstream"), its upstream was deleted on the remote, it is checked out in a linked worktree, or it tracks a local branch ("Tracks a local branch"); each shows its reason as a tooltip. |
| Push `<branch>` / Publish `<branch>` to origin (local branches) | A write is running, a merge, rebase or cherry-pick is in progress, or the branch tracks a local branch ("Tracks a local branch"). The label is "Publish `<branch>` to `<remote>`" when the branch has no upstream or its upstream was deleted (the push re-creates it); with no remote to publish to it is disabled ("No remote"). |
| Merge `<branch>` into `<head>` | It is the current branch, a write is running, the head is detached, or a merge is already in progress. The confirmation asks "Merge `<branch>` into `<head>`? A merge commit is always created." When `<branch>` is a local branch behind its upstream (counted from the last fetch; nothing is fetched), it becomes a choice instead: "Merge `<upstream>`" (the default) or "Merge `<branch>` as it is", with a message naming how many commits only the upstream has (and, when the branch also has commits of its own, that merging the upstream leaves those out); the chosen one is merged. A remote-tracking branch, a branch without an upstream or not behind it, or counts that cannot be read keep the plain confirmation. |
| Rebase `<head>` onto `<branch>` | It is the current branch, the head already contains it, a write is running, the head is detached, or any conflicted operation is in progress. Uncommitted changes to tracked files refuse it on click ("Commit or stash your changes first"). The confirmation states how many commits are replayed and warns, without blocking, when some are already on the upstream (a force-push, which the application does not offer, would be needed) or when merge commits in the range will be flattened. |
| New branch from here… | Never. |
| New tag here… | Never. |
| Delete… (local) / Delete on remote… (remote-tracking) | It is the current branch. |

Checking out a remote-tracking branch `<remote>/<name>` switches to the
local branch `<name>`, creating it to track the remote branch when it does
not exist. When it exists and is only behind the remote branch, it is
fast-forwarded to it after the switch, and a notice says "`<name>`
fast-forwarded to `<remote>/<name>`". When it has commits the remote branch
lacks, it is checked out as it is and a notice says so ("`<name>` has
commits not on `<remote>/<name>` — checked out your local `<name>`, not the
remote commit"). A local branch that is ahead is checked out silently. The
chat's checkout tool reports the same two cases in its result.

The toolbar's Merge button offers the same merge for any local or
remote-tracking branch through a searchable picker (see
`05-remote-and-stash.md`, "Merge branch picker").

The toolbar's Flow button, next to Merge, starts and finishes git-flow
branches; see [git-flow](10-git-flow.md).

A disabled entry that has a reason shows it as a tooltip.

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

The Stash section header always shows a count. For a worktree, its tooltip
says the stash is shared with the main repository and its other worktrees —
git keeps one stash per repository. Like Tags, it starts
collapsed, and whether it has been expanded is remembered per repository.
When expanded it shows one row per stash entry,
newest first, labelled with the stash's message alone (no icon — the row's
position under the Stash header is enough context). "No stashed changes" is
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

### Submodules

Shown last, only when the repository has at least one submodule. The header
always shows a count (initialised and not). Like Tags and Stash it starts
collapsed, and whether it has been expanded is remembered per repository.
Expanding it reads the flat, recursive list described above (Repository
list › Submodules); a submodule read that fails shows an error row
"Could not read submodules: &lt;reason&gt;" in place of the list, and an
empty (but successfully read) list shows "No submodules".

Each row shows the submodule's path — the leading folders dimmed, the last
segment in normal weight — and, right-aligned, either its state markers or a
dimmed label when it cannot be opened as-is:

| State | Marker / label |
|---|---|
| In sync | none |
| Moved | `↕` + the checked-out commit's short hash |
| Modified content | `●` |
| Untracked content only | `○` |
| Conflict (unmerged gitlink) | `!` |
| Not initialised | row dimmed, label "not initialised" |
| Not configured (a `.gitmodules` entry with no gitlink, or a gitlink with no `.gitmodules` entry) | row dimmed, label "not configured" |

"Moved" and "modified"/"untracked" markers combine when both apply. Hovering
a row shows the recorded commit, the checked-out commit (and its branch, if
any), the submodule's URL, and its name when it differs from its path.

Clicking an initialised row opens it as its own repository (its Changes,
Branches, Remotes, Tags and Stash appear under the same row — see above);
clicking the already-open row again returns to the parent, the same
click-to-deselect idea as the log. Opening a submodule also expands its
parent repository's own row in the sidebar if it was folded, since the
Submodules section — where the opened row is highlighted — only renders
under an expanded row. A row that is not initialised, or not configured,
does nothing on click.

A row's context menu offers Open, Initialise, Update to recorded commit,
Sync URL, Show in Finder and Open terminal here, each shown only in the
states given for the writes above (Repository list › Submodules) — Open,
Show in Finder and Open terminal here alongside them whenever the row is
initialised. The one exception: a "not configured" row's menu offers only
Show in Finder, and only when it already has a working tree to show — git
has no way to initialise or update a submodule with no `.gitmodules` entry,
or clone one with no gitlink.

If the submodule an opened row points at is deinitialised or removed while
selected, the next read drops its item and the selection falls back to the
parent repository — the parent stays selected and expanded, rather than the
selection being cleared outright as it would be for a vanished worktree.

Checking out a branch, a remote branch or a commit, resetting the current
branch, or merging a branch into it — each elsewhere in this repository, not
in the Submodules section itself — can leave a submodule pointing behind its
recorded commit: git only moves a submodule's checkout along with these when
`submodule.recurse` is set, which CommitTree honours (because git does) but does
not set itself. When a write leaves any submodule moved, a toast reports how
many and offers an "Update all" action that runs the same update the
section header's own Update all does. There is no automatic update; the
toast is the only nudge.

## What is refused while something else is running

The application holds one write lock per repository (never a single global
lock — two different repositories can be written to at the same time).
Checking out a branch, a remote branch or a commit, resetting the current
branch, creating or deleting a branch, creating or deleting a tag, and every
stash write (push, apply, pop, drop) and worktree/merge write — including a
rebase, a cherry-pick and skipping a step of either — share this same
per-repository lock. A second write attempted against a repository
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
9. Tags, Stash and Submodules start collapsed per repository, and their
   expanded state is remembered per repository; branch groups and remotes
   each follow their own, independent default-open rule and are not all
   remembered the same way (see each section above).
10. Every write to a repository — branch, tag, stash, worktree or merge
    operations alike — is serialised through one lock per repository; a
    second concurrent write on the same repository is always refused, never
    queued.

## Known divergences

None: no earlier design document describing the sidebar or repository
groups was found in this repository to compare the built behaviour against.
