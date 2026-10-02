# Background fetch + "new commits on the remote" Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** CommitTree fetches every repository in the background on a timer and notifies when the checked-out branch's upstream gained commits HEAD lacks — never prompting, never blocking a user write, never repeating a notice.

**Architecture:** Go `ops.AutoFetch` fetches with a no-prompt environment and reports what the fetch itself brought; `App.AutoFetch` runs it under the repository write lock and registers a cancel so a user write cancels it instead of getting `ErrBusy`. The frontend schedules rounds (`lib/autoFetch.ts`), turns results into actions with a pure function, and notifies through phase 1's `notify` with a new `remote` category.

**Tech Stack:** Go 1.x + Wails v2.16, Svelte + TypeScript, vitest, `go test`.

**Spec:** `docs/superpowers/specs/2026-10-02-auto-fetch-design.md`

## Global Constraints

- Interval choices: Off / 5 / 15 / 30 / 60 min (stored as minutes, 0 = Off), default 15. First round 30 s after start.
- Background fetch: `git fetch --all --prune`, timeout 60 s, origin Auto, env `GIT_ASKPASS=false`, `SSH_ASKPASS=false`, `SSH_ASKPASS_REQUIRE=never`, `GCM_INTERACTIVE=never` (plus the existing `GIT_TERMINAL_PROMPT=0`). Never touch `core.sshCommand` or credential helpers.
- Auth errors cross to the frontend as text starting `auto-fetch auth:`.
- Notification body: `"3 new commits on origin/main"` / `"1 new commit on origin/main"`; title = repo name; target `repo`.
- Settings copy: select **Fetch in the background** (`Off`, `Every 5 min`, `Every 15 min`, `Every 30 min`, `Every 60 min`); checkbox **New commits on the remote**; hint when Off: `Turn on Fetch in the background first`.
- Behaviour changes update `docs/spec/` in the same commit. Merges `--no-ff`. Commits without `Co-Authored-By`. In the worktree run git as `/usr/bin/git`.
- After every task that changes the app: `npm run build` in `frontend`, regenerate bindings if Go API changed, `make dev` (node 22) to recompile and reopen.

## Review Focus

1. A user clicks Pull/Push/Commit while a background fetch holds the lock → the user's operation runs (after the fetch is cancelled), never "another operation is already running". Pinned in Task 2.
2. A remote that asks for a username/password or SSH passphrase → the background fetch fails fast with an `auth` error, no dialog, no hang. Pinned in Task 1 (HTTP 401 server test).
3. Commits brought by a manual Fetch, then a background fetch with nothing new → no notice. Pinned in Task 1 ("nothing new" after a manual fetch).
4. A user operation is running (toolbar busy) when a round finishes on the selected repo with `refsChanged` → no concurrent refresh; the operation's own refresh covers it. Pinned in Task 4 (`outcome` with `busy`).
5. Changing the interval while a round is running → no second overlapping round. Pinned in Task 4 (scheduler test).

---

### Task 1: `ops.AutoFetch` — fetch, no-prompt env, new-commit count, auth classification

**Files:**
- Create: `internal/ops/autofetch.go`
- Test: `internal/ops/autofetch_test.go`

**Interfaces:**
- Produces:
  - `ops.AutoFetchTimeout = 60 * time.Second`
  - `ops.NoPromptEnv []string`
  - `ops.ErrAutoFetchAuth` (`errors.New("auto-fetch auth")`)
  - `type ops.AutoFetchResult struct { Skipped bool; Branch, Upstream string; NewCommits int; RefsChanged bool }` (json tags `skipped`, `branch`, `upstream`, `newCommits`, `refsChanged`)
  - `func ops.AutoFetch(ctx context.Context, dir string) (AutoFetchResult, error)` — auth failures wrap `ErrAutoFetchAuth` (message `auto-fetch auth: <git error>`)
  - `func ops.IsAuthError(err error) bool`

- [ ] **Step 1: Write the failing tests** — `internal/ops/autofetch_test.go`:

```go
package ops_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"git-ui/internal/gitcmd"
	"git-ui/internal/ops"
	"git-ui/internal/testrepo"
)

// tracked returns a repo whose main tracks origin/main on a bare remote, and
// a clone of that remote to push new commits from.
func tracked(t *testing.T) (r, other *testrepo.Repo) {
	t.Helper()
	r = testrepo.New(t)
	r.Commit("base")
	bare := testrepo.NewBareFrom(t, r)
	r.Git("remote", "add", "origin", bare)
	r.Git("fetch", "-q", "origin")
	r.Git("branch", "-q", "--set-upstream-to=origin/main", "main")
	return r, testrepo.Clone(t, bare)
}

func TestAutoFetchCountsCommitsTheFetchBrought(t *testing.T) {
	r, other := tracked(t)
	other.Commit("one")
	other.Commit("two")
	other.Git("push", "-q", "origin", "main")

	res, err := ops.AutoFetch(context.Background(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	want := ops.AutoFetchResult{Branch: "main", Upstream: "origin/main", NewCommits: 2, RefsChanged: true}
	if res != want {
		t.Errorf("res = %+v, want %+v", res, want)
	}
}

func TestAutoFetchAfterAManualFetchReportsNothing(t *testing.T) {
	r, other := tracked(t)
	other.Commit("one")
	other.Git("push", "-q", "origin", "main")
	r.Git("fetch", "-q", "origin") // the user's manual Fetch

	res, err := ops.AutoFetch(context.Background(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if res.NewCommits != 0 || res.RefsChanged {
		t.Errorf("res = %+v, want nothing new", res)
	}
}

func TestAutoFetchIgnoresCommitsHEADAlreadyHas(t *testing.T) {
	r, _ := tracked(t)
	r.Commit("mine")
	bare := r.Git("remote", "get-url", "origin")
	// Update the remote's main without moving r's origin/main, as a push
	// from another clone of the same work would.
	r.Git("push", "-q", bare, "main:main")

	res, err := ops.AutoFetch(context.Background(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if res.NewCommits != 0 || !res.RefsChanged {
		t.Errorf("res = %+v, want 0 new commits but refs changed", res)
	}
}

func TestAutoFetchDetachedOrNoUpstream(t *testing.T) {
	r, other := tracked(t)
	other.Commit("one")
	other.Git("push", "-q", "origin", "main")
	r.Git("switch", "-q", "--detach", "HEAD")

	res, err := ops.AutoFetch(context.Background(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if res.Branch != "" || res.Upstream != "" || res.NewCommits != 0 || !res.RefsChanged {
		t.Errorf("res = %+v", res)
	}
}

func TestAutoFetchSkipsARepositoryWithNoRemote(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	res, err := ops.AutoFetch(context.Background(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Skipped {
		t.Errorf("res = %+v, want Skipped", res)
	}
}

func TestAutoFetchNeverPromptsForCredentials(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", `Basic realm="x"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	r := testrepo.New(t)
	r.Commit("base")
	r.Git("remote", "add", "origin", srv.URL+"/repo.git")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	start := time.Now()
	_, err := ops.AutoFetch(ctx, r.Dir)
	if !errors.Is(err, ops.ErrAutoFetchAuth) {
		t.Fatalf("err = %v, want ErrAutoFetchAuth", err)
	}
	if d := time.Since(start); d > 15*time.Second {
		t.Errorf("took %v: something waited for input", d)
	}
}

func TestIsAuthError(t *testing.T) {
	for _, stderr := range []string{
		"fatal: Authentication failed for 'https://x/'",
		"fatal: could not read Username for 'https://x': terminal prompts disabled",
		"fatal: could not read Password for 'https://x'",
		"git@x: Permission denied (publickey).",
		"Host key verification failed.",
	} {
		if !ops.IsAuthError(&gitcmd.Error{Stderr: stderr}) {
			t.Errorf("IsAuthError(%q) = false", stderr)
		}
	}
	if ops.IsAuthError(&gitcmd.Error{Stderr: "fatal: unable to access 'https://x/': Could not resolve host: x"}) {
		t.Error("a network error counted as auth")
	}
	if ops.IsAuthError(errors.New("plain")) {
		t.Error("a non-git error counted as auth")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ops/ -run 'AutoFetch|IsAuthError'`
Expected: FAIL — `undefined: ops.AutoFetch`.

- [ ] **Step 3: Implement** — `internal/ops/autofetch.go`:

```go
package ops

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"git-ui/internal/gitcmd"
)

// AutoFetchTimeout bounds a background fetch: long enough for a slow
// remote, short enough that a hung one does not hold the write lock long.
const AutoFetchTimeout = 60 * time.Second

// NoPromptEnv makes every way git could ask the user fail instead:
// askpass programs (git's and ssh's) and Git Credential Manager's UI.
// Helpers that answer silently — osxkeychain, ssh-agent — still work.
var NoPromptEnv = []string{
	"GIT_ASKPASS=false",
	"SSH_ASKPASS=false",
	"SSH_ASKPASS_REQUIRE=never",
	"GCM_INTERACTIVE=never",
}

// ErrAutoFetchAuth wraps a background fetch that failed for want of
// credentials; the frontend matches its text, "auto-fetch auth".
var ErrAutoFetchAuth = errors.New("auto-fetch auth")

// AutoFetchResult is what one background fetch brought.
type AutoFetchResult struct {
	Skipped     bool   `json:"skipped"`
	Branch      string `json:"branch"`
	Upstream    string `json:"upstream"`
	NewCommits  int    `json:"newCommits"`
	RefsChanged bool   `json:"refsChanged"`
}

var authMarkers = []string{
	"authentication failed",
	"could not read username",
	"could not read password",
	"permission denied (publickey",
	"host key verification failed",
	"terminal prompts disabled",
}

// IsAuthError reports whether err is a git command that failed for want
// of credentials, as opposed to the network, a timeout or a cancel.
func IsAuthError(err error) bool {
	var gerr *gitcmd.Error
	if !errors.As(err, &gerr) {
		return false
	}
	s := strings.ToLower(gerr.Stderr)
	for _, m := range authMarkers {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}

// AutoFetch fetches every remote without ever prompting and reports the
// commits the checked-out branch's upstream gained in this fetch that HEAD
// lacks. Only this fetch's change counts, so commits a manual Fetch already
// brought are never reported again.
func AutoFetch(ctx context.Context, dir string) (AutoFetchResult, error) {
	var res AutoFetchResult
	remotes, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "remote")
	if err != nil {
		return res, err
	}
	if strings.TrimSpace(remotes) == "" {
		res.Skipped = true
		return res, nil
	}
	if b, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "symbolic-ref", "-q", "--short", "HEAD"); err == nil {
		res.Branch = strings.TrimSpace(b)
	}
	if res.Branch != "" {
		if u, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}"); err == nil {
			res.Upstream = strings.TrimSpace(u)
		}
	}
	oldTip := upstreamTip(ctx, dir, res.Upstream)
	before, err := remoteRefs(ctx, dir)
	if err != nil {
		return res, err
	}
	if _, err := gitcmd.RunEnv(ctx, dir, AutoFetchTimeout, NoPromptEnv, "fetch", "--all", "--prune"); err != nil {
		if IsAuthError(err) {
			return res, fmt.Errorf("%w: %v", ErrAutoFetchAuth, err)
		}
		return res, err
	}
	after, err := remoteRefs(ctx, dir)
	if err != nil {
		return res, err
	}
	res.RefsChanged = before != after
	newTip := upstreamTip(ctx, dir, res.Upstream)
	if oldTip != "" && newTip != "" && oldTip != newTip {
		n, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-list", "--count", newTip, "^"+oldTip, "^HEAD")
		if err == nil {
			res.NewCommits, _ = strconv.Atoi(strings.TrimSpace(n))
		}
	}
	return res, nil
}

// upstreamTip is the upstream's commit, "" when there is none or it is gone.
func upstreamTip(ctx context.Context, dir, upstream string) string {
	if upstream == "" {
		return ""
	}
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--verify", "-q", "@{upstream}^{commit}")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// remoteRefs is every remote-tracking ref with its commit, one per line.
func remoteRefs(ctx context.Context, dir string) (string, error) {
	return gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "for-each-ref", "--format=%(refname) %(objectname)", "refs/remotes")
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ops/ -run 'AutoFetch|IsAuthError' -v`
Expected: PASS (all 7). If `TestAutoFetchNeverPromptsForCredentials` fails because git reports the 401 differently on this machine, print `err` and add the observed line to `authMarkers` only if it is an authentication message.

- [ ] **Step 5: Commit**

```bash
/usr/bin/git add internal/ops/autofetch.go internal/ops/autofetch_test.go
/usr/bin/git commit -m "feat(ops): background fetch that never prompts and counts what it brought"
```

---

### Task 2: `App.AutoFetch` + user writes cancel a background fetch

**Files:**
- Create: `internal/app/autofetch.go`, `internal/app/autofetch_test.go`
- Modify: `internal/app/app.go` (App struct ~line 79: new field; `write` ~489; `writeAll` ~508)
- Modify: `frontend/wailsjs/go/app/App.{js,d.ts}`, `frontend/wailsjs/go/models.ts` (regenerated)
- Modify: `docs/spec/05-remote-and-stash.md`, `docs/spec/09-command-log.md`

**Interfaces:**
- Consumes: `ops.AutoFetch`, `ops.AutoFetchResult` (Task 1); `cmdlog.WithOrigin`, `cmdlog.OriginAuto`.
- Produces: `func (a *App) AutoFetch(id string) (ops.AutoFetchResult, error)` (Wails-bound → `Go.AutoFetch(id)`); `a.autoFetches sync.Map` (repo ID → `context.CancelFunc`); `a.writeMutex(id) *sync.Mutex`; `a.lockWrite(id) (*sync.Mutex, error)`.

- [ ] **Step 1: Write the failing tests** — `internal/app/autofetch_test.go`:

```go
package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"git-ui/internal/cmdlog"
	"git-ui/internal/testrepo"
)

func TestAutoFetchThroughTheAppLayerIsAuto(t *testing.T) {
	a, r, id := newPlainApp(t)
	bare := testrepo.NewBareFrom(t, r)
	r.Git("remote", "add", "origin", bare)
	r.Git("push", "-q", "-u", "origin", "main")
	other := testrepo.Clone(t, bare)
	other.Commit("new")
	other.Git("push", "-q", "origin", "main")

	res, err := a.AutoFetch(id)
	if err != nil {
		t.Fatal(err)
	}
	if res.NewCommits != 1 || res.Upstream != "origin/main" {
		t.Errorf("res = %+v", res)
	}
	view, _ := a.CommandLog(id)
	found := false
	for _, e := range view.Entries {
		if len(e.Args) > 0 && e.Args[0] == "fetch" {
			found = true
			if e.Origin != cmdlog.OriginAuto {
				t.Errorf("fetch origin = %q, want auto", e.Origin)
			}
		}
	}
	if !found {
		t.Error("the background fetch is not in the command log")
	}
	if _, ok := a.autoFetches.Load(id); ok {
		t.Error("the cancel is still registered after the fetch")
	}
}

func TestAutoFetchSkipsWhileAUserWriteRuns(t *testing.T) {
	a, r, id := newPlainApp(t)
	r.Git("remote", "add", "origin", testrepo.NewBareFrom(t, r))
	mu := a.writeMutex(id)
	mu.Lock()
	defer mu.Unlock()

	res, err := a.AutoFetch(id)
	if err != nil || !res.Skipped {
		t.Errorf("res = %+v, err = %v; want Skipped", res, err)
	}
}

func TestUserWriteCancelsABackgroundFetch(t *testing.T) {
	a, _, id := newPlainApp(t)
	mu := a.writeMutex(id)
	mu.Lock() // the background fetch holds the lock …
	cancelled := make(chan struct{})
	a.autoFetches.Store(id, context.CancelFunc(func() {
		close(cancelled)
		go func() { // … and lets go once git has stopped.
			time.Sleep(20 * time.Millisecond)
			a.autoFetches.Delete(id)
			mu.Unlock()
		}()
	}))

	ran := false
	if err := a.write(id, func(context.Context, string) error { ran = true; return nil }); err != nil {
		t.Fatalf("write = %v, want it to run", err)
	}
	select {
	case <-cancelled:
	default:
		t.Error("the background fetch was not cancelled")
	}
	if !ran {
		t.Error("the user write did not run")
	}
}

func TestUserWriteStillRefusedBehindAnotherUserWrite(t *testing.T) {
	a, _, id := newPlainApp(t)
	mu := a.writeMutex(id)
	mu.Lock()
	defer mu.Unlock()
	if err := a.write(id, func(context.Context, string) error { return nil }); !errors.Is(err, ErrBusy) {
		t.Errorf("write = %v, want ErrBusy", err)
	}
	if err := a.writeAll([]string{id}, func(context.Context) error { return nil }); !errors.Is(err, ErrBusy) {
		t.Errorf("writeAll = %v, want ErrBusy", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/app/ -run 'AutoFetch|BackgroundFetch|AnotherUserWrite'`
Expected: FAIL — `a.AutoFetch undefined`, `a.writeMutex undefined`, `a.autoFetches undefined`.

- [ ] **Step 3: Implement**

In `internal/app/app.go`, add to the `App` struct right after `writes sync.Map // repo ID → *sync.Mutex`:

```go
	// autoFetches holds the cancel of the background fetch holding a
	// repository's write lock (repo ID → context.CancelFunc), so a user
	// write stops it instead of being refused.
	autoFetches sync.Map
```

Replace `write` and `writeAll` (and add the two helpers above them):

```go
func (a *App) writeMutex(id string) *sync.Mutex {
	m, _ := a.writes.LoadOrStore(id, &sync.Mutex{})
	return m.(*sync.Mutex)
}

// lockWrite takes id's write lock for a user write. A background fetch
// holding it is cancelled and waited for (git stops as on Ctrl+C, within
// gitcmd's WaitDelay); any other holder means ErrBusy.
func (a *App) lockWrite(id string) (*sync.Mutex, error) {
	mu := a.writeMutex(id)
	if mu.TryLock() {
		return mu, nil
	}
	c, ok := a.autoFetches.Load(id)
	if !ok {
		return nil, ErrBusy
	}
	c.(context.CancelFunc)()
	mu.Lock()
	return mu, nil
}

func (a *App) write(id string, fn func(ctx context.Context, dir string) error) error {
	dir, err := a.dir(id)
	if err != nil {
		return err
	}
	mu, err := a.lockWrite(id)
	if err != nil {
		return err
	}
	defer mu.Unlock()
	return fn(a.ctx, dir)
}

// writeAll runs fn under every id's write lock at once (lockWrite on each,
// in order), for an operation such as a submodule write that must hold both
// the parent repository's lock and the lock of each submodule it touches.
// Any id busy with another user write fails the whole call with ErrBusy and
// releases whatever locks it had already acquired, so a failed call never
// leaves a lock held.
func (a *App) writeAll(ids []string, fn func(ctx context.Context) error) error {
	var held []*sync.Mutex
	defer func() {
		for _, mu := range held {
			mu.Unlock()
		}
	}()
	for _, id := range ids {
		mu, err := a.lockWrite(id)
		if err != nil {
			return err
		}
		held = append(held, mu)
	}
	return fn(a.ctx)
}
```

Keep the existing doc comment above `write` if there is one. Check `internal/app/worktree_remove.go:52`'s comment ("both with TryLock (never blocking)") and amend it to say a background fetch is cancelled rather than refused.

Create `internal/app/autofetch.go`:

```go
package app

import (
	"context"

	"git-ui/internal/cmdlog"
	"git-ui/internal/ops"
)

// AutoFetch is one background fetch of repository id
// (docs/spec/05-remote-and-stash.md). It never waits: when another write
// holds the repository it is skipped. While it runs, a user write cancels
// it (see lockWrite). Its commands are logged as Auto.
func (a *App) AutoFetch(id string) (ops.AutoFetchResult, error) {
	dir, err := a.dir(id)
	if err != nil {
		return ops.AutoFetchResult{}, err
	}
	mu := a.writeMutex(id)
	if !mu.TryLock() {
		return ops.AutoFetchResult{Skipped: true}, nil
	}
	ctx, cancel := context.WithCancel(cmdlog.WithOrigin(a.ctx, cmdlog.OriginAuto))
	a.autoFetches.Store(id, cancel)
	defer func() {
		a.autoFetches.Delete(id)
		cancel()
		mu.Unlock()
	}()
	return ops.AutoFetch(ctx, dir)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/app/ ./internal/ops/`
Expected: PASS, including every existing test (the lock refactor touches all writes).

- [ ] **Step 5: Regenerate bindings**

```bash
cd frontend && npm ci --prefer-offline && npm run build && cd .. && ~/go/bin/wails generate module
/usr/bin/git diff --stat frontend/wailsjs
```
Expected: `App.js`/`App.d.ts` gain `AutoFetch`, `models.ts` gains `ops.AutoFetchResult`. (Skip `npm ci` when `frontend/node_modules` exists. Use node 22 as the Makefile's `NODE` does.)

- [ ] **Step 6: Update the behaviour spec**

`docs/spec/05-remote-and-stash.md`:
- Add a section **Background fetch** after the Fetch description:

```markdown
### Background fetch

When Settings → General → **Fetch in the background** is not Off (Every 5,
15 — the default —, 30 or 60 min), CommitTree runs `git fetch --all --prune`
for every repository in the sidebar that is not missing, one at a time:
first 30 s after the app starts, then every interval after the previous
round ends. A round is skipped while the computer is offline; a repository
with no remote, or with another write running, is skipped.

A background fetch never asks for anything: askpass programs and Git
Credential Manager's dialogs are turned off for it, so a remote that needs a
password or a passphrase fails instead (helpers that answer on their own,
such as the macOS keychain or ssh-agent, still work). Such an authentication
failure stops background fetches of that repository until a manual Fetch or
Pull of it succeeds or the app restarts. No failure is shown or notified;
the Commands panel has it. It times out after 60 s, never shows the busy
label, and when it changes the selected repository's remote branches, the
log and the ahead/behind badges refresh.
```

- Amend rule 2 of the rules list (currently "Only one write operation runs per repository at a time; any of Fetch, Pull, Push, or a stash action refuses rather than interleaving with another already running for the same repository.") to:

```markdown
2. Only one write operation runs per repository at a time; any of Fetch,
   Pull, Push, or a stash action refuses rather than interleaving with
   another already running for the same repository — except a background
   fetch, which is cancelled (as Cancel in the Commands panel would) so the
   user's operation runs instead. A background fetch itself never waits: it
   is skipped while another write runs.
```

- Amend the sentence near line 79 ("Both also refuse while any other write operation for the repository is in progress — the application allows only one write at a time per repository.") by appending: ` A background fetch in progress is cancelled instead (see "Background fetch").`

`docs/spec/09-command-log.md`: in the Origin list, after the Auto bullet, add: `Background fetches (see 05-remote-and-stash.md) are Auto too — the fetch and the reads around it; one cancelled because the user started a write shows as cancelled.`

- [ ] **Step 7: Commit**

```bash
/usr/bin/git add internal/app/app.go internal/app/autofetch.go internal/app/autofetch_test.go internal/app/worktree_remove.go frontend/wailsjs docs/spec/05-remote-and-stash.md docs/spec/09-command-log.md
/usr/bin/git commit -m "feat(app): AutoFetch under the write lock; a user write cancels a background fetch"
```

---

### Task 3: `remote` notification category + settings stores

**Files:**
- Modify: `frontend/src/lib/notifyRules.ts`, `frontend/src/lib/notifyRules.test.ts`
- Modify: `frontend/src/lib/stores.ts` (~line 49-53)
- Modify: `frontend/src/lib/notify.ts` (`notify`'s context)
- Modify: `frontend/src/lib/types.ts` (after `AheadBehind`, ~line 285)
- Modify: `frontend/src/lib/api.ts`
- Test: `frontend/src/lib/notify.test.ts`, `frontend/src/lib/stores.test.ts`

**Interfaces:**
- Consumes: `Go.AutoFetch` (Task 2).
- Produces:
  - `NotifyCategory = 'done' | 'ai' | 'problem' | 'remote'`; `NotifyContext.remote: boolean`
  - `remoteBody(count: number, upstream: string): string`
  - stores `notifyRemote: Writable<boolean>` (key `notifyRemote`, default true), `autoFetchMinutes: Writable<number>` (key `autoFetchMinutes`, default 15), `AUTO_FETCH_CHOICES = [0, 5, 15, 30, 60] as const`
  - `interface AutoFetchResult { skipped: boolean; branch: string; upstream: string; newCommits: number; refsChanged: boolean }` in `types.ts`
  - `api.autoFetch(id: string): Promise<AutoFetchResult>`

- [ ] **Step 1: Write the failing tests**

Append to `frontend/src/lib/notifyRules.test.ts` (and add `remote: true` to the `ctx` helper defaults; import `remoteBody`):

```ts
describe('remote', () => {
  it('follows its own switch and the usual focus rules', () => {
    const e = ev({ category: 'remote', repoID: 'b' })
    expect(decide(e, ctx({ remote: false }))).toBe('none')
    expect(decide(e, ctx())).toBe('system')
    expect(decide(e, ctx({ focused: true }))).toBe('toast')
    expect(decide(ev({ category: 'remote', repoID: 'a' }), ctx({ focused: true }))).toBe('none')
  })

  it('pluralises the body', () => {
    expect(remoteBody(1, 'origin/main')).toBe('1 new commit on origin/main')
    expect(remoteBody(3, 'origin/main')).toBe('3 new commits on origin/main')
  })
})
```

Append to `frontend/src/lib/stores.test.ts` (match that file's existing import style for stores):

```ts
describe('auto fetch settings', () => {
  it('defaults to every 15 minutes with the remote notice on', () => {
    expect(get(autoFetchMinutes)).toBe(15)
    expect(get(notifyRemote)).toBe(true)
  })

  it('rejects a stored interval that is not one of the choices', async () => {
    localStorage.setItem('autoFetchMinutes', '7')
    vi.resetModules()
    const fresh = await import('./stores')
    expect(get(fresh.autoFetchMinutes)).toBe(15)
    localStorage.removeItem('autoFetchMinutes')
  })
})
```

In `frontend/src/lib/notify.test.ts`, add:

```ts
it('honours the New commits on the remote switch', async () => {
  windowFocused.set(false)
  notifyRemote.set(false)
  expect(await notify({ category: 'remote', repoID: 'b', body: '1 new commit on origin/main', target: 'repo' })).toBe('none')
  notifyRemote.set(true)
  expect(await notify({ category: 'remote', repoID: 'b', body: '1 new commit on origin/main', target: 'repo' })).toBe('system')
})
```
(import `notifyRemote` from `./stores`; set `notifyRemote.set(true)` in `beforeEach`.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd frontend && npx vitest run src/lib/notifyRules.test.ts src/lib/notify.test.ts src/lib/stores.test.ts`
Expected: FAIL — `remoteBody` / `notifyRemote` / `autoFetchMinutes` not exported.

- [ ] **Step 3: Implement**

`notifyRules.ts`:

```ts
export type NotifyCategory = 'done' | 'ai' | 'problem' | 'remote'
```
add `remote: boolean` to `NotifyContext` after `problem`, and:

```ts
export const remoteBody = (count: number, upstream: string) =>
  `${count} new commit${count === 1 ? '' : 's'} on ${upstream}`
```
(`decide` already indexes `c[e.category]`, so no change there.)

`stores.ts`, after `notifyProblem`:

```ts
export const notifyRemote = persisted('notifyRemote', true, isBool)

/** Settings → General → Fetch in the background, in minutes; 0 is Off
 *  (docs/spec/05-remote-and-stash.md). */
export const AUTO_FETCH_CHOICES = [0, 5, 15, 30, 60] as const
const isAutoFetchMinutes = (v: unknown): v is number => AUTO_FETCH_CHOICES.includes(v as (typeof AUTO_FETCH_CHOICES)[number])
export const autoFetchMinutes = persisted<number>('autoFetchMinutes', 15, isAutoFetchMinutes)
```

`notify.ts`: import `notifyRemote` and add `remote: get(notifyRemote),` to the `decide` context.

`types.ts` after `AheadBehind`:

```ts
/** One background fetch (Go ops.AutoFetchResult). */
export interface AutoFetchResult {
  skipped: boolean
  branch: string
  upstream: string
  newCommits: number
  refsChanged: boolean
}
```

`api.ts`: add `AutoFetchResult` to the types import and, after `fetch:`:

```ts
  autoFetch: (id: string) => call<AutoFetchResult>(Go.AutoFetch(id)),
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd frontend && npx vitest run && npx svelte-check --threshold error` (use the project's `npm run check` if it exists)
Expected: PASS; no type errors (every `NotifyContext` literal in tests/code now needs `remote`).

- [ ] **Step 5: Commit**

```bash
/usr/bin/git add frontend/src/lib
/usr/bin/git commit -m "feat(notify): remote category, interval and remote-notice settings, autoFetch API"
```

---

### Task 4: `lib/autoFetch.ts` — scheduler, result handling, pause on auth

**Files:**
- Create: `frontend/src/lib/autoFetch.ts`, `frontend/src/lib/autoFetch.test.ts`
- Modify: `frontend/src/lib/actions.ts` (`fetchRemote` ~line 761, `pull` ~763)
- Modify: `frontend/src/App.svelte` (`onMount`, ~line 83)

**Interfaces:**
- Consumes: `api.autoFetch`, `AutoFetchResult`, `autoFetchMinutes`, `remoteBody`, `notify`, `refreshRepo`, `busy`, `repos`, `selectedRepoId`.
- Produces:
  - `type AutoFetchAction = { pause: boolean; refresh: boolean; event?: NotifyEvent }`
  - `outcome(id: string, r: { result?: AutoFetchResult; error?: unknown }, s: { selectedId: string; busy: boolean }): AutoFetchAction` (pure)
  - `FIRST_ROUND_MS = 30_000`
  - `pausedRepos: Set<string>`; `resumeAutoFetch(id: string): void`
  - `runRound(deps: AutoFetchDeps): Promise<void>`
  - `startAutoFetch(deps?: Partial<AutoFetchDeps>): () => void`
  - `interface AutoFetchDeps { repos: () => Repo[]; fetch: (id: string) => Promise<AutoFetchResult>; online: () => boolean; selectedId: () => string; busy: () => boolean; refresh: () => Promise<void>; notify: (e: NotifyEvent) => void }`

- [ ] **Step 1: Write the failing tests** — `frontend/src/lib/autoFetch.test.ts`:

```ts
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { FIRST_ROUND_MS, outcome, pausedRepos, resumeAutoFetch, runRound, startAutoFetch, type AutoFetchDeps } from './autoFetch'
import { autoFetchMinutes } from './stores'
import type { AutoFetchResult, Repo } from './types'

const res = (over: Partial<AutoFetchResult> = {}): AutoFetchResult => ({
  skipped: false, branch: 'main', upstream: 'origin/main', newCommits: 0, refsChanged: false, ...over,
})
const repo = (id: string, missing = false) => ({ id, name: id, path: `/x/${id}`, missing, branch: 'main' }) as Repo
const sel = { selectedId: 'a', busy: false }

describe('outcome', () => {
  it('pauses on an auth error and ignores any other error', () => {
    expect(outcome('a', { error: new Error('auto-fetch auth: git fetch: could not read Username') }, sel)).toEqual({ pause: true, refresh: false })
    expect(outcome('a', { error: new Error('git fetch: timed out') }, sel)).toEqual({ pause: false, refresh: false })
    expect(outcome('a', { error: 'auto-fetch auth: x' }, sel)).toEqual({ pause: true, refresh: false })
  })

  it('refreshes the selected repository only when refs changed and nothing is busy', () => {
    expect(outcome('a', { result: res({ refsChanged: true }) }, sel).refresh).toBe(true)
    expect(outcome('b', { result: res({ refsChanged: true }) }, sel).refresh).toBe(false)
    expect(outcome('a', { result: res({ refsChanged: true }) }, { selectedId: 'a', busy: true }).refresh).toBe(false)
    expect(outcome('a', { result: res() }, sel).refresh).toBe(false)
  })

  it('turns new commits into a remote event', () => {
    expect(outcome('b', { result: res({ newCommits: 2, refsChanged: true }) }, sel).event).toEqual({
      category: 'remote', repoID: 'b', target: 'repo', body: '2 new commits on origin/main',
    })
    expect(outcome('b', { result: res({ skipped: true }) }, sel)).toEqual({ pause: false, refresh: false })
  })
})

function deps(over: Partial<AutoFetchDeps> = {}): AutoFetchDeps {
  return {
    repos: () => [repo('a'), repo('gone', true), repo('b')],
    fetch: vi.fn().mockResolvedValue(res()),
    online: () => true,
    selectedId: () => 'a',
    busy: () => false,
    refresh: vi.fn().mockResolvedValue(undefined),
    notify: vi.fn(),
    ...over,
  }
}

describe('runRound', () => {
  beforeEach(() => pausedRepos.clear())

  it('fetches present, unpaused repositories one at a time, in order', async () => {
    const order: string[] = []
    let inFlight = 0
    const fetch = vi.fn(async (id: string) => {
      inFlight++
      expect(inFlight).toBe(1)
      order.push(id)
      await Promise.resolve()
      inFlight--
      return res()
    })
    await runRound(deps({ fetch }))
    expect(order).toEqual(['a', 'b'])
  })

  it('skips the whole round offline', async () => {
    const d = deps({ online: () => false })
    await runRound(d)
    expect(d.fetch).not.toHaveBeenCalled()
  })

  it('pauses a repository on an auth failure until resumed', async () => {
    const d = deps({ fetch: vi.fn(async (id: string) => { if (id === 'a') throw new Error('auto-fetch auth: no'); return res() }) })
    await runRound(d)
    expect(pausedRepos.has('a')).toBe(true)
    await runRound(d)
    expect(vi.mocked(d.fetch).mock.calls.filter(([id]) => id === 'a')).toHaveLength(1)
    resumeAutoFetch('a')
    expect(pausedRepos.has('a')).toBe(false)
  })

  it('notifies and refreshes from the results', async () => {
    const d = deps({ fetch: vi.fn().mockResolvedValue(res({ newCommits: 1, refsChanged: true })) })
    await runRound(d)
    expect(d.notify).toHaveBeenCalledTimes(2)
    expect(d.refresh).toHaveBeenCalledTimes(1) // only the selected 'a'
  })
})

describe('startAutoFetch', () => {
  beforeEach(() => { vi.useFakeTimers(); pausedRepos.clear(); autoFetchMinutes.set(15) })
  afterEach(() => vi.useRealTimers())

  it('runs the first round 30 s after start, then every interval after a round ends', async () => {
    const d = deps()
    const stop = startAutoFetch(d)
    await vi.advanceTimersByTimeAsync(FIRST_ROUND_MS - 1)
    expect(d.fetch).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(1)
    expect(d.fetch).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(15 * 60_000)
    expect(d.fetch).toHaveBeenCalledTimes(4)
    stop()
  })

  it('stops when Off and never overlaps a running round when the interval changes', async () => {
    let release!: () => void
    const fetch = vi.fn(() => new Promise<AutoFetchResult>((r) => { release = () => r(res()) }))
    const d = deps({ repos: () => [repo('a')], fetch })
    const stop = startAutoFetch(d)
    await vi.advanceTimersByTimeAsync(FIRST_ROUND_MS)
    expect(fetch).toHaveBeenCalledTimes(1) // round in flight
    autoFetchMinutes.set(5)
    await vi.advanceTimersByTimeAsync(5 * 60_000)
    expect(fetch).toHaveBeenCalledTimes(1) // still the same round
    release()
    await vi.advanceTimersByTimeAsync(5 * 60_000)
    expect(fetch).toHaveBeenCalledTimes(2)
    release()
    autoFetchMinutes.set(0)
    await vi.advanceTimersByTimeAsync(60 * 60_000)
    expect(fetch).toHaveBeenCalledTimes(2)
    stop()
  })
})
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd frontend && npx vitest run src/lib/autoFetch.test.ts`
Expected: FAIL — cannot resolve `./autoFetch`.

- [ ] **Step 3: Implement** — `frontend/src/lib/autoFetch.ts`:

```ts
import { get } from 'svelte/store'
import { api } from './api'
import { notify } from './notify'
import { remoteBody, type NotifyEvent } from './notifyRules'
import { autoFetchMinutes, busy, refreshRepo, repos, selectedRepoId } from './stores'
import type { AutoFetchResult, Repo } from './types'
import { errorMessage } from './ui'

/** Background fetch (docs/spec/05-remote-and-stash.md): rounds over every
 *  repository, one at a time; what a result means is `outcome`'s, pure. */

export const FIRST_ROUND_MS = 30_000

export interface AutoFetchAction {
  pause: boolean
  refresh: boolean
  event?: NotifyEvent
}

const isAuth = (e: unknown) => (typeof e === 'string' ? e : errorMessage(e)).includes('auto-fetch auth')

export function outcome(
  id: string,
  r: { result?: AutoFetchResult; error?: unknown },
  s: { selectedId: string; busy: boolean },
): AutoFetchAction {
  if (r.error !== undefined) return { pause: isAuth(r.error), refresh: false }
  const res = r.result
  if (!res || res.skipped) return { pause: false, refresh: false }
  const action: AutoFetchAction = { pause: false, refresh: res.refsChanged && id === s.selectedId && !s.busy }
  if (res.newCommits > 0) action.event = { category: 'remote', repoID: id, target: 'repo', body: remoteBody(res.newCommits, res.upstream) }
  return action
}

/** Repositories whose background fetch failed for want of credentials;
 *  a manual Fetch or Pull that succeeds takes one out. In memory only. */
export const pausedRepos = new Set<string>()
export const resumeAutoFetch = (id: string) => void pausedRepos.delete(id)

export interface AutoFetchDeps {
  repos: () => Repo[]
  fetch: (id: string) => Promise<AutoFetchResult>
  online: () => boolean
  selectedId: () => string
  busy: () => boolean
  refresh: () => Promise<void>
  notify: (e: NotifyEvent) => void
}

const defaultDeps: AutoFetchDeps = {
  repos: () => get(repos),
  fetch: (id) => api.autoFetch(id),
  online: () => navigator.onLine,
  selectedId: () => get(selectedRepoId),
  busy: () => get(busy) !== '',
  refresh: refreshRepo,
  notify: (e) => void notify(e),
}

export async function runRound(d: AutoFetchDeps): Promise<void> {
  if (!d.online()) return
  for (const repo of d.repos()) {
    if (repo.missing || pausedRepos.has(repo.id)) continue
    let r: { result?: AutoFetchResult; error?: unknown }
    try {
      r = { result: await d.fetch(repo.id) }
    } catch (error) {
      r = { error }
    }
    const a = outcome(repo.id, r, { selectedId: d.selectedId(), busy: d.busy() })
    if (a.pause) pausedRepos.add(repo.id)
    if (a.event) d.notify(a.event)
    if (a.refresh) await d.refresh()
  }
}

/** startAutoFetch runs rounds: the first FIRST_ROUND_MS after start, each
 *  next one the chosen interval after the previous ends — so rounds never
 *  overlap, even when the interval changes mid-round. Off stops it. */
export function startAutoFetch(over: Partial<AutoFetchDeps> = {}): () => void {
  const d = { ...defaultDeps, ...over }
  let timer: ReturnType<typeof setTimeout> | undefined
  let running = false
  let first = true
  const schedule = () => {
    clearTimeout(timer)
    timer = undefined
    const minutes = get(autoFetchMinutes)
    if (minutes <= 0 || running) return
    timer = setTimeout(round, first ? FIRST_ROUND_MS : minutes * 60_000)
  }
  const round = async () => {
    first = false
    running = true
    try {
      await runRound(d)
    } finally {
      running = false
      schedule()
    }
  }
  const unsubscribe = autoFetchMinutes.subscribe(schedule)
  return () => {
    unsubscribe()
    clearTimeout(timer)
  }
}
```

Note on the "first" flag: turning the interval Off and back On before the first round still waits 30 s; after that, a change waits the new interval.

`actions.ts`: import `resumeAutoFetch` from `./autoFetch` and change:

```ts
export async function fetchRemote(id: string) {
  if (await runOp(id, 'fetch', 'Fetching…', () => api.fetch(id))) resumeAutoFetch(id)
}
```
and in `pull`, right after the `const result = await track(...)` line, add `resumeAutoFetch(id)`.

Check for an import cycle: `autoFetch.ts` imports `notify` and `stores`, `actions.ts` imports `autoFetch` — none of those import `actions.ts`. Verify with `grep -n "from './actions'" frontend/src/lib/{notify,stores,autoFetch}.ts` (expect no output).

`App.svelte`: import `startAutoFetch` from `./lib/autoFetch`; in `onMount` after `const stopNotifications = startNotifications()` add `const stopAutoFetch = startAutoFetch()`, and call `stopAutoFetch()` in the same returned cleanup that calls `stopNotifications()`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd frontend && npx vitest run && npm run build`
Expected: all PASS; the build succeeds. If `actions.test.ts` mocks `./api` without `autoFetch`, it does not matter (actions only call `resumeAutoFetch`); if it fails on the new import, add `vi.mock('./autoFetch', () => ({ resumeAutoFetch: vi.fn() }))` there.

- [ ] **Step 5: Commit**

```bash
/usr/bin/git add frontend/src
/usr/bin/git commit -m "feat(fetch): background fetch rounds, pause on auth failure, remote notice"
```

---

### Task 5: Settings UI + notifications spec, rebuild and reopen

**Files:**
- Modify: `frontend/src/components/SettingsDialog.svelte` (Notifications section, ~line 264-283; import line ~10)
- Modify: `docs/spec/11-notifications.md`

**Interfaces:**
- Consumes: `autoFetchMinutes`, `AUTO_FETCH_CHOICES`, `notifyRemote` (Task 3).

- [ ] **Step 1: Settings markup**

Add `autoFetchMinutes, AUTO_FETCH_CHOICES, notifyRemote` to the stores import. Insert before `<section>` that holds `<h4>Notifications</h4>`:

```svelte
            <section>
              <h4>Fetch</h4>
              <label class="row">
                <span>Fetch in the background</span>
                <select bind:value={$autoFetchMinutes}>
                  {#each AUTO_FETCH_CHOICES as m}
                    <option value={m}>{m === 0 ? 'Off' : `Every ${m} min`}</option>
                  {/each}
                </select>
              </label>
            </section>
```

Match the markup of the existing pull-strategy `<select>` (~line 289) for classes and layout if it differs from `label.row`. After the Problems checkbox add:

```svelte
              <label class="row check sub">
                <input type="checkbox" bind:checked={$notifyRemote} disabled={!$notifyEnabled || $autoFetchMinutes === 0} />
                <span>New commits on the remote</span>
              </label>
              {#if $autoFetchMinutes === 0}<p class="hint sub">Turn on Fetch in the background first</p>{/if}
```
If `p.hint.sub` has no indent style, reuse whatever indents `label.row.check.sub`.

- [ ] **Step 2: Update `docs/spec/11-notifications.md`**

- Events table, new row after the Problems rows:
  `| New commits on the remote | A background fetch brought commits to the checked-out branch's upstream that it does not have — "3 new commits on origin/main" | always (while Fetch in the background is on) |`
- After "One user operation is one event…" paragraph add: `Only commits that arrived in that background fetch count: commits a manual Fetch already brought, or that the branch already contains, are never announced, and a restart does not repeat a notice. Other branches, new remote branches and background fetch failures never notify (see 05-remote-and-stash.md).`
- Settings paragraph: add **New commits on the remote** as the fourth category, noting it is disabled while Fetch in the background is Off, and mention **Fetch in the background** (Off / Every 5, 15, 30, 60 min; default 15) sits above Notifications.

- [ ] **Step 3: Verify**

```bash
cd frontend && npx vitest run && npm run build && cd .. && go test ./... && go vet ./...
```
Expected: all pass.

- [ ] **Step 4: Rebuild and reopen**

Stop any running `wails dev`, then from the worktree root run `make dev` (node 22) in the background. Check in the app: Settings → General shows **Fetch in the background** = Every 15 min; switching to Off disables **New commits on the remote** and shows the hint; with a repo whose remote gets a new commit (push from another clone, e.g. in `/tmp/git-ui-demo`), within 30 s of start the Commands panel shows an Auto `git fetch --all --prune` and, with that repo not selected, a toast "<repo>: 1 new commit on origin/main" with View.

- [ ] **Step 5: Commit**

```bash
/usr/bin/git add frontend/src/components/SettingsDialog.svelte docs/spec/11-notifications.md
/usr/bin/git commit -m "feat(settings): Fetch in the background and the New commits on the remote notice"
```

---

### After the tasks

- Final whole-branch review (most capable model) against the spec and Review Focus.
- Merge into main with `git merge --no-ff` ("Merge claude/notifications-phase-2-auto-fetch-6730ac: background fetch and new-commits notice"), update memory `git-ui-notifications.md`.
