# Log and history

This area is the commit log: the scrolling, graphed list of commits for the
selected repository, the filters above it, and the details pane that opens
below it for one selected commit. It is where a user reads a repository's
history and inspects any single change in it.

## Concepts

- **Row**: one commit as it appears in the log — subject, author, relative
  date, short hash, its badges for any ref pointing at it, and (when the
  graph is drawn) its position in the graph.
- **Lane**: a column in the graph. A commit occupies exactly one lane on its
  row; lines between rows connect a commit's lane to the lane(s) its
  parent(s) or children continue in.
- **Page**: one batch of commits fetched from the backend. The log grows by
  fetching further pages as the user scrolls, never all at once.
- **Filters**: the set of constraints — free text, a specific branch/tag/
  ref, an author, a date range, one or more paths — narrowing which commits
  the log requests. An unfiltered log requests history from every ref, not
  just the checked-out branch.
- **Details pane**: the panel that opens beneath the log once a commit is
  selected, showing that commit's metadata, its changed files, and the diff
  for whichever file is currently chosen.
- **Uncommitted changes row**: a synthetic row above the newest commit,
  present only while the working tree has changes, that opens the working
  tree — not a commit — in the details pane.

## The log

The log lists commits for the selected repository, newest first, in one of
two orders: topological (children before their parents, keeping a merged
branch's own history together rather than interleaving it with the trunk by
date) or commit date (a strict walk by commit date). Topological is the
default. The order is a single global preference — not per repository — set
from the filter bar and persisted like the interface's other UI
preferences; changing it re-reads the log from the first page, the same as
changing a filter, since a page fetched under one order cannot be continued
under the other and the graph's lane layout has to be recomputed for the new
commit sequence rather than reused. With no filters applied the log requests
history from every ref (`--all`), not only the current branch, so commits
reachable only from another branch or a remote-tracking ref still appear.
Choosing a specific branch, tag or ref in the filter bar switches the
request to that one ref only.

Each row shows, left to right: a badge for every non-`HEAD` ref that
decorates the commit (local branch, remote-tracking branch, or tag — each
styled differently) followed by the commit subject, then the author, a
relative date ("just now", "3m ago", "5h ago", "2d ago", or a calendar date
once older than a week, with the year added once it differs from the
current one), and the short hash. A merge commit's subject is styled
distinctly from an ordinary commit's. The user's own commits have their
subject and author in semibold, with the author in the main text colour
rather than grey. A commit is the user's when its author email
matches the repository's `user.email`, ignoring case, or, when no email is
configured, its author name matches `user.name`. The identity is re-read
when the repository is selected and when the window regains focus. The commit currently checked out as
`HEAD` is marked by drawing its graph dot hollow (outlined, not filled)
rather than by an extra badge.

A ref's badge is coloured by its kind, not by where its commit sits in the
graph: every local or remote-tracking branch badge takes one hue (a soft
tinted background with its label text in a saturated shade of the same
hue), and every tag badge takes a second, distinct hue in the same style.
The graph's lane colour (see below) is deliberately not reused here — the
lane is already shown two columns to the left, so repeating it on the badge
would add nothing, while "is this a branch or a tag" is information the
badge can usefully carry instead. The branch currently checked out is drawn
with the same branch hue as any other branch badge, not a different colour,
but its own badge's text is bolder than an ordinary branch badge's — the
one piece of the current-branch emphasis this view still carries, alongside
the hollow `HEAD` dot in the graph.

### The header

The view's header normally shows the selected repository's name and path.
When the selection is a submodule (it has no row of its own — see
Repository list › Submodules in docs/spec/01-repositories-and-sidebar.md),
it instead shows a breadcrumb: the parent repository's name as a clickable
crumb, then its path relative to the top repository. Clicking the crumb
selects the parent again.

### Paging

Commits are fetched in pages of a fixed size; the next page is requested
once the user has scrolled to within a fixed distance of the bottom of what
has loaded so far and no page is already in flight. Reaching the end of
history is reported by the backend when a fetched page comes back shorter
than the page size that was requested; the log then knows there is nothing
more to ask for. A shallow clone that has run out of the history it holds
locally shows a note in place of a further page ("Shallow clone: older
history is not available locally.") instead of silently looking like
history has ended.

Paging is stateful on the backend: a page is only accepted immediately
after the page before it, for the same filters and the same order. Any
request that does not follow on from the page most recently served for that
repository — filters or order that changed, or an offset that does not
match what should come next — is refused, and the caller must start over
from the beginning. Changing any filter, changing the order, or an
externally-triggered refresh of the repository (for example after a write
elsewhere changed the history), starts the log over from the first page;
scroll position resets to the top when that happens.

### The graph

The graph is drawn only when the active filters keep the commit set exactly
as git returns it, with parent-child links intact: text, author and date
filters drop commits out of the middle of history without rewriting the
remaining commits' parents, which would leave graph lines dangling, so the
graph is hidden (no lane column at all) whenever any of those three filters
is set. Filtering by a specific ref, or by path, keeps the graph, because
git itself rewrites parents to skip commits that do not match a path
filter, and choosing a ref simply narrows which commits are walked.

The graph's layout carries state across pages of the same log — lanes that
are still open (waiting for a parent that has not appeared yet) at the end
of one page continue into the next page fetched for it, rather than each
page being laid out in isolation. A commit's lane is chosen, in order of
preference: the lane a lane already waiting for that exact commit occupies;
otherwise the lane a same-coloured line that had been cut and is now
arriving occupies; otherwise the first free lane, with a newly assigned
colour. A merge commit's additional parents each open, or join, one lane of
their own alongside the commit's own lane.

A lane that runs straight for a very long stretch without its awaited
commit turning up is cut: drawn as a downward arrow where it is cut and,
later, an upward arrow where the line it belongs to eventually resumes,
rather than letting the graph grow one lane wider for the entire stretch.
Hovering that arrow shows the subject of the commit it leads to when that
commit is already loaded, or "Go to `<hash>`" when it is not; clicking the
arrow jumps the log to that commit, loading further pages as needed (up to
a fixed number of pages) before giving up and telling the user to clear
filters and try again if it still cannot be found. Colours are drawn from a
fixed palette and reused cyclically as lanes are opened; a colour is not
permanently tied to any one branch or ref.

## Filters

The filter bar offers: free text (matched against the commit message only,
case-insensitively, as a literal substring — not a regular expression, and
not matched against diff content, author, or file paths), a ref picker
(all branches, one local branch, or one remote-tracking branch), an author
picker (populated from every author who has ever committed on any ref, not
just those visible in the current filtered log), a since date, an until
date, a comma-separated list of paths, and the order picker (topological or
date; see "The log" above). The until date is stored as the end of the
chosen day, since git's own `--until` is otherwise exclusive of it, so
picking "today" still includes commits made today.

Unlike every other control in the bar, the order picker is not part of the
filters: it does not change which commits are selected, only the sequence
they are returned in, and it is remembered globally rather than reset by
`selectRepo` when the user switches repositories.

Typing what looks like a hex commit hash (4 to 40 hex characters) into the
free-text box does not filter the log by that text: it is instead resolved
against the repository, and on success clears the text filter and jumps the
log to that commit instead, loading further pages if needed to reach it. If
it cannot be resolved to any commit, the text is filtered on as ordinary
text.

If a filter is already pointing at a ref that has since been removed from
the picker's own list (for instance a branch filter set from a tag or from
a branch since deleted), the picker still shows that ref as the current
selection rather than silently reverting to "All branches".

## Selecting a commit and the details pane

A log row has no hover highlight — passing the pointer over it leaves it
unchanged, so the log doesn't read as banded. The selected row is still
highlighted, and stays highlighted regardless of the pointer's position.

Selecting a row (a click, or landing on it via a graph-arrow jump) opens the
details pane beneath the log. A left click on the row — or its graph dot —
that is already selected clears the selection instead, closing the details
pane; the same holds for the "Uncommitted changes" row. Opening a row's
context menu, following a graph arrow, and selecting from anywhere outside
the log always select and never clear. The pane shows the commit's full subject and
body, its author and email, its full commit date, its full hash (copyable)
and a link for each of its parents that jumps the log to that parent. Below
that is the list of files the commit changed; the first file is opened
automatically. For a merge commit the file list, like the diff, is computed
against the merge's first parent only — the same "what did this branch's
own history change" view git's own tools default to — not a combined view
of every parent.

Each file in the list shows a one-letter status (added, deleted, renamed,
copied, or modified) and its path; a rename or copy also shows the old path
in its tooltip. A file that is a submodule is flagged as such in the list,
with a package icon ahead of its status letter. Selecting a file loads that
file's diff against the commit's first parent (or, for a commit with no
parent at all, the file as introduced). The diff is rendered as plain text
lines, coloured by whether a line is an addition, a deletion, a hunk
header, or file-header metadata; it is truncated after a fixed number of
lines with a note that it was cut, rather than rendering an arbitrarily
long diff in full. A submodule's diff is the same `--submodule=log`
commit-range summary the Changes view shows, not a raw `Subproject commit`
line, and is rendered the same way — the submodule's path, the old and new
commit it points at, the commits between them, and, when it has modified or
untracked content of its own, a note plus an "Open submodule" link that
selects it as a repository (shown only when it is initialised — see
Working tree). The log list itself is unaffected by any of this: a
submodule is only ever shown differently in the details pane.

The details pane also offers, from the row's context menu, checking the
commit out detached (with a confirmation naming the consequence — new
commits made there won't belong to any branch), creating a new branch or
tag at that commit, cherry-picking the commit onto the current branch,
rebasing the current branch onto the commit, resetting the current branch
to it, copying its hash, and asking the AI assistant to explain the commit
in the chat panel. Both cherry-pick and rebase confirm first.

When branches point at the commit, the menu starts (after "Explain in chat")
with one "Check out `<branch>`" entry per branch, in badge order. A local
branch and its remote-tracking twin on the same commit are one entry, the
local branch. A branch only on remotes is checked out through the remote its
local branch tracks, else `origin`, else the first remote carrying it, the
same way as a remote-tracking branch in the sidebar (see Repositories and
sidebar): a missing local branch is created tracking it, and a local branch
that is only behind is fast-forwarded to it, so the checkout lands on the
commit that was clicked. Tags and `origin/HEAD` get no entry. When the
current branch itself is on the commit, its entry reads "Check out
`<branch>` (current)" and is disabled (a current branch that is behind a
remote twin on the commit is offered, and fast-forwards in place);
one for a branch checked out in another worktree is disabled with that
worktree's path as its tooltip; all of them are disabled while a write is
running. Reset
is refused when there is no current branch to move, the head is detached, a
merge is already in progress, the target is already where the branch points
(nothing to move), or a write is already running.

Cherry-pick is refused when the commit is a merge commit, it is already
contained in `HEAD`, the head is detached, a write is running, or any
conflicted operation is in progress; uncommitted changes to tracked files
refuse it on click ("Commit or stash your changes first"). A cherry-pick
whose changes the current branch already has reports "Nothing to apply"
and leaves nothing in progress. Rebase is refused under the same
conditions as the branch menu's "Rebase `<head>` onto `<branch>`" (see
Repositories and sidebar): the commit is `HEAD`, the current branch already
contains it, a write is running, the head is detached, any conflicted
operation is in progress, or there are uncommitted changes to tracked
files.

## The "Uncommitted changes" row

A row reading "Uncommitted changes (N)" sits above the newest commit whenever
the working tree has any staged, unstaged or untracked file; N is the number
of distinct paths involved (a partially staged file counts once, not twice).
The row exists regardless of the log's filters and ordering — it is not
history, so it is never filtered or reordered away — and it disappears the
moment the tree becomes clean. It carries no author, date or hash, and no
context menu.

Selecting the row (or landing on it, since it participates in the log's
selection the same way a commit row does) opens the working-tree view (the
Changes view) in the details pane below the log: the file list with stage,
unstage and discard, the diff, and the commit box. Selecting the row and selecting a
commit are mutually exclusive: choosing one clears the other. While a
conflict owns the screen, the conflict view is shown instead, regardless of
which of the row or a commit was last selected.

If the row was selected and the tree becomes clean (a commit, or a discard
or stash that empties it), the selection moves on to whatever is now `HEAD`,
so the details pane shows the commit that was just made; if there is no
`HEAD` to move to, the selection is cleared and the details pane closes.

### The row in the graph

The row draws its own marker: a hollow, dashed dot, always joined to `HEAD`
by a dashed line, in `HEAD`'s lane colour.

- If `HEAD` is the newest commit (the log's first row), the dot sits
  straight above it, in `HEAD`'s own lane.
- If `HEAD` is further down the loaded rows (for example under commit-date
  ordering, with newer commits on another branch shown first), a lane is
  reserved to its left for the dashed line alone and the ordinary graph is
  drawn one lane further right; the line runs down that reserved lane to
  `HEAD`'s row and only then steps sideways into `HEAD`'s own lane, so it
  never crosses another branch's line.
- If `HEAD` has not been loaded yet but more pages remain, the line runs to
  the end of what is currently loaded, and extends to meet `HEAD` once the
  page containing it is fetched.
- If `HEAD` is not in the log at all (filtered out, or an unborn branch with
  no commits), the dot stands alone in the reserved lane with no line drawn.

The row does not change the graph's paging, lane assignment or colour
choices for the real commits below it — it only ever adds, at most, one
reserved lane of its own to their left.

## Blame

Right-clicking a file in the details pane's file list offers Blame, which
shows that file as it stood at the selected commit, one origin per line. The
entry is disabled, with a reason as its tooltip, for a deleted file
(deleted files have nothing to blame) and for a submodule (submodules have
no blame).

Opening it replaces the main pane with the blame view: a header with Back,
the file's path, the revision being blamed, and an Ignore whitespace
checkbox whose setting is remembered across files and revisions. Each line
carries a gutter showing, on the first line of a run of lines that share an
origin, that origin's short hash, author and relative date, with the full
commit summary on hover; runs alternate background bands so long ones stay
easy to tell apart. A `^` ahead of the hash marks a boundary commit — the
oldest one available in a shallow clone, past which blame cannot go further
back. Working-tree lines not yet committed show "Not committed yet" instead
of a hash, and refresh as the working tree changes while the view is open.
Clicking a hash shows that commit in the log.

Lines can be selected: a click selects one line, and a shift-click extends
the selection from the last plain click. Right-clicking a line — selected
or not — opens a menu scoped to the selection when the click lands inside
it, or to the clicked line's own run otherwise:

- **Explain these lines in chat** opens the chat and asks it to explain the
  selected range.
- **Blame previous revision** reblames the file as it stood just before the
  commit that introduced these lines, pushing the current file and revision
  onto a stack. It is disabled when the lines come from more than one
  commit, when they are not committed, when they were added in this commit
  (there is no earlier revision to show), or when the commit is the
  boundary of a shallow clone — each with its own reason as the tooltip.
- **Show commit** and **Copy hash** act on the run's commit, and are
  disabled when the selection spans more than one.

Back walks back one step through that stack at a time, revisiting each
earlier file and revision Blame previous revision produced, and once the
stack is empty, returns to wherever the view was opened from — the details
pane or the Changes view.

Blame caps a file at the first 20 000 lines, with a notice when a file was
truncated. Lines of a file with Windows line endings (CRLF) are shown
without the carriage return. Blame works in both SHA-1 and SHA-256
repositories. A binary file or a path that no longer exists at the requested
revision is shown as an error with a Retry button rather than any lines.
Changing the selected repository closes the blame view, and a merge in
progress takes the main pane as it always does, ahead of blame the same as
ahead of the Changes view or a stash preview.

## Searching history

There is no separate search feature: the free-text filter is history
search, always scoped to the commit message. It runs against whatever the
current filters otherwise select (a ref, a date range, paths), so searching
"within a branch" or "within a date range" is done by setting those filters
alongside the text, not through a distinct search mode. Because a text
filter (like an author or date filter) breaks the parent chain the graph
needs, searching hides the graph column for the results it returns.

## Rules

1. With no ref filter set, the log is built from every ref, not the
   checked-out branch alone.
2. A page is only ever accepted immediately following the page before it
   for the same filters and the same order; anything else is rejected and
   the log must restart from the first page.
3. Changing any filter, changing the order, or a refresh triggered by a
   write elsewhere, restarts the log from its first page and scrolls back
   to the top.
4. The graph is drawn only when the commit set's parent-child links are
   intact under the active filters — never under a text, author, or date
   filter, always under a plain, ref, or path filter.
5. A lane's colour identifies the lane while it exists, not the branch or
   ref associated with it; colours are reused once a lane closes. A ref
   badge's colour identifies its kind (branch or tag) instead, and is
   unrelated to any lane colour.
6. A merge commit's file list and diff are always computed against its
   first parent only.
7. The free-text filter matches the commit message alone, case-insensitively,
   as a literal substring, and is superseded entirely when the typed text
   resolves as a commit hash.
8. A diff is rendered up to a fixed line cap and then explicitly truncated,
   never silently cut off without saying so.
9. The "Uncommitted changes" row exists exactly while the working tree has
   any staged, unstaged or untracked path, independent of the log's filters
   and ordering, and selecting it is mutually exclusive with selecting a
   commit.

## Known divergences

None: no earlier design document describing the commit log or graph was
found in this repository to compare the built behaviour against for the log
itself. The "Uncommitted changes" row's design document originally left the
row's dot unjoined to `HEAD` whenever `HEAD` was not the first row; that was
revised, before this document was written, to the reserved-lane, dashed-line
behaviour described above, so there is no remaining divergence to record for
it either.

Added since the previous revision of this document: ref badges are now
coloured by kind (branch vs tag) rather than left uncoloured, the log can be
ordered by commit date as an alternative to the topological default,
hovering a log row no longer highlights it (selecting one still does), and
an "Uncommitted changes" row above the newest commit gives the log its own
way into the working tree.
