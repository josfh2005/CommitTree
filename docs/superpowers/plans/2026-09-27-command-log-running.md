# Running Commands and Cancel Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show git writes in the Commands panel while they run, let the user cancel them like Ctrl+C, and move the "Operation running" notice from the terminal header to the bottom dock.

**Architecture:** `gitcmd.RunEnv` gains a start hook (`Recorder.Begin`) that receives a `Cancel` func and returns an ID the end record carries; git runs in its own process group and is stopped with SIGINT on cancel or timeout. `cmdlog.Log.Begin` lists a write as `running` and keeps its cancel func; `Add` replaces it in place. `internal/app` emits both through `cmdlog:entry` and exposes `CancelCommand`. The panel merges running and finished entries by ID and shows a spinner, elapsed time and a Cancel button.

**Tech Stack:** Go 1.2x + Wails v2 backend; Svelte 3 + TypeScript + Vitest frontend.

**Spec:** `docs/superpowers/specs/2026-09-27-command-log-running-design.md`

## Global Constraints

- Only writes appear while running; a running read never gets a row, whatever **Show reads** says.
- Every running write can be cancelled: SIGINT to git's process group; killed if still alive 5 s later (`WaitDelay`). Timeouts stop git the same way. On Windows: kill.
- Outcomes: `running`, `ok`, `failed`, `timeout`, `cancelled`. `gitcmd.ErrCancelled` message: `git command cancelled`.
- One event name, `cmdlog:entry`, carries both the running entry and the finished one (same ID); emitted only after `Startup`.
- A finished entry is never replaced by a running one in the frontend.
- `Clear` keeps running entries.
- The recorder must never change a git command's result: panics in `Begin` or `End` recovered (`Begin` panic → ID 0).
- "Operation running: `<label>`" is a strip at the top of the bottom dock, shown only while `busy` is set and the dock is open; the terminal header no longer shows it.
- Behaviour changes update `docs/spec/` in the same commit (project rule).
- Commit messages: conventional style as in `git log`; NEVER add a `Co-Authored-By` line or any trailer.
- Go tests: `go test ./... -race`. Frontend: `source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && cd frontend && npm test && npm run check && npm run build` (2 pre-existing svelte-check warnings, BlameView and ContextMenu; add none).
- `wails generate module` also emits an unrelated, never-committed `App.Menu()` / `menu.Menu` / `keys.Accelerator` binding: strip it before committing.

## Review Focus

1. **Cancel racing the command's natural end** — clicking Cancel just as a push finishes must not leave a "running" row or show a scary error: `Cancel` on a finished ID returns `ErrNotRunning` (Task 2 test) and the panel ignores that error when the row already finished (Task 4 code).
2. **Events out of order** — the initial `CommandLog` load and live events overlap; a late running event must not turn a finished row back into running (Task 4 `mergeEntries` test).
3. **A hook or credential helper that keeps running** — cancelling a commit whose `pre-commit` hook sleeps returns in well under 5 s, with no commit and no `index.lock` left (Task 1 test).
4. **Timeouts after the SIGINT change** — a command that times out is still reported as `timeout`, not `cancelled` (Task 1 test).
5. **Several repositories with running writes** — cancelling with another repository's ID must not stop the command (Task 2 test).

---

## File Structure

- Modify `internal/gitcmd/gitcmd.go` — `Start`, `Recorder`, `ErrCancelled`, `Record.ID`, cancellation in `RunEnv`.
- Create `internal/gitcmd/proc_unix.go`, `internal/gitcmd/proc_windows.go` — process group + interrupt per OS.
- Create `internal/gitcmd/cancel_test.go` (unix-only tests using a shell hook); modify `internal/gitcmd/gitcmd_test.go`.
- Modify `internal/cmdlog/log.go` (+ `log_test.go`) — running entries, `Begin`, `Cancel`, `ErrNotRunning`.
- Modify `internal/app/cmdlog.go`, `internal/app/app.go` (+ `cmdlog_test.go`) — start hook, `CancelCommand`.
- Regenerate `frontend/wailsjs/go/app/App.{js,d.ts}`.
- Modify `frontend/src/lib/types.ts`, `lib/api.ts`, `lib/cmdlog.ts` (+ test), `components/CommandLogPanel.svelte`, `components/BottomDock.svelte`, `components/TerminalPanel.svelte`.
- Docs: `docs/spec/09-command-log.md`, `docs/spec/08-terminal.md`.

---

### Task 1: gitcmd — start hook and cancellation

**Files:**
- Modify: `internal/gitcmd/gitcmd.go`
- Create: `internal/gitcmd/proc_unix.go`, `internal/gitcmd/proc_windows.go`
- Modify: `internal/gitcmd/gitcmd_test.go` (two `SetRecorder` calls, new tests)
- Create: `internal/gitcmd/cancel_test.go`
- Modify: `internal/app/app.go:122` (keep the build green)

**Interfaces:**
- Produces:
  ```go
  var ErrCancelled = errors.New("git command cancelled")
  type Start struct { Ctx context.Context; Dir string; Args []string; Start time.Time; Cancel func() }
  type Recorder struct { Begin func(Start) int64; End func(Record) } // either may be nil
  func SetRecorder(r *Recorder) // nil removes it
  // Record gains: ID int64 — Begin's ID, 0 without Begin
  ```

- [ ] **Step 1: Update the existing recorder tests to the new API** — in `internal/gitcmd/gitcmd_test.go`:
  - replace `gitcmd.SetRecorder(func(rec gitcmd.Record) {` (line ~117) with `gitcmd.SetRecorder(&gitcmd.Recorder{End: func(rec gitcmd.Record) {` and close it with `}})` instead of `})`;
  - replace `gitcmd.SetRecorder(func(gitcmd.Record) { panic("boom") })` with `gitcmd.SetRecorder(&gitcmd.Recorder{End: func(gitcmd.Record) { panic("boom") }})`.

- [ ] **Step 2: Write the failing tests** — append to `internal/gitcmd/gitcmd_test.go` (imports `sync`, `strings`, `context` are already there):

```go
func TestRecorderBeginAndEndShareID(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("first")
	var mu sync.Mutex
	var begun []gitcmd.Start
	var ended []gitcmd.Record
	gitcmd.SetRecorder(&gitcmd.Recorder{
		Begin: func(s gitcmd.Start) int64 {
			mu.Lock()
			defer mu.Unlock()
			begun = append(begun, s)
			return 42
		},
		End: func(rec gitcmd.Record) {
			mu.Lock()
			defer mu.Unlock()
			ended = append(ended, rec)
		},
	})
	t.Cleanup(func() { gitcmd.SetRecorder(nil) })

	if _, err := gitcmd.Run(context.Background(), r.Dir, gitcmd.ReadTimeout, "rev-parse", "HEAD"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(begun) != 1 || len(ended) != 1 {
		t.Fatalf("begun %d, ended %d", len(begun), len(ended))
	}
	b := begun[0]
	if b.Dir != r.Dir || strings.Join(b.Args, " ") != "rev-parse HEAD" || b.Cancel == nil || b.Start.IsZero() {
		t.Fatalf("start wrong: %+v", b)
	}
	if ended[0].ID != 42 || !ended[0].Start.Equal(b.Start) {
		t.Fatalf("end must carry Begin's ID and start: %+v", ended[0])
	}
}

func TestPanickingBeginDoesNotBreakRun(t *testing.T) {
	r := testrepo.New(t)
	hash := r.Commit("first")
	var gotID int64 = -1
	gitcmd.SetRecorder(&gitcmd.Recorder{
		Begin: func(gitcmd.Start) int64 { panic("boom") },
		End:   func(rec gitcmd.Record) { gotID = rec.ID },
	})
	t.Cleanup(func() { gitcmd.SetRecorder(nil) })

	out, err := gitcmd.Run(context.Background(), r.Dir, gitcmd.ReadTimeout, "rev-parse", "HEAD")
	if err != nil || strings.TrimSpace(out) != strings.TrimSpace(hash) {
		t.Fatalf("got %q, %v", out, err)
	}
	if gotID != 0 {
		t.Fatalf("a panicking Begin must yield ID 0, got %d", gotID)
	}
}
```

Create `internal/gitcmd/cancel_test.go`:

```go
//go:build !windows

package gitcmd_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git-ui/internal/gitcmd"
	"git-ui/internal/testrepo"
)

// sleepyCommit prepares r so that committing runs a pre-commit hook that
// sleeps for 30 s, and returns the -c argument that enables that hook.
func sleepyCommit(t *testing.T, r *testrepo.Repo) string {
	t.Helper()
	hooks := t.TempDir()
	if err := os.WriteFile(filepath.Join(hooks, "pre-commit"), []byte("#!/bin/sh\nsleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	r.WriteFile("new.txt", "x\n")
	r.Git("add", "new.txt")
	return "core.hooksPath=" + hooks
}

func TestCancelStopsCommitAndItsHook(t *testing.T) {
	r := testrepo.New(t)
	head := strings.TrimSpace(r.Commit("first"))
	hooksPath := sleepyCommit(t, r)
	gitcmd.SetRecorder(&gitcmd.Recorder{Begin: func(s gitcmd.Start) int64 {
		time.AfterFunc(300*time.Millisecond, s.Cancel)
		return 1
	}})
	t.Cleanup(func() { gitcmd.SetRecorder(nil) })

	started := time.Now()
	_, err := gitcmd.Run(context.Background(), r.Dir, gitcmd.HookTimeout, "-c", hooksPath, "commit", "-m", "x")
	if !errors.Is(err, gitcmd.ErrCancelled) {
		t.Fatalf("got %v, want ErrCancelled", err)
	}
	if d := time.Since(started); d > 4*time.Second {
		t.Fatalf("took %v: the hook was not interrupted", d)
	}
	if got := strings.TrimSpace(r.Git("rev-parse", "HEAD")); got != head {
		t.Fatal("a cancelled commit must not be made")
	}
	if _, err := os.Stat(filepath.Join(r.Dir, ".git", "index.lock")); !os.IsNotExist(err) {
		t.Fatalf("index.lock left behind: %v", err)
	}
}

func TestTimeoutIsNotCancel(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("first")
	hooksPath := sleepyCommit(t, r)

	started := time.Now()
	_, err := gitcmd.Run(context.Background(), r.Dir, 300*time.Millisecond, "-c", hooksPath, "commit", "-m", "x")
	if !errors.Is(err, gitcmd.ErrTimeout) || errors.Is(err, gitcmd.ErrCancelled) {
		t.Fatalf("got %v, want ErrTimeout only", err)
	}
	if d := time.Since(started); d > 4*time.Second {
		t.Fatalf("took %v: the hook was not interrupted on timeout", d)
	}
}
```

- [ ] **Step 3: Run to verify failure**

Run: `go test ./internal/gitcmd/ -v`
Expected: FAIL to compile — `SetRecorder` takes a func, undefined `gitcmd.Recorder`, `gitcmd.Start`, `gitcmd.ErrCancelled`, `Record.ID`.

- [ ] **Step 4: Implement** — in `internal/gitcmd/gitcmd.go`:

After `var ErrTimeout = …` add:

```go
// ErrCancelled is wrapped by the error of a command stopped through its
// Start.Cancel — the Commands panel's Cancel button.
var ErrCancelled = errors.New("git command cancelled")
```

In `Record`, add as the first field:

```go
	// ID is the ID Recorder.Begin returned for this command; 0 without Begin.
	ID int64
```

Replace everything from `var recorder atomic.Pointer[func(Record)]` through the end of `func record` with:

```go
// Start is a git command about to run, handed to Recorder.Begin.
type Start struct {
	// Ctx is the caller's context, as in Record.
	Ctx   context.Context
	Dir   string
	Args  []string
	Start time.Time
	// Cancel stops the command as Ctrl+C would; the caller then gets an
	// error wrapping ErrCancelled. Safe to call more than once, and after
	// the command ended (it then does nothing).
	Cancel func()
}

// Recorder receives every command Run and RunEnv run — the app's command
// log. Either func may be nil.
type Recorder struct {
	// Begin is called just before git starts; the ID it returns comes back
	// in the Record passed to End.
	Begin func(Start) int64
	// End is called when git has ended.
	End func(Record)
}

var recorder atomic.Pointer[Recorder]

// SetRecorder makes r receive every command; nil removes it. There is one
// recorder for the whole process.
func SetRecorder(r *Recorder) { recorder.Store(r) }

// begin and record hand a command to the recorder. The recorder is a
// convenience: whatever it does, including panicking, must not change the
// command's result.
func begin(s Start) (id int64) {
	r := recorder.Load()
	if r == nil || r.Begin == nil {
		return 0
	}
	defer func() {
		if recover() != nil {
			id = 0
		}
	}()
	return r.Begin(s)
}

func record(rec Record) {
	r := recorder.Load()
	if r == nil || r.End == nil {
		return
	}
	defer func() { _ = recover() }()
	r.End(rec)
}
```

Replace the body of `RunEnv` with:

```go
func RunEnv(ctx context.Context, dir string, timeout time.Duration, env []string, args ...string) (string, error) {
	caller := ctx
	// cancellable is what Start.Cancel stops, with ErrCancelled as the cause
	// so the result can tell a cancel from a timeout.
	cancellable, cancelCmd := context.WithCancelCause(ctx)
	defer cancelCmd(nil)
	ctx, cancel := context.WithTimeout(cancellable, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	cmd.Env = append(cmd.Env, env...)
	// On cancel or timeout, stop git as Ctrl+C in a terminal would: git
	// removes its lock files, and its hooks and helpers get the signal too.
	startInGroup(cmd)
	cmd.Cancel = func() error { return interrupt(cmd) }
	// A git still alive this long after that, or whose child (e.g. a
	// credential helper) holds the pipe open, is killed and Wait returns.
	cmd.WaitDelay = 5 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	start := time.Now()
	id := begin(Start{Ctx: caller, Dir: dir, Args: args, Start: start, Cancel: func() { cancelCmd(ErrCancelled) }})
	runErr := cmd.Run()
	out, errOut := stdout.String(), stderr.String()
	rec := Record{ID: id, Ctx: caller, Dir: dir, Args: args, Start: start, Duration: time.Since(start), Stdout: out, Stderr: errOut}
	if runErr != nil {
		gerr := &Error{Args: args, Stderr: errOut, ExitCode: -1, Err: runErr}
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			gerr.ExitCode = exitErr.ExitCode()
		}
		switch {
		case errors.Is(context.Cause(cancellable), ErrCancelled):
			gerr.Err = ErrCancelled
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			gerr.Err = ErrTimeout
		}
		rec.ExitCode, rec.Err = gerr.ExitCode, gerr
		record(rec)
		return out, gerr
	}
	record(rec)
	return out, nil
}
```

Create `internal/gitcmd/proc_unix.go`:

```go
//go:build !windows

package gitcmd

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// startInGroup starts git in a process group of its own, so interrupt
// reaches the hooks and helpers it runs too, as Ctrl+C in a terminal does.
func startInGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// interrupt sends SIGINT to git's process group.
func interrupt(cmd *exec.Cmd) error {
	err := syscall.Kill(-cmd.Process.Pid, syscall.SIGINT)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}
```

Create `internal/gitcmd/proc_windows.go`:

```go
//go:build windows

package gitcmd

import "os/exec"

// Windows has no SIGINT to send to a process: git is killed instead.
func startInGroup(*exec.Cmd) {}

func interrupt(cmd *exec.Cmd) error { return cmd.Process.Kill() }
```

In `internal/app/app.go` (line ~122) replace `gitcmd.SetRecorder(a.recordGit)` with:

```go
	gitcmd.SetRecorder(&gitcmd.Recorder{End: a.recordGit})
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/gitcmd/ -race -v && go build ./... && GOOS=windows go vet ./internal/gitcmd/`
Expected: PASS (all old and new gitcmd tests); build and the Windows vet succeed.

- [ ] **Step 6: Run the whole Go suite**

Run: `go test ./... -race`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/gitcmd internal/app/app.go
git commit -m "feat(gitcmd): start hook with a Cancel that interrupts git like Ctrl+C; timeouts interrupt too"
```

---

### Task 2: cmdlog — running entries and Cancel

**Files:**
- Modify: `internal/cmdlog/log.go`
- Test: `internal/cmdlog/log_test.go`

**Interfaces:**
- Consumes: `gitcmd.Start`, `gitcmd.Record.ID`, `gitcmd.ErrCancelled` (Task 1).
- Produces:
  ```go
  const OutcomeRunning Outcome = "running"; const OutcomeCancelled Outcome = "cancelled"
  var ErrNotRunning = errors.New("command is not running")
  func (l *Log) Begin(s gitcmd.Start) (Entry, bool) // bool: an entry was stored (writes only)
  func (l *Log) Cancel(repo string, id int64) error  // ErrNotRunning when not running / other repo
  // Add(r): r.ID known → replaces in place; r.ID unknown → appended with r.ID; r.ID 0 → new ID
  // Clear(repo): keeps running entries
  ```

- [ ] **Step 1: Write the failing tests** — append to `internal/cmdlog/log_test.go` (it already has `rec(dir, args...)`, and imports `context`, `errors`, `strings`, `time`, `gitcmd`):

```go
func start(dir string, cancel func(), args ...string) gitcmd.Start {
	return gitcmd.Start{Ctx: context.Background(), Dir: dir, Args: args, Start: time.Unix(100, 0), Cancel: cancel}
}

func TestBeginWriteIsRunningThenReplacedInPlace(t *testing.T) {
	l := New()
	before := l.Add(rec("/r", "commit", "-m", "a"))
	b, shown := l.Begin(start("/r/", func() {}, "push"))
	if !shown || b.Outcome != OutcomeRunning || b.Origin != OriginYou || b.Kind != KindWrite || b.Repo != "/r" || b.ID <= before.ID {
		t.Fatalf("running entry wrong: %+v %v", b, shown)
	}
	if got := l.List("/r"); len(got) != 2 || got[0].ID != b.ID || got[0].Outcome != OutcomeRunning {
		t.Fatalf("list: %+v", got)
	}
	r := rec("/r", "push")
	r.ID = b.ID
	r.Stdout = "done"
	e := l.Add(r)
	got := l.List("/r")
	if e.ID != b.ID || len(got) != 2 || got[0].ID != b.ID || got[0].Outcome != OutcomeOK {
		t.Fatalf("not replaced in place: %+v", got)
	}
	if out, err := l.Output("/r", b.ID); err != nil || out.Stdout != "done" {
		t.Fatalf("output: %+v %v", out, err)
	}
}

func TestBeginReadStoresNothing(t *testing.T) {
	l := New()
	b, shown := l.Begin(start("/r", func() {}, "status"))
	if shown || b.ID == 0 || len(l.List("/r")) != 0 {
		t.Fatalf("a running read must not be listed: %+v %v", b, shown)
	}
	if err := l.Cancel("/r", b.ID); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("a read cannot be cancelled: %v", err)
	}
	r := rec("/r", "status")
	r.ID = b.ID
	if e := l.Add(r); e.ID != b.ID || len(l.List("/r")) != 1 {
		t.Fatalf("read end: %+v", e)
	}
}

func TestBeginKeepsOriginAndRedacts(t *testing.T) {
	l := New()
	s := start("/r", func() {}, "push", "https://bob:hunter22@h/r.git")
	s.Ctx = WithOrigin(context.Background(), OriginAI)
	b, _ := l.Begin(s)
	if b.Origin != OriginAI || strings.Contains(strings.Join(b.Args, " "), "hunter22") {
		t.Fatalf("got %+v", b)
	}
}

func TestCancel(t *testing.T) {
	l := New()
	called := 0
	b, _ := l.Begin(start("/r", func() { called++ }, "push"))
	if err := l.Cancel("/other", b.ID); !errors.Is(err, ErrNotRunning) || called != 0 {
		t.Fatalf("another repository's cancel: %v, called %d", err, called)
	}
	if err := l.Cancel("/r/", b.ID); err != nil || called != 1 {
		t.Fatalf("cancel: %v, called %d", err, called)
	}
	r := rec("/r", "push")
	r.ID = b.ID
	r.ExitCode = -1
	r.Err = &gitcmd.Error{ExitCode: -1, Err: gitcmd.ErrCancelled}
	if e := l.Add(r); e.Outcome != OutcomeCancelled {
		t.Fatalf("got %s", e.Outcome)
	}
	if err := l.Cancel("/r", b.ID); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("finished command: %v", err)
	}
	if err := l.Cancel("/r", 999); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("unknown id: %v", err)
	}
}

func TestClearKeepsRunning(t *testing.T) {
	l := New()
	l.Add(rec("/r", "commit", "-m", "x"))
	b, _ := l.Begin(start("/r", func() {}, "push"))
	l.Clear("/r")
	if got := l.List("/r"); len(got) != 1 || got[0].ID != b.ID {
		t.Fatalf("got %+v", got)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/cmdlog/ -v`
Expected: FAIL to compile — undefined `Begin`, `Cancel`, `ErrNotRunning`, `OutcomeRunning`, `OutcomeCancelled`.

- [ ] **Step 3: Implement** — in `internal/cmdlog/log.go`:

Add `"context"` to the imports. Replace the `Outcome` constants block with:

```go
const (
	// OutcomeRunning is a write that has started and not ended yet.
	OutcomeRunning   Outcome = "running"
	OutcomeOK        Outcome = "ok"
	OutcomeFailed    Outcome = "failed"
	OutcomeTimeout   Outcome = "timeout"
	OutcomeCancelled Outcome = "cancelled"
)
```

After `var ErrNotFound = …` add:

```go
// ErrNotRunning is Cancel's error for a command that is not running (it
// ended, was never listed, or belongs to another repository).
var ErrNotRunning = errors.New("command is not running")
```

Replace the `Log` struct and `New` with:

```go
// runningCmd is a listed write that has not ended: what Cancel needs.
type runningCmd struct {
	repo   string
	cancel func()
}

// Log holds every repository's commands. Safe for concurrent use: git runs
// from many goroutines at once.
type Log struct {
	mu      sync.Mutex
	next    int64
	repos   map[string]*ring
	running map[int64]runningCmd
}

func New() *Log { return &Log{repos: map[string]*ring{}, running: map[int64]runningCmd{}} }
```

After `RepoKey`, add:

```go
// origin is who asked for a command: ctx's origin, else You for a write
// and Auto for a read.
func origin(ctx context.Context, kind Kind) Origin {
	if o, ok := OriginFrom(ctx); ok {
		return o
	}
	if kind == KindRead {
		return OriginAuto
	}
	return OriginYou
}

// Begin reserves the ID of a command about to run. A write is listed at
// once, as running, and can be cancelled until Add logs its end; a read is
// listed only when it ends. The bool says whether an entry was stored.
func (l *Log) Begin(s gitcmd.Start) (Entry, bool) {
	kind := Classify(s.Args)
	var e Entry
	if kind == KindWrite {
		args, _ := RedactArgs(s.Args)
		e = Entry{Repo: RepoKey(s.Dir), Args: args, Origin: origin(s.Ctx, kind), Kind: kind, Start: s.Start, Outcome: OutcomeRunning}
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	l.next++
	e.ID = l.next
	if kind != KindWrite {
		return e, false
	}
	l.insert(e, Output{})
	if s.Cancel != nil {
		l.running[e.ID] = runningCmd{repo: e.Repo, cancel: s.Cancel}
	}
	return e, true
}

// Cancel stops command id of repo if it is still running.
func (l *Log) Cancel(repo string, id int64) error {
	l.mu.Lock()
	rc, ok := l.running[id]
	l.mu.Unlock()
	if !ok || rc.repo != RepoKey(repo) {
		return ErrNotRunning
	}
	rc.cancel()
	return nil
}
```

Replace `Add` with:

```go
// Add logs the end of r and returns its entry: it replaces the running
// entry Begin listed under r.ID, or appends a new one.
func (l *Log) Add(r gitcmd.Record) Entry {
	args, secrets := RedactArgs(r.Args)
	kind := Classify(r.Args)
	outcome := OutcomeOK
	if r.Err != nil {
		switch {
		case errors.Is(r.Err, gitcmd.ErrCancelled):
			outcome = OutcomeCancelled
		case errors.Is(r.Err, gitcmd.ErrTimeout):
			outcome = OutcomeTimeout
		default:
			outcome = OutcomeFailed
		}
	}
	stdout, truncOut := maskAndCap(r.Stdout, secrets)
	stderr, truncErr := maskAndCap(r.Stderr, secrets)
	out := Output{Stdout: stdout, Stderr: stderr}
	e := Entry{
		Repo: RepoKey(r.Dir), Args: args, Origin: origin(r.Ctx, kind), Kind: kind,
		Start: r.Start, DurationMs: r.Duration.Milliseconds(), ExitCode: r.ExitCode,
		Outcome: outcome, OutputTruncated: truncOut || truncErr,
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.running, r.ID)
	if r.ID == 0 {
		l.next++
		e.ID = l.next
	} else {
		e.ID = r.ID
	}
	if rg := l.repos[e.Repo]; rg != nil {
		for i := range rg.items {
			if rg.items[i].entry.ID == e.ID {
				old := rg.items[i].out
				rg.bytes += len(stdout) + len(stderr) - len(old.Stdout) - len(old.Stderr)
				rg.items[i] = item{entry: e, out: out}
				trimOutput(rg)
				return e
			}
		}
	}
	l.insert(e, out)
	return e
}

// insert appends e to its repository's ring, evicting the oldest entry
// past MaxEntries and old outputs past MaxOutputBytes. l.mu must be held.
func (l *Log) insert(e Entry, out Output) {
	rg := l.repos[e.Repo]
	if rg == nil {
		rg = &ring{}
		l.repos[e.Repo] = rg
	}
	rg.items = append(rg.items, item{entry: e, out: out})
	rg.bytes += len(out.Stdout) + len(out.Stderr)
	if len(rg.items) > MaxEntries {
		old := rg.items[0]
		rg.bytes -= len(old.out.Stdout) + len(old.out.Stderr)
		rg.items = rg.items[1:]
	}
	trimOutput(rg)
}

// trimOutput drops the output of the oldest entries (never the newest)
// until rg is within MaxOutputBytes.
func trimOutput(rg *ring) {
	for i := 0; rg.bytes > MaxOutputBytes && i < len(rg.items)-1; i++ {
		it := &rg.items[i]
		if n := len(it.out.Stdout) + len(it.out.Stderr); n > 0 {
			rg.bytes -= n
			it.out = Output{}
			it.entry.OutputDropped = true
		}
	}
}
```

Replace `Clear` with:

```go
// Clear forgets repo's log, except the commands still running: their end
// is still to come.
func (l *Log) Clear(repo string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	key := RepoKey(repo)
	rg := l.repos[key]
	if rg == nil {
		return
	}
	var kept []item
	for _, it := range rg.items {
		if it.entry.Outcome == OutcomeRunning {
			kept = append(kept, it)
		}
	}
	if len(kept) == 0 {
		delete(l.repos, key)
		return
	}
	rg.items, rg.bytes = kept, 0
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/cmdlog/ -race -v`
Expected: PASS (old and new).

- [ ] **Step 5: Commit**

```bash
git add internal/cmdlog
git commit -m "feat(cmdlog): list writes while they run and cancel them; cancelled outcome"
```

---

### Task 3: App — start hook, CancelCommand, bindings

**Files:**
- Modify: `internal/app/cmdlog.go`, `internal/app/app.go` (`New`)
- Test: `internal/app/cmdlog_test.go`
- Regenerate: `frontend/wailsjs/go/app/App.js`, `App.d.ts`

**Interfaces:**
- Consumes: `(*cmdlog.Log).Begin/Cancel`, `cmdlog.ErrNotRunning`, `cmdlog.OutcomeRunning`, `gitcmd.Recorder`, `gitcmd.Start` (Tasks 1–2).
- Produces: `func (a *App) CancelCommand(id string, entryID int64) error` (binding); `cmdlog:entry` emitted with outcome `running` when a write starts, then with its final outcome and the same ID.

- [ ] **Step 1: Write the failing tests** — append to `internal/app/cmdlog_test.go` (it imports `errors`, `sync`, `testing`, `cmdlog`):

```go
func TestCancelCommandUnknownErrors(t *testing.T) {
	a, id := newTestApp(t)
	if err := a.CancelCommand(id, 123456); !errors.Is(err, cmdlog.ErrNotRunning) {
		t.Fatalf("got %v", err)
	}
}

func TestRunningEntryEmittedBeforeFinished(t *testing.T) {
	a, id := newTestApp(t)
	var mu sync.Mutex
	var got []cmdlog.Entry
	a.cmdEmit = func(e cmdlog.Entry) {
		mu.Lock()
		got = append(got, e)
		mu.Unlock()
	}
	a.started.Store(true)
	if err := a.CreateBranch(id, "running", "HEAD", false); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	var branch []cmdlog.Entry
	for _, e := range got {
		if len(e.Args) > 0 && e.Args[0] == "branch" {
			branch = append(branch, e)
		}
	}
	if len(branch) != 2 || branch[0].Outcome != cmdlog.OutcomeRunning || branch[1].Outcome != cmdlog.OutcomeOK || branch[0].ID != branch[1].ID {
		t.Fatalf("branch events: %+v", branch)
	}
	for _, e := range got {
		if e.Kind == cmdlog.KindRead && e.Outcome == cmdlog.OutcomeRunning {
			t.Fatalf("a running read was emitted: %+v", e)
		}
	}
}

func TestAIWriteMarkAppliesWhileRunning(t *testing.T) {
	a, id := newTestApp(t)
	var mu sync.Mutex
	var running []cmdlog.Entry
	a.cmdEmit = func(e cmdlog.Entry) {
		mu.Lock()
		if e.Outcome == cmdlog.OutcomeRunning {
			running = append(running, e)
		}
		mu.Unlock()
	}
	a.started.Store(true)
	done := a.markAIWrite(id)
	err := a.CreateBranch(id, "by-ai-running", "HEAD", false)
	done()
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(running) == 0 || running[len(running)-1].Origin != cmdlog.OriginAI {
		t.Fatalf("running entries: %+v", running)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/app/ -run 'CancelCommand|RunningEntry|AIWriteMarkApplies' -v`
Expected: FAIL — `a.CancelCommand` undefined (and, once added, no running events until the start hook is wired).

- [ ] **Step 3: Implement** — in `internal/app/cmdlog.go`, replace the `EventCommand` comment and `recordGit` with:

```go
// EventCommand carries git commands to the Commands panel: a write when it
// starts (outcome "running") and every command when it ends, the end with
// the same ID as its start.
const EventCommand = "cmdlog:entry"

// beginGit is the recorder's start hook: a write appears in the panel as
// running as soon as git starts.
func (a *App) beginGit(s gitcmd.Start) int64 {
	s.Ctx = a.originContext(s.Ctx, s.Dir, s.Args)
	e, shown := a.cmds.Begin(s)
	if shown {
		a.emitCommand(e)
	}
	return e.ID
}

// recordGit is the recorder's end hook: every command the app runs lands
// in the log, and once the window is up, in the panel.
func (a *App) recordGit(r gitcmd.Record) {
	r.Ctx = a.originContext(r.Ctx, r.Dir, r.Args)
	a.emitCommand(a.cmds.Add(r))
}

// originContext marks ctx as AI for a write run in a repository where an
// approved AI write is running: that write runs through the same App
// methods as the UI, whose ctx does not carry the AI origin (see
// markAIWrite).
func (a *App) originContext(ctx context.Context, dir string, args []string) context.Context {
	if o, ok := cmdlog.OriginFrom(ctx); ok && o == cmdlog.OriginAI {
		return ctx
	}
	if a.aiWriting(cmdlog.RepoKey(dir)) && cmdlog.Classify(args) == cmdlog.KindWrite {
		return cmdlog.WithOrigin(ctx, cmdlog.OriginAI)
	}
	return ctx
}

// emitCommand sends e to the panel. Not through a.emit: the AI deps' Emit
// is what tests watch for chat events, and every git command would flood
// it. Before Startup (and in tests) there is no Wails runtime to emit to.
func (a *App) emitCommand(e cmdlog.Entry) {
	if a.started.Load() && a.cmdEmit != nil {
		a.cmdEmit(e)
	}
}
```

After `ClearCommandLog`, add:

```go
// CancelCommand stops command entryID of repository id as Ctrl+C would;
// cmdlog.ErrNotRunning when it is no longer running.
func (a *App) CancelCommand(id string, entryID int64) error {
	dir, err := a.dir(id)
	if err != nil {
		return err
	}
	return a.cmds.Cancel(dir, entryID)
}
```

In `internal/app/app.go` `New`, replace `gitcmd.SetRecorder(&gitcmd.Recorder{End: a.recordGit})` with:

```go
	gitcmd.SetRecorder(&gitcmd.Recorder{Begin: a.beginGit, End: a.recordGit})
```

- [ ] **Step 4: Run tests**

Run: `go test ./... -race`
Expected: PASS — new tests and every existing one.

- [ ] **Step 5: Regenerate bindings**

Run: `source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && ~/go/bin/wails generate module && git checkout -- frontend/wailsjs/runtime 2>/dev/null || true`
Then remove the unrelated `Menu` binding it adds: in `frontend/wailsjs/go/app/App.d.ts` drop `import {menu} from '../models';` and `export function Menu():Promise<menu.Menu>;`; in `App.js` drop the `Menu()` function; in `frontend/wailsjs/go/models.ts` drop the added `keys` and `menu` namespaces. Check: `git diff frontend/wailsjs` shows only `CancelCommand` added to `App.d.ts` and `App.js`.

- [ ] **Step 6: Commit**

```bash
git add internal/app frontend/wailsjs/go
git commit -m "feat(app): emit writes as they start; CancelCommand binding"
```

---

### Task 4: Commands panel — running rows and Cancel

**Files:**
- Modify: `frontend/src/lib/types.ts`, `frontend/src/lib/api.ts`, `frontend/src/lib/cmdlog.ts`, `frontend/src/lib/cmdlog.test.ts`, `frontend/src/components/CommandLogPanel.svelte`
- Docs: `docs/spec/09-command-log.md`

**Interfaces:**
- Consumes: binding `CancelCommand(id, entryId)`; `cmdlog:entry` with outcome `running` (Task 3).
- Produces:
  ```ts
  // types.ts: CommandEntry.outcome: 'running' | 'ok' | 'failed' | 'timeout' | 'cancelled'
  // api.ts:   cancelCommand(id: string, entryId: number): Promise<void>
  // cmdlog.ts
  export function mergeEntries(list: CommandEntry[], more: CommandEntry[], repoKey: string): CommandEntry[] // now replaces running entries
  export function elapsedMs(e: CommandEntry, now: number): number
  export function formatElapsed(ms: number): string // whole seconds, "0 s", "12 s"
  // outcomeText: 'Running' for running, 'Cancelled' for cancelled
  ```

- [ ] **Step 1: Write the failing tests** — in `frontend/src/lib/cmdlog.test.ts`, add `elapsedMs, formatElapsed` to the import from `./cmdlog`, then append:

```ts
describe('running entries', () => {
  const running = (id: number) => entry(id, { kind: 'write', origin: 'you', args: ['push'], outcome: 'running', durationMs: 0 })
  it('replaces a running entry with its end', () => {
    const list = mergeEntries([], [running(5)], '/r')
    const done = entry(5, { kind: 'write', args: ['push'], outcome: 'ok', durationMs: 900 })
    const next = mergeEntries(list, [done], '/r')
    expect(next).toHaveLength(1)
    expect(next[0].outcome).toBe('ok')
  })
  it('never turns a finished entry back into running', () => {
    const done = entry(5, { kind: 'write', args: ['push'], outcome: 'cancelled' })
    const list = [done]
    expect(mergeEntries(list, [running(5)], '/r')).toBe(list)
  })
  it('ignores a repeated running event', () => {
    const list = mergeEntries([], [running(5)], '/r')
    expect(mergeEntries(list, [running(5)], '/r')).toBe(list)
  })
  it('counts elapsed time in whole seconds', () => {
    const e = running(1)
    const start = Date.parse(e.start)
    expect(elapsedMs(e, start + 2500)).toBe(2500)
    expect(elapsedMs(e, start - 10)).toBe(0)
    expect(formatElapsed(2500)).toBe('2 s')
    expect(formatElapsed(0)).toBe('0 s')
  })
  it('describes running and cancelled', () => {
    expect(outcomeText(running(1))).toBe('Running')
    expect(outcomeText(entry(1, { outcome: 'cancelled', exitCode: -1 }))).toBe('Cancelled')
  })
})
```

- [ ] **Step 2: Run to verify failure**

Run: `cd frontend && npm test -- cmdlog`
Expected: FAIL — `elapsedMs`/`formatElapsed` not exported; the type of `outcome` rejects `'running'`/`'cancelled'`.

- [ ] **Step 3: Implement the helpers**

In `frontend/src/lib/types.ts`, change `outcome: 'ok' | 'failed' | 'timeout'` in `CommandEntry` to:

```ts
  /** 'running' until a write ends; reads are only sent once they end. */
  outcome: 'running' | 'ok' | 'failed' | 'timeout' | 'cancelled'
```

In `frontend/src/lib/api.ts`, after `clearCommandLog`, add:

```ts
  cancelCommand: (id: string, entryId: number) => call<void>(Go.CancelCommand(id, entryId)),
```

In `frontend/src/lib/cmdlog.ts`, replace `mergeEntries` (and its doc comment) with:

```ts
/** mergeEntries adds more to list — only entries whose repo matches the key
 *  the backend returned from CommandLog (repoKey; see CommandLogView),
 *  never a frontend-computed Repo.path. An entry list already holds is
 *  replaced only when it is running and the new one is its end: the initial
 *  load and live events overlap, and a late running event must not bring a
 *  finished row back. Newest first, capped; the same array when nothing
 *  changes. */
export function mergeEntries(list: CommandEntry[], more: CommandEntry[], repoKey: string): CommandEntry[] {
  const byId = new Map(list.map((e) => [e.id, e]))
  let changed = false
  for (const e of more) {
    if (e.repo !== repoKey) continue
    const had = byId.get(e.id)
    if (had && (had.outcome !== 'running' || e.outcome === 'running')) continue
    byId.set(e.id, e)
    changed = true
  }
  if (!changed) return list
  return [...byId.values()].sort((a, b) => b.id - a.id).slice(0, MAX_ENTRIES)
}
```

After `formatDuration`, add:

```ts
/** How long a running command has been running at now (ms since epoch). */
export function elapsedMs(e: CommandEntry, now: number): number {
  return Math.max(0, now - Date.parse(e.start))
}

/** Elapsed time of a running command, in whole seconds: it ticks once a second. */
export function formatElapsed(ms: number): string {
  return `${Math.floor(ms / 1000)} s`
}
```

Replace `outcomeText` with:

```ts
export function outcomeText(e: CommandEntry): string {
  if (e.outcome === 'running') return 'Running'
  if (e.outcome === 'cancelled') return 'Cancelled'
  if (e.outcome === 'timeout') return 'Timed out'
  if (e.outcome === 'failed') return `Failed · exit code ${e.exitCode}`
  return `Exit code ${e.exitCode}`
}
```

- [ ] **Step 4: Run tests**

Run: `cd frontend && npm test -- cmdlog`
Expected: PASS (old and new; the old "returns the same array when nothing is added" still holds: a finished duplicate is not replaced).

- [ ] **Step 5: The panel** — in `frontend/src/components/CommandLogPanel.svelte`:

Change the cmdlog import to:

```ts
  import { commandLine, elapsedMs, emptyMessage, formatClock, formatDuration, formatElapsed, mergeEntries, ORIGIN_LABEL, outcomeText, visibleEntries } from '../lib/cmdlog'
```

After `let list: HTMLElement` add:

```ts
  let cancelling: Record<number, boolean> = {}
  // now ticks once a second while a command is running, for its elapsed time.
  let now = Date.now()
  let timer: ReturnType<typeof setInterval> | undefined
  $: anyRunning = entries.some((e) => e.outcome === 'running')
  $: if (anyRunning && !timer) {
    now = Date.now()
    timer = setInterval(() => (now = Date.now()), 1000)
  } else if (!anyRunning && timer) {
    clearInterval(timer)
    timer = undefined
  }
  onDestroy(() => clearInterval(timer))
```

In `load`, after `expanded = null`, add `cancelling = {}`.

At the top of `toggle`, add:

```ts
    if (e.outcome === 'running') return
```

Replace `clear` with:

```ts
  async function clear() {
    const id = loadedId
    try {
      await api.clearCommandLog(id)
      if (loadedId === id) {
        // The backend keeps running commands: their end is still to come.
        entries = entries.filter((e) => e.outcome === 'running')
        outputs = {}
        outputErrors = {}
        expanded = null
      }
    } catch (e) {
      if (loadedId === id) error = String(e)
    }
  }

  async function cancel(e: CommandEntry) {
    const id = loadedId
    cancelling = { ...cancelling, [e.id]: true }
    try {
      await api.cancelCommand(id, e.id)
    } catch (err) {
      if (loadedId !== id) return
      cancelling = Object.fromEntries(Object.entries(cancelling).filter(([k]) => Number(k) !== e.id))
      // It ended on its own while the click was on its way: nothing to report.
      if (entries.find((x) => x.id === e.id)?.outcome === 'running') error = String(err)
    }
  }
```

Replace the Clear button with:

```svelte
    <button class="btn" disabled={!entries.some((e) => e.outcome !== 'running')} on:click={clear}>Clear</button>
```

Replace the whole `<div class="entry" …>` opening tag and its `<div class="line">…</div>` with:

```svelte
      <div class="entry" role="listitem" class:failed={e.outcome === 'failed' || e.outcome === 'timeout'} class:cancelled={e.outcome === 'cancelled'}>
        <div class="line">
          <button class="main" aria-expanded={e.outcome === 'running' ? undefined : expanded === e.id} title={commandLine(e.args)} on:click={() => toggle(e)}>
            {#if e.outcome === 'running'}
              <span class="mark"><span class="spinner" role="img" aria-label="Running"></span></span>
            {:else}
              <span class="mark" aria-label={e.outcome === 'ok' ? 'Succeeded' : outcomeText(e)}>{e.outcome === 'ok' ? '✓' : e.outcome === 'cancelled' ? '⊘' : '✗'}</span>
            {/if}
            <span class="cmd ellipsis">{commandLine(e.args)}</span>
            <span class="badge {e.origin}">{ORIGIN_LABEL[e.origin]}</span>
            <span class="time">{formatClock(e.start)}</span>
            <span class="dur">{e.outcome === 'running' ? formatElapsed(elapsedMs(e, now)) : formatDuration(e.durationMs)}</span>
          </button>
          {#if e.outcome === 'running'}
            <button class="btn cancel" disabled={cancelling[e.id]} on:click={() => cancel(e)}>Cancel</button>
          {/if}
          <button class="icon-btn" title="Copy command" on:click={() => copyText(commandLine(e.args))}><Icon name="copy" size={14} /></button>
        </div>
```

In `<style>`, replace `.failed .mark { color: var(--danger); }` with:

```css
  .failed .mark { color: var(--danger); }
  .cancelled .mark { color: var(--muted); }
  .spinner { display: inline-block; width: 9px; height: 9px; border: 2px solid var(--border); border-top-color: var(--accent); border-radius: 50%; animation: spin 0.8s linear infinite; }
  @keyframes spin { to { transform: rotate(360deg); } }
  @media (prefers-reduced-motion: reduce) { .spinner { animation: none; } }
  .cancel { flex: none; padding: 1px 8px; font-size: 11px; }
```

- [ ] **Step 6: Spec** — in `docs/spec/09-command-log.md`, in "What it shows", replace the first bullet (`- ✓ or ✗ (a timed-out command is …);`) with:

```markdown
- ✓ or ✗ (a timed-out command is a ✗ that says "Timed out" when expanded),
  ⊘ for a cancelled command ("Cancelled"), or a spinner while it runs;
```

replace `- the time it started (HH:MM:SS) and how long it took.` with:

```markdown
- the time it started (HH:MM:SS) and how long it took — while it runs, the
  seconds elapsed so far.
```

and before "## Retention and privacy" add:

```markdown
## Running commands and Cancel

A write appears as soon as git starts, with a spinner and its elapsed time
counting every second; when it ends the same row shows how it ended. Reads
appear only when they end, whether or not **Show reads** is ticked. A
running row cannot be expanded (there is no live output) and has a
**Cancel** button: it stops the command as Ctrl+C would in a terminal — git
and any hook or helper it started get an interrupt, and whatever is still
alive five seconds later is killed (on Windows the command is killed at
once). The operation that ran it reports an error ("git command cancelled")
the way it reports any failure — a toast for an action from the interface,
a failed change for the AI chat. An interrupted rebase, merge or
cherry-pick stays in progress and is continued or aborted from its banner.
A command that runs past its time limit is interrupted the same way and
shows "Timed out". **Clear** leaves running commands in place.
```

- [ ] **Step 7: Check, test, build**

Run: `source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && cd frontend && npm test && npm run check && npm run build`
Expected: all tests PASS; svelte-check 0 errors, only the 2 pre-existing warnings; build succeeds.

- [ ] **Step 8: Commit**

```bash
git add frontend/src docs/spec/09-command-log.md
git commit -m "feat(ui): running writes in the Commands panel with elapsed time and Cancel"
```

---

### Task 5: Busy notice moves to the bottom dock

**Files:**
- Modify: `frontend/src/components/BottomDock.svelte`, `frontend/src/components/TerminalPanel.svelte`
- Docs: `docs/spec/08-terminal.md`

**Interfaces:**
- Consumes: `busy` store (`frontend/src/lib/stores.ts`, a string label, `''` when idle).
- Produces: nothing new.

- [ ] **Step 1: BottomDock** — in `frontend/src/components/BottomDock.svelte`:
  - change the stores import to `import { busy, commandsOpen, dockSplit, terminalOpen } from '../lib/stores'`;
  - wrap the existing `<div class="dock" bind:clientWidth={width}>…</div>` (keep its content unchanged) so the markup reads:

```svelte
<div class="wrap">
  {#if $busy}<div class="busy ellipsis" title="Operation running: {$busy}">Operation running: {$busy}</div>{/if}
  <div class="dock" bind:clientWidth={width}>
    <!-- existing terminal / splitter / commands markup, unchanged -->
  </div>
</div>
```

  - in `<style>`, replace `.dock { display: flex; height: 100%; min-height: 0; }` with:

```css
  .wrap { display: flex; flex-direction: column; height: 100%; min-height: 0; }
  .busy { flex: none; padding: 2px 8px; font-size: 11px; color: var(--muted); background: var(--surface); border-bottom: 1px solid var(--border); }
  .dock { flex: 1; display: flex; min-height: 0; }
```

- [ ] **Step 2: TerminalPanel** — in `frontend/src/components/TerminalPanel.svelte`:
  - delete the line `{#if $busy}<span class="busy ellipsis">Operation running: {$busy}</span>{/if}`;
  - delete the style rule `.busy { font-size: 11px; color: var(--muted); max-width: 40%; }`;
  - remove `busy, ` from the `../lib/stores` import (it has no other use in this file — confirm with `grep -n busy frontend/src/components/TerminalPanel.svelte`, which must print nothing afterwards).

- [ ] **Step 3: Spec** — in `docs/spec/08-terminal.md`, replace the paragraph under "## Busy notice" with:

```markdown
While an application operation (a commit, a pull, a merge, and so on) is
running, the bottom dock (see Layout and visibility) shows a thin strip at
its top, "Operation running: `<label>`", using the same label the rest of
the interface already shows for that operation — `busy` is a single,
application-wide label (see Conventions and constraints), not one scoped to
the selected repository, so the notice shows regardless of which repository
the operation targets. It shows whenever the dock is open, with the
terminal, the Commands panel (see `09-command-log.md`) or both. This is
informational only — see Safety below for why it does not stop the user
from typing.
```

- [ ] **Step 4: Check, test, build**

Run: `source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && cd frontend && npm test && npm run check && npm run build`
Expected: all tests PASS; svelte-check 0 errors, only the 2 pre-existing warnings; build succeeds.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components docs/spec/08-terminal.md
git commit -m "feat(ui): the Operation running notice moves from the terminal header to the bottom dock"
```

---

### Task 6: Build, run, and check by hand

**Files:** none (fixes found here go in their own commit)

- [ ] **Step 1: Full test run**

Run: `go test ./... -race && (source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && cd frontend && npm test)`
Expected: PASS.

- [ ] **Step 2: Run the app** — `make dev` (Node 22), select `/tmp/git-ui-demo` (recreate it with a few commits if missing).

- [ ] **Step 3: Check, in order**
  1. Open Commands. In the terminal: `git remote add slow https://10.255.255.1/r.git` (a non-routable address hangs). Push to `slow` from the UI → a running row with a spinner, the elapsed seconds counting, **Cancel**.
  2. Click Cancel → within a few seconds the row shows ⊘, expanded "Cancelled"; the push's toast reports "git command cancelled".
  3. With only the Terminal open (Commands closed), start the push again → "Operation running: …" strip at the top of the dock; the terminal header no longer shows it. With only Commands open → the strip shows there too. With the dock closed → no strip.
  4. Tick and untick Show reads while a push runs → the running row stays; no running reads ever appear.
  5. Commit with a slow `pre-commit` hook (`printf '#!/bin/sh\nsleep 20\n' > .git/hooks/pre-commit && chmod +x .git/hooks/pre-commit`), cancel it → ⊘, no commit, `git status` works (no `index.lock`). Remove the hook afterwards.
  6. Clear while a push runs → finished rows go, the running one stays and ends normally.
  7. Light, dark and high contrast: spinner, ⊘ and the Cancel button readable.

- [ ] **Step 4: Commit any fixes**, each with a message naming what it fixes.
