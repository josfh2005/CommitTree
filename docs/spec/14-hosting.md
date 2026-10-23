# Code hosting: Bitbucket and GitHub

CommitTree can look past git at the code host behind a repository. For
repositories on Bitbucket Cloud (bitbucket.org) or GitHub (github.com) it
shows the state of the pipelines or checks of each commit, using an account
the user connects in Settings. Every other repository looks and behaves
exactly as before: no icons, no extra rows, nothing in the status bar.

This document grows with the feature. It now covers detecting the host,
accounts, and commit statuses with their cache and rate limits. The log
icons, the commit details, the Pull requests view and the notifications are
described here as they land.

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
