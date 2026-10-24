# Code hosting: Bitbucket and GitHub

CommitTree can look past git at the code host behind a repository. For
repositories on Bitbucket Cloud (bitbucket.org) or GitHub (github.com) it
shows the state of the pipelines or checks of each commit, using an account
the user connects in Settings. Every other repository looks and behaves
exactly as before: no icons, no extra rows, nothing in the status bar.

This document covers detecting the host, accounts, commit statuses with their
cache and rate limits, the pull request data and checkout behind the Pull
requests view, the watch of a pushed commit and its notification, the status
icons in the log and the commit details, and the Pull requests row, list and
detail.

The section is called **Pipelines** for a Bitbucket repository and **Checks**
for a GitHub one, everywhere it appears.

## Detecting the host

The remotes are read again every time they are needed, so a remote edited in
Repository settings (`12-repository-settings.md`) is picked up at once, with
no stale owner or name.

- **Which remote.** `origin` when it points to a supported host; otherwise
  the first remote, in name order, that does. Its fetch URL is read, and its
  push URL when the fetch URL is not on a supported host. One remote per
  repository: its name is kept.
- **Accepted forms**, for both hosts; a user name or credentials in the URL
  are ignored, and a trailing `.git` is optional:
  - `https://[user@]bitbucket.org/<workspace>/<repo>`
  - `git@bitbucket.org:<workspace>/<repo>`
  - `ssh://git@bitbucket.org/<workspace>/<repo>`
  - the same three for `github.com/<owner>/<repo>`.
- SSH host aliases from `~/.ssh/config` (such as `git@bitbucket-work:…`) are
  not resolved: a repository reached through one has no host.
- A linked worktree uses its main repository's remotes. A submodule uses its
  own.
- What the frontend learns about a repository: whether the host is
  supported, the host, owner and name, the remote's name, the repository's
  page and pull request list in the browser, the noun ("Pipelines" or
  "Checks"), and how the account for that host stands: **none** (no account
  saved), **connected**, or **rejected** (the host answered 401 since the
  account was last saved). This never goes to the network.

## Accounts

One account per host, connected in **Settings → Accounts**. An account is
checked against the host before anything is stored; a failed check stores
nothing and shows the host's reason.

**Bitbucket**: an email (the Atlassian account) and an API token. The pair
is verified with the host's "current user" call; success reads "Connected as
<display name>". Remove deletes the token and the email.

**GitHub**: either the **GitHub CLI login** or a **personal access token**.

- The CLI choice is offered when a `gh` program is found on the PATH or in
  `/opt/homebrew/bin`, `/usr/local/bin` or `/usr/bin` (an app started from
  the Dock has a short PATH). The token is asked of `gh auth token
  --hostname github.com` the first time it is needed, kept in memory for the
  session, and dropped on a 401, which re-runs `gh` once. Only the choice is
  stored. When `gh` cannot give a token the message is "GitHub CLI isn't
  logged in (gh auth login)".
- A pasted token goes to the keychain; switching to the CLI removes it.
- Success reads "Connected as <login>".

**The Accounts tab** (Settings, after General) has one block per host. It
does not need the AI settings, so it opens even when those cannot be loaded.

- **Bitbucket block.** An email field and an API token field (a password
  field), Save, and Remove once an account is connected. Save is enabled only
  with a non-blank email and token. A link, opened in the system browser, goes
  to `https://id.atlassian.com/manage-profile/security/api-tokens`, with the
  scopes the token needs: `read:user:bitbucket`, `read:repository:bitbucket`,
  `read:pullrequest:bitbucket` and `read:pipeline:bitbucket` (names as in
  Atlassian's "API token permissions" page).
- **GitHub block.** Two choices: "Use GitHub CLI login", disabled with "gh not
  found" beside it when no `gh` program is found, and "Personal access token",
  which shows the token field (a password field) and a link, opened in the
  system browser, to `https://github.com/settings/personal-access-tokens`.
  The choice follows the saved account; with none saved, the CLI is chosen when
  `gh` exists. Save, and Remove once connected; Save with a token needs a
  non-blank one.
- **Status line** under each block: "Connected as <name>", or "Bitbucket
  rejected the token" / "GitHub rejected the token" when the account is
  marked rejected. A block with no account shows none.
- **Errors.** A failed Save or Remove shows its message under its block and
  leaves the fields as typed. A successful Save empties the token field (a
  token is never shown again) and refreshes the status. Remove asks no
  confirmation and only forgets the credential and the stored email, name
  and login.

**A blank or whitespace-only email or token is refused** before the keychain
or the network is touched.

**Storage.** Tokens live only in the operating system's secret store, under
the service `git-ui`, accounts `bitbucket` and `github`; there is no
plain-text fallback, and when the store is unusable saving fails with the
same message as for AI keys. The rest (the Bitbucket email and display name,
the GitHub source and login) is in `hosting.json` next to the AI settings
file, written atomically. A token is never written there, nor to the command
log, an error message, the chat or a URL.

**Where a token goes.** Only to its own API host (`api.bitbucket.org`,
`api.github.com`), checked on every request; a redirect to any other host is
refused.

**Rejected tokens.** A 401 marks the account rejected: the Accounts tab says
"Bitbucket rejected the token" (or "GitHub rejected the token") and no
further request goes to that host until the account is saved again, which
clears the mark at once with no restart. A repository the account cannot see
(403 or 404) is "No access to <owner>/<repo> with this account", followed by
the host's own text when it names a missing scope; the log shows nothing
else.

## Statuses

Each commit has a state: **success**, **failed**, **running**, **stopped** or
**none** (none draws nothing), and the list of checks behind it, each with a
name, state, link and, when the host gives them, start and duration.

- Only the latest run of each check counts, so a pipeline that failed and
  was re-run green is green. Aggregation over those: any failed gives
  failed; else any running gives running; else any stopped gives stopped;
  else success; no checks give none.
- The visible rows of the log (plus a margin) are asked for; the
  uncommitted-changes row, and anything that is not a commit id, never are.
  A commit the host does not know, or one with no checks, is none.
- **Two ways to ask.** For the log, a batch of commits (GitHub: one query
  per 50 commits; Bitbucket: the repository's newest pipelines, at most 3
  pages of 100). For one commit's details, every check of that commit,
  always asked of the host.
- **No account, rejected account, or an unsupported repository:** the log
  asks nothing and shows nothing, without an error. A rate limit, a lost
  connection or a rejected token while asking for the log show what is
  already known and no error; only "No access to <owner>/<repo> with this
  account" is reported.

### Cache

In memory, per host and `owner/name` (so editing a remote starts afresh), for
the session; nothing is written to disk.

- final states (success, failed, stopped) never expire;
- none is asked again after 10 minutes, since a pipeline may still start;
- running is asked again after 30 seconds.

A request only goes to the host for the commits the cache does not hold
fresh. The answer for one commit's details is cached too.

### Rate limits

One state per host, shared by every repository.

- **GitHub.** When a response says no requests remain, or a 403 or 429 names
  a rate limit, nothing more goes to GitHub until the reset time it gave.
- **Bitbucket.** On a 429 it waits for `Retry-After`; without one, 60 s,
  doubling up to 15 minutes while 429s continue.
- While a host is blocked no request is sent to it. The frontend is told
  when a host becomes blocked (host, until when, and whether that time was
  given by the host) and when the block ends. Wording: "GitHub: request
  limit reached, retrying at HH:MM", or "Bitbucket: request limit reached,
  retrying shortly" when no time is known. The end of a block is announced by
  the first request that notices it.
- No network connection is not announced per row; the next cycle tries again.

No network call is made while a repository's write lock is held.

## Pull requests: data and checkout

What the app asks of the host for the Pull requests view. All of it needs
the repository to be on a supported host and an account for it that is
connected; otherwise each call fails with the host's reason ("Bitbucket: no
account", "Bitbucket rejected the token", or a repository that is not on
Bitbucket or GitHub), and no request is sent.

- **List.** Open pull requests by one of three filters: **open** (all),
  **mine** (the account is the author) and **review** (the account is a
  reviewer and has not approved). "Mine" and "review" use the user saved with
  the account. Any other filter is an error. At most 100 rows, most recently
  updated first; the answer says whether the host has more. An empty list is
  an empty list, not an error.
- **Row data.** Number, title, draft, author, `source → target`, the head
  commit, link, whether it comes from a fork, approvals, the comment count
  and update time. On GitHub the state of the head commit comes with the
  list. On Bitbucket it is filled only from the status cache (the one the
  log uses); a head commit the cache does not hold fresh has no state, and
  the list never makes extra requests for it.
- **Count.** The number of open pull requests for the sidebar badge, from
  the cheapest call the host has; it says when the real number is larger.
- **Detail.** One pull request: description, dates, reviewers with their
  verdict, commits, comments (up to 500, with a flag when there are more)
  and the checks of the last commit.
- **Files.** The changed files with status, old path and the host's diff for
  each; a file the host gave no diff for is marked so. Everything comes from
  the host's API, so no fetch is needed to read a pull request.
- Pull request numbers must be positive. A repository the account cannot see
  is "No access to <owner>/<repo> with this account"; rate-limit state is
  announced as for statuses.

### Checkout branch

Checks out the source branch of a pull request in the local repository.

1. The pull request is read from the host first, with no repository lock
   held.
2. A pull request from a fork (Bitbucket: its source repository differs from
   the target; GitHub: cross-repository, or the head repository is gone) is
   refused with "From a fork. Open it in the browser." Nothing is fetched.
3. Otherwise the detected remote (see Detecting the host) is fetched, then
   the existing checkout of a remote branch runs with that remote and the
   source branch (`05-remote-and-stash.md`): the local branch of the same
   name is created tracking it, or reused. Its outcome comes back unchanged.
   Errors from the fetch and the checkout (a dirty working tree, a diverged
   local branch, a busy repository, credentials) pass through unchanged and
   nothing is forced.

The fetch and the checkout take the repository's write lock themselves; the
request to the host does not.

## Watching a push

After a push from CommitTree the pipeline or checks of the pushed commit are
followed until they finish, so the log and a notification can say how it
ended without the user asking.

- **What is watched.** The tip of each branch just pushed, in a repository
  on a supported host with a connected account; otherwise nothing is
  watched and nothing is asked. Every way of pushing counts: the toolbar,
  the branch menu, Push all (each branch it pushed) and the chat's push
  tool. A push that fails, or that finds the branch already up to
  date (from any of these), starts nothing, since nothing was pushed. A
  push counts when it moved the branch's upstream; a new branch is
  included. Pushing from a detached HEAD watches nothing.
- **No waiting.** The watch starts after the push has returned and its
  repository lock is released; the push never waits for it, and no network
  call is made while the lock is held.
- **One watch per commit.** Pushing a tip that is already being watched in
  the same repository starts no second watch; once it ends, a later push of
  the same tip starts a new one.
- **Polling.** Every 30 seconds the commit's checks are read from the host,
  the way the commit details read them, and the answer goes into the status
  cache, so the log shows the new state at once. The remotes and the account
  are read again on every poll.
- **Events.** Each change of state is announced to the frontend as a
  `hosting:status` event with the repository, the branch, the commit, the new
  state and the section's name ("Pipelines" or "Checks"). The first answer
  of "no checks yet" is not a change. When the state reaches success, failed
  or stopped the event is marked final, and it is the last one, sent once.
- **When it stops.** At a final state; after 60 minutes; after 5 minutes
  with no checks at all (counted from the last time there were none); when
  the host rejects the token or the account is removed; and when the app
  quits. Stopping for any reason but a final state sends nothing.
- **Errors are not changes.** A lost connection, a request limit or "no
  access" while polling is not announced and does not end the watch; the next
  poll tries again.

## Statuses in the log and the commit details

What the frontend does with the statuses above. Everything here needs a
supported host and a connected account (see Detecting the host); otherwise
nothing is asked and nothing is drawn, and the rows have no reserved space.

- **Log icon.** A 12 px icon left of the short hash of each row: check
  (success), cross (failed), turning arrow (running), square (stopped), none
  for a commit without checks. The tooltip reads, for one pipeline,
  "Pipeline #412 passed · 3 min" (name, outcome, duration when known), or
  "Pipeline running · started 2 min ago" while it runs; for several,
  "2 of 3 checks failed", "3 checks passed" or "1 of 2 checks running" (the
  noun in lower case: "pipelines" on Bitbucket). Durations read "42 s",
  "3 min" or "1 h 5 min".
- **What is asked.** The rows on screen plus one screen of rows above and
  below, 500 ms after the last change of the visible range; calls inside that
  window are one request carrying every hash. Commits already known as
  success, failed or stopped are not asked again in the session; none and
  running ones are.
- **Which repository.** An answer is filed under the repository it was asked
  for. Switching repositories while a request is in flight never paints its
  answer into the other repository's log.
- **Asking again.** On window focus, after a Refresh, fetch, pull, push or
  any other write to the selected repository, and when Settings closes (an
  account may have been connected or removed), the unfinished statuses
  (running and none) of that repository are forgotten and the visible set is
  asked again; the backend's cache (see Cache) decides what goes to the
  network. Every 30 seconds the visible commits still running are asked
  again.
- **Watch events.** A `hosting:status` event puts the state of the watched
  commit into the log at once, whatever repository is selected.
- **Rate limits.** A `hosting:limit` event is kept by host until the event
  that ends it. While a host is blocked, the status bar shows "GitHub:
  request limit reached, retrying at HH:MM" (the local time the host gave) or
  "Bitbucket: request limit reached, retrying shortly" (no time known) in
  place of HEAD's pipeline, for a repository on that host. Nothing is asked
  of the host meanwhile by the backend; the log keeps what it has.
- **Status bar.** The selected repository's HEAD is asked for with the
  visible rows (and again with them on every refresh and poll), and its state
  shows at the right of the status bar (`13-status-bar.md`).
- **Final events.** A final `hosting:status` event notifies once ("Pipeline
  passed on <branch>", `11-notifications.md`) and asks the repository's
  statuses again. The cached checks of a commit whose state changed are
  dropped and that commit is asked for once more, even when its state is
  final, so tooltips never show checks that disagree with the state.
- **Commit details.** Under the commit's author and date, a **Pipelines** or
  **Checks** section lists every pipeline or check of the commit, always asked
  of the host: state icon, name, state, duration when known and an **Open**
  link, which opens the host's page in the system browser and only for an
  `https:` address. While loading it reads "Loading…"; a failure shows its
  message with **Retry**. A commit with no pipelines or checks hides the
  section.
- Only a "No access to <owner>/<repo> with this account" answer is said (as a
  message, once per repository); other failures show what is already known.

## Pull requests: sidebar row and list

The Pull requests view (the main pane) shows the open pull requests of the
selected repository. Everything here needs a supported host; the sidebar row
that opens it is described in `01-repositories-and-sidebar.md`.

- **Layout.** The view has a header (**Pull requests** with the repository's
  name), three tabs, **Refresh** and **Open in browser**, then the list, and
  below it, behind a splitter dragged like the log's, the pull request that is
  selected (a click on a row selects it; the choice is remembered per repository
  while the app runs), described under Pull request detail below. When the
  view cannot be shown (the repository is not on a supported host, is missing,
  or a merge in progress owns the screen) the main pane goes back to the log
  and stays there.
- **Tabs.** **Open** (all), **Mine** (the account is the author) and **Review
  requested** (the account is a reviewer and has not approved): the three
  filters of the data calls. **Open in browser** opens the host's pull
  request list for the repository, in the system browser and only for an
  `https:` address.
- **Rows.** Newest update first, as the host gave them. Each row shows the
  status icon of the head commit (the pull request's own state, as for the
  log; nothing when it is unknown or has no checks), `#<number>` and the
  title with a **Draft** label when it is one, below it the author and
  `source → target`, and at the right "✓ <approvals>", a speech bubble with
  the comment count, and when it was last updated ("just now", "5m ago",
  "3h ago", "2d ago", then the date). The status icon's tooltip is "Pipeline
  passed", "Checks failed" and so on, as in the status bar.
- **More than 100.** The last line reads "Showing 100. Open the rest in the
  browser" and is a link that opens the same list.
- **Empty and error states** replace the rows with one line. No account:
  "Connect your Bitbucket account to see pull requests" (GitHub likewise)
  with **Open Settings**, which opens Settings on the Accounts tab. A
  rejected token: "Bitbucket rejected the token" (GitHub likewise). A failed
  load: its message with **Retry** (a failed refresh with rows on screen
  shows the message above the rows). A filter with nothing: "No open pull
  requests", "No open pull requests of yours" or "No open pull requests
  waiting for your review". While the first answer is awaited the list reads
  "Loading…".
- **When it is read.** When the view opens, when a tab is chosen, on
  **Refresh**, when the window gets focus while the view is shown, after a
  fetch or any other refresh of the repository, and when Settings changes the
  account. A refresh keeps the rows on screen until the new answer is in;
  switching tab or repository clears them. An answer for a repository or tab
  that is no longer shown is dropped. Nothing is requested without a
  connected account. A list read for **Open** also sets the sidebar badge.
- **The sidebar badge** is the open pull request count (`Count` of the data
  calls: the number, with "+" when the host has more), asked for the selected
  repository on selection, on window focus and after each refresh.
- **Text from the host is untrusted.** A pull request's description and
  comments are shown as escaped text: HTML in them is never interpreted (no
  images, no handlers), a Markdown link becomes a link only when its address
  is `https:` and otherwise reads as its text, and an image reads as a link
  with its alt text. Links open in the system browser, `https:` addresses
  only. This is one rule for every place the detail shows host text.
- **Comment order.** Comments read oldest first, each followed by its replies
  (oldest first), indented by depth up to 4 levels; a reply whose parent is
  not among the comments reads as a first-level one.

## Pull request detail

The pane below the list shows the selected pull request: a left column with
the information and a right column with its files. Everything is read from the
host (see Pull requests: data and checkout); nothing needs to be fetched.
Switching to another pull request or repository drops what was loading.

**Left column**, top to bottom:

1. **Header.** Title and `#<number>`, **Open** or **Draft** with the author,
   `source → target`, and when it was created and last updated (the tooltip
   has the full date).
2. **Checkout branch** and **Open in browser**. Open in browser opens the pull
   request on the host, only for an `https:` address. For a pull request from
   a fork Checkout branch is disabled, and the pane says "From a fork. Open it
   in the browser."
3. **Description**, as untrusted text (see the Pull requests list).
4. **Reviewers**, each with the verdict: Approved, Changes requested or
   Pending.
5. **Pipelines** or **Checks** of the last commit, as in the commit details
   (hidden when there are none).
6. **Commits**, short hash and subject. A click selects the commit in the log
   (the main pane goes back to the log) when the commit is in the local
   repository; otherwise the row says "Not fetched" and nothing else happens.
7. **Comments**, oldest first with replies under their parent, indented one
   step per level (up to 4). A comment on a file shows `path:line` above its
   text; a review verdict is shown beside the author. When the host has more
   than the 500 shown, the last line reads "Showing 500. Open the rest in the
   browser" and is a link that opens the pull request.

**Right column.** A **Changed files** list (status letter A, D, M or R, and
the path) and below it the diff of the selected file, the first one on
opening, coloured as in the commit details (cut after 5000 lines). A file the
host gave no diff for shows "No diff available here. Open in browser" with
the link to the pull request.

**Links in host text.** A click on a link inside a description or a comment
never navigates the app: it opens the system browser when the address is
`https:` and does nothing otherwise.

**Checkout.** **Checkout branch** runs as a task in the status bar, "Checkout
<source branch>" (`13-status-bar.md`): the remote is fetched and the branch
checked out as in the sidebar (see Checkout branch above). When it ends the
repository is refreshed. On success the notices of a branch checkout (a
fast-forward, a diverged local branch) are shown, the main pane goes back to
the log and selects the branch's tip commit. On failure the task ends failed
and git's message is its tooltip; the log stays as it was.
