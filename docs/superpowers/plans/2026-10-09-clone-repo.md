# Clone a Repository Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** "Add repo" → "Clone…" clones a URL into a chosen folder with live progress and Cancel, then adds and selects the new repository.

**Architecture:** `gitcmd.RunStream` runs git with stderr streamed line by line (split on `\r` and `\n`) and a stall watchdog instead of a fixed timeout. A new `internal/clone` package parses progress, validates the destination, runs `git clone --progress --recurse-submodules -- <url> <dest>` and turns errors into user messages. `internal/app/clone.go` runs one clone at a time in a goroutine and emits `clone:progress` / `clone:done`; the Svelte side has a pure `lib/clone.ts` (name from URL, view state, event handling) and a `CloneDialog.svelte` with a form view and a progress view.

**Tech Stack:** Go 1.x (Wails v2 backend), Svelte 5 in legacy syntax (`on:click`, `$store`), TypeScript, vitest.

**Spec:** `docs/superpowers/specs/2026-10-09-clone-repo-design.md`

## Global Constraints

- git is invoked as the system `git` through `internal/gitcmd` only; every run keeps `GIT_TERMINAL_PROMPT=0` and `LC_ALL=C`.
- No in-app credential prompts: helpers, keychain, ssh-agent and GCM only.
- Clone command, exactly: `git -C <parent> clone --progress --recurse-submodules -- <url> <dest>`.
- A URL that is empty or starts with `-` is refused before git runs.
- Stall watchdog: `clone.Stall = 5 * time.Minute` without any git output ends the clone as a timeout.
- A destination that did not exist before the clone is removed after a failure or cancel; one that existed (empty folder) is never removed.
- One clone at a time; a second `CloneRepo` returns `ErrCloneRunning`.
- Events: `clone:progress` (at most one per 100 ms per phase, phase changes always sent) and `clone:done`.
- User-facing messages, verbatim:
  - `Authentication failed. Set up a credential helper or an SSH key for this host, then try again.`
  - `The host's SSH key isn't trusted yet. Connect once from a terminal (ssh -T <host>) to accept it.`
  - `Repository not found at <url>.` (URL redacted)
  - `Clone stalled: git sent nothing for 5 minutes.`
  - Success toast when the dialog is closed: `Cloned <name>`
- Menu labels: `Open folder…`, `Clone…`. Dialog buttons: `Clone`, `Cancel`, `Continue in background`, `Choose…`.
- Commits: run git as `/usr/bin/git`; **no `Co-Authored-By` line**; docs/spec changes go in the same commit as the behaviour they describe.
- Go tests: `go test ./internal/...`. Frontend: `cd frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npx vitest run` and `npm run check`.

## Review Focus

- A URL with credentials (`https://user:tok3n@host/r.git`) that fails must not show `tok3n` in the error, toast or status — Task 3 tests `Explain` with it.
- Cancelling right after Clone (before any progress) leaves no destination folder and the dialog back on the form, no error — Task 3 (cancel test) and Task 4 (app cancel test).
- Cloning into an existing empty folder works, and a failure there leaves the folder in place — Task 3.
- `clone:done` arriving before the `CloneRepo` promise resolves (a tiny local repo) must not leave the dialog stuck on "running" — Task 5 tests `startClone` with that ordering.
- A folder name with spaces or non-ASCII (`my repo ñ`) is accepted and cloned to exactly that path — Task 3 (`Validate`) and Task 5 (`nameError`).

---

### Task 1: `gitcmd.RunStream` with line streaming and a stall watchdog

**Files:**
- Modify: `internal/gitcmd/gitcmd.go` (RunEnv body → shared `run`; new `RunStream`, `lineWriter`, `activityWriter`)
- Create: `internal/gitcmd/stream_test.go`
- Modify: `docs/spec/07-conventions-and-constraints.md` (section "The application drives the git command line", the timeouts bullet)

**Interfaces:**
- Produces: `func RunStream(ctx context.Context, dir string, env []string, stall time.Duration, onLine func(string), args ...string) (string, error)` — no fixed timeout; `stall == 0` disables the watchdog; `onLine` may be nil; errors are `*Error` wrapping `ErrCancelled` (Start.Cancel) or `ErrTimeout` (stall), exactly like `RunEnv`.
- `RunEnv`'s signature and behaviour are unchanged.

- [ ] **Step 1: Write the failing tests**

Create `internal/gitcmd/stream_test.go`:

```go
package gitcmd

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestLineWriterSplitsOnCRAndLF(t *testing.T) {
	var buf bytes.Buffer
	var lines []string
	w := &lineWriter{buf: &buf, onLine: func(s string) { lines = append(lines, s) }}
	w.Write([]byte("Receiving objects:  10% (1/10)\rReceiving objects:  50% (5/10)\r"))
	w.Write([]byte("Receiving objects: 100% (10/10), done.\nResolving del"))
	w.Write([]byte("tas: 100% (2/2)   \n\n"))
	w.Write([]byte("tail without end"))
	w.flush()
	want := []string{
		"Receiving objects:  10% (1/10)",
		"Receiving objects:  50% (5/10)",
		"Receiving objects: 100% (10/10), done.",
		"Resolving deltas: 100% (2/2)",
		"tail without end",
	}
	if len(lines) != len(want) {
		t.Fatalf("lines = %q, want %q", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}
	if !bytes.Contains(buf.Bytes(), []byte("tail without end")) {
		t.Fatal("buffer must keep everything written")
	}
}

// newBare makes a bare repository with one commit with plain exec, keeping
// this package's tests free of other internal packages.
func newBare(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	work := filepath.Join(dir, "work")
	bare := filepath.Join(dir, "bare.git")
	for _, args := range [][]string{
		{"init", "-q", "-b", "main", work},
		{"-C", work, "-c", "user.name=T", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "base"},
		{"clone", "-q", "--bare", work, bare},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return bare
}

func TestRunStreamPassesStderrLines(t *testing.T) {
	bare := newBare(t)
	parent := t.TempDir()
	var lines []string
	_, err := RunStream(context.Background(), parent, nil, 0, func(s string) { lines = append(lines, s) },
		"clone", "--progress", "file://"+bare, "copy")
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) == 0 || lines[0] != "Cloning into 'copy'..." {
		t.Fatalf("lines = %q", lines)
	}
}

func TestRunStreamStallIsTimeout(t *testing.T) {
	parent := t.TempDir()
	start := time.Now()
	// ssh never answers: after "Cloning into 'x'..." git writes nothing.
	_, err := RunStream(context.Background(), parent, []string{"GIT_SSH_COMMAND=sleep 30;:"}, 300*time.Millisecond, nil,
		"clone", "--progress", "ssh://example.invalid/x.git", "x")
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v, want ErrTimeout", err)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Fatalf("stall took %v", d)
	}
}

func TestRunStreamIsRecorded(t *testing.T) {
	var got []Record
	SetRecorder(&Recorder{End: func(r Record) { got = append(got, r) }})
	t.Cleanup(func() { SetRecorder(nil) })
	parent := t.TempDir()
	RunStream(context.Background(), parent, nil, 0, nil, "clone", "--progress", "file://"+newBare(t), "c")
	if len(got) != 1 || got[0].Args[0] != "clone" || got[0].Dir != parent {
		t.Fatalf("records = %+v", got)
	}
}

func TestRunStreamCancelViaStart(t *testing.T) {
	var cancel func()
	SetRecorder(&Recorder{Begin: func(s Start) int64 { cancel = s.Cancel; return 1 }})
	t.Cleanup(func() { SetRecorder(nil) })
	done := make(chan error, 1)
	go func() {
		_, err := RunStream(context.Background(), t.TempDir(), []string{"GIT_SSH_COMMAND=sleep 30;:"}, 0, nil,
			"clone", "ssh://example.invalid/x.git", "x")
		done <- err
	}()
	time.Sleep(300 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, ErrCancelled) {
			t.Fatalf("err = %v, want ErrCancelled", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("cancel did not stop git")
	}
}
```

Note: `TestRunStreamCancelViaStart` reads `cancel` from another goroutine after a sleep; if `go test -race` flags it, guard it with a `chan func()` filled in `Begin`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/gitcmd/ -run 'LineWriter|RunStream' -v`
Expected: compile failure — `undefined: lineWriter`, `undefined: RunStream`.

- [ ] **Step 3: Implement**

In `internal/gitcmd/gitcmd.go`, replace the body of `RunEnv` with a call to a shared `run`, and add `RunStream`, `runOpts`, `errStalled`, `lineWriter`, `activityWriter`. `RunEnv`'s doc comment stays.

```go
func RunEnv(ctx context.Context, dir string, timeout time.Duration, env []string, args ...string) (string, error) {
	return run(ctx, dir, runOpts{timeout: timeout, env: env}, args)
}

// RunStream is RunEnv for a long command that reports progress, such as a
// clone: there is no fixed timeout, each line git writes to stderr is passed
// to onLine as it arrives (git ends progress updates with \r, so \r and \n
// both end a line; blank lines are skipped), and a command that writes
// nothing to stdout or stderr for stall ends with ErrTimeout. stall 0 turns
// the watchdog off; onLine may be nil.
func RunStream(ctx context.Context, dir string, env []string, stall time.Duration, onLine func(string), args ...string) (string, error) {
	return run(ctx, dir, runOpts{env: env, stall: stall, onLine: onLine}, args)
}

type runOpts struct {
	timeout time.Duration // 0: none
	env     []string
	stall   time.Duration // 0: no watchdog
	onLine  func(string)
}

// errStalled is the cancel cause of a command the stall watchdog stopped.
var errStalled = errors.New("git command stalled")

func run(ctx context.Context, dir string, o runOpts, args []string) (string, error) {
	caller := ctx
	// cancellable is what Start.Cancel stops, with ErrCancelled as the cause
	// so the result can tell a cancel from a timeout.
	cancellable, cancelCmd := context.WithCancelCause(ctx)
	defer cancelCmd(nil)
	ctx = cancellable
	if o.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(cancellable, o.timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	cmd.Env = append(cmd.Env, o.env...)
	// On cancel or timeout, stop git as Ctrl+C in a terminal would: git
	// removes its lock files, and its hooks and helpers get the signal too.
	startInGroup(cmd)
	cmd.Cancel = func() error { return interrupt(cmd) }
	// A git still alive this long after that, or whose child (e.g. a
	// credential helper) holds the pipe open, is killed and Wait returns.
	cmd.WaitDelay = 5 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	var activity func()
	if o.stall > 0 {
		watchdog := time.AfterFunc(o.stall, func() { cancelCmd(errStalled) })
		defer watchdog.Stop()
		activity = func() { watchdog.Reset(o.stall) }
		cmd.Stdout = &activityWriter{w: &stdout, on: activity}
	}
	lines := &lineWriter{buf: &stderr, onLine: o.onLine, activity: activity}
	if o.onLine != nil || activity != nil {
		cmd.Stderr = lines
	}

	start := time.Now()
	id := begin(Start{Ctx: caller, Dir: dir, Args: args, Start: start, Cancel: func() { cancelCmd(ErrCancelled) }})
	runErr := cmd.Run()
	lines.flush()
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
		case errors.Is(context.Cause(cancellable), errStalled), errors.Is(ctx.Err(), context.DeadlineExceeded):
			gerr.Err = ErrTimeout
		}
		rec.ExitCode, rec.Err = gerr.ExitCode, gerr
		record(rec)
		return out, gerr
	}
	record(rec)
	return out, nil
}

// lineWriter keeps everything written to it in buf and, when onLine is set,
// passes each line — ended by \r or \n, trimmed, blank ones skipped — to
// onLine; flush passes a last line that had no ending. activity, when set,
// is called on every write (the stall watchdog). exec writes from a single
// goroutine, so no lock is needed.
type lineWriter struct {
	buf      *bytes.Buffer
	onLine   func(string)
	activity func()
	part     []byte
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.buf.Write(p)
	if w.activity != nil {
		w.activity()
	}
	if w.onLine == nil {
		return len(p), nil
	}
	for _, b := range p {
		if b == '\r' || b == '\n' {
			w.emit()
			continue
		}
		w.part = append(w.part, b)
	}
	return len(p), nil
}

func (w *lineWriter) emit() {
	s := strings.TrimSpace(string(w.part))
	w.part = w.part[:0]
	if s != "" {
		w.onLine(s)
	}
}

func (w *lineWriter) flush() {
	if w.onLine != nil {
		w.emit()
	}
}

// activityWriter forwards to w and calls on for every write.
type activityWriter struct {
	w  io.Writer
	on func()
}

func (a *activityWriter) Write(p []byte) (int, error) {
	a.on()
	return a.w.Write(p)
}
```

Add `"io"` to the imports.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/gitcmd/ -race -v` then `go test ./internal/...`
Expected: all PASS (RunEnv's existing tests prove the refactor kept its behaviour).

- [ ] **Step 5: Update the conventions spec**

In `docs/spec/07-conventions-and-constraints.md`, bullet "Different timeouts for different kinds of command", append after the sentence about the five-minute remote limit:

```markdown
  A clone is the exception: it has no fixed limit, since a large
  repository can take far longer. Its output is read as git writes it (to
  show progress), and a stall watchdog ends it as a timeout when git has
  written nothing for five minutes.
```

- [ ] **Step 6: Commit**

```bash
/usr/bin/git add internal/gitcmd docs/spec/07-conventions-and-constraints.md
/usr/bin/git commit -m "feat(gitcmd): RunStream: stderr line by line and a stall watchdog instead of a fixed timeout"
```

---

### Task 2: `internal/clone` — progress parsing and destination checks

**Files:**
- Create: `internal/clone/clone.go`
- Create: `internal/clone/clone_test.go`

**Interfaces:**
- Produces:
  - `type Progress struct { Phase string \`json:"phase"\`; Percent int \`json:"percent"\`; Detail string \`json:"detail"\` }` — `Percent` is -1 when git gave none.
  - `func ParseProgress(line string) (Progress, bool)`
  - `func Validate(parent, name string) (dest string, err error)`
  - `var ErrParentMissing, ErrBadName, ErrDestNotEmpty error`

- [ ] **Step 1: Write the failing tests**

`internal/clone/clone_test.go`:

```go
package clone

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestParseProgress(t *testing.T) {
	cases := []struct {
		line string
		want Progress
		ok   bool
	}{
		{"Cloning into 'cl'...", Progress{Phase: "Cloning into 'cl'", Percent: -1}, true},
		{"Cloning into '/Users/me/src/app/vendor/lib'...", Progress{Phase: "Cloning into 'lib'", Percent: -1}, true},
		{"remote: Enumerating objects: 5466, done.", Progress{Phase: "Enumerating objects", Percent: -1, Detail: "5466"}, true},
		{"remote: Counting objects:   1% (55/5466)", Progress{Phase: "Counting objects", Percent: 1, Detail: "55/5466"}, true},
		{"remote: Compressing objects: 100% (2142/2142), done.", Progress{Phase: "Compressing objects", Percent: 100, Detail: "2142/2142"}, true},
		{"Receiving objects:  45% (2460/5466), 3.10 MiB | 6.01 MiB/s", Progress{Phase: "Receiving objects", Percent: 45, Detail: "2460/5466, 3.10 MiB | 6.01 MiB/s"}, true},
		{"Receiving objects: 100% (5466/5466), 8.92 MiB | 6.92 MiB/s, done.", Progress{Phase: "Receiving objects", Percent: 100, Detail: "5466/5466, 8.92 MiB | 6.92 MiB/s"}, true},
		{"Resolving deltas: 100% (3620/3620), done.", Progress{Phase: "Resolving deltas", Percent: 100, Detail: "3620/3620"}, true},
		{"Updating files:  12% (120/1000)", Progress{Phase: "Updating files", Percent: 12, Detail: "120/1000"}, true},
		{"remote: Total 5466 (delta 3620), reused 5000 (delta 3000), pack-reused 0", Progress{}, false},
		{"Submodule 'lib' (https://h/lib.git) registered for path 'lib'", Progress{}, false},
		{"fatal: repository 'https://h/x.git/' not found", Progress{}, false},
		{"", Progress{}, false},
	}
	for _, c := range cases {
		got, ok := ParseProgress(c.line)
		if ok != c.ok || got != c.want {
			t.Errorf("ParseProgress(%q) = %+v, %v; want %+v, %v", c.line, got, ok, c.want, c.ok)
		}
	}
}

func TestValidate(t *testing.T) {
	parent := t.TempDir()
	if err := os.Mkdir(filepath.Join(parent, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(parent, "full", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "file"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"new", "empty", "my repo ñ"} {
		dest, err := Validate(parent, name)
		if err != nil || dest != filepath.Join(parent, name) {
			t.Errorf("Validate(%q) = %q, %v", name, dest, err)
		}
	}
	for name, want := range map[string]error{
		"":     ErrBadName,
		".":    ErrBadName,
		"..":   ErrBadName,
		"a/b":  ErrBadName,
		`a\b`:  ErrBadName,
		"full": ErrDestNotEmpty,
		"file": ErrDestNotEmpty,
	} {
		if _, err := Validate(parent, name); !errors.Is(err, want) {
			t.Errorf("Validate(%q) err = %v, want %v", name, err, want)
		}
	}
	if _, err := Validate(filepath.Join(parent, "nope"), "x"); !errors.Is(err, ErrParentMissing) {
		t.Errorf("missing parent err = %v", err)
	}
	if _, err := Validate(filepath.Join(parent, "file"), "x"); !errors.Is(err, ErrParentMissing) {
		t.Errorf("file as parent err = %v", err)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/clone/ -v`
Expected: compile failure — package has no non-test files / undefined symbols.

- [ ] **Step 3: Implement**

`internal/clone/clone.go`:

```go
// Package clone clones a remote repository with the git CLI, reporting
// git's progress as it goes.
package clone

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Progress is one progress line from git clone.
type Progress struct {
	Phase string `json:"phase"`
	// Percent is the phase's percentage, or -1 when git gave none.
	Percent int    `json:"percent"`
	Detail  string `json:"detail"`
}

var (
	// "Receiving objects:  45% (2460/5466), 3.10 MiB | 6.01 MiB/s", with
	// "remote: " in front for the server's phases.
	percentLine = regexp.MustCompile(`^(?:remote: )?([A-Z][a-z]+(?: [a-z]+)*):\s+(\d{1,3})% \(([^)]*)\)(.*)$`)
	// "remote: Enumerating objects: 5466, done."
	countLine = regexp.MustCompile(`^(?:remote: )?([A-Z][a-z]+(?: [a-z]+)*): (\d+), done\.$`)
	// "Cloning into 'app'..." — the repository itself, then each submodule
	// by its absolute path.
	cloningLine = regexp.MustCompile(`^Cloning into '(.+)'\.\.\.$`)
)

// ParseProgress reads one line of git clone's stderr; ok is false for a
// line that is not progress (summaries, submodule notices, errors).
func ParseProgress(line string) (p Progress, ok bool) {
	line = strings.TrimSpace(line)
	if m := percentLine.FindStringSubmatch(line); m != nil {
		pct, _ := strconv.Atoi(m[2])
		detail := m[3]
		rest := strings.TrimSuffix(strings.TrimSpace(m[4]), "done.")
		if rest = strings.Trim(rest, ", "); rest != "" {
			detail += ", " + rest
		}
		return Progress{Phase: m[1], Percent: pct, Detail: detail}, true
	}
	if m := countLine.FindStringSubmatch(line); m != nil {
		return Progress{Phase: m[1], Percent: -1, Detail: m[2]}, true
	}
	if m := cloningLine.FindStringSubmatch(line); m != nil {
		return Progress{Phase: "Cloning into '" + filepath.Base(m[1]) + "'", Percent: -1}, true
	}
	return Progress{}, false
}

var (
	ErrParentMissing = errors.New("The parent folder doesn't exist.")
	ErrBadName       = errors.New(`The folder name can't be empty, "." or "..", or contain a slash.`)
	ErrDestNotEmpty  = errors.New("The destination already exists and isn't an empty folder.")
)

// Validate checks that name can be cloned into under parent, as git would
// accept it: parent is a directory, name is one plain path segment, and the
// destination does not exist or is an empty directory. It returns the
// destination path.
func Validate(parent, name string) (string, error) {
	if info, err := os.Stat(parent); err != nil || !info.IsDir() {
		return "", ErrParentMissing
	}
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return "", ErrBadName
	}
	dest := filepath.Join(parent, name)
	info, err := os.Stat(dest)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return dest, nil
	case err != nil:
		return "", err
	case !info.IsDir():
		return "", ErrDestNotEmpty
	}
	entries, err := os.ReadDir(dest)
	if err != nil {
		return "", err
	}
	if len(entries) > 0 {
		return "", ErrDestNotEmpty
	}
	return dest, nil
}
```

(The error strings are sentences because they reach the dialog unchanged; if `staticcheck`/`go vet` in this repo rejects capitalised error strings, keep them and add `//nolint:stylecheck` only if the repo already uses such comments — check with `grep -rn nolint internal | head`.)

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/clone/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
/usr/bin/git add internal/clone
/usr/bin/git commit -m "feat(clone): parse git clone progress and check the destination folder"
```

---

### Task 3: `clone.Run` and `clone.Explain`

**Files:**
- Modify: `internal/clone/clone.go`
- Create: `internal/clone/run_test.go`

**Interfaces:**
- Consumes: `gitcmd.RunStream` (Task 1), `ParseProgress`, `Validate` (Task 2), `ops.IsAuthError(err error) bool`, `cmdlog.RedactArgs(args []string) ([]string, []string)`, `cmdlog.MaskOutput(s string, secrets []string) string`, `testrepo.New(t) *Repo`, `(*Repo).Commit(msg) string`, `testrepo.NewBareFrom(t, src *Repo) string`.
- Produces:
  - `const Stall = 5 * time.Minute`
  - `var ErrBadURL, ErrCancelled error`
  - `func Run(ctx context.Context, url, dest string, stall time.Duration, onProgress func(Progress)) error` — returns `ErrCancelled` (wrapped) when ctx was cancelled or the command was cancelled.
  - `func Explain(err error, url string) string`

- [ ] **Step 1: Write the failing tests**

`internal/clone/run_test.go`:

```go
package clone

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

func bareRepo(t *testing.T) string {
	t.Helper()
	r := testrepo.New(t)
	r.Commit("base")
	return testrepo.NewBareFrom(t, r)
}

func TestRunClones(t *testing.T) {
	bare := bareRepo(t)
	dest := filepath.Join(t.TempDir(), "copy")
	var phases []string
	err := Run(context.Background(), "file://"+bare, dest, Stall, func(p Progress) { phases = append(phases, p.Phase) })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, ".git")); err != nil {
		t.Fatalf("no .git in dest: %v", err)
	}
	if len(phases) == 0 || phases[0] != "Cloning into 'copy'" {
		t.Fatalf("phases = %q", phases)
	}
}

func TestRunWithSubmodule(t *testing.T) {
	sub := testrepo.New(t)
	sub.Commit("sub base")
	subBare := testrepo.NewBareFrom(t, sub)
	top := testrepo.New(t)
	top.Commit("base")
	top.Git("-c", "protocol.file.allow=always", "submodule", "add", "-q", "file://"+subBare, "lib")
	top.Git("commit", "-q", "-m", "add lib")
	bare := testrepo.NewBareFrom(t, top)
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "protocol.file.allow")
	t.Setenv("GIT_CONFIG_VALUE_0", "always")

	dest := filepath.Join(t.TempDir(), "top")
	if err := Run(context.Background(), "file://"+bare, dest, Stall, func(Progress) {}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "lib", ".git")); err != nil {
		t.Fatalf("submodule not cloned: %v", err)
	}
}

func TestRunIntoEmptyFolder(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "empty")
	os.Mkdir(dest, 0o755)
	if err := Run(context.Background(), "file://"+bareRepo(t), dest, Stall, func(Progress) {}); err != nil {
		t.Fatal(err)
	}
}

func TestRunFailureKeepsPreexistingEmptyFolder(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "empty")
	os.Mkdir(dest, 0o755)
	err := Run(context.Background(), filepath.Join(t.TempDir(), "nope"), dest, Stall, func(Progress) {})
	if err == nil {
		t.Fatal("want an error")
	}
	if info, statErr := os.Stat(dest); statErr != nil || !info.IsDir() {
		t.Fatalf("pre-existing folder removed: %v", statErr)
	}
}

func TestRunFailureLeavesNoFolder(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope")
	dest := filepath.Join(t.TempDir(), "x")
	err := Run(context.Background(), missing, dest, Stall, func(Progress) {})
	if err == nil {
		t.Fatal("want an error")
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatalf("dest left behind: %v", statErr)
	}
	if msg := Explain(err, missing); msg != "Repository not found at "+missing+"." {
		t.Fatalf("Explain = %q", msg)
	}
}

func TestRunCancelLeavesNoFolder(t *testing.T) {
	t.Setenv("GIT_SSH_COMMAND", "sleep 30;:")
	dest := filepath.Join(t.TempDir(), "x")
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(300 * time.Millisecond); cancel() }()
	err := Run(ctx, "ssh://example.invalid/x.git", dest, Stall, func(Progress) {})
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("err = %v, want ErrCancelled", err)
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatalf("dest left behind: %v", statErr)
	}
}

func TestRunStall(t *testing.T) {
	t.Setenv("GIT_SSH_COMMAND", "sleep 30;:")
	err := Run(context.Background(), "ssh://example.invalid/x.git", filepath.Join(t.TempDir(), "x"), 300*time.Millisecond, func(Progress) {})
	if !errors.Is(err, gitcmd.ErrTimeout) {
		t.Fatalf("err = %v, want ErrTimeout", err)
	}
	if msg := Explain(err, "ssh://example.invalid/x.git"); msg != "Clone stalled: git sent nothing for 5 minutes." {
		t.Fatalf("Explain = %q", msg)
	}
}

func TestRunRefusesBadURL(t *testing.T) {
	for _, url := range []string{"", "  ", "--upload-pack=touch /tmp/pwned", "-x"} {
		if err := Run(context.Background(), url, filepath.Join(t.TempDir(), "x"), Stall, func(Progress) {}); !errors.Is(err, ErrBadURL) {
			t.Errorf("Run(%q) err = %v, want ErrBadURL", url, err)
		}
	}
}

func gitErr(stderr string) error {
	return &gitcmd.Error{Args: []string{"clone"}, Stderr: stderr, ExitCode: 128, Err: errors.New("exit status 128")}
}

func TestExplain(t *testing.T) {
	cases := []struct{ stderr, url, want string }{
		{"Cloning into 'x'...\nfatal: could not read Username for 'https://github.com': terminal prompts disabled\n",
			"https://github.com/o/x.git",
			"Authentication failed. Set up a credential helper or an SSH key for this host, then try again."},
		{"git@github.com: Permission denied (publickey).\nfatal: Could not read from remote repository.\n",
			"git@github.com:o/x.git",
			"Authentication failed. Set up a credential helper or an SSH key for this host, then try again."},
		{"Host key verification failed.\nfatal: Could not read from remote repository.\n",
			"git@github.com:o/x.git",
			"The host's SSH key isn't trusted yet. Connect once from a terminal (ssh -T git@github.com) to accept it."},
		{"Host key verification failed.\n",
			"ssh://git@example.com:2222/o/x.git",
			"The host's SSH key isn't trusted yet. Connect once from a terminal (ssh -T git@example.com) to accept it."},
		{"Cloning into 'x'...\nremote: Counting objects: 1% (1/9)\rfatal: unable to access 'https://h/x.git/': Could not resolve host: h\n",
			"https://h/x.git",
			"fatal: unable to access 'https://h/x.git/': Could not resolve host: h"},
	}
	for _, c := range cases {
		if got := Explain(gitErr(c.stderr), c.url); got != c.want {
			t.Errorf("Explain(%q) = %q, want %q", c.stderr, got, c.want)
		}
	}
}

func TestExplainHidesCredentials(t *testing.T) {
	url := "https://user:tok3n@h.invalid/x.git"
	msgs := []string{
		Explain(gitErr("fatal: unable to access 'https://user:tok3n@h.invalid/x.git/': Could not resolve host: h.invalid\n"), url),
		Explain(gitErr("fatal: repository 'https://user:tok3n@h.invalid/x.git/' not found\n"), url),
	}
	for _, m := range msgs {
		if strings.Contains(m, "tok3n") {
			t.Fatalf("token leaked: %q", m)
		}
	}
}
```

If `testrepo.New`'s default branch makes `NewBareFrom` need arguments different from these, read `internal/testrepo/testrepo.go:32-58` and adapt.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/clone/ -v`
Expected: compile failure — `undefined: Run`, `Explain`, `Stall`, `ErrBadURL`, `ErrCancelled`.

- [ ] **Step 3: Implement**

Append to `internal/clone/clone.go` (merge the imports: add `context`, `fmt`, `net/url` as `neturl`, `time`, and `git-ui/internal/cmdlog`, `git-ui/internal/gitcmd`, `git-ui/internal/ops`):

```go
// Stall is how long a clone may go without git writing anything before it
// is stopped as a timeout.
const Stall = 5 * time.Minute

var (
	ErrBadURL = errors.New(`Enter a repository URL; it can't start with "-".`)
	// ErrCancelled is returned when the clone was cancelled.
	ErrCancelled = errors.New("clone cancelled")
)

// Run clones url into dest (an absolute path that Validate accepted),
// with its submodules, passing git's progress to onProgress as it comes.
// When the clone fails or is cancelled and dest did not exist before, dest
// is removed; a destination that existed (an empty folder) is left alone.
func Run(ctx context.Context, url, dest string, stall time.Duration, onProgress func(Progress)) error {
	if strings.TrimSpace(url) == "" || strings.HasPrefix(url, "-") {
		return ErrBadURL
	}
	_, statErr := os.Stat(dest)
	existed := statErr == nil
	_, err := gitcmd.RunStream(ctx, filepath.Dir(dest), nil, stall, func(line string) {
		if p, ok := ParseProgress(line); ok {
			onProgress(p)
		}
	}, "clone", "--progress", "--recurse-submodules", "--", url, dest)
	if err == nil {
		return nil
	}
	if !existed {
		_ = os.RemoveAll(dest)
	}
	if ctx.Err() != nil || errors.Is(err, gitcmd.ErrCancelled) {
		return fmt.Errorf("%w: %w", ErrCancelled, err)
	}
	return err
}

// Explain turns a failed Run into the message the user sees, with any
// credentials in url or in git's output hidden.
func Explain(err error, url string) string {
	redacted, secrets := cmdlog.RedactArgs([]string{url})
	if errors.Is(err, gitcmd.ErrTimeout) {
		return "Clone stalled: git sent nothing for 5 minutes."
	}
	var gerr *gitcmd.Error
	if !errors.As(err, &gerr) {
		return cmdlog.MaskOutput(err.Error(), secrets)
	}
	s := strings.ToLower(gerr.Stderr)
	switch {
	// Before IsAuthError, which counts this marker as an auth failure too.
	case strings.Contains(s, "host key verification failed"):
		return fmt.Sprintf("The host's SSH key isn't trusted yet. Connect once from a terminal (ssh -T %s) to accept it.", sshHost(url))
	case ops.IsAuthError(err):
		return "Authentication failed. Set up a credential helper or an SSH key for this host, then try again."
	case strings.Contains(s, "does not exist"), strings.Contains(s, "not found"),
		strings.Contains(s, "does not appear to be a git repository"):
		return fmt.Sprintf("Repository not found at %s.", redacted[0])
	}
	return cmdlog.MaskOutput(lastMessage(gerr), secrets)
}

// lastMessage is git's stderr without its progress lines.
func lastMessage(gerr *gitcmd.Error) string {
	var kept []string
	for _, l := range strings.FieldsFunc(gerr.Stderr, func(r rune) bool { return r == '\r' || r == '\n' }) {
		l = strings.TrimSpace(l)
		if _, ok := ParseProgress(l); ok || l == "" {
			continue
		}
		kept = append(kept, l)
	}
	if len(kept) == 0 {
		return gerr.Err.Error()
	}
	return strings.Join(kept, "\n")
}

// sshHost is the user@host to try `ssh -T` with: from an ssh:// URL or an
// scp-like git@host:path; otherwise url itself.
func sshHost(url string) string {
	if u, err := neturl.Parse(url); err == nil && u.Scheme == "ssh" {
		if u.User != nil {
			return u.User.Username() + "@" + u.Hostname()
		}
		return u.Hostname()
	}
	if host, _, ok := strings.Cut(url, ":"); ok && !strings.Contains(host, "/") {
		return host
	}
	return url
}
```

Note: `"Repository not found"` must be checked **after** `IsAuthError`: GitHub answers a private or missing HTTPS repo with "could not read Username", which is an auth failure.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/clone/ -race -v`
Expected: PASS. If `TestRunFailureLeavesNoFolder`'s Explain message differs because git words a missing local path differently, print the stderr with `t.Log` and add the exact marker to the not-found case — do not loosen the test.

- [ ] **Step 5: Commit**

```bash
/usr/bin/git add internal/clone
/usr/bin/git commit -m "feat(clone): run git clone with progress, clean up after a failure or cancel, explain errors without credentials"
```

---

### Task 4: App bindings — `CloneRepo`, `CancelClone`, `CloneStatus`, pickers, events

**Files:**
- Create: `internal/app/clone.go`
- Create: `internal/app/clone_test.go`
- Modify: `internal/app/app.go` (App struct: three fields)
- Regenerate: `frontend/wailsjs/go/app/App.d.ts`, `App.js`, `frontend/wailsjs/go/models.ts`

**Interfaces:**
- Consumes: `clone.Validate`, `clone.Run`, `clone.Explain`, `clone.Stall`, `clone.ErrBadURL`, `clone.ErrCancelled`, `clone.Progress` (Tasks 2–3); `a.store.Add(ctx, path) (repos.Repo, error)`; `a.emit(name string, data any)`.
- Produces (Go, bound to the frontend):
  - `func (a *App) CloneRepo(url, parent, name string) error`
  - `func (a *App) CancelClone()`
  - `func (a *App) CloneStatus() CloneState`
  - `func (a *App) PickCloneParent(start string) (string, error)`
  - `func (a *App) DefaultCloneParent() string`
  - `type CloneState struct { Running bool \`json:"running"\`; URL string \`json:"url"\`; Dest string \`json:"dest"\`; Progress *clone.Progress \`json:"progress"\`; LastError string \`json:"lastError"\` }`
  - `type CloneDone struct { Repo *repos.Repo \`json:"repo"\`; Error string \`json:"error"\`; Cancelled bool \`json:"cancelled"\` }`
  - `var ErrCloneRunning error`
  - Events `clone:progress` (payload `clone.Progress`) and `clone:done` (payload `CloneDone`).

- [ ] **Step 1: Write the failing tests**

`internal/app/clone_test.go` (imports: `context`, `errors`, `os`, `path/filepath`, `testing`, `time`, and `git-ui/internal/clone`, `git-ui/internal/testrepo`; check `merge_test.go:160-180` for how an `Emit` capture is wired through `WithAI(a, AIDeps{Emit: ...})` and mirror it):

```go
func cloneApp(t *testing.T) (*App, chan CloneDone) {
	t.Helper()
	a, _ := newTestApp(t)
	a.ctx = context.Background()
	done := make(chan CloneDone, 4)
	WithAI(a, AIDeps{Emit: func(name string, data any) {
		if name == "clone:done" {
			done <- data.(CloneDone)
		}
	}})
	return a, done
}

func waitDone(t *testing.T, ch chan CloneDone) CloneDone {
	t.Helper()
	select {
	case d := <-ch:
		return d
	case <-time.After(30 * time.Second):
		t.Fatal("no clone:done")
		return CloneDone{}
	}
}

func TestCloneRepoAddsTheRepository(t *testing.T) {
	a, done := cloneApp(t)
	src := testrepo.New(t)
	src.Commit("base")
	bare := testrepo.NewBareFrom(t, src)
	parent := t.TempDir()

	if err := a.CloneRepo("file://"+bare, parent, "copy"); err != nil {
		t.Fatal(err)
	}
	d := waitDone(t, done)
	if d.Repo == nil || d.Error != "" || d.Cancelled {
		t.Fatalf("done = %+v", d)
	}
	if d.Repo.Path != canonical(filepath.Join(parent, "copy")) {
		t.Fatalf("path = %q", d.Repo.Path)
	}
	if _, ok := a.store.Get(d.Repo.ID); !ok {
		t.Fatal("repo not in the store")
	}
	if s := a.CloneStatus(); s.Running || s.LastError != "" {
		t.Fatalf("status = %+v", s)
	}
}

func TestCloneRepoOneAtATimeAndCancel(t *testing.T) {
	t.Setenv("GIT_SSH_COMMAND", "sleep 30;:")
	a, done := cloneApp(t)
	parent := t.TempDir()

	if err := a.CloneRepo("ssh://example.invalid/x.git", parent, "x"); err != nil {
		t.Fatal(err)
	}
	if s := a.CloneStatus(); !s.Running || s.Dest != filepath.Join(parent, "x") {
		t.Fatalf("status = %+v", s)
	}
	if err := a.CloneRepo("ssh://example.invalid/y.git", parent, "y"); !errors.Is(err, ErrCloneRunning) {
		t.Fatalf("second clone err = %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	a.CancelClone()
	d := waitDone(t, done)
	if !d.Cancelled || d.Error != "" || d.Repo != nil {
		t.Fatalf("done = %+v", d)
	}
	if _, err := os.Stat(filepath.Join(parent, "x")); !os.IsNotExist(err) {
		t.Fatalf("dest left behind: %v", err)
	}
	if s := a.CloneStatus(); s.Running {
		t.Fatalf("still running: %+v", s)
	}
}

func TestCloneRepoValidatesFirst(t *testing.T) {
	a, done := cloneApp(t)
	parent := t.TempDir()
	os.MkdirAll(filepath.Join(parent, "full", "x"), 0o755)

	if err := a.CloneRepo("file:///nowhere", parent, "full"); !errors.Is(err, clone.ErrDestNotEmpty) {
		t.Fatalf("err = %v", err)
	}
	if err := a.CloneRepo("-x", parent, "new"); !errors.Is(err, clone.ErrBadURL) {
		t.Fatalf("err = %v", err)
	}
	select {
	case d := <-done:
		t.Fatalf("unexpected clone:done %+v", d)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestCloneRepoFailureIsKeptInStatus(t *testing.T) {
	a, done := cloneApp(t)
	missing := filepath.Join(t.TempDir(), "nope")
	if err := a.CloneRepo(missing, t.TempDir(), "x"); err != nil {
		t.Fatal(err)
	}
	d := waitDone(t, done)
	want := "Repository not found at " + missing + "."
	if d.Error != want || a.CloneStatus().LastError != want {
		t.Fatalf("done = %+v, status = %+v", d, a.CloneStatus())
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/app/ -run CloneRepo -v`
Expected: compile failure — `undefined: CloneDone`, `a.CloneRepo`, …

- [ ] **Step 3: Implement**

Add to the `App` struct in `internal/app/app.go`, after `worktrees`/`wtParent` fields:

```go
	// cloneMu guards clone and cloneCancel: the one clone the Clone dialog
	// may be running, and what stops it (nil when none runs).
	cloneMu     sync.Mutex
	clone       CloneState
	cloneCancel context.CancelFunc
```

`internal/app/clone.go`:

```go
package app

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"git-ui/internal/clone"
	"git-ui/internal/repos"
)

// ErrCloneRunning refuses a second clone while one runs.
var ErrCloneRunning = errors.New("A clone is already running.")

// CloneState is what the Clone dialog shows when it opens: the running
// clone, or the last one's error.
type CloneState struct {
	Running   bool            `json:"running"`
	URL       string          `json:"url"`
	Dest      string          `json:"dest"`
	Progress  *clone.Progress `json:"progress"`
	LastError string          `json:"lastError"`
}

// CloneDone is the clone:done event: the added repository, or why not.
type CloneDone struct {
	Repo      *repos.Repo `json:"repo"`
	Error     string      `json:"error"`
	Cancelled bool        `json:"cancelled"`
}

// progressEvery throttles clone:progress within one phase.
const progressEvery = 100 * time.Millisecond

// CloneRepo checks the destination and URL, then clones url into
// parent/name in the background, reporting through clone:progress and
// clone:done. One clone runs at a time.
func (a *App) CloneRepo(url, parent, name string) error {
	url = strings.TrimSpace(url)
	dest, err := clone.Validate(parent, strings.TrimSpace(name))
	if err != nil {
		return err
	}
	if url == "" || strings.HasPrefix(url, "-") {
		return clone.ErrBadURL
	}
	a.cloneMu.Lock()
	defer a.cloneMu.Unlock()
	if a.clone.Running {
		return ErrCloneRunning
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.clone = CloneState{Running: true, URL: url, Dest: dest}
	a.cloneCancel = cancel
	go a.runClone(ctx, cancel, url, dest)
	return nil
}

func (a *App) runClone(ctx context.Context, cancel context.CancelFunc, url, dest string) {
	defer cancel()
	var lastPhase string
	var lastSent time.Time
	err := clone.Run(ctx, url, dest, clone.Stall, func(p clone.Progress) {
		a.cloneMu.Lock()
		a.clone.Progress = &p
		a.cloneMu.Unlock()
		if p.Phase == lastPhase && time.Since(lastSent) < progressEvery {
			return
		}
		lastPhase, lastSent = p.Phase, time.Now()
		a.emit("clone:progress", p)
	})
	var done CloneDone
	switch {
	case err == nil:
		repo, addErr := a.store.Add(context.Background(), dest)
		if addErr != nil {
			done.Error = addErr.Error()
		} else {
			done.Repo = &repo
		}
	case errors.Is(err, clone.ErrCancelled):
		done.Cancelled = true
	default:
		done.Error = clone.Explain(err, url)
	}
	a.cloneMu.Lock()
	a.clone = CloneState{LastError: done.Error}
	a.cloneCancel = nil
	a.cloneMu.Unlock()
	a.emit("clone:done", done)
}

// CancelClone stops the running clone, if any; clone:done follows.
func (a *App) CancelClone() {
	a.cloneMu.Lock()
	defer a.cloneMu.Unlock()
	if a.cloneCancel != nil {
		a.cloneCancel()
	}
}

// CloneStatus is the running clone or the last one's error.
func (a *App) CloneStatus() CloneState {
	a.cloneMu.Lock()
	defer a.cloneMu.Unlock()
	s := a.clone
	if s.Progress != nil {
		p := *s.Progress
		s.Progress = &p
	}
	return s
}

// PickCloneParent asks for the folder to clone into, starting at start;
// "" when the user cancels.
func (a *App) PickCloneParent(start string) (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "Clone into folder", DefaultDirectory: start})
}

// DefaultCloneParent is the parent folder offered before any clone: home.
func (a *App) DefaultCloneParent() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}
```

Check `a.store.Get` exists with that name (`grep -n "func (s \*Store) Get" internal/repos/repos.go`); if not, adapt the test to the store's lookup.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/app/ -run CloneRepo -race -v` then `go test ./internal/...`
Expected: PASS.

- [ ] **Step 5: Regenerate the Wails bindings**

Run: `source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && ~/go/bin/wails generate module`
Expected: `frontend/wailsjs/go/app/App.d.ts` gains `CloneRepo`, `CancelClone`, `CloneStatus`, `PickCloneParent`, `DefaultCloneParent`; `models.ts` gains `app.CloneState` and `clone.Progress`. Then `/usr/bin/git checkout -- frontend/wailsjs/runtime 2>/dev/null || true` (as `make build` does) and check `/usr/bin/git status --short frontend/wailsjs` lists only `go/` files.

- [ ] **Step 6: Commit**

```bash
/usr/bin/git add internal/app/clone.go internal/app/clone_test.go internal/app/app.go frontend/wailsjs/go
/usr/bin/git commit -m "feat(app): CloneRepo runs one clone at a time with clone:progress and clone:done events, cancel and status"
```

---

### Task 5: `lib/clone.ts` — name from URL, view state and events

**Files:**
- Create: `frontend/src/lib/clone.ts`
- Create: `frontend/src/lib/clone.test.ts`
- Modify: `frontend/src/lib/types.ts` (clone types)
- Modify: `frontend/src/lib/api.ts` (five calls)
- Modify: `frontend/src/lib/stores.ts` (`cloneParent` persisted store)

**Interfaces:**
- Consumes: Go bindings from Task 4 (`Go.CloneRepo`, `Go.CancelClone`, `Go.CloneStatus`, `Go.PickCloneParent`, `Go.DefaultCloneParent`); `persisted<T>(key, initial)` from `stores.ts`.
- Produces (TS):
  - types: `CloneProgress { phase: string; percent: number; detail: string }`, `CloneStatus { running: boolean; url: string; dest: string; progress: CloneProgress | null; lastError: string }`, `CloneDone { repo: Repo | null; error: string; cancelled: boolean }`
  - `api.cloneRepo(url, parent, name): Promise<void>`, `api.cancelClone(): Promise<void>`, `api.cloneStatus(): Promise<CloneStatus>`, `api.pickCloneParent(start): Promise<string>`, `api.defaultCloneParent(): Promise<string>`
  - `clone.ts`: `dirName(url): string`, `nameError(name): string`, `joinPath(parent, name): string`, `type CloneView`, `interface CloneForm`, `emptyForm`, stores `cloneView`, `cloneForm`, `cloneDialogOpen`, `viewFromStatus(s)`, `editURL(f, url)`, `editName(f, name)`, `startClone(parent, run)`, `startCloneEvents(deps)`
  - `stores.ts`: `cloneParent: Writable<string>`

- [ ] **Step 1: Write the failing tests**

`frontend/src/lib/clone.test.ts`:

```ts
import { describe, expect, it, beforeEach } from 'vitest'
import { get } from 'svelte/store'
import {
  cloneDialogOpen, cloneForm, cloneView, dirName, editName, editURL, emptyForm, joinPath, nameError,
  startClone, startCloneEvents, viewFromStatus,
} from './clone'
import type { CloneDone, CloneProgress, Repo } from './types'

describe('dirName', () => {
  it.each([
    ['https://github.com/org/repo.git', 'repo'],
    ['https://github.com/org/repo', 'repo'],
    ['https://github.com/org/repo.git/', 'repo'],
    ['git@github.com:org/repo.git', 'repo'],
    ['git@host:repo.git', 'repo'],
    ['ssh://git@host:2222/org/repo.git', 'repo'],
    ['/src/thing/', 'thing'],
    ['file:///src/thing.git', 'thing'],
    ['  https://h/o/spaced.git  ', 'spaced'],
    ['', ''],
  ])('%s → %s', (url, want) => expect(dirName(url)).toBe(want))
})

describe('nameError', () => {
  it('accepts plain names, spaces and non-ASCII', () => {
    expect(nameError('repo')).toBe('')
    expect(nameError('my repo ñ')).toBe('')
  })
  it('refuses empty, dots and slashes', () => {
    expect(nameError('  ')).toBe('Enter a folder name.')
    expect(nameError('.')).toBe('Choose another folder name.')
    expect(nameError('..')).toBe('Choose another folder name.')
    expect(nameError('a/b')).toBe("The folder name can't contain a slash.")
    expect(nameError('a\\b')).toBe("The folder name can't contain a slash.")
  })
})

describe('joinPath', () => {
  it('joins without doubling the separator', () => {
    expect(joinPath('/Users/me', 'repo')).toBe('/Users/me/repo')
    expect(joinPath('/Users/me/', 'repo')).toBe('/Users/me/repo')
    expect(joinPath('C:\\src', 'repo')).toBe('C:\\src\\repo')
  })
})

describe('form editing', () => {
  it('follows the URL until the name is edited by hand', () => {
    let f = editURL(emptyForm, 'https://h/o/one.git')
    expect(f.name).toBe('one')
    f = editName(f, 'mine')
    f = editURL(f, 'https://h/o/two.git')
    expect(f.name).toBe('mine')
  })
  it('follows the URL again once the name is cleared', () => {
    let f = editName(editURL(emptyForm, 'https://h/o/one.git'), '')
    f = editURL(f, 'https://h/o/two.git')
    expect(f.name).toBe('two')
  })
})

describe('viewFromStatus', () => {
  it('maps running and idle status', () => {
    const p: CloneProgress = { phase: 'Receiving objects', percent: 40, detail: '4/10' }
    expect(viewFromStatus({ running: true, url: 'u', dest: '/d', progress: p, lastError: '' }))
      .toEqual({ kind: 'running', url: 'u', dest: '/d', progress: p })
    expect(viewFromStatus({ running: false, url: '', dest: '', progress: null, lastError: 'boom' }))
      .toEqual({ kind: 'form', error: 'boom' })
  })
})

type Handler = (payload: any) => unknown
function fakeEvents() {
  const handlers = new Map<string, Handler>()
  const on = (name: string, fn: Handler) => { handlers.set(name, fn); return () => handlers.delete(name) }
  return { on, fire: async (name: string, payload: unknown) => { await handlers.get(name)?.(payload) } }
}

const repo = { id: 'r1', name: 'copy', path: '/p/copy' } as Repo

describe('startClone and events', () => {
  let toasts: { message: string; kind: string; action?: { label: string } }[]
  let added: Repo[]
  let ev: ReturnType<typeof fakeEvents>
  let stop: () => void

  beforeEach(() => {
    stop?.()
    toasts = []
    added = []
    ev = fakeEvents()
    cloneView.set({ kind: 'form', error: '' })
    cloneForm.set(editURL(emptyForm, 'https://h/o/copy.git'))
    cloneDialogOpen.set(true)
    stop = startCloneEvents({
      on: ev.on as any,
      added: async (r) => { added.push(r) },
      toast: (message, kind, action) => { toasts.push({ message, kind, action }) },
    })
  })

  it('shows running at once, then progress', async () => {
    let resolve!: () => void
    const run = () => new Promise<void>((r) => { resolve = r })
    const p = startClone('/p', run)
    expect(get(cloneView)).toEqual({ kind: 'running', url: 'https://h/o/copy.git', dest: '/p/copy', progress: null })
    await ev.fire('clone:progress', { phase: 'Receiving objects', percent: 10, detail: '1/10' })
    expect(get(cloneView)).toMatchObject({ kind: 'running', progress: { percent: 10 } })
    resolve()
    await p
  })

  it('does not get stuck on running when clone:done beats the CloneRepo promise', async () => {
    let resolve!: () => void
    const p = startClone('/p', () => new Promise<void>((r) => { resolve = r }))
    await ev.fire('clone:done', { repo, error: '', cancelled: false } satisfies CloneDone)
    resolve()
    await p
    expect(get(cloneView).kind).toBe('form')
    expect(get(cloneDialogOpen)).toBe(false)
    expect(added).toEqual([repo])
  })

  it('returns to the form with the backend refusal', async () => {
    await startClone('/p', () => Promise.reject('The destination already exists and isn\'t an empty folder.'))
    expect(get(cloneView)).toEqual({ kind: 'form', error: "The destination already exists and isn't an empty folder." })
  })

  it('on success with the dialog open: closes it, adds the repo, resets the form, no toast', async () => {
    await ev.fire('clone:done', { repo, error: '', cancelled: false })
    expect(get(cloneDialogOpen)).toBe(false)
    expect(added).toEqual([repo])
    expect(get(cloneForm)).toEqual(emptyForm)
    expect(toasts).toEqual([])
  })

  it('on success with the dialog closed: toasts "Cloned <name>"', async () => {
    cloneDialogOpen.set(false)
    await ev.fire('clone:done', { repo, error: '', cancelled: false })
    expect(toasts).toEqual([{ message: 'Cloned copy', kind: 'info', action: undefined }])
  })

  it('on failure: form with the error and the values kept; a toast with Show when closed', async () => {
    await ev.fire('clone:done', { repo: null, error: 'boom', cancelled: false })
    expect(get(cloneView)).toEqual({ kind: 'form', error: 'boom' })
    expect(get(cloneForm).url).toBe('https://h/o/copy.git')
    expect(toasts).toEqual([])
    cloneDialogOpen.set(false)
    await ev.fire('clone:done', { repo: null, error: 'boom', cancelled: false })
    expect(toasts[0]).toMatchObject({ message: 'boom', kind: 'error', action: { label: 'Show' } })
  })

  it('on cancel: form, no error, no toast', async () => {
    cloneView.set({ kind: 'running', url: 'u', dest: '/d', progress: null })
    await ev.fire('clone:done', { repo: null, error: '', cancelled: true })
    expect(get(cloneView)).toEqual({ kind: 'form', error: '' })
    expect(toasts).toEqual([])
  })
})
```

- [ ] **Step 2: Run to verify failure**

Run: `cd frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npx vitest run src/lib/clone.test.ts`
Expected: FAIL — cannot resolve `./clone`.

- [ ] **Step 3: Implement**

`frontend/src/lib/types.ts` — append:

```ts
export interface CloneProgress { phase: string; percent: number; detail: string }
export interface CloneStatus { running: boolean; url: string; dest: string; progress: CloneProgress | null; lastError: string }
export interface CloneDone { repo: Repo | null; error: string; cancelled: boolean }
```

`frontend/src/lib/api.ts` — add `CloneStatus` to the type import and, after `addRepo`:

```ts
  cloneRepo: (url: string, parent: string, name: string) => call<void>(Go.CloneRepo(url, parent, name)),
  cancelClone: () => call<void>(Go.CancelClone()),
  cloneStatus: () => call<CloneStatus>(Go.CloneStatus()),
  pickCloneParent: (start: string) => call<string>(Go.PickCloneParent(start)),
  defaultCloneParent: () => call<string>(Go.DefaultCloneParent()),
```

`frontend/src/lib/stores.ts` — next to the other `persisted(...)` stores (follow their key naming):

```ts
/** The parent folder the last clone went into; '' until the first clone. */
export const cloneParent = persisted<string>('cloneParent', '')
```

`frontend/src/lib/clone.ts`:

```ts
import { get, writable } from 'svelte/store'
import type { EventsOn } from '../../wailsjs/runtime/runtime'
import type { CloneProgress, CloneStatus, Repo } from './types'
import { errorMessage, type Toast } from './ui'

/** The folder git would clone url into: its last path segment without a
 *  trailing slash or ".git" (https, ssh://, scp-like host:path, local). */
export function dirName(url: string): string {
  const s = url.trim().replace(/[/\\]+$/, '').replace(/\.git$/, '').replace(/[/\\]+$/, '')
  return s.slice(Math.max(s.lastIndexOf('/'), s.lastIndexOf('\\'), s.lastIndexOf(':')) + 1)
}

/** Why name can't be a clone's folder name; '' when it can. Mirrors
 *  clone.Validate's name rule so the form can say so as the user types. */
export function nameError(name: string): string {
  const n = name.trim()
  if (n === '') return 'Enter a folder name.'
  if (n === '.' || n === '..') return 'Choose another folder name.'
  if (/[/\\]/.test(n)) return "The folder name can't contain a slash."
  return ''
}

/** parent/name for the preview line, with parent's own separator. */
export function joinPath(parent: string, name: string): string {
  const sep = parent.includes('\\') && !parent.includes('/') ? '\\' : '/'
  return parent.replace(/[/\\]+$/, '') + sep + name
}

export type CloneView =
  | { kind: 'form'; error: string }
  | { kind: 'running'; url: string; dest: string; progress: CloneProgress | null }

export interface CloneForm { url: string; name: string; nameEdited: boolean }
export const emptyForm: CloneForm = { url: '', name: '', nameEdited: false }

export const cloneView = writable<CloneView>({ kind: 'form', error: '' })
/** The form's values, kept while the dialog is closed so a clone that fails
 *  in the background reopens with them. */
export const cloneForm = writable<CloneForm>(emptyForm)
export const cloneDialogOpen = writable(false)

export function editURL(f: CloneForm, url: string): CloneForm {
  return { ...f, url, name: f.nameEdited ? f.name : dirName(url) }
}

/** A hand-edited name stops following the URL; clearing it resumes. */
export function editName(f: CloneForm, name: string): CloneForm {
  return { ...f, name, nameEdited: name !== '' }
}

export function viewFromStatus(s: CloneStatus): CloneView {
  return s.running
    ? { kind: 'running', url: s.url, dest: s.dest, progress: s.progress }
    : { kind: 'form', error: s.lastError }
}

/** Starts the clone in cloneForm under parent. The view turns to running
 *  before the backend is called: a tiny clone's clone:done can arrive
 *  before run resolves, and must not be overwritten by it. */
export async function startClone(parent: string, run: (url: string, parent: string, name: string) => Promise<void>) {
  const f = get(cloneForm)
  const url = f.url.trim()
  const name = f.name.trim()
  cloneView.set({ kind: 'running', url, dest: joinPath(parent, name), progress: null })
  try {
    await run(url, parent, name)
  } catch (e) {
    cloneView.set({ kind: 'form', error: errorMessage(e) })
  }
}

export interface CloneDeps {
  on: typeof EventsOn
  /** Called with the new repository: reload the list and select it. */
  added: (repo: Repo) => Promise<void>
  toast: (message: string, kind: Toast['kind'], action?: Toast['action']) => void
}

/** Follows clone:progress and clone:done whether or not the dialog is
 *  open; returns the unsubscribe. */
export function startCloneEvents(deps: CloneDeps): () => void {
  const offProgress = deps.on('clone:progress', (p: CloneProgress) =>
    cloneView.update((v) => (v.kind === 'running' ? { ...v, progress: p } : v)))
  const offDone = deps.on('clone:done', async (d: { repo: Repo | null; error: string; cancelled: boolean }) => {
    const open = get(cloneDialogOpen)
    cloneView.set({ kind: 'form', error: d.error })
    if (d.repo) {
      cloneDialogOpen.set(false)
      cloneForm.set(emptyForm)
      await deps.added(d.repo)
      if (!open) deps.toast(`Cloned ${d.repo.name}`, 'info')
    } else if (d.error && !open) {
      deps.toast(d.error, 'error', { label: 'Show', run: () => cloneDialogOpen.set(true) })
    }
  })
  return () => {
    offProgress()
    offDone()
  }
}
```

If `errorMessage` turns a rejected string into something other than the string itself, read `ui.ts:30-35` and adjust the "backend refusal" test's expectation to what `errorMessage` returns for a string — the dialog must show the backend's sentence.

- [ ] **Step 4: Run the tests**

Run: `cd frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npx vitest run && npm run check`
Expected: all PASS, svelte-check 0 errors.

- [ ] **Step 5: Commit**

```bash
/usr/bin/git add frontend/src/lib/clone.ts frontend/src/lib/clone.test.ts frontend/src/lib/types.ts frontend/src/lib/api.ts frontend/src/lib/stores.ts
/usr/bin/git commit -m "feat(clone): frontend clone state: folder name from the URL, running view, clone:progress and clone:done"
```

---

### Task 6: The Clone dialog, the Add repo menu, and the sidebar spec

**Files:**
- Create: `frontend/src/components/CloneDialog.svelte`
- Modify: `frontend/src/components/Sidebar.svelte:138` (Add repo button → menu)
- Modify: `frontend/src/lib/actions.ts` (`openCloneDialog`)
- Modify: `frontend/src/App.svelte` (mount `<CloneDialog />`, start clone events)
- Modify: `docs/spec/01-repositories-and-sidebar.md` (section "Repository list": Add repo menu + new "### Cloning a repository")

**Interfaces:**
- Consumes: everything `clone.ts` exports (Task 5); `api.*Clone*` (Task 5); `cloneParent` (stores); `openMenu(event, items)`, `toast`, `errorMessage` (ui.ts); `addRepo`, `loadRepos`, `selectRepo`.
- Produces: `openCloneDialog(): Promise<void>` in `actions.ts`.

- [ ] **Step 1: `openCloneDialog` in `actions.ts`**

Next to `addRepo`:

```ts
/** Opens the Clone dialog on the running clone's progress, or on the form
 *  (with the last clone's error, if it failed). */
export async function openCloneDialog() {
  try {
    cloneView.set(viewFromStatus(await api.cloneStatus()))
  } catch (e) {
    cloneView.set({ kind: 'form', error: errorMessage(e) })
  }
  cloneDialogOpen.set(true)
}
```

Import `cloneDialogOpen`, `cloneView`, `viewFromStatus` from `./clone`.

- [ ] **Step 2: The Add repo menu in `Sidebar.svelte`**

Replace line 138:

```svelte
  <button
    class="row-item add"
    on:click={(e) => openMenu(e, [
      { label: 'Open folder…', action: addRepo },
      { label: 'Clone…', action: openCloneDialog },
    ])}
  ><Icon name="plus" /> Add repo</button>
```

Add `openCloneDialog` to the `../lib/actions` import and `openMenu` from `../lib/ui`.

- [ ] **Step 3: `CloneDialog.svelte`**

Copy the backdrop/dialog markup and CSS variables from `RepoSettingsDialog.svelte` (`<div class="backdrop" on:click|self={…} role="presentation"><div class="dialog" role="dialog" aria-label=…>`), and the button classes it uses for primary/secondary buttons.

```svelte
<script lang="ts">
  import { api } from '../lib/api'
  import {
    cloneDialogOpen, cloneForm, cloneView, editName, editURL, joinPath, nameError, startClone,
  } from '../lib/clone'
  import { cloneParent } from '../lib/stores'

  let parent = ''
  $: if ($cloneDialogOpen && parent === '') initParent()
  async function initParent() {
    parent = $cloneParent || (await api.defaultCloneParent())
  }

  $: form = $cloneForm
  $: nameErr = form.name === '' ? '' : nameError(form.name)
  $: canClone = form.url.trim() !== '' && form.name.trim() !== '' && nameErr === '' && parent !== ''

  async function choose() {
    const picked = await api.pickCloneParent(parent)
    if (picked) parent = picked
  }

  function submit() {
    if (!canClone) return
    cloneParent.set(parent)
    startClone(parent, api.cloneRepo)
  }

  // Closing never stops a running clone: that is "Continue in background".
  const close = () => cloneDialogOpen.set(false)
</script>

<svelte:window on:keydown={(e) => $cloneDialogOpen && e.key === 'Escape' && close()} />

{#if $cloneDialogOpen}
  <div class="backdrop" on:click|self={close} role="presentation">
    <div class="dialog" role="dialog" aria-label="Clone a repository">
      <h3>Clone a repository</h3>
      {#if $cloneView.kind === 'form'}
        <form on:submit|preventDefault={submit}>
          <label>URL
            <!-- svelte-ignore a11y-autofocus -->
            <input autofocus spellcheck="false" placeholder="https://github.com/org/repo.git"
              value={form.url} on:input={(e) => cloneForm.set(editURL(form, e.currentTarget.value))} />
          </label>
          <label>Parent folder
            <span class="parent">
              <input readonly value={parent} title={parent} />
              <button type="button" on:click={choose}>Choose…</button>
            </span>
          </label>
          <label>Folder name
            <input spellcheck="false" value={form.name}
              on:input={(e) => cloneForm.set(editName(form, e.currentTarget.value))} />
          </label>
          {#if nameErr}<p class="field-error">{nameErr}</p>{/if}
          {#if parent && form.name.trim()}<p class="preview ellipsis">{joinPath(parent, form.name.trim())}</p>{/if}
          {#if $cloneView.error}<p class="error" role="alert">{$cloneView.error}</p>{/if}
          <div class="buttons">
            <button type="button" on:click={close}>Cancel</button>
            <button type="submit" class="primary" disabled={!canClone}>Clone</button>
          </div>
        </form>
      {:else}
        <p class="ellipsis" title={$cloneView.url}>{$cloneView.url}</p>
        <p class="preview ellipsis" title={$cloneView.dest}>→ {$cloneView.dest}</p>
        {@const p = $cloneView.progress}
        <p class="phase">{p ? p.phase : 'Starting…'}</p>
        {#if p && p.percent >= 0}
          <progress max="100" value={p.percent} aria-label={p.phase}></progress>
        {:else}
          <progress aria-label="Cloning"></progress>
        {/if}
        {#if p?.detail}<p class="detail">{p.detail}</p>{/if}
        <div class="buttons">
          <button type="button" on:click={() => api.cancelClone()}>Cancel</button>
          <button type="button" class="primary" on:click={close}>Continue in background</button>
        </div>
      {/if}
    </div>
  </div>
{/if}
```

Styles: reuse the dialog's existing tokens; `progress { width: 100% }`; `.detail, .preview { color: var(--text-muted…) ; font-size: 12px }` (use the variable names `RepoSettingsDialog.svelte` uses); `.error, .field-error` with the error colour token already used for errors in the app (grep `--danger\|--error` in `frontend/src/style*` or `App.svelte`). `{@const}` must be a direct child of a block — if svelte-check rejects it in `{:else}`, compute `$: p = $cloneView.kind === 'running' ? $cloneView.progress : null` in the script instead.

- [ ] **Step 4: Mount it and start the events in `App.svelte`**

Import `CloneDialog` and mount `<CloneDialog />` just before `<DialogHost />` (line ~166), so a confirm opened from DialogHost still stacks on top. Next to the other `EventsOn` subscriptions (lines ~86–94), and released where they are released:

```ts
const offClone = startCloneEvents({
  on: EventsOn,
  added: async (repo) => {
    await loadRepos()
    selectRepo(repo.id)
  },
  toast,
})
```

(Import `startCloneEvents` from `./lib/clone`, `loadRepos`/`selectRepo` from `./lib/stores`, `toast` from `./lib/ui`; call `offClone()` wherever `offRepoAI()`/`offWorktree()` are called.)

- [ ] **Step 5: Update the sidebar spec**

In `docs/spec/01-repositories-and-sidebar.md`, section "Repository list", change "Adding a repository opens a directory picker;" to:

```markdown
"Add repo" opens a menu with **Open folder…** and **Clone…**. Open folder…
opens a directory picker;
```

and add, before `### Worktrees`:

```markdown
### Cloning a repository

**Clone…** opens a dialog with the URL, the parent folder (shown read-only,
changed with "Choose…"; the last one used is remembered, the home folder
before that) and the folder name. The name follows the URL as it is typed —
its last path segment without `.git` — until the user edits it; clearing it
makes it follow again. The name may not be empty, `.` or `..`, or contain a
slash, and the destination must not exist or must be an empty folder; a URL
that is empty or starts with `-` is refused.

**Clone** runs `git clone --progress --recurse-submodules -- <url> <dest>`
from the parent folder. Credentials come only from what is already set up
(credential helpers, the keychain, ssh-agent, Git Credential Manager); the
application never asks for a password. The dialog shows git's current phase
(counting, compressing, receiving, resolving, updating files, each
submodule), a bar with the phase's percentage when git gives one, and git's
detail line. **Cancel** stops git as Ctrl+C would; **Continue in
background** closes the dialog without stopping the clone. One clone runs at
a time: **Clone…** during a clone reopens its progress.

There is no fixed time limit; the clone is stopped as stalled when git
writes nothing for five minutes. When it succeeds, the repository is added
as Open folder… would add it, and selected; with the dialog closed, a toast
says "Cloned <name>". When it fails, the dialog returns to the form with its
values and the reason — authentication, an untrusted SSH host key, a
repository not found, a stall, or git's own message, with credentials
hidden; with the dialog closed, an error toast offers **Show**. A destination
folder the clone created is removed after a failure or cancel; an empty
folder that existed before is left in place. The clone is recorded like any
command but, belonging to no repository, does not appear in the Commands
panel.
```

- [ ] **Step 6: Verify**

Run: `cd frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npm run check && npx vitest run && npm run build`, then `go test ./internal/... && go vet ./...`
Expected: 0 svelte-check errors, all tests PASS, build succeeds.

Manual (owner, after `make dev`): Add repo → Clone… a small public HTTPS repo; Cancel mid-clone; Continue in background and wait for the toast; a private repo without credentials shows the authentication message.

- [ ] **Step 7: Commit**

```bash
/usr/bin/git add frontend/src/components/CloneDialog.svelte frontend/src/components/Sidebar.svelte frontend/src/lib/actions.ts frontend/src/App.svelte docs/spec/01-repositories-and-sidebar.md
/usr/bin/git commit -m "feat(sidebar): Clone… in the Add repo menu with a dialog showing progress, cancel and continue in background"
```
