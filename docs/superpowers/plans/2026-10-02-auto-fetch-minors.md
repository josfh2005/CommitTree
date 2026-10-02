# Background fetch minors Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the deferred phase 2 minors of the background fetch: no ErrBusy windows, Off stops a round, no tty prompts, coalesced refreshes, per-remote pause kept in Go and cleared by any successful fetch or pull, and a hermetic 401 test.

**Architecture:** A new `writeLock` type in `internal/app` replaces the per-repository `sync.Mutex` + `autoFetches` map. `ops.AutoFetch` fetches remote by remote and reports auth failures; `App` keeps the paused remotes and clears them from the command recorder. The frontend loses its pause set, gains a stop check per repository, and coalesces `refreshRepo`.

**Tech Stack:** Go 1.2x (Wails v2.16), Svelte + TypeScript, vitest.

**Spec:** `docs/superpowers/specs/2026-10-02-auto-fetch-minors-design.md`

## Global Constraints

- Work in the worktree; run git as `/usr/bin/git` (the rtk hook clashes with the worktree protection).
- Commit messages: conventional style, **no `Co-Authored-By` line**.
- A behaviour change updates `docs/spec/*` in the same commit.
- Go tests: `go test ./...` (use `-race` for `internal/app`). Frontend: `source ~/.nvm/nvm.sh && nvm use 22` then `cd frontend && npx vitest run && npm run check` (node 14 is the default and fails).
- Writes never queue: a second user write while one runs or waits is `ErrBusy`.
- Background fetch env stays `ops.NoPromptEnv`, timeout `ops.AutoFetchTimeout` (60 s) per git call.
- Pause state is in memory only (lost on restart), keyed by `cmdlog.RepoKey(dir)`.

## Review Focus

1. A user write that arrives exactly as the background fetch finishes must run, not get ErrBusy, and no third write may slip in before it — Task 1 pins the handoff.
2. A repository whose only remote is paused must not run `git fetch` at all (no Commands-panel noise every round) — Task 3 pins `Skipped`.
3. A `git fetch .` (git-flow's local branch update) or a failed user fetch must not unpause — Task 4 pins both.
4. Turning Off while a round is mid-way must not fetch the next repository — Task 5 pins it.
5. A refresh requested while one runs must still see the state after the caller's change (it must start after the call, not reuse the running one) — Task 6 pins it.

Known, accepted edge: a toolbar Pull that ends in a conflict exits non-zero, so it does not unpause (its fetch did succeed). The next successful Fetch or Pull unpauses.

---

### Task 1: Write lock with handoff

**Files:**
- Create: `internal/app/writelock.go`
- Create: `internal/app/writelock_test.go`
- Modify: `internal/app/app.go` (field `autoFetches` and its comment ~lines 80-83; `writeMutex`, `lockWrite`, `write`, `writeAll` ~lines 493-550)
- Modify: `internal/app/autofetch.go`
- Modify: `internal/app/autofetch_test.go` (tests that used `writeMutex`/`autoFetches`)
- Modify: `docs/spec/07-conventions-and-constraints.md` ("One write lock per repository")

**Interfaces:**
- Produces: `func newWriteLock() *writeLock`; `(*writeLock).lockUser() error`; `(*writeLock).tryLockAuto(cancel func()) bool`; `(*writeLock).unlock()`; `func (a *App) lockFor(id string) *writeLock`; `func (a *App) lockWrite(id string) (unlock func(), err error)`.

- [ ] **Step 1: Write the failing unit tests** (`internal/app/writelock_test.go`)

```go
package app

import (
	"errors"
	"testing"
	"time"
)

func (l *writeLock) state() (holder, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.holder, l.waiting
}

func TestWriteLockUserThenUserIsBusy(t *testing.T) {
	l := newWriteLock()
	if err := l.lockUser(); err != nil {
		t.Fatal(err)
	}
	if err := l.lockUser(); !errors.Is(err, ErrBusy) {
		t.Errorf("second lockUser = %v, want ErrBusy", err)
	}
	if l.tryLockAuto(func() {}) {
		t.Error("tryLockAuto took a lock a user write holds")
	}
	l.unlock()
	if h, _ := l.state(); h != holderFree {
		t.Errorf("holder = %v after unlock, want free", h)
	}
}

func TestWriteLockUserCancelsAutoAndGetsItNext(t *testing.T) {
	l := newWriteLock()
	cancelled := make(chan struct{})
	if !l.tryLockAuto(func() { close(cancelled) }) {
		t.Fatal("tryLockAuto on a free lock = false")
	}
	got := make(chan error, 1)
	go func() { got <- l.lockUser() }()
	<-cancelled
	// While the first user write waits, a second one is refused …
	if err := l.lockUser(); !errors.Is(err, ErrBusy) {
		t.Errorf("second lockUser while one waits = %v, want ErrBusy", err)
	}
	l.unlock() // the background fetch stops
	// … and so is one arriving right after the fetch let go: it is handed over.
	if err := l.lockUser(); !errors.Is(err, ErrBusy) {
		t.Errorf("lockUser right after the handoff = %v, want ErrBusy", err)
	}
	select {
	case err := <-got:
		if err != nil {
			t.Fatalf("waiting lockUser = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the waiting user write never got the lock")
	}
	if h, w := l.state(); h != holderUser || w {
		t.Errorf("state = %v waiting=%v, want user, not waiting", h, w)
	}
	l.unlock()
	if !l.tryLockAuto(func() {}) {
		t.Error("the lock is not free after the user write")
	}
}

func TestWriteLockAutoAfterAutoIsRefused(t *testing.T) {
	l := newWriteLock()
	if !l.tryLockAuto(func() {}) || l.tryLockAuto(func() {}) {
		t.Error("want the first tryLockAuto to succeed and the second to fail")
	}
}
```

Note: if the handoff assertion races (the waiter may already have taken the lock, holder `user`), `lockUser` still returns ErrBusy — the assertion holds either way.

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/app -run TestWriteLock -race`
Expected: FAIL to compile (`newWriteLock` undefined).

- [ ] **Step 3: Implement `internal/app/writelock.go`**

```go
package app

import "sync"

// holder is who holds a repository's write lock.
type holder int

const (
	holderFree holder = iota
	holderUser
	holderAuto
	// holderHandoff: a background fetch let go while a user write was
	// waiting for it; the lock is that write's, no one else's.
	holderHandoff
)

// writeLock is one repository's write lock (docs/spec/07, "One write lock
// per repository"). A user write never queues behind another user write
// (ErrBusy); one that finds a background fetch cancels it and gets the
// lock next. Every change of holder happens under mu, so there is no
// moment where the lock is held but its background fetch can't be found.
type writeLock struct {
	mu      sync.Mutex
	cond    *sync.Cond
	holder  holder
	cancel  func()
	waiting bool
}

func newWriteLock() *writeLock {
	l := &writeLock{}
	l.cond = sync.NewCond(&l.mu)
	return l
}

// lockUser takes the lock for a user write: at once when free; after
// cancelling and waiting for a background fetch that holds it, when no
// other user write is already waiting for it; ErrBusy otherwise.
func (l *writeLock) lockUser() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	switch {
	case l.holder == holderFree:
		l.holder = holderUser
		return nil
	case l.holder == holderAuto && !l.waiting:
		l.waiting = true
		l.cancel()
		for l.holder != holderHandoff {
			l.cond.Wait()
		}
		l.waiting = false
		l.holder = holderUser
		return nil
	}
	return ErrBusy
}

// tryLockAuto takes a free lock for a background fetch; cancel is what a
// user write calls to stop it. False when anything holds the lock.
func (l *writeLock) tryLockAuto(cancel func()) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.holder != holderFree {
		return false
	}
	l.holder, l.cancel = holderAuto, cancel
	return true
}

// unlock releases the lock, straight to the user write waiting for this
// background fetch if there is one.
func (l *writeLock) unlock() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.holder == holderAuto && l.waiting {
		l.holder = holderHandoff
	} else {
		l.holder = holderFree
	}
	l.cancel = nil
	l.cond.Broadcast()
}
```

- [ ] **Step 4: Run the unit tests**

Run: `go test ./internal/app -run TestWriteLock -race`
Expected: PASS.

- [ ] **Step 5: Rewire `App`** (`internal/app/app.go`)

Delete the `autoFetches sync.Map` field and its comment. Keep `writes sync.Map` (its values become `*writeLock`). Replace `writeMutex` and `lockWrite`, and update `write`/`writeAll`:

```go
// lockFor is id's write lock, made on first use.
func (a *App) lockFor(id string) *writeLock {
	l, _ := a.writes.LoadOrStore(id, newWriteLock())
	return l.(*writeLock)
}

// lockWrite takes id's write lock for a user write (see writeLock) and
// returns its unlock.
func (a *App) lockWrite(id string) (func(), error) {
	l := a.lockFor(id)
	if err := l.lockUser(); err != nil {
		return nil, err
	}
	return l.unlock, nil
}

func (a *App) write(id string, fn func(ctx context.Context, dir string) error) error {
	dir, err := a.dir(id)
	if err != nil {
		return err
	}
	unlock, err := a.lockWrite(id)
	if err != nil {
		return err
	}
	defer unlock()
	return fn(a.ctx, dir)
}
```

In `writeAll`: `var held []func()`, the deferred loop calls `unlock()` for each, and the loop appends the `unlock` that `lockWrite` returned. Keep its doc comment.

Note: `LoadOrStore(id, newWriteLock())` allocates a lock each call even when one exists; that is fine (cheap), and matches the old `&sync.Mutex{}` pattern.

- [ ] **Step 6: Rewire `App.AutoFetch`** (`internal/app/autofetch.go`)

```go
func (a *App) AutoFetch(id string) (ops.AutoFetchResult, error) {
	dir, err := a.dir(id)
	if err != nil {
		return ops.AutoFetchResult{}, err
	}
	// Cancelled with gitcmd.ErrCancelled as the cause, so the Commands
	// panel shows the stopped fetch as cancelled, not failed.
	ctx, cancel := context.WithCancelCause(cmdlog.WithOrigin(a.ctx, cmdlog.OriginAuto))
	defer cancel(nil)
	l := a.lockFor(id)
	if !l.tryLockAuto(func() { cancel(gitcmd.ErrCancelled) }) {
		return ops.AutoFetchResult{Skipped: true}, nil
	}
	defer l.unlock()
	return ops.AutoFetch(ctx, dir)
}
```

Update its doc comment: "While it runs, a user write cancels it (see writeLock)."

- [ ] **Step 7: Port the app tests** (`internal/app/autofetch_test.go`)

- `TestAutoFetchThroughTheAppLayerIsAuto`: replace the `a.autoFetches.Load` check with
  ```go
  if h, _ := a.lockFor(id).state(); h != holderFree {
      t.Errorf("lock holder = %v after the fetch, want free", h)
  }
  ```
- `TestAutoFetchSkipsWhileAUserWriteRuns`: `l := a.lockFor(id); if err := l.lockUser(); err != nil { t.Fatal(err) }; defer l.unlock()`.
- `TestUserWriteCancelsABackgroundFetch`:
  ```go
  a, _, id := newPlainApp(t)
  l := a.lockFor(id)
  cancelled := make(chan struct{})
  l.tryLockAuto(func() {
      close(cancelled)
      go func() { // … and lets go once git has stopped.
          time.Sleep(20 * time.Millisecond)
          l.unlock()
      }()
  })
  ```
  (rest unchanged).
- `TestUserWriteStillRefusedBehindAnotherUserWrite`: take the lock with `l.lockUser()` and `defer l.unlock()`.
- `TestOnlyOneUserWriteWaitsForABackgroundFetch`:
  ```go
  a, _, id := newPlainApp(t)
  l := a.lockFor(id)
  l.tryLockAuto(func() {}) // a background fetch that takes a while to stop
  first := make(chan error, 1)
  go func() { first <- a.write(id, func(context.Context, string) error { return nil }) }()
  deadline := time.Now().Add(2 * time.Second)
  for {
      if _, w := l.state(); w {
          break
      }
      if time.Now().After(deadline) {
          l.unlock()
          t.Fatal("the first write did not start waiting for the background fetch")
      }
      time.Sleep(time.Millisecond)
  }
  second := make(chan error, 1)
  go func() { second <- a.write(id, func(context.Context, string) error { return nil }) }()
  select {
  case err := <-second:
      if !errors.Is(err, ErrBusy) {
          t.Errorf("second write = %v, want ErrBusy", err)
      }
  case <-time.After(2 * time.Second):
      t.Error("the second write waited instead of being refused")
  }
  l.unlock()
  if err := <-first; err != nil {
      t.Errorf("first write = %v", err)
  }
  ```
- `TestUserWriteCancelsARealBackgroundFetchAsCancelled`: unchanged.

Then `grep -rn "writeMutex\|autoFetches" internal` must print nothing.

- [ ] **Step 8: Docs** — in `docs/spec/07-conventions-and-constraints.md`, after the paragraph ending "Read operations are never blocked by the lock.", add:

```markdown
A background fetch (see Remote and stash) takes the same lock, but only
when it is free, and gives way: a user write that finds it holding the lock
cancels it and gets the lock as soon as git has stopped, before any other
write. A second write arriving meanwhile is busy, as usual.
```

- [ ] **Step 9: Run the package tests**

Run: `go test ./internal/app -race`
Expected: PASS.

- [ ] **Step 10: Commit**

```bash
/usr/bin/git add internal/app/writelock.go internal/app/writelock_test.go internal/app/app.go internal/app/autofetch.go internal/app/autofetch_test.go docs/spec/07-conventions-and-constraints.md
/usr/bin/git commit -m "fix(app): write lock hands a cancelled background fetch over to the user write" -m "One writeLock per repository records who holds it under its own mutex, so a user write never finds the lock held with no background fetch to cancel, and nothing slips in between the fetch letting go and the waiting write."
```

---

### Task 2: git without a controlling terminal

**Files:**
- Modify: `internal/gitcmd/proc_unix.go`
- Create: `internal/gitcmd/proc_unix_test.go`

**Interfaces:** none new (`startInGroup`, `interrupt` keep their signatures).

- [ ] **Step 1: Write the failing test** (`internal/gitcmd/proc_unix_test.go`)

```go
//go:build !windows

package gitcmd_test

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"git-ui/internal/gitcmd"
)

// TestSessionHelper is not a test: git runs it through an alias to print
// the session it lives in.
func TestSessionHelper(t *testing.T) {
	if os.Getenv("GITCMD_SESSION_HELPER") != "1" {
		t.Skip("helper")
	}
	sid, _ := syscall.Getsid(0)
	fmt.Print(sid)
	os.Exit(0)
}

// git runs in a session of its own, so neither it nor ssh can open the
// terminal the app was started from to ask for a passphrase.
func TestGitRunsInASessionOfItsOwn(t *testing.T) {
	alias := "alias.sid=!" + os.Args[0] + " -test.run=^TestSessionHelper$"
	out, err := gitcmd.RunEnv(context.Background(), t.TempDir(), 30*time.Second,
		[]string{"GITCMD_SESSION_HELPER=1"}, "-c", alias, "sid")
	if err != nil {
		t.Fatal(err)
	}
	child, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		t.Fatalf("helper printed %q", out)
	}
	own, _ := syscall.Getsid(0)
	if child == own {
		t.Errorf("git shares the test's session %d", own)
	}
}
```

- [ ] **Step 2: Run it to see it fail**

Run: `go test ./internal/gitcmd -run TestGitRunsInASessionOfItsOwn -v`
Expected: FAIL, "git shares the test's session".

- [ ] **Step 3: Implement** — in `proc_unix.go`:

```go
// startInGroup starts git in a session of its own: a new process group, so
// interrupt reaches the hooks and helpers it runs too, as Ctrl+C in a
// terminal does, and no controlling terminal, so neither git nor ssh can
// ask for a passphrase or a host key on the terminal the app was started
// from (make dev); they fail instead, as in the bundled app.
func startInGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
```

`interrupt` is unchanged: a new session's process group id is git's pid.

- [ ] **Step 4: Run the gitcmd tests (cancel tests rely on the group)**

Run: `go test ./internal/gitcmd -race`
Expected: PASS (including `cancel_test.go`).

- [ ] **Step 5: Commit**

```bash
/usr/bin/git add internal/gitcmd/proc_unix.go internal/gitcmd/proc_unix_test.go
/usr/bin/git commit -m "fix(gitcmd): run git without a controlling terminal" -m "Setsid instead of Setpgid: under make dev, ssh could ask for a passphrase on the terminal the app started from; now it fails as in the bundled app. The new session is still git's own process group, so interrupt is unchanged."
```

---

### Task 3: Fetch remote by remote

**Files:**
- Modify: `internal/ops/autofetch.go`
- Modify: `internal/ops/autofetch_test.go`
- Modify: `internal/app/autofetch.go` (call site only: pass `nil` for now)

**Interfaces:**
- Produces: `func AutoFetch(ctx context.Context, dir string, paused func(remote string) bool) (AutoFetchResult, error)`; `AutoFetchResult.AuthFailed []string` (json `authFailed`). `ErrAutoFetchAuth` is removed.

- [ ] **Step 1: Update the existing tests and add the new ones** (`internal/ops/autofetch_test.go`)

Every existing `ops.AutoFetch(ctx, r.Dir)` call becomes `ops.AutoFetch(ctx, r.Dir, nil)`. `TestAutoFetchCountsCommitsTheFetchBrought` compares with `reflect.DeepEqual(res, want)` (the struct now has a slice; add `"reflect"` to the imports). Replace `TestAutoFetchNeverPromptsForCredentials` with:

```go
// lockedServer is a remote that always asks for credentials.
func lockedServer(t *testing.T) string {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", `Basic realm="x"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/repo.git"
}

// hermetic keeps the developer's credential helpers and URL rewrites out.
func hermetic(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

func TestAutoFetchNeverPromptsAndKeepsFetchingTheOtherRemotes(t *testing.T) {
	hermetic(t)
	r, other := tracked(t)
	r.Git("remote", "add", "locked", lockedServer(t))
	other.Commit("one")
	other.Git("push", "-q", "origin", "main")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	start := time.Now()
	res, err := ops.AutoFetch(ctx, r.Dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 15*time.Second {
		t.Errorf("took %v: something waited for input", d)
	}
	if !reflect.DeepEqual(res.AuthFailed, []string{"locked"}) {
		t.Errorf("AuthFailed = %v, want [locked]", res.AuthFailed)
	}
	if res.NewCommits != 1 {
		t.Errorf("NewCommits = %d, want 1 from origin", res.NewCommits)
	}
}

func TestAutoFetchSkipsPausedRemotes(t *testing.T) {
	r, other := tracked(t)
	other.Commit("one")
	other.Git("push", "-q", "origin", "main")
	res, err := ops.AutoFetch(context.Background(), r.Dir, func(remote string) bool { return remote == "origin" })
	if err != nil || !res.Skipped || res.NewCommits != 0 {
		t.Errorf("res = %+v, err = %v; want Skipped with nothing fetched", res, err)
	}
}
```

Add `"os"` and `"reflect"` imports; drop `"errors"` if nothing else uses it (`TestIsAuthError` uses `gitcmd`, not `errors` — check).

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/ops -run AutoFetch`
Expected: FAIL to compile (too many arguments / `AuthFailed` undefined).

- [ ] **Step 3: Implement** (`internal/ops/autofetch.go`)

Delete `ErrAutoFetchAuth` (and `"fmt"` if unused). Add to `AutoFetchResult`:

```go
	// AuthFailed names the remotes this fetch could not reach for want of
	// credentials; the caller pauses them.
	AuthFailed []string `json:"authFailed"`
```

Replace `AutoFetch`'s head and fetch call:

```go
// AutoFetch fetches each remote paused does not report, one at a time,
// without ever prompting, and reports the commits the checked-out branch's
// upstream gained in this fetch that HEAD lacks. Only this fetch's change
// counts, so commits a manual Fetch already brought are never reported
// again. A remote failing for want of credentials lands in AuthFailed;
// any other failure of one remote is left to the Commands panel. Both let
// the next remote run; a cancel stops them all.
func AutoFetch(ctx context.Context, dir string, paused func(remote string) bool) (AutoFetchResult, error) {
	var res AutoFetchResult
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "remote")
	if err != nil {
		return res, err
	}
	var remotes []string
	for _, r := range strings.Fields(out) {
		if paused == nil || !paused(r) {
			remotes = append(remotes, r)
		}
	}
	if len(remotes) == 0 {
		res.Skipped = true
		return res, nil
	}
	// … branch, upstream, oldTip and before exactly as today …
	for _, remote := range remotes {
		_, err := gitcmd.RunEnv(ctx, dir, AutoFetchTimeout, NoPromptEnv, "fetch", "--prune", remote)
		switch {
		case err == nil:
		case ctx.Err() != nil:
			return res, err
		case IsAuthError(err):
			res.AuthFailed = append(res.AuthFailed, remote)
		}
	}
	// … after, RefsChanged, newTip and NewCommits exactly as today …
}
```

- [ ] **Step 4: Fix the one caller** — in `internal/app/autofetch.go`: `return ops.AutoFetch(ctx, dir, nil)` (Task 4 wires the pause).

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/ops ./internal/app -race`
Expected: PASS. (`TestUserWriteCancelsARealBackgroundFetchAsCancelled` looks for an entry whose `Args[0] == "fetch"`: still true.)

- [ ] **Step 6: Commit** (no behaviour visible to the user yet beyond per-remote log lines; the docs change lands with Task 4, which completes the behaviour)

```bash
/usr/bin/git add internal/ops/autofetch.go internal/ops/autofetch_test.go internal/app/autofetch.go
/usr/bin/git commit -m "refactor(ops): background fetch goes remote by remote and reports auth failures" -m "A remote that needs credentials no longer stops the others; AuthFailed names it. The 401 test ignores the developer's global and system git config."
```

---

### Task 4: Pause per remote, in Go, cleared by any successful fetch or pull

**Files:**
- Create: `internal/app/autopause.go`
- Create: `internal/app/autopause_test.go`
- Modify: `internal/app/app.go` (field `paused autoPause` next to `writes`)
- Modify: `internal/app/autofetch.go`
- Modify: `internal/app/cmdlog.go` (`recordGit`)
- Modify: `internal/cmdlog/classify.go`, `internal/cmdlog/classify_test.go`
- Modify: `frontend/src/lib/autoFetch.ts`, `frontend/src/lib/autoFetch.test.ts`, `frontend/src/lib/actions.ts`, `frontend/src/lib/types.ts`
- Modify: `docs/spec/05-remote-and-stash.md`, `docs/spec/09-command-log.md`

**Interfaces:**
- Consumes: `ops.AutoFetch(ctx, dir, paused)` and `AutoFetchResult.AuthFailed` (Task 3); `(*writeLock)` (Task 1).
- Produces: `func cmdlog.UpdatesFromRemote(args []string) bool`; `type autoPause`; `(*autoPause).paused(key, remote string) bool`, `.pause(key string, remotes []string)`, `.resume(key string)`; `func (a *App) noteRemoteUpdate(r gitcmd.Record)`.

- [ ] **Step 1: Failing classifier test** — append to `internal/cmdlog/classify_test.go`:

```go
func TestUpdatesFromRemote(t *testing.T) {
	for _, c := range []struct {
		args []string
		want bool
	}{
		{[]string{"fetch", "--all", "--prune"}, true},
		{[]string{"fetch", "--prune", "origin"}, true},
		{[]string{"-c", "x=y", "pull", "--"}, true},
		{[]string{"fetch", ".", "origin/main:refs/heads/main"}, false},
		{[]string{"push"}, false},
		{[]string{"status"}, false},
	} {
		if got := UpdatesFromRemote(c.args); got != c.want {
			t.Errorf("UpdatesFromRemote(%v) = %v, want %v", c.args, got, c.want)
		}
	}
}
```

(Check the file's package clause: if it is `package cmdlog_test`, call `cmdlog.UpdatesFromRemote`.)

- [ ] **Step 2: Implement** — append to `internal/cmdlog/classify.go`:

```go
// UpdatesFromRemote reports whether args is a pull, or a fetch from a
// remote — not `fetch .`, which only moves local branches (git-flow).
func UpdatesFromRemote(args []string) bool {
	sub, rest := subcommand(args)
	switch sub {
	case "pull":
		return true
	case "fetch":
		pos := positional(rest)
		return len(pos) == 0 || pos[0] != "."
	}
	return false
}
```

Run: `go test ./internal/cmdlog` → PASS.

- [ ] **Step 3: Failing app tests** (`internal/app/autopause_test.go`)

```go
package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"git-ui/internal/cmdlog"
	"git-ui/internal/gitcmd"
	"git-ui/internal/testrepo"
)

func lockedRemote(t *testing.T) string {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", `Basic realm="x"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/repo.git"
}

func TestAutoFetchPausesOnlyTheRemoteThatNeedsCredentials(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	a, r, id := newPlainApp(t)
	bare := testrepo.NewBareFrom(t, r)
	r.Git("remote", "add", "origin", bare)
	r.Git("remote", "add", "locked", lockedRemote(t))
	key := cmdlog.RepoKey(r.Dir)

	if _, err := a.AutoFetch(id); err != nil {
		t.Fatal(err)
	}
	if !a.paused.paused(key, "locked") || a.paused.paused(key, "origin") {
		t.Fatal("want locked paused and origin not")
	}

	// A successful Fetch by the user unpauses the repository.
	r.Git("remote", "set-url", "locked", bare)
	if err := a.Fetch(id); err != nil {
		t.Fatal(err)
	}
	if a.paused.paused(key, "locked") {
		t.Error("a successful Fetch left locked paused")
	}
}

func TestOnlyASuccessfulNonAutoRemoteUpdateUnpauses(t *testing.T) {
	a, r, _ := newPlainApp(t)
	key := cmdlog.RepoKey(r.Dir)
	rec := func(ctx context.Context, exit int, args ...string) gitcmd.Record {
		return gitcmd.Record{Ctx: ctx, Dir: r.Dir, Args: args, ExitCode: exit}
	}
	auto := cmdlog.WithOrigin(context.Background(), cmdlog.OriginAuto)
	for _, c := range []gitcmd.Record{
		rec(context.Background(), 0, "fetch", ".", "origin/main:refs/heads/main"),
		rec(context.Background(), 1, "fetch", "--all", "--prune"),
		rec(auto, 0, "fetch", "--prune", "origin"),
		rec(context.Background(), 0, "push"),
	} {
		a.paused.pause(key, []string{"origin"})
		a.noteRemoteUpdate(c)
		if !a.paused.paused(key, "origin") {
			t.Errorf("%v (exit %d) unpaused", c.Args, c.ExitCode)
		}
	}
	a.noteRemoteUpdate(rec(context.Background(), 0, "pull", "--"))
	if a.paused.paused(key, "origin") {
		t.Error("a successful pull did not unpause")
	}
}
```

Run: `go test ./internal/app -run 'Pause|Unpause'` → FAIL to compile.

- [ ] **Step 4: Implement `internal/app/autopause.go`**

```go
package app

import (
	"sync"

	"git-ui/internal/cmdlog"
	"git-ui/internal/gitcmd"
)

// autoPause is the remotes background fetches skip because they failed
// for want of credentials, by repository (cmdlog.RepoKey). In memory only:
// a restart retries them.
type autoPause struct {
	mu      sync.Mutex
	remotes map[string]map[string]bool
}

func (p *autoPause) paused(key, remote string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.remotes[key][remote]
}

func (p *autoPause) pause(key string, remotes []string) {
	if len(remotes) == 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.remotes == nil {
		p.remotes = map[string]map[string]bool{}
	}
	if p.remotes[key] == nil {
		p.remotes[key] = map[string]bool{}
	}
	for _, r := range remotes {
		p.remotes[key][r] = true
	}
}

func (p *autoPause) resume(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.remotes, key)
}

// noteRemoteUpdate unpauses a repository's background fetches when a fetch
// or pull of it that was not itself a background fetch succeeds — from the
// toolbar, the AI or git-flow alike: credentials work again.
func (a *App) noteRemoteUpdate(r gitcmd.Record) {
	if r.ExitCode != 0 || r.Ctx == nil || !cmdlog.UpdatesFromRemote(r.Args) {
		return
	}
	if o, ok := cmdlog.OriginFrom(r.Ctx); ok && o == cmdlog.OriginAuto {
		return
	}
	a.paused.resume(cmdlog.RepoKey(r.Dir))
}
```

In `internal/app/app.go`, add next to `writes`:

```go
	// paused is the remotes background fetches skip (see autoPause).
	paused autoPause
```

In `internal/app/cmdlog.go`, `recordGit` becomes:

```go
func (a *App) recordGit(r gitcmd.Record) {
	r.Ctx = a.originContext(r.Ctx, r.Dir, r.Args)
	a.noteRemoteUpdate(r)
	a.emitCommandSafe(a.cmds.Add(r))
}
```

In `internal/app/autofetch.go`, replace the `return ops.AutoFetch(ctx, dir, nil)` with:

```go
	key := cmdlog.RepoKey(dir)
	res, err := ops.AutoFetch(ctx, dir, func(remote string) bool { return a.paused.paused(key, remote) })
	a.paused.pause(key, res.AuthFailed)
	return res, err
```

`New` installs `recordGit` as the process's git recorder (`app.go`, `gitcmd.SetRecorder`), so the Fetch in `TestAutoFetchPausesOnlyTheRemoteThatNeedsCredentials` goes through it. The `internal/app` tests do not use `t.Parallel`; keep it that way for these (the recorder is process-wide).

- [ ] **Step 5: Run the Go tests**

Run: `go test ./internal/... -race`
Expected: PASS.

- [ ] **Step 6: Frontend — drop the pause** 

`frontend/src/lib/types.ts`, `AutoFetchResult`: add `authFailed: string[] | null`.

`frontend/src/lib/autoFetch.ts`:
- `AutoFetchAction` loses `pause`: `{ refresh: boolean; event?: NotifyEvent }`.
- Delete `isAuth`, `pausedRepos`, `resumeAutoFetch`, and the `errorMessage` import if unused.
- `outcome`: `if (r.error !== undefined) return { refresh: false }`; `if (!res || res.skipped) return { refresh: false }`; the action literal drops `pause: false`.
- `runRound`: drop `|| pausedRepos.has(repo.id)` and `if (a.pause) pausedRepos.add(repo.id)`.
- Update the file comment: pausing lives in Go (`internal/app/autopause.go`).

`frontend/src/lib/actions.ts`: remove the `resumeAutoFetch` import; `fetchRemote` becomes `export const fetchRemote = (id: string) => runOp(id, 'fetch', 'Fetching…', () => api.fetch(id))` (check callers don't rely on its `Promise<void>` type — `runOp` returns a boolean promise; if a caller is typed for void, keep `async function` with `await runOp(...)`); in `pull`, delete the `resumeAutoFetch(id)` line.

`frontend/src/lib/autoFetch.test.ts`:
- `res()` gets `authFailed: null`.
- Import line drops `pausedRepos, resumeAutoFetch`; delete every `pausedRepos.clear()`.
- First `outcome` test becomes:
  ```ts
  it('does nothing on an error', () => {
    expect(outcome('a', { error: new Error('git fetch: timed out') }, sel)).toEqual({ refresh: false })
    expect(outcome('a', { error: 'anything' }, sel)).toEqual({ refresh: false })
  })
  ```
- Skipped expectation: `toEqual({ refresh: false })`.
- Delete the test 'pauses a repository on an auth failure until resumed'.

Run (node 22): `cd frontend && npx vitest run && npm run check` → PASS, 0 errors.

- [ ] **Step 7: Docs**

`docs/spec/05-remote-and-stash.md`, Background fetch, first paragraph: replace "runs `git fetch --all --prune`" with "runs `git fetch --prune <remote>` for each remote". Replace the sentence "Such an authentication failure stops background fetches of that repository until a manual Fetch or Pull of it succeeds or the app restarts." with:

```markdown
Such an authentication failure stops background fetches of that remote —
the repository's other remotes are still fetched — until any fetch or pull
of the repository succeeds, whether from the toolbar, the AI chat or a
git-flow action, or the app restarts. A repository whose remotes are all
stopped this way is skipped. The macOS keychain may ask once for access to
a stored credential; "Always Allow" ends that.
```

`docs/spec/09-command-log.md`: "the fetch and the reads around it" → "the fetches and the reads around them".

- [ ] **Step 8: Commit**

```bash
/usr/bin/git add internal/app/autopause.go internal/app/autopause_test.go internal/app/app.go internal/app/autofetch.go internal/app/cmdlog.go internal/cmdlog/classify.go internal/cmdlog/classify_test.go frontend/src/lib/autoFetch.ts frontend/src/lib/autoFetch.test.ts frontend/src/lib/actions.ts frontend/src/lib/types.ts docs/spec/05-remote-and-stash.md docs/spec/09-command-log.md
/usr/bin/git commit -m "fix(fetch): pause background fetches per remote and unpause on any successful fetch or pull" -m "The pause moves from the frontend to Go: only the remote that needs credentials is skipped, and a fetch or pull from the toolbar, the AI or git-flow that succeeds clears it."
```

---

### Task 5: Off stops a running round

**Files:**
- Modify: `frontend/src/lib/autoFetch.ts`
- Modify: `frontend/src/lib/autoFetch.test.ts`
- Modify: `docs/spec/05-remote-and-stash.md`

**Interfaces:**
- Produces: `runRound(d: AutoFetchDeps, keepGoing?: () => boolean): Promise<void>`.

- [ ] **Step 1: Failing tests** — in `autoFetch.test.ts`, inside `describe('runRound')`:

```ts
  it('stops before the next repository once keepGoing turns false', async () => {
    let go = true
    const fetch = vi.fn(async () => { go = false; return res() })
    await runRound(deps({ fetch }), () => go)
    expect(fetch).toHaveBeenCalledTimes(1)
  })
```

and inside `describe('startAutoFetch')`:

```ts
  it('Off or stop mid-round ends the round at the next repository', async () => {
    let release!: () => void
    const fetch = vi.fn(() => new Promise<AutoFetchResult>((r) => { release = () => r(res()) }))
    const d = deps({ fetch }) // repositories a, gone (missing), b
    const stop = startAutoFetch(d)
    await vi.advanceTimersByTimeAsync(FIRST_ROUND_MS)
    expect(fetch).toHaveBeenCalledTimes(1) // a in flight
    autoFetchMinutes.set(0)
    release()
    await vi.advanceTimersByTimeAsync(0)
    expect(fetch).toHaveBeenCalledTimes(1) // b never fetched

    autoFetchMinutes.set(15)
    const d2 = deps({ fetch: vi.fn(() => new Promise<AutoFetchResult>((r) => { release = () => r(res()) })) })
    const stop2 = startAutoFetch(d2)
    await vi.advanceTimersByTimeAsync(FIRST_ROUND_MS)
    stop2()
    release()
    await vi.advanceTimersByTimeAsync(0)
    expect(d2.fetch).toHaveBeenCalledTimes(1)
    stop()
  })
```

Run: `npx vitest run src/lib/autoFetch.test.ts` → FAIL (second fetch happens).

- [ ] **Step 2: Implement** — in `autoFetch.ts`:

```ts
export async function runRound(d: AutoFetchDeps, keepGoing: () => boolean = () => true): Promise<void> {
  if (!d.online()) return
  for (const repo of d.repos()) {
    if (!keepGoing()) return
    if (repo.missing) continue
    // … unchanged …
  }
}
```

In `startAutoFetch`: add `let stopped = false`; in `round`, `await runRound(d, () => !stopped && get(autoFetchMinutes) > 0)`; in `schedule`, return early when `stopped`; the returned stop function sets `stopped = true` before unsubscribing and clearing the timer. Update its doc comment: "Off, or stop, ends a running round before its next repository (the fetch in flight finishes)."

- [ ] **Step 3: Run** — `npx vitest run && npm run check` → PASS.

- [ ] **Step 4: Docs** — in `docs/spec/05-remote-and-stash.md`, Background fetch, after "…is skipped." in the first paragraph add: "Turning it Off during a round lets the repository being fetched finish and fetches no other."

- [ ] **Step 5: Commit**

```bash
/usr/bin/git add frontend/src/lib/autoFetch.ts frontend/src/lib/autoFetch.test.ts docs/spec/05-remote-and-stash.md
/usr/bin/git commit -m "fix(fetch): Off or stop ends a running background round at the next repository"
```

---

### Task 6: Coalesced repository refresh

**Files:**
- Create: `frontend/src/lib/coalesce.ts`
- Create: `frontend/src/lib/coalesce.test.ts`
- Modify: `frontend/src/lib/stores.ts` (`refreshRepo`, ~line 475)

**Interfaces:**
- Produces: `export function coalesce(job: () => Promise<void>): () => Promise<void>`; `refreshRepo` keeps its name and `() => Promise<void>` type.

- [ ] **Step 1: Failing tests** (`coalesce.test.ts`)

```ts
import { describe, expect, it } from 'vitest'
import { coalesce } from './coalesce'

function gated() {
  const releases: (() => void)[] = []
  let runs = 0
  const job = () => new Promise<void>((r) => { runs++; releases.push(r) })
  return { job, releases, runs: () => runs }
}
const tick = () => new Promise((r) => setTimeout(r, 0))

describe('coalesce', () => {
  it('runs once when called alone', async () => {
    const g = gated()
    const run = coalesce(g.job)
    const p = run()
    g.releases[0]()
    await p
    expect(g.runs()).toBe(1)
  })

  it('calls during a run share one more run that starts after it', async () => {
    const g = gated()
    const run = coalesce(g.job)
    const first = run()
    const done: string[] = []
    const a = run().then(() => done.push('a'))
    const b = run().then(() => done.push('b'))
    expect(g.runs()).toBe(1)
    g.releases[0]()
    await first
    await tick()
    expect(g.runs()).toBe(2) // the follow-up started only after the first ended
    expect(done).toEqual([]) // and the waiting callers are not done yet
    g.releases[1]()
    await Promise.all([a, b])
    expect(g.runs()).toBe(2)
  })

  it('a failed run still lets the follow-up run', async () => {
    let n = 0
    const run = coalesce(async () => { n++; if (n === 1) throw new Error('x') })
    const first = run().catch(() => 'failed')
    const second = run()
    expect(await first).toBe('failed')
    await second
    expect(n).toBe(2)
  })
})
```

Run: `npx vitest run src/lib/coalesce.test.ts` → FAIL (module missing).

- [ ] **Step 2: Implement** (`coalesce.ts`)

```ts
/** coalesce wraps an async job so it never runs twice at once: a call
 *  while it runs gets one more run that starts when the current one ends,
 *  shared with every other call made meanwhile. Each caller so awaits a
 *  run that started after its call — it sees what it changed — and at most
 *  two runs happen back to back. */
export function coalesce(job: () => Promise<void>): () => Promise<void> {
  let current: Promise<void> | null = null
  let next: Promise<void> | null = null
  const run = (): Promise<void> => {
    if (!current) {
      current = job().finally(() => {
        current = null
      })
      return current
    }
    if (!next) {
      next = current
        .catch(() => {})
        .then(() => {
          next = null
          return run()
        })
    }
    return next
  }
  return run
}
```

- [ ] **Step 3: Use it** — in `stores.ts`, import `{ coalesce } from './coalesce'` and turn `refreshRepo` into:

```ts
/** Reloads everything the selected repository shows. Coalesced: a call
 *  while one runs waits for one more run instead of racing it (a background
 *  fetch's refresh and an operation's, say). */
export const refreshRepo = coalesce(async () => {
  await loadRepos()
  await loadRefs()
  await loadMergeState()
  await loadWorktreeState()
  await loadRemoteInfo()
  await loadStashEntries()
  await loadOwedStashDrop()
  logVersion.update((v) => v + 1)
})
```

Check nothing in `stores.ts` calls `refreshRepo` at module evaluation time above this line (a `const` is not hoisted like a function): `grep -n "refreshRepo" frontend/src/lib/stores.ts`.

- [ ] **Step 4: Run** — `npx vitest run && npm run check` → PASS, 0 errors (the 2 known a11y warnings in ContextMenu/BlameView are pre-existing).

- [ ] **Step 5: Commit**

```bash
/usr/bin/git add frontend/src/lib/coalesce.ts frontend/src/lib/coalesce.test.ts frontend/src/lib/stores.ts
/usr/bin/git commit -m "fix(app): coalesce repository refreshes so a background one never races an operation's"
```

---

### Task 7: Verify and reopen

- [ ] **Step 1:** `go vet ./... && go test ./... -race` → PASS.
- [ ] **Step 2:** `cd frontend && npx vitest run && npm run check` (node 22) → PASS.
- [ ] **Step 3:** `grep -rn "pausedRepos\|resumeAutoFetch\|ErrAutoFetchAuth\|writeMutex\|autoFetches" internal frontend/src` → nothing.
- [ ] **Step 4:** Rebuild and reopen: stop any running dev app, `cd frontend && npm run build`, then `make dev` in the background; revert `frontend/wailsjs/runtime` afterwards (`/usr/bin/git checkout -- frontend/wailsjs/runtime`). Check the Commands panel ~30 s after start shows one Auto `git fetch --prune <remote>` per remote.
