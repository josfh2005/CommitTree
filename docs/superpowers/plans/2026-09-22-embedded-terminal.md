# Embedded Terminal Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A real pty shell, with several tabs per repository, in a split below the chat panel, that keeps the app's views fresh after commands typed into it.

**Architecture:** A Wails-free Go package `internal/terminal` owns the pty sessions (batching output, detecting "output settled after Enter", killing process groups). `internal/app/terminal.go` binds it to the frontend and emits `terminal:*` events through the existing `a.emit`. The frontend keeps per-repo tab state in a pure, tested module (`lib/terminal.ts`) and renders one xterm.js instance per tab in a `TerminalPanel` under `ChatPanel`.

**Tech Stack:** Go 1.26, `github.com/creack/pty`, Wails v2, Svelte 5 (legacy `$:` syntax as in the rest of the app), `@xterm/xterm`, `@xterm/addon-fit`, vitest.

**Spec:** `docs/superpowers/specs/2026-09-22-embedded-terminal-design.md` — read it before starting any task.

## Global Constraints

- macOS and Linux only. No Windows code paths.
- Go dependency: `github.com/creack/pty` only. No `golang.org/x/sys` import (use `syscall`).
- Frontend dependencies: `@xterm/xterm` and `@xterm/addon-fit` only.
- Output flush interval 16 ms; settle delay 400 ms; kill grace 2 s; xterm scrollback 5 000 lines.
- Shell: `$SHELL -l`; empty `$SHELL` → `/bin/zsh` on darwin, `/bin/sh` elsewhere. Env adds `TERM=xterm-256color`, `COLORTERM=truecolor`.
- Event names exactly `terminal:data` `{tab, data}`, `terminal:settled` `{tab, repo}`, `terminal:exit` `{tab, code}`.
- Tab label: `<shell basename> <n>`, `n` per repository, never reused while the app runs; exited: `<label> — exited (<code>)`.
- The AI never reads from or writes to a terminal. Nothing in `internal/ai` may import `internal/terminal`.
- Every behaviour change updates `docs/spec/` in the same commit (Task 4 carries the spec, because Tasks 1–3 add nothing a user can reach).
- Commit messages: conventional style (`feat(terminal): …`). **Never** add a `Co-Authored-By` line.
- Run Go tests with `go test ./...`; frontend with `cd frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npm test && npm run check`.

## Review Focus

1. **UTF-8 split across a read boundary** (e.g. `ñ`, emoji in `git log`) must arrive whole, never as U+FFFD — Task 1 test `TestFlushKeepsSplitRune`.
2. **Closing a tab while a foreground program runs** (`sleep 100`, `vim`) must kill it, not leave an orphan — Task 1 test `TestCloseKillsForegroundJob`.
3. **Removing a repository with open shells** must close them; the frontend must drop its tabs — Task 2 `TestRemoveRepoClosesTerminals`, Task 3 `removeRepoTabs` test.
4. **A settle event for a repository that is not selected** must not reload the selected repository's views — Task 3 `settledAction` test.
5. **Ctrl+` on a non-US keyboard** (Spanish layout, where backtick is a dead key) must still toggle — Task 4 matches on `event.code === 'Backquote'`, not `event.key`.

---

### Task 1: `internal/terminal` — pty sessions

**Files:**
- Create: `internal/terminal/terminal.go`
- Create: `internal/terminal/terminal_test.go`
- Modify: `go.mod`, `go.sum` (via `go get github.com/creack/pty@latest`)

**Interfaces:**
- Consumes: nothing.
- Produces:
  ```go
  type Callbacks struct {
      OnData    func(tab, data string)
      OnSettled func(tab, repoID string)
      OnExit    func(tab string, code int)
  }
  var ErrUnknownTab = errors.New("unknown terminal tab")
  var ErrExited     = errors.New("terminal has exited")
  func DefaultShell() string
  func NewManager(cb Callbacks) *Manager
  func (m *Manager) Shell() string
  func (m *Manager) Open(repoID, dir string, cols, rows int) (string, error)
  func (m *Manager) Write(tab, data string) error
  func (m *Manager) Resize(tab string, cols, rows int) error
  func (m *Manager) Close(tab string) error
  func (m *Manager) CloseRepo(repoID string)
  func (m *Manager) CloseAll()
  ```

- [ ] **Step 1: Add the dependency**

Run: `go get github.com/creack/pty@latest`
Expected: `go.mod` gains `github.com/creack/pty vX.Y.Z` in the direct `require` block.

- [ ] **Step 2: Write the failing tests**

`internal/terminal/terminal_test.go`:

```go
package terminal

import (
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// recorder collects callbacks. Output is accumulated so tests can wait for
// a substring regardless of how the reader batched it.
type recorder struct {
	mu      sync.Mutex
	out     map[string]*strings.Builder
	settled chan string
	exited  chan int
}

func newRecorder() *recorder {
	return &recorder{out: map[string]*strings.Builder{}, settled: make(chan string, 16), exited: make(chan int, 16)}
}

func (r *recorder) callbacks() Callbacks {
	return Callbacks{
		OnData: func(tab, data string) {
			r.mu.Lock()
			defer r.mu.Unlock()
			if r.out[tab] == nil {
				r.out[tab] = &strings.Builder{}
			}
			r.out[tab].WriteString(data)
		},
		OnSettled: func(tab, repoID string) { r.settled <- repoID },
		OnExit:    func(tab string, code int) { r.exited <- code },
	}
}

func (r *recorder) output(tab string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.out[tab] == nil {
		return ""
	}
	return r.out[tab].String()
}

func (r *recorder) waitFor(t *testing.T, tab, substr string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if s := r.output(tab); strings.Contains(s, substr) {
			return s
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("output never contained %q; got %q", substr, r.output(tab))
	return ""
}

func newTestManager(t *testing.T) (*Manager, *recorder) {
	t.Helper()
	r := newRecorder()
	m := NewManager(r.callbacks())
	m.shell = "/bin/sh"
	t.Cleanup(m.CloseAll)
	return m, r
}

func TestOpenRunsInDirAndStreamsOutput(t *testing.T) {
	m, r := newTestManager(t)
	dir := t.TempDir()
	tab, err := m.Open("repo1", dir, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Write(tab, "pwd; echo done-$((1+1))\r"); err != nil {
		t.Fatal(err)
	}
	out := r.waitFor(t, tab, "done-2")
	// macOS TempDir lives under /var → /private/var; compare the tail.
	if !strings.Contains(out, dir[strings.LastIndex(dir, "/"):]) {
		t.Fatalf("pwd not in %s: %q", dir, out)
	}
}

func TestSettleFiresOnceAfterEnter(t *testing.T) {
	m, r := newTestManager(t)
	tab, err := m.Open("repo1", t.TempDir(), 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	r.waitFor(t, tab, "$") // prompt printed; nothing armed yet
	select {
	case <-r.settled:
		t.Fatal("settled without Enter")
	case <-time.After(700 * time.Millisecond):
	}
	m.Write(tab, "echo hi\r")
	select {
	case repo := <-r.settled:
		if repo != "repo1" {
			t.Fatalf("repo = %q", repo)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("never settled")
	}
	select {
	case <-r.settled:
		t.Fatal("settled twice for one Enter")
	case <-time.After(700 * time.Millisecond):
	}
}

func TestTypingWithoutEnterDoesNotSettle(t *testing.T) {
	m, r := newTestManager(t)
	tab, _ := m.Open("repo1", t.TempDir(), 80, 24)
	r.waitFor(t, tab, "$")
	m.Write(tab, "echo not yet")
	select {
	case <-r.settled:
		t.Fatal("settled without Enter")
	case <-time.After(700 * time.Millisecond):
	}
}

func TestExitReportsCodeAndRejectsWrites(t *testing.T) {
	m, r := newTestManager(t)
	tab, _ := m.Open("repo1", t.TempDir(), 80, 24)
	m.Write(tab, "exit 3\r")
	select {
	case code := <-r.exited:
		if code != 3 {
			t.Fatalf("code = %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no exit")
	}
	if err := m.Write(tab, "x"); err != ErrExited {
		t.Fatalf("write after exit: %v", err)
	}
	if err := m.Close(tab); err != nil {
		t.Fatalf("close after exit: %v", err)
	}
	if err := m.Write(tab, "x"); err != ErrUnknownTab {
		t.Fatalf("write after close: %v", err)
	}
}

func TestCloseKillsForegroundJob(t *testing.T) {
	m, r := newTestManager(t)
	tab, _ := m.Open("repo1", t.TempDir(), 80, 24)
	// Print the pid of a foreground sleep: exec replaces the subshell, so
	// $$ inside it is the sleep's own pid.
	// The echoed command line also contains "PID=", so match digits only.
	m.Write(tab, "sh -c 'echo PID=$$; exec sleep 100'\r")
	pidRe := regexp.MustCompile(`PID=(\d+)`)
	var pid int
	deadline := time.Now().Add(5 * time.Second)
	for pid == 0 && time.Now().Before(deadline) {
		if match := pidRe.FindStringSubmatch(r.output(tab)); match != nil {
			pid, _ = strconv.Atoi(match[1])
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatalf("no pid in %q", r.output(tab))
	}
	if err := m.Close(tab); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if syscall.Kill(pid, 0) == syscall.ESRCH {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("sleep %d still alive after Close", pid)
}

func TestCloseRepoOnlyClosesThatRepo(t *testing.T) {
	m, _ := newTestManager(t)
	a, _ := m.Open("repoA", t.TempDir(), 80, 24)
	b, _ := m.Open("repoB", t.TempDir(), 80, 24)
	m.CloseRepo("repoA")
	if err := m.Write(a, "x"); err != ErrUnknownTab {
		t.Fatalf("repoA tab: %v", err)
	}
	if err := m.Write(b, "x"); err != nil {
		t.Fatalf("repoB tab: %v", err)
	}
}

func TestResizeUnknownTab(t *testing.T) {
	m, _ := newTestManager(t)
	if err := m.Resize("nope", 80, 24); err != ErrUnknownTab {
		t.Fatalf("got %v", err)
	}
}

func TestFlushKeepsSplitRune(t *testing.T) {
	full := []byte("añb😀")
	for cut := 0; cut <= len(full); cut++ {
		head, rest := splitUTF8(full[:cut])
		joined := string(head) + string(append(rest, full[cut:]...))
		if joined != string(full) {
			t.Fatalf("cut %d: %q", cut, joined)
		}
		if !utf8Valid(head) {
			t.Fatalf("cut %d: head %q not valid", cut, head)
		}
	}
}

func TestDefaultShellFallback(t *testing.T) {
	t.Setenv("SHELL", "")
	if s := DefaultShell(); s != "/bin/zsh" && s != "/bin/sh" {
		t.Fatalf("got %q", s)
	}
	t.Setenv("SHELL", "/usr/local/bin/fish")
	if s := DefaultShell(); s != "/usr/local/bin/fish" {
		t.Fatalf("got %q", s)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/terminal/`
Expected: FAIL — compile errors (`NewManager`, `splitUTF8`, `utf8Valid` undefined).

- [ ] **Step 4: Implement**

`internal/terminal/terminal.go`:

```go
// Package terminal runs interactive shells on pseudo-terminals for the
// embedded terminal panel. It knows nothing about Wails: output, settle and
// exit are reported through Callbacks, and the app layer turns them into
// events.
package terminal

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
	"unsafe"

	"github.com/creack/pty"
)

const (
	flushEvery  = 16 * time.Millisecond
	settleAfter = 400 * time.Millisecond
	killAfter   = 2 * time.Second
)

var (
	ErrUnknownTab = errors.New("unknown terminal tab")
	ErrExited     = errors.New("terminal has exited")
)

// Callbacks receive a session's output, the moment its output settles after
// the user pressed Enter, and its exit code. They are called from the
// session's own goroutines and must not block for long.
type Callbacks struct {
	OnData    func(tab, data string)
	OnSettled func(tab, repoID string)
	OnExit    func(tab string, code int)
}

type session struct {
	id, repoID string
	cmd        *exec.Cmd
	pty        *os.File
	done       chan struct{} // closed once the shell has been reaped

	mu       sync.Mutex
	buf      []byte
	flushing bool
	armed    bool
	exited   bool
	settle   *time.Timer
}

type Manager struct {
	cb    Callbacks
	shell string

	mu   sync.Mutex
	tabs map[string]*session
	next int
}

// DefaultShell is $SHELL, or the platform's default when it is unset.
func DefaultShell() string {
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	if runtime.GOOS == "darwin" {
		return "/bin/zsh"
	}
	return "/bin/sh"
}

func NewManager(cb Callbacks) *Manager {
	return &Manager{cb: cb, shell: DefaultShell(), tabs: map[string]*session{}}
}

// Shell is the basename of the shell new tabs run, for tab labels.
func (m *Manager) Shell() string { return filepath.Base(m.shell) }

// Open starts a login shell in dir on a new pty and returns its tab ID.
func (m *Manager) Open(repoID, dir string, cols, rows int) (string, error) {
	cmd := exec.Command(m.shell, "-l")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor")
	// StartWithSize makes the shell a session leader with the pty as its
	// controlling terminal, so it is also its own process group.
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		return "", fmt.Errorf("start %s: %w", m.shell, err)
	}
	m.mu.Lock()
	m.next++
	s := &session{id: fmt.Sprintf("t%d", m.next), repoID: repoID, cmd: cmd, pty: f, done: make(chan struct{})}
	m.tabs[s.id] = s
	m.mu.Unlock()

	s.settle = time.AfterFunc(time.Hour, func() { m.fireSettle(s) })
	s.settle.Stop()
	go m.read(s)
	return s.id, nil
}

func (m *Manager) get(tab string) (*session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.tabs[tab]
	if !ok {
		return nil, ErrUnknownTab
	}
	return s, nil
}

// Write sends keystrokes to the shell. A carriage return arms the settle
// detector: once output then stays quiet for settleAfter, OnSettled fires.
func (m *Manager) Write(tab, data string) error {
	s, err := m.get(tab)
	if err != nil {
		return err
	}
	s.mu.Lock()
	if s.exited {
		s.mu.Unlock()
		return ErrExited
	}
	if strings.ContainsRune(data, '\r') {
		s.armed = true
		s.settle.Reset(settleAfter)
	}
	s.mu.Unlock()
	_, err = s.pty.Write([]byte(data))
	return err
}

func (m *Manager) Resize(tab string, cols, rows int) error {
	s, err := m.get(tab)
	if err != nil {
		return err
	}
	return pty.Setsize(s.pty, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

// Close ends a tab: SIGHUP to the shell's group and the terminal's
// foreground group, close the pty, and SIGKILL both if the shell outlives
// killAfter. Closing an exited tab just forgets it.
func (m *Manager) Close(tab string) error {
	m.mu.Lock()
	s, ok := m.tabs[tab]
	delete(m.tabs, tab)
	m.mu.Unlock()
	if !ok {
		return ErrUnknownTab
	}
	s.settle.Stop()
	groups := []int{s.cmd.Process.Pid}
	if fg := foregroundGroup(s.pty); fg > 0 && fg != groups[0] {
		groups = append(groups, fg)
	}
	for _, g := range groups {
		syscall.Kill(-g, syscall.SIGHUP)
	}
	s.pty.Close()
	select {
	case <-s.done:
	case <-time.After(killAfter):
		for _, g := range groups {
			syscall.Kill(-g, syscall.SIGKILL)
		}
		<-s.done
	}
	return nil
}

func (m *Manager) CloseRepo(repoID string) {
	for _, id := range m.ids(func(s *session) bool { return s.repoID == repoID }) {
		m.Close(id)
	}
}

func (m *Manager) CloseAll() {
	for _, id := range m.ids(func(*session) bool { return true }) {
		m.Close(id)
	}
}

func (m *Manager) ids(keep func(*session) bool) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for id, s := range m.tabs {
		if keep(s) {
			out = append(out, id)
		}
	}
	return out
}

// foregroundGroup reads the pty's foreground process group without calling
// Fd(), which would switch the file to blocking mode and stop Close from
// unblocking the reader.
func foregroundGroup(f *os.File) int {
	rc, err := f.SyscallConn()
	if err != nil {
		return 0
	}
	var pgrp int32
	rc.Control(func(fd uintptr) {
		syscall.Syscall(syscall.SYS_IOCTL, fd, uintptr(syscall.TIOCGPGRP), uintptr(unsafe.Pointer(&pgrp)))
	})
	return int(pgrp)
}

func (m *Manager) read(s *session) {
	chunk := make([]byte, 32*1024)
	for {
		n, err := s.pty.Read(chunk)
		if n > 0 {
			s.mu.Lock()
			s.buf = append(s.buf, chunk[:n]...)
			if !s.flushing {
				s.flushing = true
				time.AfterFunc(flushEvery, func() { m.flush(s) })
			}
			if s.armed {
				s.settle.Reset(settleAfter)
			}
			s.mu.Unlock()
		}
		if err != nil {
			break
		}
	}
	m.flush(s)
	s.cmd.Wait()
	code := -1
	if s.cmd.ProcessState != nil {
		code = s.cmd.ProcessState.ExitCode()
	}
	s.mu.Lock()
	s.exited = true
	s.mu.Unlock()
	close(s.done)
	m.cb.OnExit(s.id, code)
}

func (m *Manager) flush(s *session) {
	s.mu.Lock()
	head, rest := splitUTF8(s.buf)
	s.buf = append([]byte(nil), rest...)
	s.flushing = false
	s.mu.Unlock()
	if len(head) > 0 {
		m.cb.OnData(s.id, strings.ToValidUTF8(string(head), "�"))
	}
}

func (m *Manager) fireSettle(s *session) {
	s.mu.Lock()
	armed := s.armed
	s.armed = false
	s.mu.Unlock()
	if armed {
		m.cb.OnSettled(s.id, s.repoID)
	}
}

// splitUTF8 splits b into a prefix ending on a rune boundary and the bytes
// of a trailing rune that is not complete yet.
func splitUTF8(b []byte) (head, rest []byte) {
	for i := len(b) - 1; i >= 0 && i >= len(b)-utf8.UTFMax; i-- {
		if utf8.RuneStart(b[i]) {
			if !utf8.FullRune(b[i:]) {
				return b[:i], b[i:]
			}
			break
		}
	}
	return b, nil
}

func utf8Valid(b []byte) bool { return utf8.Valid(b) }
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test -race -count=3 ./internal/terminal/`
Expected: PASS all three runs. If `TestCloseKillsForegroundJob` is flaky, the foreground-group SIGHUP is the thing to debug (superpowers:systematic-debugging), not the timeout.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/terminal
git commit -m "feat(terminal): run shells on a pty with batched output and settle detection"
```

---

### Task 2: App bindings, shutdown and repository removal

**Files:**
- Create: `internal/app/terminal.go`
- Create: `internal/app/terminal_test.go`
- Modify: `internal/app/app.go` (App struct, `New`, `RemoveRepo`)
- Modify: `main.go` (`OnShutdown`)
- Regenerate: `frontend/wailsjs/go/app/App.d.ts`, `App.js` (via `~/go/bin/wails generate module`)

**Interfaces:**
- Consumes: Task 1's `terminal.Manager`, `terminal.Callbacks`.
- Produces (Wails bindings, callable from `frontend/wailsjs/go/app/App`):
  ```go
  func (a *App) TerminalOpen(repoID string, cols, rows int) (string, error)
  func (a *App) TerminalWrite(tab, data string) error
  func (a *App) TerminalResize(tab string, cols, rows int) error
  func (a *App) TerminalClose(tab string) error
  func (a *App) TerminalShell() string
  func (a *App) Shutdown(ctx context.Context)
  var ErrRepoMissing = errors.New("repository folder is missing")
  ```
  Events: `terminal:data` `TerminalData{Tab, Data}`, `terminal:settled` `TerminalSettled{Tab, Repo}`, `terminal:exit` `TerminalExit{Tab, Code}` — JSON keys `tab`, `data`, `repo`, `code`.

- [ ] **Step 1: Write the failing tests**

`internal/app/terminal_test.go` (uses `newPlainApp` from `remote_test.go` and `events` from `ai_test.go` — read both helpers first; `newPlainApp` wires `WithAI(a, AIDeps{Emit: …})`, which is what keeps `a.emit` off the Wails runtime in tests):

```go
package app

import (
	"errors"
	"os"
	"testing"

	"git-ui/internal/repos"
	"git-ui/internal/terminal"
)

func TestTerminalOpenUnknownRepo(t *testing.T) {
	a, _, _ := newPlainApp(t)
	if _, err := a.TerminalOpen("nope", 80, 24); !errors.Is(err, repos.ErrUnknownRepo) {
		t.Fatalf("got %v", err)
	}
}

func TestTerminalOpenMissingRepo(t *testing.T) {
	a, r, id := newPlainApp(t)
	if err := os.RemoveAll(r.Dir); err != nil {
		t.Fatal(err)
	}
	if _, err := a.TerminalOpen(id, 80, 24); !errors.Is(err, ErrRepoMissing) {
		t.Fatalf("got %v", err)
	}
}

func TestRemoveRepoClosesTerminals(t *testing.T) {
	a, _, id := newPlainApp(t)
	t.Cleanup(func() { a.Shutdown(nil) })
	tab, err := a.TerminalOpen(id, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.RemoveRepo(id); err != nil {
		t.Fatal(err)
	}
	if err := a.TerminalWrite(tab, "x"); !errors.Is(err, terminal.ErrUnknownTab) {
		t.Fatalf("tab still open: %v", err)
	}
}

func TestTerminalShellIsBasename(t *testing.T) {
	a, _, _ := newPlainApp(t)
	if s := a.TerminalShell(); s == "" || s[0] == '/' {
		t.Fatalf("got %q", s)
	}
}
```

Note: the `events` helper's channel holds 1000 events and `emit` blocks when it is full. These tests produce a few dozen at most; do not add a test that streams large output through `newPlainApp`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/app/ -run 'Terminal|RemoveRepoCloses'`
Expected: FAIL — `TerminalOpen`, `ErrRepoMissing`, `Shutdown` undefined.

- [ ] **Step 3: Implement**

In `internal/app/app.go`, add the field to `App` (after `owedDrops`):

```go
	term            *terminal.Manager
```

and import `"git-ui/internal/terminal"`. Change `New`:

```go
func New(store *repos.Store) *App {
	a := &App{ctx: context.Background(), store: store, logs: map[string]*logState{}}
	a.term = terminal.NewManager(terminal.Callbacks{
		OnData:    func(tab, data string) { a.emit("terminal:data", TerminalData{tab, data}) },
		OnSettled: func(tab, repo string) { a.emit("terminal:settled", TerminalSettled{tab, repo}) },
		OnExit:    func(tab string, code int) { a.emit("terminal:exit", TerminalExit{tab, code}) },
	})
	return a
}
```

Change `RemoveRepo`:

```go
func (a *App) RemoveRepo(id string) error {
	a.forgetLog(id)
	a.term.CloseRepo(id)
	return a.store.Remove(id)
}
```

`internal/app/terminal.go`:

```go
package app

import (
	"context"
	"errors"
	"os"
)

// The embedded terminal. It is deliberately not an app operation: it does
// not go through a.write, so a command typed there can race an app
// operation on the same repository — the same exposure an external terminal
// has. Git's own index and ref locks turn such a race into a git error, not
// a corrupted repository. See docs/spec/08-terminal.md.

var ErrRepoMissing = errors.New("repository folder is missing")

type TerminalData struct {
	Tab  string `json:"tab"`
	Data string `json:"data"`
}

type TerminalSettled struct {
	Tab  string `json:"tab"`
	Repo string `json:"repo"`
}

type TerminalExit struct {
	Tab  string `json:"tab"`
	Code int    `json:"code"`
}

// TerminalOpen starts a shell at the repository's root and returns its tab.
func (a *App) TerminalOpen(repoID string, cols, rows int) (string, error) {
	dir, err := a.dir(repoID)
	if err != nil {
		return "", err
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return "", ErrRepoMissing
	}
	return a.term.Open(repoID, dir, cols, rows)
}

func (a *App) TerminalWrite(tab, data string) error { return a.term.Write(tab, data) }

func (a *App) TerminalResize(tab string, cols, rows int) error {
	return a.term.Resize(tab, cols, rows)
}

func (a *App) TerminalClose(tab string) error { return a.term.Close(tab) }

// TerminalShell is the basename of the shell new tabs run, for tab labels.
func (a *App) TerminalShell() string { return a.term.Shell() }

// Shutdown is Wails' OnShutdown hook: no shell outlives the app.
func (a *App) Shutdown(ctx context.Context) { a.term.CloseAll() }
```

In `main.go`, add to `options.App` after `OnStartup`:

```go
		OnShutdown:       api.Shutdown,
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/app/ ./internal/terminal/ && go vet ./...`
Expected: PASS, no vet findings.

- [ ] **Step 5: Regenerate the bindings**

Run: `~/go/bin/wails generate module`
Expected: `frontend/wailsjs/go/app/App.d.ts` now declares `TerminalOpen(arg1:string,arg2:number,arg3:number):Promise<string>`, `TerminalWrite`, `TerminalResize`, `TerminalClose`, `TerminalShell():Promise<string>`, `Shutdown`. Run `git checkout -- frontend/wailsjs/runtime 2>/dev/null || true` afterwards (the Makefile does the same: the runtime files must not churn).

- [ ] **Step 6: Commit**

```bash
git add internal/app main.go frontend/wailsjs/go
git commit -m "feat(terminal): bind terminal sessions and close them with their repository"
```

---

### Task 3: Frontend tab state, API and the settle rule

**Files:**
- Create: `frontend/src/lib/terminal.ts`
- Create: `frontend/src/lib/terminal.test.ts`
- Modify: `frontend/src/lib/api.ts` (terminal calls)
- Modify: `frontend/src/lib/stores.ts` (`terminalOpen`, `terminalHeight`)
- Modify: `frontend/src/lib/actions.ts:292-334` (extract `checkExternalChanges`)

**Interfaces:**
- Consumes: Task 2 bindings.
- Produces:
  ```ts
  // lib/terminal.ts
  export interface TermTab { id: string; repoId: string; label: string; exitCode: number | null }
  export interface TermState { tabs: TermTab[]; counters: Record<string, number>; active: Record<string, string> }
  export const emptyTermState: () => TermState
  export function addTab(s: TermState, repoId: string, id: string, shell: string): TermState
  export function removeTab(s: TermState, id: string): TermState
  export function markExited(s: TermState, id: string, code: number): TermState
  export function removeRepoTabs(s: TermState, repoId: string): TermState
  export function tabsFor(s: TermState, repoId: string): TermTab[]
  export function setActive(s: TermState, repoId: string, id: string): TermState
  export function tabTitle(t: TermTab): string
  export function settledAction(selectedRepoId: string, repo: string): 'check' | 'list'
  export const terminalState: Writable<TermState>
  // lib/stores.ts
  export const terminalOpen: Writable<boolean>   // persisted 'terminalOpen', false
  export const terminalHeight: Writable<number>  // persisted 'terminalHeight', 260
  // lib/actions.ts
  export async function checkExternalChanges(): Promise<void>
  // lib/api.ts
  api.terminalOpen(repoId, cols, rows): Promise<string>
  api.terminalWrite(tab, data): Promise<void>
  api.terminalResize(tab, cols, rows): Promise<void>
  api.terminalClose(tab): Promise<void>
  api.terminalShell(): Promise<string>
  ```

- [ ] **Step 1: Write the failing tests**

`frontend/src/lib/terminal.test.ts`:

```ts
import { describe, expect, it } from 'vitest'
import { addTab, emptyTermState, markExited, removeRepoTabs, removeTab, setActive, settledAction, tabTitle, tabsFor } from './terminal'

describe('terminal tabs', () => {
  it('numbers tabs per repository and makes the new one active', () => {
    let s = addTab(emptyTermState(), 'r1', 't1', 'zsh')
    s = addTab(s, 'r1', 't2', 'zsh')
    s = addTab(s, 'r2', 't3', 'zsh')
    expect(tabsFor(s, 'r1').map((t) => t.label)).toEqual(['zsh 1', 'zsh 2'])
    expect(tabsFor(s, 'r2').map((t) => t.label)).toEqual(['zsh 1'])
    expect(s.active).toEqual({ r1: 't2', r2: 't3' })
  })

  it('never reuses a number after a tab is closed', () => {
    let s = addTab(emptyTermState(), 'r1', 't1', 'zsh')
    s = addTab(s, 'r1', 't2', 'zsh')
    s = removeTab(s, 't2')
    s = addTab(s, 'r1', 't3', 'zsh')
    expect(tabsFor(s, 'r1').map((t) => t.label)).toEqual(['zsh 1', 'zsh 3'])
  })

  it('moves the active tab to a neighbour when the active one closes', () => {
    let s = addTab(emptyTermState(), 'r1', 't1', 'zsh')
    s = addTab(s, 'r1', 't2', 'zsh')
    s = addTab(s, 'r1', 't3', 'zsh')
    s = setActive(s, 'r1', 't2')
    s = removeTab(s, 't2')
    expect(s.active.r1).toBe('t1')
    s = removeTab(s, 't1')
    expect(s.active.r1).toBe('t3')
    s = removeTab(s, 't3')
    expect(s.active.r1).toBeUndefined()
  })

  it('keeps an exited tab and titles it with its code', () => {
    let s = addTab(emptyTermState(), 'r1', 't1', 'zsh')
    s = markExited(s, 't1', 0)
    expect(tabTitle(tabsFor(s, 'r1')[0])).toBe('zsh 1 — exited (0)')
  })

  it('ignores events for unknown tabs', () => {
    const s = addTab(emptyTermState(), 'r1', 't1', 'zsh')
    expect(markExited(s, 'nope', 1)).toBe(s)
    expect(removeTab(s, 'nope')).toBe(s)
  })

  it('drops every tab of a removed repository', () => {
    let s = addTab(emptyTermState(), 'r1', 't1', 'zsh')
    s = addTab(s, 'r2', 't2', 'zsh')
    s = removeRepoTabs(s, 'r1')
    expect(s.tabs.map((t) => t.id)).toEqual(['t2'])
    expect(s.active.r1).toBeUndefined()
  })
})

describe('settledAction', () => {
  it('runs the full check only for the selected repository', () => {
    expect(settledAction('r1', 'r1')).toBe('check')
    expect(settledAction('r1', 'r2')).toBe('list')
    expect(settledAction('', 'r2')).toBe('list')
  })
})
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npx vitest run src/lib/terminal.test.ts`
Expected: FAIL — cannot resolve `./terminal`.

- [ ] **Step 3: Implement `lib/terminal.ts`**

```ts
import { writable } from 'svelte/store'

/** One shell tab. Tabs belong to a repository; only the selected
 *  repository's tabs are shown, the rest keep running in the background. */
export interface TermTab {
  id: string
  repoId: string
  label: string
  /** null while the shell runs. */
  exitCode: number | null
}

export interface TermState {
  tabs: TermTab[]
  /** Last number handed out per repository — never reused while the app runs. */
  counters: Record<string, number>
  /** Active tab per repository. */
  active: Record<string, string>
}

export const emptyTermState = (): TermState => ({ tabs: [], counters: {}, active: {} })

export function addTab(s: TermState, repoId: string, id: string, shell: string): TermState {
  const n = (s.counters[repoId] ?? 0) + 1
  return {
    tabs: [...s.tabs, { id, repoId, label: `${shell} ${n}`, exitCode: null }],
    counters: { ...s.counters, [repoId]: n },
    active: { ...s.active, [repoId]: id },
  }
}

export function removeTab(s: TermState, id: string): TermState {
  const tab = s.tabs.find((t) => t.id === id)
  if (!tab) return s
  const siblings = tabsFor(s, tab.repoId)
  const at = siblings.findIndex((t) => t.id === id)
  const tabs = s.tabs.filter((t) => t.id !== id)
  const active = { ...s.active }
  if (active[tab.repoId] === id) {
    const next = siblings[at - 1] ?? siblings[at + 1]
    if (next) active[tab.repoId] = next.id
    else delete active[tab.repoId]
  }
  return { ...s, tabs, active }
}

export function markExited(s: TermState, id: string, code: number): TermState {
  if (!s.tabs.some((t) => t.id === id)) return s
  return { ...s, tabs: s.tabs.map((t) => (t.id === id ? { ...t, exitCode: code } : t)) }
}

export function removeRepoTabs(s: TermState, repoId: string): TermState {
  const active = { ...s.active }
  delete active[repoId]
  return { ...s, tabs: s.tabs.filter((t) => t.repoId !== repoId), active }
}

export function tabsFor(s: TermState, repoId: string): TermTab[] {
  return s.tabs.filter((t) => t.repoId === repoId)
}

export function setActive(s: TermState, repoId: string, id: string): TermState {
  return { ...s, active: { ...s.active, [repoId]: id } }
}

export function tabTitle(t: TermTab): string {
  return t.exitCode === null ? t.label : `${t.label} — exited (${t.exitCode})`
}

/** What a settled terminal command should refresh. Selecting a repository
 *  already reloads everything about it, so a background repository only
 *  needs the sidebar list (its branch label) refreshed. */
export function settledAction(selectedRepoId: string, repo: string): 'check' | 'list' {
  return repo === selectedRepoId ? 'check' : 'list'
}

export const terminalState = writable<TermState>(emptyTermState())
```

- [ ] **Step 4: Add the API calls and stores**

In `frontend/src/lib/api.ts`, inside `api` after `fingerprint`:

```ts
  terminalOpen: (repoId: string, cols: number, rows: number) => call<string>(Go.TerminalOpen(repoId, cols, rows)),
  terminalWrite: (tab: string, data: string) => call<void>(Go.TerminalWrite(tab, data)),
  terminalResize: (tab: string, cols: number, rows: number) => call<void>(Go.TerminalResize(tab, cols, rows)),
  terminalClose: (tab: string) => call<void>(Go.TerminalClose(tab)),
  terminalShell: () => call<string>(Go.TerminalShell()),
```

In `frontend/src/lib/stores.ts`, after `chatOpen`:

```ts
export const terminalOpen = persisted('terminalOpen', false)
/** Height of the terminal under the chat, in pixels. */
export const terminalHeight = persisted('terminalHeight', 260)
```

- [ ] **Step 5: Extract `checkExternalChanges` in `lib/actions.ts`**

Replace `startFocusRefresh` (currently lines ~292–334) with the version below. Behaviour on focus is unchanged; the fingerprint memory moves to module scope so the terminal can share it.

```ts
// Reloads refs and log when the selected repo changed behind the app's back
// — in a terminal while the window was in the background, or in the
// embedded terminal (see TerminalPanel's settle handling). The merge and
// worktree state are reloaded on every check: the fingerprint covers refs
// and HEAD only, so a `git add` or `git merge --abort` would never reach
// those views through it.
let knownFingerprint = ''
let knownFingerprintId = ''

async function rememberFingerprint() {
  const id = get(selectedRepoId)
  if (!id) return
  try {
    knownFingerprint = await api.fingerprint(id)
    knownFingerprintId = id
  } catch {
    knownFingerprint = ''
  }
}

export async function checkExternalChanges() {
  const id = get(selectedRepoId)
  if (!id) return
  loadMergeState()
  loadWorktreeState()
  try {
    const current = await api.fingerprint(id)
    if (id === knownFingerprintId && knownFingerprint && current !== knownFingerprint) await refreshRepo()
    knownFingerprint = current
    knownFingerprintId = id
  } catch {
    // Missing repo: the sidebar already shows it.
  }
}

export function startFocusRefresh(): () => void {
  const stopVersion = logVersion.subscribe(() => rememberFingerprint())
  const stopRepo = selectedRepoId.subscribe(() => rememberFingerprint())
  window.addEventListener('focus', checkExternalChanges)
  return () => {
    stopVersion()
    stopRepo()
    window.removeEventListener('focus', checkExternalChanges)
  }
}
```

- [ ] **Step 6: Run all frontend checks**

Run: `cd frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npm test && npm run check`
Expected: all vitest suites PASS (including the existing `actions.test.ts`), `svelte-check` reports 0 errors.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/lib
git commit -m "feat(terminal): per-repository tab state and a shared external-change check"
```

---

### Task 4: Terminal panel UI and specification

**Files:**
- Modify: `frontend/package.json`, `frontend/package-lock.json` (via `npm install @xterm/xterm @xterm/addon-fit`)
- Create: `frontend/src/components/TerminalView.svelte` (one xterm instance)
- Create: `frontend/src/components/TerminalPanel.svelte` (tab bar, busy notice, views, events)
- Modify: `frontend/src/App.svelte` (right column, Ctrl+`)
- Modify: `frontend/src/components/LogView.svelte:24-26` (terminal toggle beside "Show chat")
- Modify: `frontend/src/components/Icon.svelte` (`terminal` icon)
- Modify: `frontend/src/lib/actions.ts` (`removeRepo` drops the repo's tabs)
- Create: `docs/spec/08-terminal.md`
- Modify: `docs/spec/README.md`, `docs/spec/01-repositories-and-sidebar.md`, `docs/spec/06-ai.md`, `docs/spec/07-conventions-and-constraints.md`
- Modify: `docs/superpowers/specs/2026-09-22-embedded-terminal-design.md` (Status line)

**Interfaces:**
- Consumes: everything from Task 3; `busy` store (`lib/stores.ts:62`, a global label set by `run()` in `lib/actions.ts`); `Splitter` (`direction="horizontal"` emits `drag` with a y delta); `EventsOn` from `../../wailsjs/runtime/runtime`.
- Produces: user-visible feature. No new exports beyond components.

- [ ] **Step 1: Install the frontend dependencies**

Run: `cd frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npm install @xterm/xterm @xterm/addon-fit`
Expected: both added under `dependencies` in `package.json`.

- [ ] **Step 2: Add the icon**

In `Icon.svelte`'s path map, after `'panel-right'`:

```ts
    terminal: 'M2.5 3.5h11v9h-11zM5 6.5l2 1.5-2 1.5M8.5 10h2.5',
```

- [ ] **Step 3: `TerminalView.svelte` — one tab**

```svelte
<script lang="ts">
  import { onDestroy, onMount } from 'svelte'
  import { Terminal } from '@xterm/xterm'
  import { FitAddon } from '@xterm/addon-fit'
  import '@xterm/xterm/css/xterm.css'
  import { api } from '../lib/api'

  export let tab: string
  export let visible: boolean
  /** Receives this tab's writer so the panel can route terminal:data here. */
  export let register: (tab: string, write: ((data: string) => void) | null) => void

  let host: HTMLDivElement
  let term: Terminal
  let fit: FitAddon
  let observer: ResizeObserver

  const css = (name: string) => getComputedStyle(document.documentElement).getPropertyValue(name).trim()

  function refit() {
    if (!visible || !host?.offsetWidth) return
    fit.fit()
    api.terminalResize(tab, term.cols, term.rows).catch(() => {})
  }

  onMount(() => {
    term = new Terminal({
      scrollback: 5000,
      fontSize: 12,
      fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace',
      theme: { background: css('--surface'), foreground: css('--text'), cursor: css('--text'), selectionBackground: css('--selection') },
    })
    fit = new FitAddon()
    term.loadAddon(fit)
    term.open(host)
    // Exited or unknown tab: nothing useful to tell the user.
    term.onData((data) => api.terminalWrite(tab, data).catch(() => {}))
    register(tab, (data) => term.write(data))
    observer = new ResizeObserver(refit)
    observer.observe(host)
    refit()
  })

  onDestroy(() => {
    register(tab, null)
    observer?.disconnect()
    term?.dispose()
  })

  $: if (visible && term) {
    // Wait for display:none to lift before measuring.
    requestAnimationFrame(() => {
      refit()
      term.focus()
    })
  }
</script>

<div class="view" class:hidden={!visible} bind:this={host}></div>

<style>
  .view { position: absolute; inset: 4px 0 0 8px; }
  .hidden { display: none; }
</style>
```

- [ ] **Step 4: `TerminalPanel.svelte` — tabs, events, busy notice**

```svelte
<script lang="ts">
  import { onDestroy } from 'svelte'
  import { EventsOn } from '../../wailsjs/runtime/runtime'
  import Icon from './Icon.svelte'
  import TerminalView from './TerminalView.svelte'
  import { api } from '../lib/api'
  import { checkExternalChanges } from '../lib/actions'
  import { busy, loadRepos, selectedRepo, selectedRepoId, terminalOpen } from '../lib/stores'
  import { addTab, markExited, removeTab, setActive, settledAction, tabTitle, tabsFor, terminalState } from '../lib/terminal'
  import { errorMessage, toast } from '../lib/ui'
  import { get } from 'svelte/store'

  const writers = new Map<string, (data: string) => void>()
  // Output that arrives before a tab's view has mounted.
  const early = new Map<string, string>()
  let shell = 'sh'
  api.terminalShell().then((s) => (shell = s)).catch(() => {})

  function register(tab: string, write: ((data: string) => void) | null) {
    if (!write) return void writers.delete(tab)
    writers.set(tab, write)
    const pending = early.get(tab)
    if (pending) {
      early.delete(tab)
      write(pending)
    }
  }

  const offs = [
    EventsOn('terminal:data', (p: { tab: string; data: string }) => {
      const w = writers.get(p.tab)
      if (w) w(p.data)
      else early.set(p.tab, (early.get(p.tab) ?? '') + p.data)
    }),
    EventsOn('terminal:exit', (p: { tab: string; code: number }) => terminalState.update((s) => markExited(s, p.tab, p.code))),
    EventsOn('terminal:settled', (p: { tab: string; repo: string }) => {
      if (settledAction(get(selectedRepoId), p.repo) === 'check') checkExternalChanges()
      else loadRepos()
    }),
  ]
  onDestroy(() => offs.forEach((off) => off()))

  async function open(repoId: string) {
    try {
      // The view refits to the real size as soon as it mounts.
      const id = await api.terminalOpen(repoId, 80, 24)
      terminalState.update((s) => addTab(s, repoId, id, shell))
    } catch (e) {
      toast(errorMessage(e), 'error')
    }
  }

  async function close(id: string) {
    terminalState.update((s) => removeTab(s, id))
    early.delete(id)
    await api.terminalClose(id).catch(() => {})
  }

  $: repoId = $selectedRepo && !$selectedRepo.missing ? $selectedRepo.id : ''
  $: tabs = repoId ? tabsFor($terminalState, repoId) : []
  $: active = repoId ? $terminalState.active[repoId] : undefined
  // Opening the panel on a repository with no tabs opens one. Tracked per
  // repository so closing the last tab leaves "Open shell" instead of
  // immediately spawning another.
  const autoOpened = new Set<string>()
  $: if (repoId && tabs.length === 0 && !autoOpened.has(repoId)) {
    autoOpened.add(repoId)
    open(repoId)
  }
</script>

<div class="panel">
  <header>
    <div class="tabs">
      {#each tabs as t (t.id)}
        <div class="tab" class:active={t.id === active} class:exited={t.exitCode !== null}>
          <button class="name" on:click={() => terminalState.update((s) => setActive(s, t.repoId, t.id))}>{tabTitle(t)}</button>
          <button class="x" title="Close" on:click={() => close(t.id)}>×</button>
        </div>
      {/each}
      {#if repoId}
        <button class="icon-btn" title="New shell" on:click={() => open(repoId)}>+</button>
      {/if}
    </div>
    {#if $busy}<span class="busy ellipsis">Operation running: {$busy}</span>{/if}
    <button class="icon-btn" title="Hide terminal" on:click={() => terminalOpen.set(false)}><Icon name="terminal" /></button>
  </header>
  <div class="body">
    <!-- Every tab of every repository stays mounted so its scrollback
         survives switching repositories; only the active one is shown. -->
    {#each $terminalState.tabs as t (t.id)}
      <TerminalView tab={t.id} visible={t.repoId === repoId && t.id === active} {register} />
    {/each}
    {#if repoId && tabs.length === 0}
      <div class="empty"><button class="btn" on:click={() => open(repoId)}>Open shell</button></div>
    {/if}
  </div>
</div>

<style>
  .panel { display: flex; flex-direction: column; height: 100%; min-height: 0; background: var(--surface); }
  header { display: flex; align-items: center; gap: 8px; padding: 4px 8px; border-bottom: 1px solid var(--border); flex: none; }
  .tabs { display: flex; align-items: center; gap: 2px; flex: 1; min-width: 0; overflow-x: auto; }
  .tab { display: flex; align-items: center; border-radius: 6px; font-size: 12px; white-space: nowrap; }
  .tab.active { background: var(--active); }
  .tab.exited .name { color: var(--muted); }
  .tab button { background: none; border: 0; padding: 3px 6px; color: inherit; font: inherit; cursor: pointer; }
  .tab .x { padding-left: 0; color: var(--muted); }
  .busy { font-size: 11px; color: var(--muted); max-width: 40%; }
  .body { position: relative; flex: 1; min-height: 0; }
  .empty { position: absolute; inset: 0; display: flex; align-items: center; justify-content: center; }
</style>
```

- [ ] **Step 5: Right column and shortcut in `App.svelte`**

Import `TerminalPanel`, `terminalOpen`, `terminalHeight`. Replace the `{#if $chatOpen} … {/if}` block at the end of `.app` with:

```svelte
  {#if $chatOpen || $terminalOpen}
    <Splitter on:drag={(e) => chatWidth.set(clamp($chatWidth - e.detail, 260, 560))} />
    <section class="side" style="width: {$chatWidth}px" bind:clientHeight={sideHeight}>
      {#if $chatOpen}<div class="chat"><ChatPanel /></div>{/if}
      {#if $chatOpen && $terminalOpen}
        <Splitter direction="horizontal" on:drag={(e) => terminalHeight.set(clamp($terminalHeight - e.detail, 120, Math.max(120, sideHeight - 120)))} />
      {/if}
      {#if $terminalOpen}
        <div class="terminal" style={$chatOpen ? `height: ${$terminalHeight}px` : 'flex: 1'}><TerminalPanel /></div>
      {/if}
    </section>
  {/if}
```

In the script, add `let sideHeight = 0` and a key handler; in the markup add `<svelte:window on:keydown={toggleTerminal} />`:

```ts
  // e.code, not e.key: on layouts where ` is a dead key (Spanish, among
  // others) e.key is "Dead", but the physical key is still Backquote.
  function toggleTerminal(e: KeyboardEvent) {
    if (e.ctrlKey && !e.metaKey && !e.altKey && e.code === 'Backquote') {
      e.preventDefault()
      terminalOpen.update((v) => !v)
    }
  }
```

Replace the `.chat` style rule with:

```css
  .side { flex: none; min-width: 0; display: flex; flex-direction: column; background: var(--bg); }
  .chat { flex: 1; min-height: 0; }
  .terminal { flex: none; min-height: 0; }
```

- [ ] **Step 6: Toggle in `LogView.svelte`**

Import `terminalOpen`. Before the `{#if !$chatOpen}` block in the header, add:

```svelte
    {#if !$terminalOpen}
      <button class="icon-btn" title="Show terminal (Ctrl+`)" disabled={!$selectedRepo || $selectedRepo.missing} on:click={() => terminalOpen.set(true)}><Icon name="terminal" /></button>
    {/if}
```

- [ ] **Step 7: Drop tabs when a repository is removed**

In `lib/actions.ts`, find the function that calls `api.removeRepo(id)`. After the call succeeds, add:

```ts
    terminalState.update((s) => removeRepoTabs(s, id))
```

importing `terminalState`, `removeRepoTabs` from `./terminal`. (The backend has already closed the shells; this drops their views.)

- [ ] **Step 8: Build and check**

Run: `cd frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npm test && npm run check && npm run build`
Then: `cd .. && go build ./... && go test ./...`
Expected: all PASS; `svelte-check` 0 errors; vite build succeeds.

- [ ] **Step 9: Write the specification**

Create `docs/spec/08-terminal.md` in the voice of the existing area documents (read `docs/spec/05-remote-and-stash.md` first for tone and structure: it describes behaviour, not code). It must state, as behaviour: tabs per repository, labels and numbering, exited tabs, the right-column layout and both toggles plus Ctrl+`, auto-open of the first tab and the "Open shell" state, scrollback surviving repository switches (5 000 lines), no restore on restart, closing without confirmation (SIGHUP to shell and foreground job, SIGKILL after 2 s), the settle rule (Enter, 400 ms of quiet, full check for the selected repository, repository list only for others), the busy notice, and a **Safety** section copied in substance from the design spec: not sandboxed, bypasses the write lock by design and why, the AI has no access and why.

Then:
- `docs/spec/README.md`: add a row `| [Terminal](08-terminal.md) | The embedded shell: tabs per repository, freshness, safety |`.
- `docs/spec/01-repositories-and-sidebar.md`: where removing a repository is described, add that it closes the repository's terminal tabs without asking.
- `docs/spec/06-ai.md`: add that the assistant has no access to the embedded terminal — it never sees its output and cannot type into it — with a link to `08-terminal.md`.
- `docs/spec/07-conventions-and-constraints.md`: where the write lock is described, add that the embedded terminal deliberately bypasses it (link to 08); where dependencies or platform are listed, add the pty library and the terminal emulator and that the terminal is macOS/Linux only.
- Design spec status line → `Status: Implemented (manual pass pending)`.

- [ ] **Step 10: Commit**

```bash
git add frontend docs
git commit -m "feat(terminal): tabbed shell panel under the chat"
```

- [ ] **Step 11: Manual pass (report results; do not claim them without running)**

Run `make dev` and check: vim and htop render and exit cleanly; Ctrl-C stops `sleep 100`; `ls --color`/`git log --graph --color` show colour; dragging both splitters resizes the shell (`tput cols` changes); switching repositories with `top` running keeps it running and its scrollback visible on return; `git commit --allow-empty -m x` typed in the terminal appears in the log without leaving the window; `git checkout` in a background repository's tab updates its sidebar branch label; Ctrl+` toggles on a Spanish keyboard layout; removing a repository with an open tab closes it.
