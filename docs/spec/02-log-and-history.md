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

## The log

The log lists commits for the selected repository, newest first, in the
topological order git itself produces (children before their parents). With
no filters applied it requests history from every ref (`--all`), not only
the current branch, so commits reachable only from another branch or a
remote-tracking ref still appear. Choosing a specific branch, tag or ref in
the filter bar switches the request to that one ref only.

Each row shows, left to right: a badge for every non-`HEAD` ref that
decorates the commit (local branch, remote-tracking branch, or tag — each
styled differently) followed by the commit subject, then the author, a
relative date ("just now", "3m ago", "5h ago", "2d ago", or a calendar date
once older than a week, with the year added once it differs from the
current one), and the short hash. A merge commit's subject is styled
distinctly from an ordinary commit's. The commit currently checked out as
`HEAD` is marked by drawing its graph dot hollow (outlined, not filled)
rather than by an extra badge.

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
after the page before it, for the same filters. Any request that does not
follow on from the page most recently served for that repository — filters
that changed, or an offset that does not match what should come next — is
refused, and the caller must start over from the beginning. Changing any
filter, or an externally-triggered refresh of the repository (for example
after a write elsewhere changed the history), starts the log over from the
first page; scroll position resets to the top when that happens.

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
date, and a comma-separated list of paths. The until date is stored as the
end of the chosen day, since git's own `--until` is otherwise exclusive of
it, so picking "today" still includes commits made today.

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

Selecting a row (a click, or landing on it via a graph-arrow jump) opens the
details pane beneath the log. The pane shows the commit's full subject and
body, its author and email, its full commit date, its full hash (copyable)
and a link for each of its parents that jumps the log to that parent. Below
that is the list of files the commit changed; the first file is opened
automatically. For a merge commit the file list, like the diff, is computed
against the merge's first parent only — the same "what did this branch's
own history change" view git's own tools default to — not a combined view
of every parent.

Each file in the list shows a one-letter status (added, deleted, renamed,
copied, or modified) and its path; a rename or copy also shows the old path
in its tooltip. Selecting a file loads that file's diff against the
commit's first parent (or, for a commit with no parent at all, the file as
introduced). The diff is rendered as plain text lines, coloured by whether
a line is an addition, a deletion, a hunk header, or file-header metadata;
it is truncated after a fixed number of lines with a note that it was cut,
rather than rendering an arbitrarily long diff in full.

The details pane also offers, from the row's context menu, checking the
commit out detached (with a confirmation naming the consequence — new
commits made there won't belong to any branch), creating a new branch or
tag at that commit, resetting the current branch to it, copying its hash,
and asking the AI assistant to explain the commit in the chat panel. Reset
is refused when there is no current branch to move, the head is detached, a
merge is already in progress, the target is already where the branch points
(nothing to move), or a write is already running.

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
   for the same filters; anything else is rejected and the log must restart
   from the first page.
3. Changing any filter, or a refresh triggered by a write elsewhere,
   restarts the log from its first page and scrolls back to the top.
4. The graph is drawn only when the commit set's parent-child links are
   intact under the active filters — never under a text, author, or date
   filter, always under a plain, ref, or path filter.
5. A lane's colour identifies the lane while it exists, not the branch or
   ref associated with it; colours are reused once a lane closes.
6. A merge commit's file list and diff are always computed against its
   first parent only.
7. The free-text filter matches the commit message alone, case-insensitively,
   as a literal substring, and is superseded entirely when the typed text
   resolves as a commit hash.
8. A diff is rendered up to a fixed line cap and then explicitly truncated,
   never silently cut off without saying so.

## Known divergences

None: no earlier design document describing the commit log or graph was
found in this repository to compare the built behaviour against.
