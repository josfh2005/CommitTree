# Bitbucket and GitHub: pipelines in the log and a Pull requests view

Date: 2026-10-23. Designed with the owner on 2026-10-23. It started as a
request for MCP servers in the chat ("so it knows more about commits,
pipelines, PRs"). Talking it through showed that what the owner wants first
is **to see that information in the app itself**, without asking the chat.
MCP in the chat stays a separate project, designed afterwards (see "Not in
this project").

Behaviour goes to a new `docs/spec/14-hosting.md`, plus the parts it touches:
`01-repositories-and-sidebar.md` (the Pull requests row),
`02-log-and-history.md` (status icon, Pipelines/Checks section),
`07-conventions-and-constraints.md` (the Accounts tab of the Settings
dialog), `11-notifications.md` (the new category) and `13-status-bar.md`
(the branch's status).

## Why

The owner's work repositories live on bitbucket.org. Today, to know whether a
commit's pipeline passed or how a PR is going, they leave CommitTree for the
browser. They want it in the log, next to the commits, and the repository's
PRs one click away, next to Branches. CommitTree itself lives on GitHub, so
GitHub must work too.

## Goals

1. **Pipeline status in the log**: a small ✓ / ✗ / ⏳ icon on each commit row
   that has CI, and the list of its pipelines/checks in the commit details.
2. **A Pull requests view**: an entry inside the repository in the sidebar,
   next to Branches, that opens its own main view. The view has a list and a
   detail with description, reviewers and approvals, checks, commits, changed
   files with their diff, and comments.
3. **Current branch status** in the status bar.
4. **A notification** when the pipeline of a push made from CommitTree
   passes or fails.
5. **Accounts** in Settings, with tokens in the OS keychain.

## Not in this project

- **Writing to the host.** Approving, commenting, merging, creating or
  declining PRs and re-running pipelines are phase 2, where every action will
  be confirmed like the chat's write tools. v1 is read-only. Its only actions
  are local (checkout) or open the browser.
- **MCP in the chat** and new chat tools for PRs or pipelines. This is a
  separate project (memory `git-ui-mcp-config`).
- **Other hosts**: Bitbucket Server/Data Center, GitHub Enterprise, GitLab.
  The design keeps one interface per host so they can be added, but only
  bitbucket.org and github.com are built.
- More than one account per host.
- **Checking out PRs from forks.** The button is disabled with "From a fork.
  Open it in the browser."
- Closed or merged PRs in the list. Only open ones are shown.
- **Inline comments placed inside the diff.** They appear in the comment list
  with the file and line they refer to.
- Watching pushes made outside CommitTree, and watches that survive quitting
  the app.

## Anti-goals (a failure even if it works)

- **A token leaving its host.** It is sent only to `api.bitbucket.org` or
  `api.github.com`. It never appears in the command log, error text, the
  chat or a URL.
- **A slower log.** Statuses load in the background. The log never waits on
  them, and a network failure never shows up as an error on every row.
- **Hammering the API.** Bitbucket allows roughly 1,000 requests per hour per
  user. Batching, caching and polling are designed against that budget.
- **Host content treated as code.** PR titles, descriptions and comments are
  untrusted text: no raw HTML, and links open in the external browser.

## 1. Detecting the host

- **Which remote.** Each repository's remotes are read (`ops.ListRemotes`,
  fetch URL). `origin` is used if it points to a supported host, otherwise
  the first remote in name order that does. One remote per repository; its
  name is kept and used for fetches (§5 checkout).
- **Accepted forms.** User and credentials in the URL are ignored.
  - `https://[user@]bitbucket.org/<workspace>/<repo>[.git]`
  - `git@bitbucket.org:<workspace>/<repo>[.git]`
  - `ssh://git@bitbucket.org/<workspace>/<repo>[.git]`
  - the same three forms for `github.com/<owner>/<repo>`.
- **SSH host aliases** from `~/.ssh/config` (e.g. `git@bitbucket-work:…`)
  are not resolved in v1, and such a repository counts as having no host.
  *(My call: it keeps v1 simple; `ssh -G` can resolve aliases later.)*
- A pure function `ParseRemoteURL(url) (Repo{Host, Owner, Name}, ok)` does
  the parsing, covered by table-driven tests.
- **A repository with no supported host shows nothing new**: no icons, no
  Pull requests row, no status in the bar.
- Linked worktrees use their main repository's remotes; submodules use their
  own.

## 2. Accounts and credentials

A new **Accounts** tab in the Settings dialog, not marked as AI, has one
block per host.

**Bitbucket**

- **Fields.** **Email** (the Atlassian account) and **API token** (a password
  field). A link opens Atlassian's API-token page, and the text names the
  scopes needed: read access to account/user, repositories, pull requests
  and pipelines. *The builder checks the exact scope names against
  Atlassian's documentation and writes them into the spec.*
- **Save** verifies the pair with `GET /2.0/user` (Basic auth
  `email:token`) before storing anything. Success shows "Connected as
  <display name>". Failure shows the reason and stores nothing.
- **Remove** deletes the token and the email.

**GitHub** offers two choices.

- **Use GitHub CLI login.** Offered when a `gh` binary is found on the PATH
  or in the usual install places (`/opt/homebrew/bin`, `/usr/local/bin`,
  `/usr/bin`), since an app launched from the Dock has a short PATH.
  - The app runs `gh auth token --hostname github.com` the first time a
    token is needed. The token is kept in memory for the session and dropped
    on a 401, which re-runs `gh` once.
  - Only the choice is stored. If `gh` fails, the error is "GitHub CLI isn't
    logged in (gh auth login)".
- **Personal access token** (a password field). A link opens GitHub's token
  page, and the text names the read permissions needed: metadata, contents,
  pull requests, commit statuses and checks. *The builder confirms them for
  both fine-grained and classic tokens.*
- **Save/Remove/"Connected as <login>"** work as for Bitbucket, using
  `GET /user`.

**Storage**

- Tokens go to the keychain under service `git-ui`, accounts `bitbucket` and
  `github`. Either the `known()` allow-list in `internal/ai/keys` gains those
  two names, or the builder moves the store to a shared package. The rules
  stay: only known names, and no plaintext fallback.
- Non-secret settings go to a new `hosting.json` next to `ai.json`, written
  atomically: the Bitbucket email, and the GitHub `source: "gh" | "token"`.
- When the keychain is unavailable, the same message as for AI providers
  appears.

**Errors**

- **401, at any time.** The account is marked rejected. The Accounts tab and
  the Pull requests view say "Bitbucket rejected the token" (or GitHub), and
  no more requests go to that host until the account is saved again. (For gh
  mode, the one re-run of `gh` comes first.)
- **403/404 on a repository.** "No access to <owner>/<repo> with this
  account", followed by the host's own error text when it names a missing
  scope or permission, so a token with too few scopes is told apart. The log
  shows no icons and no other notice.

## 3. Statuses (pipelines / checks)

**Model**

- Each commit has an aggregate state, `success | failed | running | stopped
  | none`, and a list of checks. Each check carries name, state, URL, and
  start and duration when the host gives them.
- **Re-runs.** Only the latest run of each check counts.
  - Bitbucket commit statuses are keyed, so the latest per `key` counts.
  - Bitbucket pipelines count the latest pipeline per trigger: its target
    type, ref and selector (custom pipelines by their pattern).
  - GitHub's `statusCheckRollup` already does this.
  - So a pipeline that failed and was re-run green shows green.
- **Aggregation** over those latest runs:
  1. any failed → `failed`;
  2. else any in progress or pending → `running`;
  3. else any stopped/cancelled → `stopped`;
  4. else all successful → `success`;
  5. no checks → `none`, which draws nothing.
- **UI wording** uses each host's own word: "Pipelines" in Bitbucket
  repositories, "Checks" in GitHub ones. *(My call.)*

**How they are fetched (the request budget)**

- **GitHub.** One GraphQL query per batch of up to 50 commits, looked up by
  oid, asking for `statusCheckRollup` and its contexts (check runs and
  statuses). Commits the host doesn't know come back `none`.
- **Bitbucket, in the log.**
  - The repository's pipeline list (`/pipelines`, newest first, 100 per
    page), matched on `target.commit.hash`, at most 3 pages per refresh.
  - Commits older than the oldest pipeline seen count as `none` for that
    refresh.
  - This does not cover external CI (Jenkins etc.) that only reports commit
    statuses; that shows in the details.
- **Bitbucket, in the commit details.** `/commit/<hash>/statuses` (every
  build status, Pipelines included), plus the matching pipeline.
- **Budget.** The builder may change the strategy if a cheaper one exists,
  but this budget holds: a log refresh costs at most 3 requests on Bitbucket
  and 1 per 50 visible commits on GitHub.

**Which commits are asked for**

- The visible rows of the log plus one screen of margin, in the repository
  shown.
- Scrolling asks for new rows once it stops (debounce ~500 ms).
- The uncommitted-changes row is never asked for. Local-only commits are
  asked for like any other, and come back `none`.

**Cache and refresh**

- **Cache.** In memory, per repository, keyed by hash.
  - Final states (`success`, `failed`, `stopped`) don't expire during the
    session.
  - `none` expires after 10 minutes, since a pipeline may still start.
  - `running` is asked again every 30 s while it is visible, in the status
    bar, or watched after a push (§6).
- **Refresh.** Also when the window comes back to the front, on the
  toolbar's Refresh, and after a fetch or push of that repository.
- No disk cache in v1.

**Rate limits and network**

- **GitHub.** When `X-RateLimit-Remaining` reaches 0, or on 403/429 with a
  rate-limit message, nothing more goes to GitHub until
  `X-RateLimit-Reset`.
- **Bitbucket.** On 429, it waits for `Retry-After` when present, otherwise
  60 s (doubling up to 15 min while 429s continue).
- **Status bar notice.** "GitHub: request limit reached, retrying at 14:05",
  or "Bitbucket: request limit reached, retrying shortly" when there is no
  reset time.
- **No network.** It retries on the next cycle, with no notice per row.

## 4. In the log

**Row icon**

- A 12 px icon next to the short hash, only in repositories with a host and
  a connected account:
  - ✓ green for `success`
  - ✗ red for `failed`
  - an animated or ⏳ icon for `running`
  - a grey circle for `stopped`
- `none` and "not loaded yet" draw nothing, and the space is reserved so
  rows don't shift.
- *The exact place (inside a widened hash column, or its own narrow column)
  is the builder's call.* It must read well in light, dark and high
  contrast, and the 28 px row height does not change.
- **Tooltip.** "Pipeline #412 passed · 3 min", "2 of 3 checks failed",
  "Pipeline running · started 2 min ago".
- The icon is not its own button. Clicking the row selects the commit as
  today.

**Section in the commit details**

- In `CommitDetails`, inside `.info`, after `.meta`: a **Pipelines** /
  **Checks** section, shown only when the commit has checks.
- One line per check: icon, name, state text, duration, and an **Open** link
  that opens the URL in the browser.
- "Loading…" while loading. On error, one line with the reason and
  **Retry**.

## 5. Pull requests view

**Sidebar row**

- In an expanded repository with a supported host, a **Pull requests** row
  comes right before the Branches section, with an icon and the number of
  open PRs as a badge. *(My call: a first row like a navigation entry, not a
  collapsible section. The owner asked for "an option next to Branches that
  opens a new view".)*
- **The count.** Asked for only for the selected repository: when it is
  selected, on focus and on Refresh, with the cheapest call each host has
  (GraphQL `totalCount`; Bitbucket `pagelen=1` and `size`). If Bitbucket
  omits `size`, the badge counts up to the first page and shows "100+"
  beyond.
- With no account for that host, the row is still there, without a badge.
- **Clicking it** selects the repository and opens the view (new value
  `mainView = 'pulls'`). The selected PR is remembered per repository while
  the app runs.

**List (top part)**

- **Filters as tabs.** **Open** (all), **Mine** (I am the author) and
  **Review requested** (I am a reviewer and haven't approved yet). Next to
  them, a **Refresh** button and **Open in browser**, which opens the
  repository's PR list.
- **Each row shows:**
  - `#id` and title, with a Draft label when it applies
  - author
  - `source → target`
  - the status icon of the PR's last commit
  - approvals (✓ 2)
  - comment count
  - relative update time
- **Order and limit.** Most recently updated first, up to 100 PRs, then
  "Showing 100. Open the rest in the browser".
- **Where row data comes from.**
  - GitHub: one GraphQL query gives all of it, including the rollup state of
    the head commit.
  - Bitbucket: the list call with `fields=+values.participants` gives
    approvals, `comment_count` and `source.commit.hash`. The status icon
    comes from the same pipeline-list cache as the log (§3, no per-PR
    calls), so a PR whose only CI is external shows no icon in the list.
  - The comment count is whatever the host reports.
- **Empty states.**
  - "No open pull requests" (or "…of yours", "…waiting for your review").
  - With no account: "Connect your Bitbucket account to see pull requests",
    with a button that opens Settings → Accounts.
  - The errors from §2 and §3.
- **Refreshed** when the view opens, on focus while it is shown, on Refresh,
  and after a fetch of the repository. No timer while the window sits idle.

**Detail (bottom part)**

The detail uses the same split and draggable splitter as the log with its
details pane. *(The builder may reuse `detailsHeight` or give it its own
store.)*

- **Left column (`.info`), in order:**
  1. Title, `#id`, state (Open/Draft), author, `source → target`, and
     created and updated dates.
  2. **Checkout branch** and **Open in browser** buttons.
  3. The description, rendered as Markdown with the chat's renderer, no
     HTML.
  4. **Reviewers**, each marked approved, changes requested or pending.
  5. **Pipelines/Checks** of the last commit, as in §4 (on Bitbucket this
     also asks for `/statuses`, so external CI shows here).
  6. **Commits**: short hash, subject, author. Clicking one that exists
     locally takes the log there (`jumpTo`); one that doesn't shows "Not
     fetched".
  7. **Comments**, read-only:
     - one chronological list, each with author, date and Markdown text;
     - replies indented under their parent, and inline comments with a
       `path:line` header;
     - on GitHub the list merges the three sources: conversation comments,
       review comments (threaded by `in_reply_to_id`), and review bodies
       that are not empty, shown as a comment with their verdict ("approved",
       "requested changes");
     - up to 500, then a note pointing to the browser.
- **Right column.**
  - **The file list** is the shared **FileList** component, with changed
    files and their status, from Bitbucket `/diffstat` or GitHub
    `/pulls/{n}/files` (paginated).
  - **The selected file's diff** sits below the list, rendered with the
    renderer `CommitDetails` uses (lines with `lineClass`). *The builder may
    extract that into a shared component.*
  - **Diff source.** Everything comes from **the host's API**: no fetch is
    needed, and forks work.
    - GitHub: each file's `patch` from `/pulls/{n}/files`.
    - Bitbucket: the PR's unified diff (`/pullrequests/{id}/diff`, which
      redirects within the API host), split by file and capped at 10 MB.
  - A file with no diff (binary, too big for the host, or past the cap)
    shows "No diff available here. Open in browser".

**Checkout branch**

- **PR from the same repository.**
  - A fetch of the detected remote (§1) and the branch, then the existing
    `CheckoutRemote` with that remote. It creates or reuses the local branch
    of the same name tracking it, with the same rules as the sidebar today
    (branch twin, dirty working tree).
  - It runs as a status-bar task with its result, and the log then shows
    the branch.
  - A local branch that already exists and has diverged gets the existing
    checkout's message. Nothing is forced.
- **PR from a fork** (Bitbucket: `source.repository.full_name` differs from
  the target; GitHub: `isCrossRepository`, or the head repository is gone).
  The button is disabled with the text from "Not in this project".

## 6. Status bar and notifications

**Status bar**

- On the right, for the selected repository: the status icon of the current
  branch's HEAD and a short text ("Pipeline passed", "failed", "running").
  It appears only when that commit has checks.
- Clicking it selects HEAD in the log and opens its details.
- Rate-limit notices (§3) share the same spot.

**Watching a push**

- **What is watched.** After a successful push from CommitTree, in a
  repository with a host and an account, the pushed tip of each branch.
  - The trigger is the toolbar, the branch menu, Push all, or the chat's
    push tool. All of these go through the same App push methods, which is
    where the hook lives.
  - New branches are included. The app has no force push.
- **How long.** It polls every 30 s and stops at a final state, after 60
  minutes, or after 5 minutes with no check at all. Watches end when the app
  quits.
- **Where it runs.** In Go, where pushes happen. It emits `hosting:status`
  (repository, commit, new state) as the state changes. At a final state,
  the frontend turns that into a notification:
  - "Pipeline passed on feature/x · acme-api"
  - "Pipeline failed on feature/x · acme-api"
  - GitHub says "Checks passed/failed".
- **New notification category "Pipelines"**, with its own switch in
  Settings → General, on by default. The existing rules (`decide()`: window
  focus, active repository) pick toast, system notification or nothing.
- **Clicking the notification** opens the repository and selects that
  commit.

## 7. Backend shape

*A guide for the builder: names may change, responsibilities may not.*

- **`internal/hosting`** holds:
  - the types: `Repo`, `Host`, `Check`, `CommitStatus`, `PullRequest`,
    `PullRequestDetail`, `Comment`, `Account`
  - `ParseRemoteURL`
  - the per-host interface: current user, statuses in bulk, checks for one
    commit, PR list by filter, PR count, PR detail, PR files and diff, PR
    comments, PR commits
  - the per-repository cache
  - the per-host rate-limit state
- **`internal/hosting/bitbucket` and `internal/hosting/github`.**
  - Plain `net/http` with one shared client, a per-request timeout
    (~20 s) and context. No host SDK *(my call: few dependencies, like the
    Ollama client)*.
  - Tests use `httptest` with recorded responses.
  - The client refuses to send the token to any host other than its API,
    checked on every request, redirects included.
- **`internal/app/hosting.go`** holds the bound methods:
  - per repository: `HostingInfo(repoID)` (host, owner/repo, remote name,
    web URL, account state), `CommitStatuses(repoID, hashes)`,
    `CommitChecks(repoID, hash)`, `ListPullRequests(repoID, filter)`,
    `PullRequestCount(repoID)`, `GetPullRequest(repoID, id)`,
    `GetPullRequestFiles(repoID, id)`, `CheckoutPullRequest(repoID, id)`
  - accounts: `GetHostingAccounts`, `SaveBitbucketAccount`,
    `SaveGitHubAccount`, `RemoveHostingAccount`
  - Network calls never hold the repository's write lock. Checkout does, as
    today.
- **Frontend.**
  - `lib/hosting.ts`: visible-row requests, debounce, asking again for
    `running` every 30 s.
  - A new `PullRequestsView.svelte`.
  - Changes to `RepoRefs`, `LogList`, `CommitDetails`, `StatusBar`,
    `SettingsDialog`/`settingsTabs`, `App.svelte` (main view), and the
    notification rules.
- **Opening URLs** uses Wails' `BrowserOpenURL`, for `https` URLs only.

## 8. Tests the builder must cover

- `ParseRemoteURL`: every form in §1, URLs with credentials, other hosts,
  aliases (not supported).
- Re-run handling and aggregation (§3), both hosts.
- **Each client against recorded responses:** success, 401, 403 with a scope
  message, 404, 429 with and without reset, pagination, and a commit unknown
  to the host.
- **Token confinement:** the token never goes to another host, redirects
  included.
- **Cache:** final states are not asked again, `none` expires, and `running`
  is asked again.
- **gh mode:** the token is cached, and a 401 re-runs `gh` once.
- **Push watching:** it stops at a final state, at 60 min, and at 5 min with
  no checks, and emits the final event once.
- **Frontend (vitest):** list filters and empty states, the per-file diff
  split, the GitHub comment merge order, and the Accounts tab.

## Calls I made (for the owner to check)

- Fork checkout and SSH host aliases are left out of v1.
- The Pull requests row is the first row inside the repository, not a
  collapsible section.
- The wording is "Pipelines" on Bitbucket and "Checks" on GitHub.
- Only the latest run of each check counts, so a red pipeline re-run green
  shows green.
- PR diffs come from the host's API, not local git (works with forks, no
  fetch).
- No host SDK: plain HTTP.
- **Bitbucket's statuses in the log and in the PR list come from the
  pipeline list (cheap).** External CI shows only in commit and PR details.
- **The GitHub CLI token is cached for the session.** The app looks for `gh`
  in the usual install places, not only the PATH.
- Only open PRs are listed, up to 100, with up to 500 comments.
- The new notification category "Pipelines" is on by default.
