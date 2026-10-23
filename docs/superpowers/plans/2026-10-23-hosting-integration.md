# Bitbucket and GitHub Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Read-only Bitbucket Cloud and GitHub.com integration. Pipeline/check status shows in the log, the commit details and the status bar; a Pull requests view opens from the sidebar; a notification fires when a push's pipeline finishes; tokens are kept in an Accounts tab.

**Architecture:** A new Go package `internal/hosting` holds:
- the host-neutral types, the remote-URL parser, status aggregation, a status cache, a confined HTTP doer with rate-limit state, and a push watcher;
- `internal/hosting/accounts`, for credentials (keychain plus `hosting.json`);
- one client per host, in `internal/hosting/bitbucket` and `internal/hosting/github`.

`internal/app/hosting.go` binds it to the frontend. The frontend adds:
- `lib/hosting.ts`, which loads visible-row statuses and polls `running` every 30 s;
- `lib/prs.ts`, pure helpers for the Pull requests view;
- new components for the Accounts tab, the checks list, the PR view and the PR detail;
- small changes to LogList, CommitDetails, StatusBar, RepoRefs, App, stores and the notification rules.

**Tech Stack:** Go 1.26 with `net/http` (no host SDK, no new Go dependency), Wails v2 bindings, Svelte with TypeScript, vitest.

**Spec:** `docs/superpowers/specs/2026-10-23-hosting-integration-design.md`. Read it before any task: section numbers below (§1–§8) refer to it.

## Global Constraints

- Hosts are `bitbucket.org` and `github.com` only. API bases are `https://api.bitbucket.org/2.0` and `https://api.github.com` (GraphQL at `https://api.github.com/graphql`).
- A token is sent only to its own API host, redirects included. It is never put in the command log, error text, the chat or a URL.
- No new Go module or npm dependency.
- Keychain service `git-ui`, accounts `bitbucket` and `github`. No plaintext fallback.
- `hosting.json` sits next to `ai.json` (`settings.DefaultPath()`) and is written atomically (temp file + rename).
- Per-request HTTP timeout is 20 s, and every request carries the caller's context.
- **Status cache:**
  - final states (`success`, `failed`, `stopped`) never expire in a session;
  - `none` expires after 10 min;
  - `running` is asked again after 30 s.
- **Bitbucket log statuses:** the `/pipelines` list, `sort=-created_on`, `pagelen=100`, at most 3 pages per refresh. **GitHub log statuses:** one GraphQL query per batch of at most 50 commits.
- Visible-row requests are debounced 500 ms, with a margin of one screen of rows.
- **Rate limits:**
  - GitHub waits until `X-RateLimit-Reset` when `X-RateLimit-Remaining: 0`, or on a 403/429 with a rate-limit message;
  - Bitbucket on 429 waits `Retry-After`, otherwise 60 s, doubling to at most 15 min while 429s continue.
- **Push watch:** poll every 30 s; stop at a final state, at 60 min, or at 5 min with no checks.
- **PR caps:** 100 PRs per list, 500 comments per PR, a 10 MB Bitbucket PR diff, 30 pages of 100 GitHub PR files.
- **UI copy, verbatim:**
  - "Connected as <name>"
  - "Bitbucket rejected the token" / "GitHub rejected the token"
  - "No access to <owner>/<repo> with this account"
  - "GitHub CLI isn't logged in (gh auth login)"
  - "From a fork. Open it in the browser."
  - "No diff available here. Open in browser"
  - "Showing 100. Open the rest in the browser"
  - "No open pull requests" / "No open pull requests of yours" / "No open pull requests waiting for your review"
  - "Connect your Bitbucket account to see pull requests" (GitHub likewise)
  - "Not fetched"
  - "GitHub: request limit reached, retrying at HH:MM" / "Bitbucket: request limit reached, retrying shortly"
  - the notification bodies "Pipeline passed on <branch>" and "Pipeline failed on <branch>" (GitHub: "Checks passed on <branch>" / "Checks failed on <branch>"). The notification title is the repository name, as for every notification.
- **Wording:** the section and status are called "Pipelines" in Bitbucket repositories and "Checks" in GitHub ones.
- **Git conventions:** run git as `/usr/bin/git`. Commit messages carry no `Co-Authored-By` line. Never merge; the owner merges with `--no-ff` when they say so.
- **Docs and bindings in the same commit:** every behaviour change updates the named `docs/spec/` file in that commit (the design doc does not count). New App methods are followed by `~/go/bin/wails generate module`, and the regenerated `frontend/wailsjs/go/**` is committed.
- **Test commands:** Go `go test ./internal/...`; frontend `cd frontend && npx vitest run` and `npm run check`.

## Review Focus

1. **The remote URL is edited after the integration has detected it**, through Repository settings → Remotes. The next `HostingInfo` must reflect the new remote, with no stale owner/name. *Test in Task 6.*
2. **The user switches repositories while a status request is in flight.** A late answer for repository A must not paint icons into repository B's log. *Test in Task 10.*
3. **The token is saved again after a 401.** The rejected flag clears and requests resume without restarting the app. *Test in Task 3.*
4. **Blank or whitespace-only token or email.** It is refused before anything touches the keychain or the network. *Test in Task 3.*
5. **A PR title, description or comment contains HTML or a `javascript:` link.** It renders as escaped text, and only `https` links open. *Test in Task 12.*

---

## File Structure

**Go, new:**

| File | Responsibility |
|---|---|
| `internal/hosting/hosting.go` | `Host`, `Repo`, `ParseRemoteURL`, `Detect`, the `Client` interface, PR types |
| `internal/hosting/status.go` | `State`, `Check`, `CommitStatus`, `Latest`, `Aggregate` |
| `internal/hosting/diffsplit.go` | `SplitUnifiedDiff` |
| `internal/hosting/errors.go` | `ErrRejected`, `ErrNoAccount`, `NoAccessError`, `RateLimitError` |
| `internal/hosting/doer.go` | the confined HTTP doer and per-host rate-limit state |
| `internal/hosting/cache.go` | the per-repository status cache |
| `internal/hosting/watch.go` | the push watcher |
| `internal/hosting/accounts/accounts.go` | `hosting.json`, the credential store, `gh` discovery |
| `internal/hosting/bitbucket/bitbucket.go` | the Bitbucket `Client` |
| `internal/hosting/github/github.go` | the GitHub `Client` (REST + GraphQL) |
| `internal/app/hosting.go` | the bound methods and the push hook |

Each package has a `_test.go` file next to it; the clients also get a `testdata/` folder of recorded JSON.

**Go, modified:**
- `internal/ai/keys/keys.go`: the `known()` allow-list
- `internal/app/remote.go`: the push hooks
- `internal/app/notify.go`: `Notification.Hash`
- `internal/app/app.go`: the `App` field for hosting state

**Frontend, new:**
- `src/lib/hosting.ts` (+ test)
- `src/lib/prs.ts` (+ test)
- `src/components/AccountsTab.svelte`
- `src/components/ChecksList.svelte`
- `src/components/PullRequestsView.svelte`
- `src/components/PullRequestDetail.svelte`
- `src/components/DiffText.svelte`, extracted from CommitDetails

**Frontend, modified:**
- `lib/types.ts`, `lib/api.ts`, `lib/stores.ts`
- `lib/settingsTabs.ts` (+ test)
- `lib/notifyRules.ts` (+ test), `lib/notify.ts`
- `components/SettingsDialog.svelte`, `LogList.svelte`, `CommitDetails.svelte`, `StatusBar.svelte`, `RepoRefs.svelte`
- `App.svelte`

**Spec:**
- new `docs/spec/14-hosting.md`, added to the list in `docs/spec/README.md`
- edits to `01-repositories-and-sidebar.md`, `02-log-and-history.md`, `07-conventions-and-constraints.md`, `11-notifications.md` and `13-status-bar.md`

---

### Task 1: Hosting core types, remote parsing, aggregation, diff split

**Files:**
- Create: `internal/hosting/hosting.go`
- Create: `internal/hosting/status.go`
- Create: `internal/hosting/diffsplit.go`
- Create: `internal/hosting/errors.go`
- Test: `internal/hosting/hosting_test.go`
- Test: `internal/hosting/status_test.go`
- Test: `internal/hosting/diffsplit_test.go`

**Interfaces:**

Produces, in `hosting.go`:

```go
type Host string
const (Bitbucket Host = "bitbucket"; GitHub Host = "github")
type Repo struct {
  Host   Host   `json:"host"`
  Owner  string `json:"owner"`
  Name   string `json:"name"`
  Remote string `json:"remote"` // git remote name
}
func (r Repo) FullName() string // "owner/name"
func (r Repo) WebURL() string   // https://bitbucket.org/o/n | https://github.com/o/n
func (r Repo) PullsURL() string // …/pull-requests | …/pulls
func ParseRemoteURL(raw string) (Repo, bool) // Remote left empty
func Detect(remotes []ops.Remote) (Repo, bool) // origin first, else first in name order; sets Remote
type Filter string
const (FilterOpen Filter = "open"; FilterMine Filter = "mine"; FilterReview Filter = "review")
type Account struct{ ID, Name, Login string } // ID: Bitbucket uuid / GitHub login
type PullRequest struct {
  Number int; Title string; Draft bool; Author, AuthorID string
  Source, Target, HeadHash, URL string; Fork bool
  Approvals, Comments int; Updated time.Time
  State State // head commit's aggregate; "" when unknown
}
type Reviewer struct{ Name, Verdict string } // "approved" | "changes" | "pending"
type Comment struct {
  ID, Parent, Author string; Created time.Time; Body string
  Path string; Line int; Verdict string
}
type PRCommit struct{ Hash, Short, Subject, Author string }
type PullRequestDetail struct {
  PullRequest
  Description string; Created time.Time
  Reviewers []Reviewer; Commits []PRCommit
  Comments []Comment; CommentsMore bool
  Checks CommitStatus
}
type PRFile struct{ Path, OldPath, Status, Patch string; NoDiff bool }
type Count struct{ N int; More bool }
type Client interface {
  User(ctx context.Context) (Account, error)
  Statuses(ctx context.Context, r Repo, hashes []string) (map[string]CommitStatus, error)
  Checks(ctx context.Context, r Repo, hash string) (CommitStatus, error)
  PullRequests(ctx context.Context, r Repo, f Filter, me Account) (prs []PullRequest, more bool, err error)
  PullRequestCount(ctx context.Context, r Repo) (Count, error)
  PullRequest(ctx context.Context, r Repo, number int) (PullRequestDetail, error)
  PullRequestFiles(ctx context.Context, r Repo, number int) ([]PRFile, error)
}
```

All exported struct fields get lowerCamel JSON tags; times are RFC 3339.

Produces, in `status.go`:

```go
type State string
const (Success State = "success"; Failed State = "failed"; Running State = "running"; Stopped State = "stopped"; None State = "none")
type Check struct {
  Key, Name string; State State; URL string
  Started time.Time; DurationSec int; Created time.Time
}
type CommitStatus struct{ Hash string; State State; Checks []Check }
func Latest(checks []Check) []Check // newest Created per Key; order by Name
func Aggregate(checks []Check) State // over Latest(checks), §3 order
func (s State) Final() bool          // success, failed, stopped
```

Produces, in `diffsplit.go`: `func SplitUnifiedDiff(diff string) map[string]string`. It maps each file's new path (old path for a deletion) to that file's text, starting at its `diff --git` line.

Produces, in `errors.go`:
- `var ErrRejected = errors.New("rejected the token")`
- `var ErrNoAccount = errors.New("no account")`
- `type NoAccessError struct{ Repo, Detail string }`. `Error()` gives "No access to <repo> with this account", plus ": <detail>" when Detail is set.
- `type RateLimitError struct{ Host Host; Until time.Time; Known bool }`.

- [ ] **Step 1: Write the failing tests**
  - `TestParseRemoteURL`, table-driven. Each case is an input and the expected `(Host, Owner, Name, ok)`:
    - `https://bitbucket.org/acme/api.git` → bitbucket/acme/api
    - `https://jose@bitbucket.org/acme/api` → bitbucket/acme/api
    - `https://user:secret@github.com/o/r.git` → github/o/r
    - `git@bitbucket.org:acme/api.git` → bitbucket/acme/api
    - `ssh://git@github.com/o/r.git` → github/o/r
    - `git@github.com:o/r` → github/o/r
    - `https://gitlab.com/o/r` → not ok
    - `git@bitbucket-work:acme/api.git` → not ok (SSH alias)
    - `https://bitbucket.org/acme` → not ok
    - `/local/path` → not ok
  - `TestDetectPrefersOrigin`: with `[{upstream, github}, {origin, bitbucket}]` it returns bitbucket with `Remote == "origin"`.
  - `TestDetectFirstByName`: with `[{zeta, github}, {alpha, github}]` and no origin, it returns `Remote == "alpha"`.
  - `TestDetectNone`: with a gitlab-only remote it returns not ok.
  - `TestLatestKeepsNewestPerKey`: two checks with Key "build", failed at T1 and success at T2, give one check, success.
  - `TestAggregate`, table-driven:
    - failed+running → failed
    - running+success → running
    - stopped+success → stopped
    - success+success → success
    - empty → none
    - failed@T1 + success@T2 on the same key → success
  - `TestSplitUnifiedDiff`: a two-file diff (one modified, one deleted) gives two keys; each value starts with `diff --git`; the deleted file is keyed by its old path.

- [ ] **Step 2: Run them to see them fail.**
  Run: `go test ./internal/hosting/` → FAIL (package does not compile).

- [ ] **Step 3: Implement the files above.**
  `ParseRemoteURL` uses `net/url` for `https://` and `ssh://`, and splits scp-like `git@host:path` by hand. It accepts only hosts `bitbucket.org` and `github.com`, case-insensitive, and exactly two path segments after trimming `.git` and slashes.

- [ ] **Step 4: Run them to see them pass.**
  Run: `go test ./internal/hosting/` → PASS.

- [ ] **Step 5: Commit.**
  `/usr/bin/git add internal/hosting && /usr/bin/git commit -m "feat(hosting): host types, remote URL parsing, check aggregation and diff split"`

---

### Task 2: Confined HTTP doer and rate-limit state

**Files:**
- Create: `internal/hosting/doer.go`
- Test: `internal/hosting/doer_test.go`

**Interfaces:**
- Consumes: Task 1's errors and `Host`.
- Produces:

```go
type Doer struct {
  Host    Host
  BaseURL string                                   // e.g. https://api.bitbucket.org/2.0 (tests: httptest URL)
  Auth    func(ctx context.Context) (string, error) // full Authorization header value
  HTTP    *http.Client                             // nil → shared client with 20 s timeout
  Limits  *Limits
  OnReject func()                                  // called on a 401
}
func (d *Doer) JSON(ctx context.Context, method, pathOrURL string, body, out any) (http.Header, error)
func (d *Doer) Raw(ctx context.Context, pathOrURL string, max int64) ([]byte, error)
type Limits struct{ /* per-host block-until + Bitbucket backoff */ }
func NewLimits(now func() time.Time) *Limits
func (l *Limits) Blocked(h Host) (RateLimitError, bool)
```

- [ ] **Step 1: Write the failing tests**, all with `httptest.NewServer`:
  - `TestDoerSendsAuthOnlyToBase`: the request reaches the server with the `Authorization` header. A `pathOrURL` that is an absolute URL on another host returns an error, and that second server records no request.
  - `TestDoerRefusesCrossHostRedirect`: server A answers 302 to server B. The error is non-nil and B receives nothing.
  - `TestDoerFollowsSameHostRedirect`: a 302 to a path on the same server succeeds.
  - `TestDoer401`: gives `errors.Is(err, ErrRejected)` and calls `OnReject` once.
  - `TestDoer403WithScopeDetail`: the body `{"error":{"message":"Your credentials lack one or more required privilege scopes."}}` (Bitbucket shape) or `{"message":"Resource not accessible by personal access token"}` (GitHub shape) gives a `*NoAccessError` whose Detail is that message.
  - `TestDoer404`: gives a `*NoAccessError` with an empty Detail.
  - `TestGitHubRateLimitHeaders`: a response with `X-RateLimit-Remaining: 0` and `X-RateLimit-Reset: <unix>` makes `Limits.Blocked(GitHub)` true with `Until` equal to that time and `Known` true. The next `JSON` call returns a `*RateLimitError` without hitting the server.
  - `TestBitbucketBackoff`: 429 with no `Retry-After` blocks for 60 s (fake clock), then 120 s on the next 429, capped at 15 min; `Known` is false. With `Retry-After: 30` it blocks for 30 s and `Known` is true.
  - `TestRawCap`: a body over `max` returns an error that wraps `ErrTooLarge`, declared in `doer.go`.

- [ ] **Step 2: Run them.**
  `go test ./internal/hosting/ -run 'Doer|RateLimit|Backoff|Raw'` → FAIL.

- [ ] **Step 3: Implement.**
  - Before each request, check the host. Set `CheckRedirect` to refuse a target whose host differs from `BaseURL`'s.
  - Read error bodies up to 64 KB. Never include the request headers in an error.

- [ ] **Step 4: Run them again** → PASS.

- [ ] **Step 5: Commit.**
  `feat(hosting): HTTP doer confined to the API host, error mapping and rate limits`

---

### Task 3: Accounts: keychain names, hosting.json and the GitHub CLI

**Files:**
- Modify: `internal/ai/keys/keys.go`. In `known()`, add `"bitbucket", "github"`.
- Modify: `internal/ai/keys/keys_test.go`
- Create: `internal/hosting/accounts/accounts.go`
- Test: `internal/hosting/accounts/accounts_test.go`

**Interfaces:**
- Consumes: `keys.Get/Set/Delete`; `hosting.Host`; `hosting.ErrNoAccount`.
- Produces:

```go
type Config struct {
  BitbucketEmail string `json:"bitbucketEmail,omitempty"`
  BitbucketName  string `json:"bitbucketName,omitempty"` // "Connected as"
  BitbucketID    string `json:"bitbucketID,omitempty"`   // uuid, for Mine/Review filters
  GitHubSource   string `json:"githubSource,omitempty"`  // "gh" | "token" | ""
  GitHubLogin    string `json:"githubLogin,omitempty"`
}
func PathNextTo(aiSettingsPath string) string // dir/hosting.json
func Load(path string) (Config, error)        // missing file → zero Config
func Save(path string, c Config) error        // atomic
type Store struct{ /* path, mu, cached gh token, rejected set, RunGH */ }
func NewStore(path string) *Store
func (s *Store) Config() (Config, error)
func (s *Store) Auth(h hosting.Host) func(ctx context.Context) (string, error)
  // Bitbucket: "Basic " + base64(email:token); GitHub: "Bearer " + token
  // (from the keychain or gh); hosting.ErrNoAccount when not set
func (s *Store) OnReject(h hosting.Host) func() // marks rejected; GitHub gh mode: drops cached token, re-run allowed once
func (s *Store) Rejected(h hosting.Host) bool
func (s *Store) SaveBitbucket(email, token string, me hosting.Account) error // clears rejected
func (s *Store) SaveGitHub(source, token string, me hosting.Account) error   // token ignored when source=="gh"; clears rejected
func (s *Store) Remove(h hosting.Host) error
func FindGH() (path string, ok bool) // exec.LookPath, then /opt/homebrew/bin/gh, /usr/local/bin/gh, /usr/bin/gh
var RunGH = func(ctx context.Context, gh string) (string, error) // `gh auth token --hostname github.com`, trimmed
```

- [ ] **Step 1: Write the failing tests.** Swap in a fake store with `keys.UseStore`, and stub `RunGH`.
  - `TestKnownAcceptsHostingAccounts`: `keys.Set("bitbucket","t")` and `keys.Set("github","t")` succeed; `keys.Set("gitlab","t")` errors.
  - `TestLoadMissingIsZero` and `TestSaveIsAtomic`: no `.tmp` file is left behind, and a round-trip keeps the fields.
  - `TestAuthBitbucketBasic`: email `a@b.c` with token `tok` gives `Basic YUBiLmM6dG9r`.
  - `TestAuthNoAccount`: errors with `hosting.ErrNoAccount`.
  - `TestGHTokenCachedAndRefreshedOnce`: two `Auth` calls run `RunGH` once. After `OnReject(GitHub)()`, the next call runs it again. After a second reject with no successful request in between, `Rejected(GitHub)` is true.
  - `TestGHFailure`: `RunGH` erroring gives the error text "GitHub CLI isn't logged in (gh auth login)".
  - **Review Focus 3**, `TestSaveClearsRejected`: `OnReject(Bitbucket)()`, then `SaveBitbucket(...)`, then `Rejected(Bitbucket)` is false.
  - **Review Focus 4**, `TestSaveRefusesBlank`: `SaveBitbucket("  ", "tok", …)` and `SaveBitbucket("a@b.c", "   ", …)` return an error, and the fake keychain stays empty. `SaveGitHub("token", " ", …)` behaves the same.
  - `TestRemoveClearsKeyAndConfig`.

- [ ] **Step 2: Run them.**
  `go test ./internal/ai/keys/ ./internal/hosting/accounts/` → FAIL.

- [ ] **Step 3: Implement.** In `Save`, write a temp file in the same directory, then rename it.

- [ ] **Step 4: Run them again** → PASS.

- [ ] **Step 5: Commit.**
  `feat(hosting): accounts in the keychain and hosting.json, GitHub CLI token`

---

### Task 4: Bitbucket client

**Files:**
- Create: `internal/hosting/bitbucket/bitbucket.go`
- Test: `internal/hosting/bitbucket/bitbucket_test.go`
- Test: `internal/hosting/bitbucket/testdata/*.json`. These are realistic, trimmed responses; build them from Atlassian's REST docs.

**Interfaces:**
- Consumes: `hosting.Doer`, `hosting.Client`, and Task 1's types and functions.
- Produces: `func New(d *hosting.Doer) hosting.Client`. `Doer.BaseURL` is `https://api.bitbucket.org/2.0`.

**Endpoints, field mapping and rules:**

- **`User`.** `GET /user` returns `{display_name, uuid}`, mapped to `Account{ID: uuid, Name: display_name}`.
- **`Statuses`.** `GET /repositories/{o}/{n}/pipelines?sort=-created_on&pagelen=100&page=N`, pages 1..3. Stop early once every requested hash is found.
  - Each pipeline becomes a `Check`:
    - `Key` = `target.type + "|" + target.ref_name + "|" + target.selector.type + ":" + target.selector.pattern`
    - `Name` = "Pipeline #<build_number>"
    - `URL` = `https://bitbucket.org/{o}/{n}/pipelines/results/{build_number}`
    - `Created` = `created_on`; `DurationSec` = `duration_in_seconds`
  - **State mapping:**
    - `state.name` PENDING, IN_PROGRESS or PAUSED → running
    - COMPLETED with `result.name` SUCCESSFUL → success
    - COMPLETED with FAILED or ERROR → failed
    - COMPLETED with STOPPED or EXPIRED → stopped
  - Requested hashes with no pipeline map to `CommitStatus{State: None}`.
- **`Checks`.** `GET /repositories/{o}/{n}/commit/{hash}/statuses?pagelen=100`. Each status becomes a check with `Key: key`, `Name: name`, `URL: url` and `Created: updated_on`.
  - Status state: SUCCESSFUL → success, FAILED → failed, INPROGRESS → running, STOPPED → stopped.
  - The result is merged with the matching pipelines from page 1 of the pipeline list, deduplicated by URL, then `Latest` and `Aggregate` run over it.
- **`PullRequests`.** `GET /repositories/{o}/{n}/pullrequests?state=OPEN&sort=-updated_on&pagelen=50&fields=%2Bvalues.participants`. Pages are followed through `next` until 100 PRs (`more` is true when a 101st exists).
  - `FilterMine` adds `q=author.uuid="<me.ID>"`.
  - `FilterReview` adds `q=reviewers.uuid="<me.ID>"`, then drops PRs where my participant has `approved: true`.
  - `Approvals` counts participants with `approved: true`.
  - `Fork` is true when `source.repository.full_name` differs from `destination.repository.full_name`.
  - `State` is left `""`; the app fills it from the cache (Task 7).
  - `URL` is `links.html.href`.
- **`PullRequestCount`.** The same list with `pagelen=1`, reading `size`. If `size` is absent, the count is the first page's 100 values (`pagelen=100`), and `More` is true when `next` exists.
- **`PullRequest`.** Four calls:
  - `GET …/pullrequests/{id}`, for the description (`description` or `summary.raw`), `created_on` and the participants. Reviewer verdicts: `approved` → "approved", `state == "changes_requested"` → "changes", otherwise "pending". Only `role: REVIEWER` participants count.
  - `GET …/pullrequests/{id}/commits?pagelen=100`
  - `GET …/pullrequests/{id}/comments?pagelen=100`, following `next` up to 500. Comments with `deleted: true` are skipped. Mapping: `parent.id` → Parent; `inline.path` → Path; `inline.to`, else `inline.from` → Line; `content.raw` → Body. `CommentsMore` is true past 500.
  - `Checks` = `Checks(r, source.commit.hash)`.
- **`PullRequestFiles`.**
  - `GET …/pullrequests/{id}/diffstat?pagelen=500` gives the status (added, removed, modified, renamed), `new.path` and `old.path`.
  - `Raw(…/pullrequests/{id}/diff, 10<<20)` is split with `SplitUnifiedDiff`, and each file gets its patch.
  - A file missing from the split, or the whole diff on `ErrTooLarge`, gives `NoDiff: true`.

- [ ] **Step 1: Write the failing tests.** Each runs against an `httptest` server that serves the testdata and asserts the request path and query.
  - `TestUser`
  - `TestStatusesFromPipelines`: covers a re-run (failed then successful on the same ref, so success) and a hash with no pipeline (none). The test also asserts at most 3 requests when hashes are missing from every page.
  - `TestStatusesStopsEarly`: a single page when every hash is on page 1.
  - `TestChecksMergesStatusesAndPipelines`
  - `TestPullRequestsMineQuery`: asserts the `q=` value.
  - `TestPullRequestsReviewDropsApproved`
  - `TestPullRequestsFork`
  - `TestPullRequestsCapsAt100`
  - `TestCountUsesSize`, `TestCountWithoutSize`
  - `TestPullRequestDetail`: reviewers' verdicts, comment threading fields, a deleted comment skipped.
  - `TestPullRequestFilesSplitsDiff`, `TestPullRequestFilesTooLarge`

- [ ] **Step 2: Run them.** `go test ./internal/hosting/bitbucket/` → FAIL.

- [ ] **Step 3: Implement `New` and the seven methods** with the rules above. The JSON structs stay private to the package.

- [ ] **Step 4: Run them again** → PASS.

- [ ] **Step 5: Commit.** `feat(hosting): Bitbucket Cloud client (pipelines, statuses, pull requests)`

---

### Task 5: GitHub client

**Files:**
- Create: `internal/hosting/github/github.go`
- Test: `internal/hosting/github/github_test.go`
- Test: `internal/hosting/github/testdata/*.json`

**Interfaces:**
- Consumes: the same as Task 4.
- Produces: `func New(d *hosting.Doer) hosting.Client`. `BaseURL` is `https://api.github.com`, and GraphQL is posted to `BaseURL + "/graphql"`.
- Every request carries the headers `Accept: application/vnd.github+json` and `X-GitHub-Api-Version: 2022-11-28`.

**Endpoints, field mapping and rules:**

- **`User`.** `GET /user` returns `{login, name}`, mapped to `Account{ID: login, Login: login, Name: name or login}`.
- **`Statuses`.** Hashes go in batches of 50, with one GraphQL query per batch. The aliases are `c0…c49`: `cN: object(oid: "<sha>") { ... on Commit { statusCheckRollup { contexts(first: 100) { nodes { __typename ... on CheckRun { name status conclusion detailsUrl startedAt completedAt } ... on StatusContext { context state targetUrl createdAt } } } } } }`. The query runs inside `repository(owner:$owner, name:$name)`.
  - A null object or a null rollup gives None.
  - A check run has `Key` = name. Its state mapping:
    - status other than COMPLETED → running
    - conclusion SUCCESS, NEUTRAL or SKIPPED → success
    - FAILURE, TIMED_OUT, STARTUP_FAILURE or ACTION_REQUIRED → failed
    - CANCELLED or STALE → stopped
  - A StatusContext has `Key` = context. Its state mapping: SUCCESS → success; FAILURE or ERROR → failed; PENDING or EXPECTED → running.
  - The aggregate comes from `Aggregate`, not the rollup's own state.
  - A GraphQL `errors` array with type `NOT_FOUND` on the repository maps to `*NoAccessError`.
- **`Checks`.** `Statuses` for one hash.
- **`PullRequests`.** One GraphQL query: `repository { pullRequests(states: OPEN, first: 100, orderBy: {field: UPDATED_AT, direction: DESC}) { totalCount nodes { … } } }`.
  - Node fields: `number title isDraft url updatedAt author{login} headRefName baseRefName headRefOid isCrossRepository headRepository{nameWithOwner} comments{totalCount} latestReviews(first: 50){nodes{author{login} state}} reviewRequests(first: 50){nodes{requestedReviewer{... on User{login}}}} commits(last: 1){nodes{commit{statusCheckRollup{state}}}}`.
  - `Approvals` counts latest reviews with state APPROVED.
  - `Fork` is `isCrossRepository || headRepository == null`.
  - `State` maps the rollup state: SUCCESS → success; FAILURE or ERROR → failed; PENDING or EXPECTED → running; null → "".
  - Filters are applied in Go: Mine is `author.login == me.Login`; Review is me among the `reviewRequests` logins.
  - `more` is `totalCount > 100`, and is only meaningful for Open.
- **`PullRequestCount`.** GraphQL `pullRequests(states: OPEN) { totalCount }`, returning `More` false.
- **`PullRequest`.** REST calls:
  - `GET /repos/{o}/{n}/pulls/{num}` for body, created_at and requested_reviewers.
  - `GET …/pulls/{num}/reviews?per_page=100`. Verdicts per user come from the latest review: APPROVED → approved, CHANGES_REQUESTED → changes. Requested reviewers with no verdict are pending.
  - `GET …/pulls/{num}/commits?per_page=100`
  - `GET /repos/{o}/{n}/issues/{num}/comments?per_page=100`
  - `GET …/pulls/{num}/comments?per_page=100`, with `in_reply_to_id` → Parent, `path` → Path, and `line`, else `original_line` → Line.
  - **The comment list** merges, in `Created` order, the issue comments, the review comments, and reviews with a non-empty body, with Verdict "approved", "requested changes" or "commented". It stops at 500 and sets `CommentsMore`.
  - `Checks` = `Checks(head.sha)`.
- **`PullRequestFiles`.** `GET …/pulls/{num}/files?per_page=100&page=N`, up to 30 pages, with `filename` → Path, `previous_filename` → OldPath, `status` and `patch`. A missing `patch` gives `NoDiff: true`.

- [ ] **Step 1: Write the failing tests.**
  - `TestUser`
  - `TestStatusesBatchesOf50`: 120 hashes give 3 POSTs, and the second body contains `c49` but not `c50`.
  - `TestStatusesMapping`: a check run cancelled → stopped; a status context error → failed; an unknown commit → none.
  - `TestStatusesRepoNotFound`
  - `TestPullRequestsFiltersMineAndReview`
  - `TestPullRequestsForkWhenHeadRepoNull`
  - `TestPullRequestDetailMergesCommentsInOrder`: an issue comment at T2, a review body at T1 and a review comment reply at T3 give the order T1, T2, T3, with Parent set on the reply.
  - `TestPullRequestFilesNoPatch`
  - `TestHeaders`: asserts Accept and the API version on every request.

- [ ] **Step 2: Run them.** `go test ./internal/hosting/github/` → FAIL.

- [ ] **Step 3: Implement.**

- [ ] **Step 4: Run them again** → PASS.

- [ ] **Step 5: Commit.** `feat(hosting): GitHub client (GraphQL statuses and PR list, REST detail and files)`

---

### Task 6: Status cache and the App methods for info, statuses and accounts

**Files:**
- Create: `internal/hosting/cache.go`
- Test: `internal/hosting/cache_test.go`
- Create: `internal/app/hosting.go`
- Test: `internal/app/hosting_test.go`
- Modify: `internal/app/app.go`. Add a `hosting *hostingState` field and initialise it where `aiState` is.
- Regenerate: `frontend/wailsjs/go/**`
- Create: `docs/spec/14-hosting.md`, covering §1–§3 now. Later tasks extend it.
- Modify: `docs/spec/README.md`, to list 14.

**Interfaces:**
- Consumes: Tasks 1–5.
- Produces, in Go:

```go
// cache.go
type Cache struct{ /* mu, entries map[repoKey]map[hash]entry, now */ }
func NewCache(now func() time.Time) *Cache
func (c *Cache) Split(repoKey string, hashes []string) (have map[string]CommitStatus, missing []string)
func (c *Cache) Put(repoKey string, sts map[string]CommitStatus)
func (c *Cache) Get(repoKey, hash string) (CommitStatus, bool)
```

`repoKey` is `string(host)+":"+owner/name`, so a remote edit changes the key.

In `internal/app/hosting.go`:

```go
type HostingInfo struct {
  Supported bool `json:"supported"`
  hosting.Repo
  WebURL   string `json:"webURL"`
  PullsURL string `json:"pullsURL"`
  Noun     string `json:"noun"`    // "Pipelines" | "Checks"
  Account  string `json:"account"` // "none" | "connected" | "rejected"
}
type HostingAccount struct {
  Host hosting.Host `json:"host"`
  Connected bool `json:"connected"`
  Rejected bool `json:"rejected"`
  Name string `json:"name"`
  Email string `json:"email"`
  Source string `json:"source"`
  GHAvailable bool `json:"ghAvailable"`
}
func (a *App) HostingInfo(id string) (HostingInfo, error) // re-detects from ops.ListRemotes on every call
func (a *App) CommitStatuses(id string, hashes []string) (map[string]hosting.CommitStatus, error)
func (a *App) CommitChecks(id, hash string) (hosting.CommitStatus, error) // always asks the host; puts the aggregate in the cache
func (a *App) GetHostingAccounts() ([]HostingAccount, error) // [bitbucket, github]
func (a *App) SaveBitbucketAccount(email, token string) (HostingAccount, error) // verifies with User before storing
func (a *App) SaveGitHubAccount(source, token string) (HostingAccount, error)
func (a *App) RemoveHostingAccount(host string) error
func (a *App) hostingClient(h hosting.Host) hosting.Client // test seam: a.hosting.newClient
```

- **`CommitStatuses`** returns cached entries for what it has, and asks the client only for `missing`. On a `*RateLimitError` or a network error it returns what it has, with no error. It emits `hosting:limit` with `{host, until, known}` when a host becomes blocked, and `{host, until: null}` when the block clears.
- **Linked worktrees** resolve to their main repository through the existing `settingsKey`/parent mapping before the remotes are read.
- **Event names:** declare the constants `EventHostingLimit = "hosting:limit"` and `EventHostingStatus = "hosting:status"` (the latter is used in Task 8).

- [ ] **Step 1: Write the failing tests.**
  - **Cache:**
    - `TestCacheFinalNeverExpires` (the fake clock advances 24 h)
    - `TestCacheNoneExpiresAfter10Min` (9m59s → have; 10m01s → missing)
    - `TestCacheRunningAfter30s`
  - **App** (`newTestApp`, with a fake `hosting.Client` injected through `a.hosting.newClient`):
    - `TestHostingInfoUnsupported`: the test repo has no remote.
    - `TestHostingInfoBitbucket`: `ops.AddRemote` origin `git@bitbucket.org:acme/api.git` gives Supported, Noun "Pipelines" and Account "none".
    - **Review Focus 1**, `TestHostingInfoFollowsRemoteEdit`: `SetRemoteURL` to `https://github.com/o/r` makes the next `HostingInfo` return github/o/r with Noun "Checks".
    - `TestCommitStatusesUsesCache`: a second call with the same final hashes makes no client call.
    - `TestCommitStatusesRateLimitedReturnsPartial`
    - `TestSaveBitbucketAccountVerifiesFirst`: a fake `User` error means nothing is stored and the error is returned.
    - `TestSaveBitbucketAccountStoresName`: `GetHostingAccounts` shows Connected with Name.

- [ ] **Step 2: Run them.** `go test ./internal/hosting/ ./internal/app/ -run 'Cache|Hosting|Account'` → FAIL.

- [ ] **Step 3: Implement.**
  - `hostingState` holds the `accounts.Store`, the `hosting.Limits`, the `Cache` and `newClient`. The latter builds a `Doer` (with `Auth` and `OnReject` from the store) plus `bitbucket.New` or `github.New`.
  - Then run `~/go/bin/wails generate module`.
  - Write `docs/spec/14-hosting.md`: detection, accounts, statuses, cache and rate limits, in the style of the other spec files (behaviour, not implementation).

- [ ] **Step 4: Run the tests.**
  `go test ./internal/...` → PASS.
  `cd frontend && npm run check` → 0 errors.

- [ ] **Step 5: Commit.**
  Include the Go files, `frontend/wailsjs`, `docs/spec/14-hosting.md` and `docs/spec/README.md`.
  Message: `feat(app): hosting info, cached commit statuses and accounts bindings; spec 14`

---

### Task 7: The App's pull request methods and the checkout

**Files:**
- Modify: `internal/app/hosting.go`
- Modify: `internal/app/hosting_test.go`
- Regenerate: the bindings
- Modify: `docs/spec/14-hosting.md`, adding the PR data and checkout rules from §5

**Interfaces:**
- Consumes: Task 6's `hostingState`; the existing `App.FetchRemote(id, remote string) error` and `App.CheckoutRemote(id, remote, name string) (string, error)`.
- Produces:

```go
func (a *App) ListPullRequests(id, filter string) (PullRequestList, error)
type PullRequestList struct{ Items []hosting.PullRequest `json:"items"`; More bool `json:"more"` }
func (a *App) PullRequestCount(id string) (hosting.Count, error)
func (a *App) GetPullRequest(id string, number int) (hosting.PullRequestDetail, error)
func (a *App) GetPullRequestFiles(id string, number int) ([]hosting.PRFile, error)
func (a *App) CheckoutPullRequest(id string, number int) (string, error) // returns CheckoutRemote's result
```

- **`ListPullRequests`** passes `me` from `hosting.json` (`BitbucketID` or `GitHubLogin`). For Bitbucket it fills each `State` from the cache by `HeadHash` (`Cache.Get`), with no extra requests.
- **`CheckoutPullRequest`** does the following:
  - A fork PR gives the error `From a fork. Open it in the browser.`
  - Otherwise it calls `FetchRemote(id, info.Remote)`, then `CheckoutRemote(id, info.Remote, pr.Source)`. Errors from those calls pass through unchanged.
- No network call is made while the repository's write lock is held. The fetch and the checkout take it themselves.

- [ ] **Step 1: Write the failing tests.** Use the fake client, plus a local bare repo as the `origin` remote, with a URL rewritten through `url.<bare>.insteadOf` so it parses as bitbucket.
  - `TestListPullRequestsFillsStateFromCache`
  - `TestListPullRequestsNoAccount` → `hosting.ErrNoAccount`
  - `TestCheckoutPullRequestFork`
  - `TestCheckoutPullRequestSameRepo`: the bare repo has branch `feature/x`. After the call, `git rev-parse --abbrev-ref HEAD` is `feature/x`, and its upstream is `origin/feature/x`.
  - `TestCheckoutPullRequestDirtyTree`: returns CheckoutRemote's existing error.

- [ ] **Step 2: Run them.** `go test ./internal/app/ -run 'PullRequest'` → FAIL.

- [ ] **Step 3: Implement.** Then regenerate the bindings and update spec 14.

- [ ] **Step 4: Run the tests.** `go test ./internal/...` → PASS. `npm run check` → 0 errors.

- [ ] **Step 5: Commit.** `feat(app): pull request list, detail, files and checkout bindings; spec 14`

---

### Task 8: The push watcher

**Files:**
- Create: `internal/hosting/watch.go`
- Test: `internal/hosting/watch_test.go`
- Modify: `internal/app/hosting.go`, adding `watchPushed`
- Modify: `internal/app/remote.go`. The hooks go in `Push`, `PushBranch` and `PushAll`, after success.
- Modify: `internal/app/hosting_test.go`
- Modify: `docs/spec/14-hosting.md`, adding the push watch

**Interfaces:**
- Consumes: `App.CommitChecks`-style fetching through the client; `ops.BranchPushResult{Branch, Status}` with `ops.PushPushed`.
- Produces:

```go
type WatchEvent struct {
  RepoID string `json:"repoID"`; Branch string `json:"branch"`; Hash string `json:"hash"`
  State State `json:"state"`; Final bool `json:"final"`; Noun string `json:"noun"`
}
type Watcher struct {
  Interval, MaxAge, NoCheckAge time.Duration // 30s, 60m, 5m
  Fetch func(ctx context.Context, repoID, hash string) (CommitStatus, error)
  Emit  func(WatchEvent)
  Sleep func(ctx context.Context, d time.Duration) error // test seam
  Now   func() time.Time
}
func (w *Watcher) Watch(ctx context.Context, repoID, branch, hash, noun string) // blocking; run in a goroutine
```

- **`Emit`** fires on every state change. The final emission has `Final: true` and happens exactly once. Running out the clock (MaxAge or NoCheckAge) emits nothing final.
- **`watchPushed(id string, branches []string)`** runs only when `HostingInfo` is supported and connected. For each branch it resolves the tip hash (`git rev-parse refs/heads/<b>`) and starts a goroutine on `a.ctx`.
  - `Push` uses the current branch.
  - `PushBranch` uses `result.Branch` when its Status is `PushPushed`.
  - `PushAll` uses every result with `PushPushed`.
- **Emission** goes to `EventHostingStatus` through `emitUnlessShutdown`.
- **One watch per (repo, hash).** A second push of the same tip does not start another.

- [ ] **Step 1: Write the failing tests.** Use a fake `Sleep` that advances a fake clock.
  - `TestWatchStopsAtFinal`: running, running, failed gives a final event `failed`, emitted once.
  - `TestWatchGivesUpWithoutChecks`: `none` for 5 min means no final event and a return.
  - `TestWatchMaxAge`: `running` forever returns after 60 min with no final event.
  - `TestWatchEmitsChangesOnly`: none, running, running, success gives exactly 2 emissions.
  - App: `TestPushBranchStartsWatch`, with an injected watcher spy, a bare remote and a fake connected account. `TestPushDoesNotWatchUnsupported`.

- [ ] **Step 2: Run them.** `go test ./internal/hosting/ ./internal/app/ -run 'Watch'` → FAIL.

- [ ] **Step 3: Implement.** Then update spec 14.

- [ ] **Step 4: Run them.** `go test ./internal/...` → PASS.

- [ ] **Step 5: Commit.** `feat(hosting): watch a pushed commit's pipeline until it finishes`

---

### Task 9: The Accounts tab

**Files:**
- Modify: `frontend/src/lib/types.ts`, adding `HostingInfo`, `HostingAccount`, `CommitStatus`, `Check`, `HostState`, `PullRequest`, `PullRequestList`, `PullRequestDetail`, `Reviewer`, `Comment`, `PRCommit`, `PRFile`, `HostCount` and `WatchEvent`, mirroring the Go JSON.
- Modify: `frontend/src/lib/api.ts`, adding one-liners for every method from Tasks 6–7.
- Modify: `frontend/src/lib/settingsTabs.ts`, adding `'accounts'` with label "Accounts" and `ai: false`, placed after `general`.
- Modify: `frontend/src/lib/settingsTabs.test.ts`
- Create: `frontend/src/components/AccountsTab.svelte`
- Modify: `frontend/src/components/SettingsDialog.svelte`, adding `{:else if $tab==='accounts'}<AccountsTab/>`
- Modify: `docs/spec/07-conventions-and-constraints.md` (the Settings dialog's tab list and the Accounts tab) and `docs/spec/14-hosting.md` (the accounts UI)

**Interfaces:**
- Consumes: `api.getHostingAccounts`, `saveBitbucketAccount`, `saveGitHubAccount` and `removeHostingAccount`.
- Produces: `AccountsTab.svelte`, which takes no props.

**The tab's behaviour (§2):**
- **Bitbucket block:**
  - Email and API token fields (`type=password`), with Save and Remove.
  - A link to `https://id.atlassian.com/manage-profile/security/api-tokens`, opened with `BrowserOpenURL`.
  - The scopes text, with the names the implementer confirmed against Atlassian's docs.
  - "Connected as <name>" or "Bitbucket rejected the token".
- **GitHub block:**
  - The radio "Use GitHub CLI login", disabled with "gh not found" when `!ghAvailable`, and "Personal access token".
  - The token field and a link to `https://github.com/settings/personal-access-tokens`.
  - The same status lines as Bitbucket.
- Save errors appear under their block, and the fields keep what was typed.

- [ ] **Step 1: Write the failing test.** `settingsTabs.test.ts` expects the labels `['General','Accounts','Providers','AI models','Prompts']`, and `tabUsesAI('accounts') === false`.

- [ ] **Step 2: Run it.** `cd frontend && npx vitest run src/lib/settingsTabs.test.ts` → FAIL.

- [ ] **Step 3: Implement** the types, the api, the tab list, the component and the dialog branch. Update specs 07 and 14.

- [ ] **Step 4: Run the checks.** `npx vitest run` → PASS. `npm run check` → 0 errors.

- [ ] **Step 5: Commit.** `feat(ui): Accounts tab for Bitbucket and GitHub; spec 07, 14`

---

### Task 10: Statuses in the log and the commit details

**Files:**
- Create: `frontend/src/lib/hosting.ts`
- Test: `frontend/src/lib/hosting.test.ts`
- Create: `frontend/src/components/ChecksList.svelte`
- Modify: `frontend/src/components/LogList.svelte`
- Modify: `frontend/src/components/CommitDetails.svelte`
- Modify: `frontend/src/App.svelte`, which subscribes to `hosting:limit` and `hosting:status` and refreshes on focus, Refresh, fetch and push
- Modify: `docs/spec/02-log-and-history.md` and `docs/spec/14-hosting.md`

**Interfaces:**
- Consumes: `api.hostingInfo`, `api.commitStatuses`, `api.commitChecks`; the `repo:changed` event; the existing focus listener in `lib/actions.ts`.
- Produces, in `lib/hosting.ts`:

```ts
export const hostingInfo: Writable<Record<string, HostingInfo>>     // by repo ID
export const commitStatuses: Writable<Record<string, Record<string, CommitStatus>>> // repo ID → hash → status
export function loadHostingInfo(repoID: string): Promise<HostingInfo>
export function requestStatuses(repoID: string, hashes: string[]): void // debounced 500 ms, merges into commitStatuses[repoID]
export function refreshStatuses(repoID: string): void // drops non-final entries for the repo and re-requests the last visible set
export function startHostingPolling(): () => void      // every 30 s re-requests visible `running` hashes
export function statusTooltip(noun: string, st: CommitStatus, now: Date): string
export function stateIcon(s: HostState): { name: string; tone: 'ok' | 'bad' | 'busy' | 'muted' } | null
```

**`statusTooltip` copy:**
- one Bitbucket pipeline, success: "Pipeline #412 passed · 3 min"
- one running: "Pipeline running · started 2 min ago"
- several checks: "2 of 3 checks failed", "3 checks passed" or "1 of 2 checks running"

`ChecksList.svelte` takes the props `{ status: CommitStatus | null; noun: string; loading: boolean; error: string; onRetry: () => void }`. It draws one line per check: icon, name, state text, duration and an **Open** link. Links open through `BrowserOpenURL`, and only for `https:` URLs.

**LogList:**
- After each visible-range change, call `requestStatuses(repoId, visibleHashes + one screen margin)`.
- Draw the 12 px icon next to the short hash, with the space reserved and the row height unchanged. It appears only when `$hostingInfo[repoId]?.supported && account === 'connected'`.

**CommitDetails:**
- In `.info` after `.meta`, show `<ChecksList>` from `api.commitChecks(repoId, hash)`.
- Hide it when the result is `none` with no checks.

- [ ] **Step 1: Write the failing tests** in `hosting.test.ts`. Mock `api` with `vi.mock` and use fake timers.
  - `requestStatuses debounces`: three calls within 500 ms make one API call carrying the union of hashes.
  - **Review Focus 2**, `late answer for another repo is not mixed in`: request repo A, request repo B, resolve A last. `commitStatuses.B` has no A hashes, and A's results land only under A.
  - `polling re-asks only running`
  - `statusTooltip`: the five strings above.
  - `stateIcon(none) is null`

- [ ] **Step 2: Run them.** `npx vitest run src/lib/hosting.test.ts` → FAIL.

- [ ] **Step 3: Implement.** Update spec 02 (row icon, Pipelines/Checks section) and spec 14.

- [ ] **Step 4: Run the checks.** `npx vitest run` → PASS. `npm run check` → 0 errors.

- [ ] **Step 5: Commit.** `feat(ui): pipeline status in log rows and a Pipelines/Checks section in commit details; spec 02, 14`

---

### Task 11: Status bar and the Pipelines notifications

**Files:**
- Modify: `frontend/src/components/StatusBar.svelte`
- Modify: `frontend/src/lib/notifyRules.ts` and its test
- Modify: `frontend/src/lib/notify.ts`
- Modify: `frontend/src/lib/stores.ts`, adding `notifyPipelines = persisted('notifyPipelines', true, isBool)`
- Modify: `frontend/src/components/SettingsDialog.svelte`, adding the switch to the General notifications
- Modify: `internal/app/notify.go`, adding ``Hash string `json:"hash,omitempty"` `` to `Notification` and to the `notify:open` payload
- Modify: `docs/spec/11-notifications.md`, `docs/spec/13-status-bar.md` and `docs/spec/14-hosting.md`

**Interfaces:**
- Consumes: the `hosting:status` (`WatchEvent`) and `hosting:limit` events; `commitStatuses`, `hostingInfo` and `requestStatuses` from Task 10; `showCommitInLog(hash)` in stores.ts.
- Produces:
  - `NotifyCategory` gains `'pipelines'`.
  - `NotifyEvent` gains `hash?: string`.
  - `openTarget(repoID, target, hash?)` selects that commit when `hash` is set.
  - `export function pipelineBody(e: WatchEvent): string` in `notifyRules.ts`.

**Status bar, right side:**
- The selected repository's HEAD status: its icon plus "Pipeline passed", "Pipeline failed" or "Pipeline running" (Checks wording for GitHub). Hidden for `none`.
- Clicking it selects HEAD in the log.
- While a host is blocked, its rate-limit notice shows in the same spot instead.
- HEAD's hash comes from the log's head row, or `api.getLog` row `isHead`, whichever the component already has. HEAD's status is kept in `requestStatuses`.

**On `hosting:status` with `final: true`:**
- `notify({category: 'pipelines', repoID, body: pipelineBody(e), target: 'repo', hash})`
- Also `refreshStatuses(repoID)`.

- [ ] **Step 1: Write the failing tests.**
  - `notifyRules.test.ts`: `decide` honours `pipelines` like `remote`, with a context flag `pipelines`.
  - `pipelineBody`:
    - Bitbucket success gives "Pipeline passed on feature/x".
    - Checks wording with failed gives "Checks failed on feature/x".

- [ ] **Step 2: Run them.** `npx vitest run src/lib/notifyRules.test.ts` → FAIL.

- [ ] **Step 3: Implement.** Add `Hash` in Go, then run `go test ./internal/app/` and regenerate the bindings. Update specs 11, 13 and 14.

- [ ] **Step 4: Run the checks.** `go test ./internal/...`, `npx vitest run` and `npm run check` → all pass.

- [ ] **Step 5: Commit.** `feat(ui): branch pipeline in the status bar and Pipelines notifications; spec 11, 13, 14`

---

### Task 12: Sidebar row and Pull requests list view

**Files:**
- Create: `frontend/src/lib/prs.ts`
- Test: `frontend/src/lib/prs.test.ts`
- Create: `frontend/src/components/PullRequestsView.svelte`
- Modify: `frontend/src/components/RepoRefs.svelte`
- Modify: `frontend/src/lib/stores.ts`
  - `mainView` gains `'pulls'`.
  - Add `selectedPR = writable<Record<string, number>>({})` and `openPullRequests(repoID)`, which runs `selectRepo` and sets `mainView` to `'pulls'`.
- Modify: `frontend/src/App.svelte`, adding the `{:else if $mainView==='pulls'}` branch
- Modify: `docs/spec/01-repositories-and-sidebar.md` and `docs/spec/14-hosting.md`

**Interfaces:**
- Consumes: `api.listPullRequests`, `api.pullRequestCount`; `hostingInfo`/`loadHostingInfo` and `stateIcon` from Task 10; `renderMarkdown` from `lib/markdown.ts`.
- Produces, in `lib/prs.ts`:

```ts
export type PRFilter = 'open' | 'mine' | 'review'
export function emptyMessage(f: PRFilter, info: HostingInfo, err: string): { text: string; action?: 'accounts' }
export function hostLabel(h: string): 'Bitbucket' | 'GitHub'
export function commentTree(cs: Comment[]): { comment: Comment; depth: number }[] // parent before replies, chronological, depth capped at 4
export function safeHref(url: string): string | null // https only
export function renderUntrusted(md: string): string // renderMarkdown with links rewritten through safeHref
```

**Sidebar row:**
- "Pull requests" is the first row of `RepoRefs`, before the Branches section, and only when `$hostingInfo[repoId]?.supported`.
- It has an icon and a badge `Count.N` (or "100+" when `More`), asked for only for the selected repository: on selection, on focus and on Refresh. There is no badge without an account.
- Clicking it calls `openPullRequests`.

**List view (top part of `PullRequestsView`):**
- Filter tabs Open, Mine and Review requested, plus Refresh and Open in browser (`info.pullsURL`).
- Each row shows:
  - `#id` and title, with a Draft label when it applies
  - author
  - `source → target`
  - the status icon
  - ✓ approvals
  - the comment count
  - the relative update time
- "Showing 100. Open the rest in the browser" appears when `more` is true.
- The empty and error states come from `emptyMessage`; the `accounts` action opens Settings on the Accounts tab.
- The list refreshes when the view opens, on focus while it is shown, on Refresh and after a fetch (`repo:changed`).
- The bottom part is a placeholder for Task 13's `PullRequestDetail`, behind the same splitter pattern as LogView.

- [ ] **Step 1: Write the failing tests** in `prs.test.ts`.
  - `emptyMessage`:
    - open → "No open pull requests"
    - mine → "No open pull requests of yours"
    - review → "No open pull requests waiting for your review"
    - account none → "Connect your Bitbucket account to see pull requests" with action `accounts`
    - rejected → "Bitbucket rejected the token"
  - `commentTree` orders replies under their parent, with depth.
  - **Review Focus 5:**
    - `renderUntrusted('<img src=x onerror=alert(1)>')` contains no `<img`.
    - `renderUntrusted('[x](javascript:alert(1))')` contains no `javascript:`.
    - `safeHref('http://x')` and `safeHref('javascript:x')` are null; `safeHref('https://bitbucket.org/a')` returns it unchanged.

- [ ] **Step 2: Run them.** `npx vitest run src/lib/prs.test.ts` → FAIL.

- [ ] **Step 3: Implement.** Update specs 01 and 14.

- [ ] **Step 4: Run the checks.** `npx vitest run` → PASS. `npm run check` → 0 errors.

- [ ] **Step 5: Commit.** `feat(ui): Pull requests row in the sidebar and the PR list view; spec 01, 14`

---

### Task 13: Pull request detail

**Files:**
- Create: `frontend/src/components/DiffText.svelte`, extracted from CommitDetails' diff lines with props `{ text: string }`. CommitDetails switches to it, and StashView may too.
- Create: `frontend/src/components/PullRequestDetail.svelte`
- Modify: `frontend/src/components/PullRequestsView.svelte`
- Modify: `frontend/src/components/CommitDetails.svelte`
- Modify: `docs/spec/14-hosting.md`

**Interfaces:**
- Consumes:
  - `api.getPullRequest`, `api.getPullRequestFiles`, `api.checkoutPullRequest`
  - `ChecksList` (Task 10); `FileList` with `sections`, `selected`, `onSelect` and `rowKey`; `commentTree`, `renderUntrusted` and `safeHref` (Task 12)
  - `jumpTo` and `showCommitInLog` from stores; `beginTask` from `lib/tasks.ts` for the checkout task
  - `selectedPR`
- Produces: `PullRequestDetail.svelte`, with props `{ repoId: string; number: number; info: HostingInfo }`.

**Layout (§5):**
- **Left column `.info`, in order:**
  1. Header: title, `#id`, Open/Draft, author, `source → target`, created and updated dates.
  2. **Checkout branch** (disabled with "From a fork. Open it in the browser." when `fork`) and **Open in browser**.
  3. Description via `renderUntrusted`.
  4. Reviewers, each with its verdict.
  5. `ChecksList`.
  6. Commits. Clicking one sets `jumpTo` when the hash is in the local log (ask `api.getDetails` and treat an error as missing), otherwise shows "Not fetched".
  7. Comments from `commentTree`: an indent per depth, a `path:line` header when `path` is set, the verdict label when set, and the "Open the rest in the browser" note when `commentsMore`.
- **Right column:**
  - `FileList`, with one section "Changed files" listing the files' status and path.
  - `DiffText` with the selected file's `patch`, or "No diff available here. Open in browser" when `noDiff`.
- **Checkout:**
  - It runs as a status-bar task labelled "Checkout <source>".
  - On success the app switches `mainView` to `'log'` with the branch shown; on error the task shows git's message.

- [ ] **Step 1: Write the failing test.**
  `prs.test.ts`: `fileSections(files)` returns `[{title: 'Changed files', files: [{path, status, oldPath}]}]`, with statuses mapped to FileList's glyph statuses: added → A, removed → D, modified → M, renamed → R. Add `fileSections` to `lib/prs.ts`.

- [ ] **Step 2: Run it.** `npx vitest run src/lib/prs.test.ts` → FAIL.

- [ ] **Step 3: Implement** the components and the extraction. Update spec 14.

- [ ] **Step 4: Run the checks.** `npx vitest run` → PASS. `npm run check` → 0 errors. `go test ./internal/...` → PASS.

- [ ] **Step 5: Commit.** `feat(ui): pull request detail with checks, commits, comments, files and checkout; spec 14`

---

### Task 14: Whole-feature verification

**Files:** none new. Fixes go in the files they belong to.

- [ ] **Step 1: Run the full suites.**
  - `go test ./...` → PASS
  - `go vet ./...` → clean
  - `cd frontend && npx vitest run && npm run check` → PASS, with 0 errors
- [ ] **Step 2: Build.** `make build` → `build/bin/CommitTree.app` is produced.
- [ ] **Step 3: Check against the spec.** Walk the design doc §1–§8 and `docs/spec/14-hosting.md`, and confirm each behaviour has code and a spec line. List any gap and fix it.
- [ ] **Step 4: Prepare the owner's manual pass.** It needs a real Bitbucket token and `gh`. Write the checklist into the SDD ledger:
  - Accounts: save and remove for both hosts.
  - Icons on a work repo's log, and the tooltip.
  - The commit details' Pipelines section.
  - The PR list, all three filters.
  - The PR detail: diff, comments, Checkout branch.
  - A push from the app firing the notification.
  - A wrong token showing "rejected".
  - A repository with no supported remote showing nothing new.
- [ ] **Step 5: Commit** any fixes with messages scoped to what they fix.
