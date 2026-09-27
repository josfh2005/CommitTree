# Command Log Panel Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Record every git command the app runs (who ran it, how it ended, its output) and show it in a new bottom dock under the repository view, where the embedded terminal also moves.

**Architecture:** `gitcmd.RunEnv` hands each finished command to a package-level recorder. `internal/cmdlog` classifies it (read/write, origin from ctx), redacts secrets and keeps a 500-entry ring per repository path. `internal/app` wires the recorder, exposes three bindings and emits `cmdlog:entry`. The frontend gets a `BottomDock` in `main` holding `TerminalPanel` (moved out of the chat column) and a new `CommandLogPanel`, side by side.

**Tech Stack:** Go 1.2x + Wails v2 backend; Svelte 3 + TypeScript + Vitest frontend.

**Spec:** `docs/superpowers/specs/2026-09-27-command-log-design.md`

## Global Constraints

- Retention: in memory only; `MaxEntries = 500` per repository; per stream (stdout, stderr) `MaxStream = 64 KB`; per repository `MaxOutputBytes = 8 MB` of stored output.
- Origin badge values: `you` / `ai` / `auto`, labels **You** / **AI** / **Auto**.
- Reads hidden by default; checkbox **Show reads**, remembered.
- Commands typed in the embedded terminal are NOT recorded.
- Secrets redacted before storing: URL credentials, `-c` values for `http.*.extraheader` or keys containing `token` / `password` / `secret`, and those secrets echoed in output.
- Shortcuts: ⌘J / Ctrl+J Terminal (unchanged); ⌘⇧J / Ctrl+Shift+J Commands.
- Dock: one height (persisted under the existing `terminalHeight` localStorage key), min 120 px, repository view keeps ≥ 200 px; Terminal/Commands min 240 px each when both open.
- The recorder must never change a git command's result: panics recovered.
- `cmdlog:entry` is emitted via the Wails runtime only after `Startup` — never through `AIDeps.Emit`.
- Behaviour changes update `docs/spec/` in the same commit (project rule).
- Commit messages: conventional style as in `git log`; NEVER add a `Co-Authored-By` line.
- Frontend tests: `cd frontend && npm test`. Go tests: `go test ./...`. Node 22 (`source ~/.nvm/nvm.sh && nvm use 22`).

## Review Focus

1. **Many commands at once (refresh burst, 500+ in a second)** — the panel stays responsive, keeps at most 500, newest first, no duplicates when the initial load and live events overlap. (Task 6 `mergeEntries` tests.)
2. **A command with huge output (blame of a long file, `log -p`)** — stored output is cut at 64 KB per stream on a UTF-8 boundary and the repository never holds more than 8 MB of output; older outputs are dropped, not entries. (Task 4 tests.)
3. **A remote URL with a token (`https://x-access-token:ghp_…@github.com/…`) in args and in git's error output** — the token never appears in the entry or the output. (Task 3 tests.)
4. **The app's test suite and headless runs** — with no Wails runtime, recording must not emit (Wails `EventsEmit` on a non-Wails ctx calls `log.Fatalf`) and must not flood the AI tests' event channel. (Task 5 test: every existing `go test ./internal/app/` still passes, plus an explicit "no emit before Startup" test.)
5. **Repository path mismatch** — the frontend filters live events by `entry.repo === $selectedRepo.path`; the backend keys by `filepath.Clean(dir)`. If a stored path is not clean, events would be ignored. (Task 5 test: `CommandLog(id)` returns commands run by an App method; Task 6 filter test; manual check in Task 8.)

---

## File Structure

- Modify `internal/gitcmd/gitcmd.go` — `Record`, `SetRecorder`, recording in `RunEnv`.
- Create `internal/cmdlog/classify.go` — `Origin`, `Kind`, ctx helpers, `Classify`.
- Create `internal/cmdlog/redact.go` — `RedactArgs`, `MaskOutput`.
- Create `internal/cmdlog/log.go` — `Entry`, `Output`, `Log` (ring + budget).
- Create `internal/app/cmdlog.go` — recorder hook, bindings, AI-write mark.
- Modify `internal/app/app.go` (`App` fields, `New`, `Startup`), `internal/app/ai.go` (`RunTool`), `internal/app/chatwrite.go` (`runWriteTool`).
- Regenerate `frontend/wailsjs/go/app/App.{js,d.ts}`, `frontend/wailsjs/go/models.ts`.
- Create `frontend/src/lib/cmdlog.ts` (+ test) — pure helpers.
- Modify `frontend/src/lib/types.ts`, `lib/api.ts`, `lib/stores.ts`, `lib/toolbar.ts` (+ test).
- Create `frontend/src/components/CommandLogPanel.svelte`, `BottomDock.svelte`.
- Modify `frontend/src/App.svelte`, `components/Toolbar.svelte`, `components/Icon.svelte`.
- Docs: create `docs/spec/09-command-log.md`; modify `docs/spec/README.md`, `08-terminal.md`, `05-remote-and-stash.md`.

---

### Task 1: gitcmd recorder

**Files:**
- Modify: `internal/gitcmd/gitcmd.go`
- Test: `internal/gitcmd/gitcmd_test.go`

**Interfaces:**
- Produces:
  ```go
  type Record struct {
      Ctx      context.Context // the caller's ctx (before the timeout wrapper)
      Dir      string
      Args     []string
      Start    time.Time
      Duration time.Duration
      ExitCode int   // 0 on success, -1 when git never ran
      Err      error // nil on success, else the *Error returned to the caller
      Stdout   string
      Stderr   string
  }
  func SetRecorder(fn func(Record)) // nil removes it
  ```

- [ ] **Step 1: Write the failing tests** — append to `internal/gitcmd/gitcmd_test.go` (add `"sync"` to imports):

```go
type ctxKey struct{}

func TestRecorderSeesSuccessAndFailure(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("first")
	var mu sync.Mutex
	var got []gitcmd.Record
	gitcmd.SetRecorder(func(rec gitcmd.Record) {
		mu.Lock()
		got = append(got, rec)
		mu.Unlock()
	})
	t.Cleanup(func() { gitcmd.SetRecorder(nil) })
	ctx := context.WithValue(context.Background(), ctxKey{}, "marker")

	gitcmd.Run(ctx, r.Dir, gitcmd.ReadTimeout, "rev-parse", "HEAD")
	gitcmd.Run(ctx, r.Dir, gitcmd.ReadTimeout, "rev-parse", "--verify", "nope")

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("got %d records, want 2", len(got))
	}
	ok, bad := got[0], got[1]
	if ok.Err != nil || ok.ExitCode != 0 || ok.Dir != r.Dir || strings.Join(ok.Args, " ") != "rev-parse HEAD" || ok.Stdout == "" {
		t.Fatalf("success record wrong: %+v", ok)
	}
	if ok.Ctx.Value(ctxKey{}) != "marker" {
		t.Fatal("record must carry the caller's ctx")
	}
	if ok.Start.IsZero() || ok.Duration <= 0 {
		t.Fatalf("timing missing: %+v", ok)
	}
	var gerr *gitcmd.Error
	if !errors.As(bad.Err, &gerr) || bad.ExitCode == 0 || bad.Stderr == "" {
		t.Fatalf("failure record wrong: %+v", bad)
	}
}

func TestPanickingRecorderDoesNotBreakRun(t *testing.T) {
	r := testrepo.New(t)
	hash := r.Commit("first")
	gitcmd.SetRecorder(func(gitcmd.Record) { panic("boom") })
	t.Cleanup(func() { gitcmd.SetRecorder(nil) })

	out, err := gitcmd.Run(context.Background(), r.Dir, gitcmd.ReadTimeout, "rev-parse", "HEAD")
	if err != nil || strings.TrimSpace(out) != hash {
		t.Fatalf("got %q, %v", out, err)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/gitcmd/ -run 'Recorder' -v`
Expected: FAIL — `undefined: gitcmd.SetRecorder` / `gitcmd.Record`.

- [ ] **Step 3: Implement** — in `internal/gitcmd/gitcmd.go` add `"sync/atomic"` to imports, then add after the `Error` methods:

```go
// Record is one finished git command, handed to the recorder set with
// SetRecorder — the app's command log.
type Record struct {
	// Ctx is the caller's context, before RunEnv's own timeout wrapper, so
	// values the caller put in it (such as who asked for the command) are
	// there.
	Ctx      context.Context
	Dir      string
	Args     []string
	Start    time.Time
	Duration time.Duration
	// ExitCode is 0 on success and -1 when git never ran or was killed.
	ExitCode int
	// Err is nil on success, else the *Error returned to the caller.
	Err    error
	Stdout string
	Stderr string
}

var recorder atomic.Pointer[func(Record)]

// SetRecorder makes fn receive every command Run and RunEnv finish; nil
// removes it. There is one recorder for the whole process.
func SetRecorder(fn func(Record)) {
	if fn == nil {
		recorder.Store(nil)
		return
	}
	recorder.Store(&fn)
}

// record hands r to the recorder. The recorder is a convenience: whatever
// it does, including panicking, must not change the command's result.
func record(r Record) {
	fn := recorder.Load()
	if fn == nil {
		return
	}
	defer func() { _ = recover() }()
	(*fn)(r)
}
```

Replace the body of `RunEnv` with:

```go
func RunEnv(ctx context.Context, dir string, timeout time.Duration, env []string, args ...string) (string, error) {
	caller := ctx
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	cmd.Env = append(cmd.Env, env...)
	// A killed git whose child (e.g. a credential helper) holds the pipe open
	// must not block Wait past this, on top of the context timeout above.
	cmd.WaitDelay = 5 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	start := time.Now()
	runErr := cmd.Run()
	rec := Record{Ctx: caller, Dir: dir, Args: args, Start: start, Duration: time.Since(start), Stdout: stdout.String(), Stderr: stderr.String()}
	if runErr != nil {
		gerr := &Error{Args: args, Stderr: stderr.String(), ExitCode: -1, Err: runErr}
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			gerr.ExitCode = exitErr.ExitCode()
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			gerr.Err = ErrTimeout
		}
		rec.ExitCode, rec.Err = gerr.ExitCode, gerr
		record(rec)
		return stdout.String(), gerr
	}
	record(rec)
	return stdout.String(), nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/gitcmd/ -v`
Expected: PASS (all, old and new).

- [ ] **Step 5: Commit**

```bash
git add internal/gitcmd
git commit -m "feat(gitcmd): hand every finished command to an optional recorder"
```

---

### Task 2: cmdlog classification and origin

**Files:**
- Create: `internal/cmdlog/classify.go`
- Test: `internal/cmdlog/classify_test.go`

**Interfaces:**
- Produces:
  ```go
  type Origin string // OriginYou "you", OriginAI "ai", OriginAuto "auto"
  type Kind string   // KindRead "read", KindWrite "write"
  func WithOrigin(ctx context.Context, o Origin) context.Context
  func OriginFrom(ctx context.Context) (Origin, bool) // nil ctx → "", false
  func Classify(args []string) Kind
  ```

- [ ] **Step 1: Write the failing tests** — `internal/cmdlog/classify_test.go`:

```go
package cmdlog

import (
	"context"
	"strings"
	"testing"
)

func TestClassify(t *testing.T) {
	reads := []string{
		"log --format=%H -n 10", "show HEAD", "status --porcelain=v2", "diff --cached",
		"rev-parse HEAD", "rev-list --count HEAD", "for-each-ref refs/heads", "show-ref",
		"cat-file -t HEAD", "blame -p f.txt", "ls-files -u", "ls-tree HEAD", "merge-base a b",
		"name-rev HEAD", "describe --tags", "diff-tree -r HEAD", "diff-index HEAD", "diff-files",
		"check-ignore x", "check-attr -a x", "var GIT_EDITOR", "version", "shortlog -s", "grep foo",
		"symbolic-ref HEAD", "symbolic-ref --short HEAD", "worktree list --porcelain",
		"submodule status", "submodule", "stash list", "stash show -p stash@{0}",
		"config --get user.name", "config --get-all remote.origin.url", "config --list",
		"config -l", "config --get-regexp ^remote", "config get user.name",
		"branch", "branch --list", "branch -a --format=%(refname)", "tag", "tag -l v*",
		"-c core.quotepath=false status",
		"reflog", "reflog show HEAD",
	}
	writes := []string{
		"commit -m x", "push", "fetch --all", "pull --rebase", "checkout main", "switch -c b",
		"merge feature", "rebase main", "cherry-pick abc", "reset --hard", "add f", "restore --staged f",
		"stash push -m x", "stash pop", "stash drop", "branch new", "branch -d old", "branch -m a b",
		"branch --set-upstream-to=origin/main", "tag v1", "tag -d v1", "symbolic-ref HEAD refs/heads/x",
		"config user.name Bob", "config set user.name Bob", "worktree add ../x", "worktree remove x",
		"submodule update --init", "reflog expire --all", "ls-remote origin", "clone url", "",
	}
	for _, c := range reads {
		if got := Classify(strings.Fields(c)); got != KindRead {
			t.Errorf("%q: got %s, want read", c, got)
		}
	}
	for _, c := range writes {
		if got := Classify(strings.Fields(c)); got != KindWrite {
			t.Errorf("%q: got %s, want write", c, got)
		}
	}
}

func TestOriginFromContext(t *testing.T) {
	if _, ok := OriginFrom(context.Background()); ok {
		t.Fatal("plain ctx has no origin")
	}
	if _, ok := OriginFrom(nil); ok { //nolint:staticcheck // nil ctx must be safe
		t.Fatal("nil ctx has no origin")
	}
	o, ok := OriginFrom(WithOrigin(context.Background(), OriginAI))
	if !ok || o != OriginAI {
		t.Fatalf("got %q %v", o, ok)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/cmdlog/ -v`
Expected: FAIL — package does not compile (undefined symbols).

- [ ] **Step 3: Implement** — `internal/cmdlog/classify.go`:

```go
// Package cmdlog keeps the log of git commands the app ran, per repository,
// for the Commands panel.
package cmdlog

import (
	"context"
	"strings"
)

// Origin is who asked for a command.
type Origin string

const (
	OriginYou  Origin = "you"
	OriginAI   Origin = "ai"
	OriginAuto Origin = "auto"
)

// Kind tells a command that only reads the repository from one that may
// change it (or talk to a remote).
type Kind string

const (
	KindRead  Kind = "read"
	KindWrite Kind = "write"
)

type originKey struct{}

// WithOrigin marks every git command run with ctx as asked for by o.
func WithOrigin(ctx context.Context, o Origin) context.Context {
	return context.WithValue(ctx, originKey{}, o)
}

// OriginFrom is the origin WithOrigin put in ctx, if any.
func OriginFrom(ctx context.Context) (Origin, bool) {
	if ctx == nil {
		return "", false
	}
	o, ok := ctx.Value(originKey{}).(Origin)
	return o, ok
}

// alwaysRead are subcommands that never change the repository.
var alwaysRead = map[string]bool{
	"log": true, "show": true, "status": true, "diff": true, "rev-parse": true,
	"rev-list": true, "for-each-ref": true, "show-ref": true, "cat-file": true,
	"blame": true, "ls-files": true, "ls-tree": true, "merge-base": true,
	"name-rev": true, "describe": true, "diff-tree": true, "diff-index": true,
	"diff-files": true, "check-ignore": true, "check-attr": true, "var": true,
	"version": true, "shortlog": true, "grep": true,
}

// Classify says whether args (git's arguments after -C dir) only read.
// Anything not known to be a read is a write, so it is always shown: a
// wrong "write" only makes the panel noisier, a wrong "read" would hide a
// change.
func Classify(args []string) Kind {
	sub, rest := subcommand(args)
	pos := positional(rest)
	read := false
	switch {
	case alwaysRead[sub]:
		read = true
	case sub == "symbolic-ref":
		read = len(pos) <= 1
	case sub == "worktree":
		read = len(pos) > 0 && pos[0] == "list"
	case sub == "submodule":
		read = len(pos) == 0 || pos[0] == "status"
	case sub == "stash":
		read = len(pos) > 0 && (pos[0] == "list" || pos[0] == "show")
	case sub == "reflog":
		read = len(pos) == 0 || pos[0] == "show"
	case sub == "config":
		read = has(rest, "--get", "--get-all", "--get-regexp", "--get-urlmatch", "--list", "-l") ||
			(len(pos) > 0 && (pos[0] == "get" || pos[0] == "list"))
	case sub == "branch":
		read = has(rest, "--list", "-l") || (len(pos) == 0 && !has(rest,
			"-d", "-D", "--delete", "-m", "-M", "--move", "-c", "-C", "--copy",
			"-u", "--unset-upstream", "--edit-description", "-f", "--force") && !prefixed(rest, "--set-upstream-to"))
	case sub == "tag":
		read = has(rest, "--list", "-l") || (len(pos) == 0 && !has(rest, "-d", "--delete"))
	}
	if read {
		return KindRead
	}
	return KindWrite
}

// subcommand skips git's global options (-c key=value, --no-pager, …) and
// returns the subcommand and what follows it.
func subcommand(args []string) (string, []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "-c" {
			i++
			continue
		}
		if strings.HasPrefix(a, "-") {
			continue
		}
		return a, args[i+1:]
	}
	return "", nil
}

// positional is rest without its options.
func positional(rest []string) []string {
	var out []string
	for _, a := range rest {
		if !strings.HasPrefix(a, "-") {
			out = append(out, a)
		}
	}
	return out
}

func has(rest []string, flags ...string) bool {
	for _, a := range rest {
		for _, f := range flags {
			if a == f {
				return true
			}
		}
	}
	return false
}

func prefixed(rest []string, prefix string) bool {
	for _, a := range rest {
		if strings.HasPrefix(a, prefix) {
			return true
		}
	}
	return false
}
```

Note: `branch -a --format=%(refname)` has no positional args and no write flag → read. `branch --set-upstream-to=origin/main` → `prefixed` → write.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/cmdlog/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cmdlog
git commit -m "feat(cmdlog): classify git commands as reads or writes; origin in the context"
```

---

### Task 3: cmdlog redaction

**Files:**
- Create: `internal/cmdlog/redact.go`
- Test: `internal/cmdlog/redact_test.go`

**Interfaces:**
- Produces:
  ```go
  func RedactArgs(args []string) (redacted []string, secrets []string)
  func MaskOutput(s string, secrets []string) string
  ```

- [ ] **Step 1: Write the failing tests** — `internal/cmdlog/redact_test.go`:

```go
package cmdlog

import (
	"reflect"
	"strings"
	"testing"
)

func TestRedactArgs(t *testing.T) {
	cases := []struct{ in, want string }{
		{"push https://bob:hunter22@github.com/o/r.git main", "push https://bob:***@github.com/o/r.git main"},
		{"fetch https://ghp_abcdef123@github.com/o/r.git", "fetch https://***@github.com/o/r.git"},
		{"-c http.https://github.com/.extraheader=AUTHORIZATION:_basic_c2VjcmV0 fetch", "-c http.https://github.com/.extraheader=*** fetch"},
		{"-c credential.token=abcd1234 push", "-c credential.token=*** push"},
		{"-c my.Password=letmein push", "-c my.Password=*** push"},
		{"-c core.quotepath=false status", "-c core.quotepath=false status"},
		{"remote add origin git@github.com:o/r.git", "remote add origin git@github.com:o/r.git"},
		{"push ssh://git@host/r.git", "push ssh://***@host/r.git"},
	}
	for _, c := range cases {
		got, _ := RedactArgs(strings.Fields(c.in))
		if strings.Join(got, " ") != c.want {
			t.Errorf("%q:\n got %q\nwant %q", c.in, strings.Join(got, " "), c.want)
		}
	}
}

func TestRedactArgsDoesNotModifyInput(t *testing.T) {
	in := []string{"push", "https://bob:hunter22@h/r"}
	RedactArgs(in)
	if !reflect.DeepEqual(in, []string{"push", "https://bob:hunter22@h/r"}) {
		t.Fatalf("input changed: %v", in)
	}
}

func TestMaskOutputHidesSecretsAndURLCreds(t *testing.T) {
	_, secrets := RedactArgs([]string{"push", "https://bob:hunter22@github.com/o/r.git"})
	out := MaskOutput("fatal: unable to access 'https://bob:hunter22@github.com/o/r.git/': denied; hunter22 again", secrets)
	if strings.Contains(out, "hunter22") {
		t.Fatalf("secret leaked: %q", out)
	}
	if !strings.Contains(out, "https://bob:***@github.com") {
		t.Fatalf("URL not redacted: %q", out)
	}
}

func TestMaskOutputIgnoresTinySecrets(t *testing.T) {
	// A 1-3 character "secret" would mask ordinary letters all over the output.
	if got := MaskOutput("a b c", []string{"a"}); got != "a b c" {
		t.Fatalf("got %q", got)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/cmdlog/ -run 'Redact|Mask' -v`
Expected: FAIL — undefined `RedactArgs`, `MaskOutput`.

- [ ] **Step 3: Implement** — `internal/cmdlog/redact.go`:

```go
package cmdlog

import (
	"regexp"
	"strings"
)

// urlCreds matches the userinfo of a URL: scheme://user[:password]@.
var urlCreds = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://)([^/@\s:]+)(:[^/@\s]*)?@`)

// minSecret is the shortest secret masked in output; shorter ones would
// mask ordinary text.
const minSecret = 4

// RedactArgs returns a copy of args with credentials replaced by ***, and
// the secrets it removed so MaskOutput can hide them in git's output too.
func RedactArgs(args []string) ([]string, []string) {
	out := make([]string, len(args))
	var secrets []string
	for i, a := range args {
		if i > 0 && args[i-1] == "-c" {
			if key, value, ok := strings.Cut(a, "="); ok && secretKey(key) {
				out[i] = key + "=***"
				secrets = append(secrets, value)
				continue
			}
		}
		out[i] = redactURLs(a, &secrets)
	}
	return out, secrets
}

// MaskOutput hides every secret, and any URL credentials, in s.
func MaskOutput(s string, secrets []string) string {
	for _, sec := range secrets {
		if len(sec) >= minSecret {
			s = strings.ReplaceAll(s, sec, "***")
		}
	}
	return redactURLs(s, nil)
}

func secretKey(key string) bool {
	k := strings.ToLower(key)
	if strings.HasPrefix(k, "http.") && strings.HasSuffix(k, ".extraheader") {
		return true
	}
	return strings.Contains(k, "token") || strings.Contains(k, "password") || strings.Contains(k, "secret")
}

// redactURLs replaces user:password@ with user:***@ and a bare user@ (often
// a token) with ***@, appending what it removed to secrets when non-nil.
func redactURLs(s string, secrets *[]string) string {
	return urlCreds.ReplaceAllStringFunc(s, func(m string) string {
		p := urlCreds.FindStringSubmatch(m)
		scheme, user, pass := p[1], p[2], p[3]
		if pass != "" {
			if secrets != nil {
				*secrets = append(*secrets, pass[1:])
			}
			return scheme + user + ":***@"
		}
		if secrets != nil {
			*secrets = append(*secrets, user)
		}
		return scheme + "***@"
	})
}
```

Note: `git@github.com:o/r.git` (scp syntax, no `://`) is not a credential and is left alone. `ssh://git@host` masks the user `git` — harmless, and a bare userinfo is a token often enough that masking wins.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/cmdlog/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cmdlog
git commit -m "feat(cmdlog): redact credentials from command lines and output"
```

---

### Task 4: cmdlog store

**Files:**
- Create: `internal/cmdlog/log.go`
- Test: `internal/cmdlog/log_test.go`

**Interfaces:**
- Consumes: `gitcmd.Record` (Task 1); `Classify`, `OriginFrom` (Task 2); `RedactArgs`, `MaskOutput` (Task 3).
- Produces:
  ```go
  const MaxEntries = 500; const MaxStream = 64 << 10; const MaxOutputBytes = 8 << 20
  type Outcome string // OutcomeOK "ok", OutcomeFailed "failed", OutcomeTimeout "timeout"
  type Entry struct {
      ID int64 `json:"id"`; Repo string `json:"repo"`; Args []string `json:"args"`
      Origin Origin `json:"origin"`; Kind Kind `json:"kind"`; Start time.Time `json:"start"`
      DurationMs int64 `json:"durationMs"`; ExitCode int `json:"exitCode"`; Outcome Outcome `json:"outcome"`
      OutputTruncated bool `json:"outputTruncated"`; OutputDropped bool `json:"outputDropped"`
  }
  type Output struct { Stdout string `json:"stdout"`; Stderr string `json:"stderr"` }
  var ErrNotFound error
  func New() *Log
  func (l *Log) Add(r gitcmd.Record) Entry
  func (l *Log) List(repo string) []Entry          // newest first; repo is a cleaned path
  func (l *Log) Output(repo string, id int64) (Output, error)
  func (l *Log) Clear(repo string)
  func RepoKey(dir string) string                  // filepath.Clean(dir)
  ```

- [ ] **Step 1: Write the failing tests** — `internal/cmdlog/log_test.go`:

```go
package cmdlog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"git-ui/internal/gitcmd"
)

func rec(dir string, args ...string) gitcmd.Record {
	return gitcmd.Record{Ctx: context.Background(), Dir: dir, Args: args, Start: time.Unix(100, 0), Duration: 42 * time.Millisecond}
}

func TestAddFillsEntry(t *testing.T) {
	l := New()
	r := rec("/r/", "push", "https://bob:hunter22@h/r.git")
	r.Err = &gitcmd.Error{ExitCode: 128}
	r.ExitCode = 128
	r.Stderr = "fatal: https://bob:hunter22@h/r.git denied"
	e := l.Add(r)

	if e.Repo != "/r" || e.Kind != KindWrite || e.Origin != OriginYou || e.Outcome != OutcomeFailed || e.ExitCode != 128 || e.DurationMs != 42 {
		t.Fatalf("entry wrong: %+v", e)
	}
	if strings.Contains(strings.Join(e.Args, " "), "hunter22") {
		t.Fatalf("args not redacted: %v", e.Args)
	}
	out, err := l.Output("/r", e.ID)
	if err != nil || strings.Contains(out.Stderr, "hunter22") {
		t.Fatalf("output not redacted: %+v %v", out, err)
	}
}

func TestOriginFallbackAndContext(t *testing.T) {
	l := New()
	if e := l.Add(rec("/r", "status")); e.Origin != OriginAuto || e.Kind != KindRead {
		t.Fatalf("read without origin: %+v", e)
	}
	if e := l.Add(rec("/r", "commit", "-m", "x")); e.Origin != OriginYou {
		t.Fatalf("write without origin: %+v", e)
	}
	r := rec("/r", "status")
	r.Ctx = WithOrigin(context.Background(), OriginAI)
	if e := l.Add(r); e.Origin != OriginAI {
		t.Fatalf("ctx origin ignored: %+v", e)
	}
}

func TestOutcomeTimeout(t *testing.T) {
	l := New()
	r := rec("/r", "fetch")
	r.Err = &gitcmd.Error{ExitCode: -1, Err: gitcmd.ErrTimeout}
	r.ExitCode = -1
	if e := l.Add(r); e.Outcome != OutcomeTimeout {
		t.Fatalf("got %s", e.Outcome)
	}
}

func TestListNewestFirstPerRepo(t *testing.T) {
	l := New()
	a1 := l.Add(rec("/a", "status"))
	l.Add(rec("/b", "status"))
	a2 := l.Add(rec("/a", "log"))
	got := l.List("/a")
	if len(got) != 2 || got[0].ID != a2.ID || got[1].ID != a1.ID {
		t.Fatalf("got %+v", got)
	}
	if len(l.List("/nope")) != 0 {
		t.Fatal("unknown repo must be empty")
	}
}

func TestRingKeepsLast500(t *testing.T) {
	l := New()
	var first, last Entry
	for i := 0; i < MaxEntries+10; i++ {
		e := l.Add(rec("/r", "status", fmt.Sprint(i)))
		if i == 0 {
			first = e
		}
		last = e
	}
	got := l.List("/r")
	if len(got) != MaxEntries || got[0].ID != last.ID {
		t.Fatalf("len %d, newest %d", len(got), got[0].ID)
	}
	if _, err := l.Output("/r", first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("evicted entry: got %v", err)
	}
}

func TestOutputTruncatedOnRuneBoundary(t *testing.T) {
	l := New()
	r := rec("/r", "log")
	r.Stdout = "a" + strings.Repeat("é", MaxStream) // the cap falls inside an é
	e := l.Add(r)
	out, _ := l.Output("/r", e.ID)
	if !e.OutputTruncated || len(out.Stdout) > MaxStream || !utf8.ValidString(out.Stdout) {
		t.Fatalf("truncated=%v len=%d valid=%v", e.OutputTruncated, len(out.Stdout), utf8.ValidString(out.Stdout))
	}
}

func TestOutputBudgetDropsOldestOutputs(t *testing.T) {
	l := New()
	big := strings.Repeat("x", MaxStream)
	n := MaxOutputBytes/MaxStream + 5
	var ids []int64
	for i := 0; i < n; i++ {
		r := rec("/r", "log")
		r.Stdout = big
		ids = append(ids, l.Add(r).ID)
	}
	list := l.List("/r")
	oldest, newest := list[len(list)-1], list[0]
	if !oldest.OutputDropped || newest.OutputDropped {
		t.Fatalf("oldest dropped=%v newest dropped=%v", oldest.OutputDropped, newest.OutputDropped)
	}
	if out, err := l.Output("/r", ids[0]); err != nil || out.Stdout != "" {
		t.Fatalf("dropped output: %q %v", out.Stdout, err)
	}
	if out, _ := l.Output("/r", ids[n-1]); out.Stdout != big {
		t.Fatal("newest output must be kept")
	}
}

func TestClear(t *testing.T) {
	l := New()
	l.Add(rec("/r", "status"))
	l.Clear("/r")
	if len(l.List("/r")) != 0 {
		t.Fatal("not cleared")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/cmdlog/ -run 'Add|Origin|Outcome|List|Ring|Output|Clear' -v`
Expected: FAIL — undefined `New`, `Entry`, etc.

- [ ] **Step 3: Implement** — `internal/cmdlog/log.go`:

```go
package cmdlog

import (
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"git-ui/internal/gitcmd"
)

const (
	// MaxEntries is how many commands each repository's log keeps.
	MaxEntries = 500
	// MaxStream caps each of stdout and stderr per command.
	MaxStream = 64 << 10
	// MaxOutputBytes caps the output kept per repository; past it the
	// oldest entries lose their output (not the entries themselves).
	MaxOutputBytes = 8 << 20
)

// Outcome is how a command ended.
type Outcome string

const (
	OutcomeOK      Outcome = "ok"
	OutcomeFailed  Outcome = "failed"
	OutcomeTimeout Outcome = "timeout"
)

var ErrNotFound = errors.New("command is no longer in the log")

// Entry is one command as the Commands panel lists it; its output is
// fetched separately with Output.
type Entry struct {
	ID              int64     `json:"id"`
	Repo            string    `json:"repo"`
	Args            []string  `json:"args"`
	Origin          Origin    `json:"origin"`
	Kind            Kind      `json:"kind"`
	Start           time.Time `json:"start"`
	DurationMs      int64     `json:"durationMs"`
	ExitCode        int       `json:"exitCode"`
	Outcome         Outcome   `json:"outcome"`
	OutputTruncated bool      `json:"outputTruncated"`
	OutputDropped   bool      `json:"outputDropped"`
}

// Output is what a command printed, redacted and capped.
type Output struct {
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
}

type item struct {
	entry Entry
	out   Output
}

type ring struct {
	items []item // oldest first
	bytes int    // output bytes held by items
}

// Log holds every repository's commands. Safe for concurrent use: git runs
// from many goroutines at once.
type Log struct {
	mu    sync.Mutex
	next  int64
	repos map[string]*ring
}

func New() *Log { return &Log{repos: map[string]*ring{}} }

// RepoKey is the key a command run in dir is logged under.
func RepoKey(dir string) string { return filepath.Clean(dir) }

// Add logs r and returns its entry.
func (l *Log) Add(r gitcmd.Record) Entry {
	args, secrets := RedactArgs(r.Args)
	kind := Classify(r.Args)
	origin, ok := OriginFrom(r.Ctx)
	if !ok {
		origin = OriginYou
		if kind == KindRead {
			origin = OriginAuto
		}
	}
	outcome := OutcomeOK
	if r.Err != nil {
		outcome = OutcomeFailed
		if errors.Is(r.Err, gitcmd.ErrTimeout) {
			outcome = OutcomeTimeout
		}
	}
	stdout, cut1 := capStream(MaskOutput(r.Stdout, secrets))
	stderr, cut2 := capStream(MaskOutput(r.Stderr, secrets))
	e := Entry{
		Repo: RepoKey(r.Dir), Args: args, Origin: origin, Kind: kind,
		Start: r.Start, DurationMs: r.Duration.Milliseconds(), ExitCode: r.ExitCode,
		Outcome: outcome, OutputTruncated: cut1 || cut2,
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	l.next++
	e.ID = l.next
	rg := l.repos[e.Repo]
	if rg == nil {
		rg = &ring{}
		l.repos[e.Repo] = rg
	}
	rg.items = append(rg.items, item{entry: e, out: Output{Stdout: stdout, Stderr: stderr}})
	rg.bytes += len(stdout) + len(stderr)
	if len(rg.items) > MaxEntries {
		old := rg.items[0]
		rg.bytes -= len(old.out.Stdout) + len(old.out.Stderr)
		rg.items = rg.items[1:]
	}
	for i := 0; rg.bytes > MaxOutputBytes && i < len(rg.items)-1; i++ {
		it := &rg.items[i]
		if n := len(it.out.Stdout) + len(it.out.Stderr); n > 0 {
			rg.bytes -= n
			it.out = Output{}
			it.entry.OutputDropped = true
		}
	}
	return e
}

// List is repo's log, newest first.
func (l *Log) List(repo string) []Entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	rg := l.repos[RepoKey(repo)]
	if rg == nil {
		return []Entry{}
	}
	out := make([]Entry, len(rg.items))
	for i, it := range rg.items {
		out[len(rg.items)-1-i] = it.entry
	}
	return out
}

// Output is what the command id printed; empty when it was dropped for the
// budget, ErrNotFound when the entry itself is gone.
func (l *Log) Output(repo string, id int64) (Output, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if rg := l.repos[RepoKey(repo)]; rg != nil {
		for _, it := range rg.items {
			if it.entry.ID == id {
				return it.out, nil
			}
		}
	}
	return Output{}, ErrNotFound
}

// Clear forgets repo's log.
func (l *Log) Clear(repo string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.repos, RepoKey(repo))
}

// capStream cuts s to MaxStream bytes without splitting a UTF-8 character.
func capStream(s string) (string, bool) {
	if len(s) <= MaxStream {
		return s, false
	}
	return strings.ToValidUTF8(s[:MaxStream], ""), true
}
```

Note: the ring drops `rg.items[0]` via reslicing; the backing array is re-allocated by `append` as it grows, so memory stays bounded by `MaxEntries`.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/cmdlog/ -v -race`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cmdlog
git commit -m "feat(cmdlog): per-repository ring of commands with capped, redacted output"
```

---

### Task 5: App wiring — recorder, bindings, AI origin

**Files:**
- Create: `internal/app/cmdlog.go`
- Modify: `internal/app/app.go` (`App` struct, `New`, `Startup`)
- Modify: `internal/app/ai.go` (`RunTool` in the chat run, ~line 377)
- Modify: `internal/app/chatwrite.go` (`runWriteTool`, around the `a.executeWrite` call)
- Regenerate: `frontend/wailsjs/go/app/App.js`, `App.d.ts`, `frontend/wailsjs/go/models.ts`
- Test: `internal/app/cmdlog_test.go`

**Interfaces:**
- Consumes: `cmdlog.New`, `(*Log).Add/List/Output/Clear`, `cmdlog.WithOrigin`, `cmdlog.OriginAI`, `gitcmd.SetRecorder`, `gitcmd.Record`.
- Produces (Wails bindings, used by Task 6):
  ```go
  const EventCommand = "cmdlog:entry"
  func (a *App) CommandLog(id string) ([]cmdlog.Entry, error)
  func (a *App) CommandLogOutput(id string, entryID int64) (cmdlog.Output, error)
  func (a *App) ClearCommandLog(id string) error
  ```

- [ ] **Step 1: Write the failing tests** — `internal/app/cmdlog_test.go`:

```go
package app

import (
	"context"
	"errors"
	"sync"
	"testing"

	"git-ui/internal/cmdlog"
)

func TestCommandLogRecordsAppCommands(t *testing.T) {
	a, id := newTestApp(t)
	if err := a.CreateBranch(id, "logged", "HEAD", false); err != nil {
		t.Fatal(err)
	}
	list, err := a.CommandLog(id)
	if err != nil {
		t.Fatal(err)
	}
	var found *cmdlog.Entry
	for i, e := range list {
		if e.Kind == cmdlog.KindWrite && len(e.Args) > 0 && e.Args[0] == "branch" {
			found = &list[i]
		}
	}
	if found == nil || found.Origin != cmdlog.OriginYou || found.Outcome != cmdlog.OutcomeOK {
		t.Fatalf("branch command not logged as a user write: %+v", list)
	}
	if _, err := a.CommandLogOutput(id, found.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.CommandLogOutput(id, -1); !errors.Is(err, cmdlog.ErrNotFound) {
		t.Fatalf("unknown id: got %v", err)
	}
	if err := a.ClearCommandLog(id); err != nil {
		t.Fatal(err)
	}
	if list, _ := a.CommandLog(id); len(list) != 0 {
		t.Fatalf("not cleared: %d", len(list))
	}
}

func TestWriteDuringAIExecutionIsAI(t *testing.T) {
	a, id := newTestApp(t)
	done := a.markAIWrite(id)
	err := a.CreateBranch(id, "by-ai", "HEAD", false)
	done()
	if err != nil {
		t.Fatal(err)
	}
	if err := a.CreateBranch(id, "by-user", "HEAD", false); err != nil {
		t.Fatal(err)
	}
	list, _ := a.CommandLog(id)
	origins := map[string]cmdlog.Origin{}
	for _, e := range list {
		if e.Kind == cmdlog.KindWrite && len(e.Args) > 1 && e.Args[0] == "branch" {
			origins[e.Args[1]] = e.Origin // git branch <name> HEAD
		}
	}
	if origins["by-ai"] != cmdlog.OriginAI || origins["by-user"] != cmdlog.OriginYou {
		t.Fatalf("origins: %v", origins)
	}
}

func TestCommandEventOnlyAfterStartup(t *testing.T) {
	a, id := newTestApp(t)
	var mu sync.Mutex
	var n int
	a.cmdEmit = func(cmdlog.Entry) { mu.Lock(); n++; mu.Unlock() }
	// Before Startup nothing may be emitted: Wails' EventsEmit on a
	// non-Wails ctx calls log.Fatalf.
	a.started.Store(false)
	a.GetRefs(id)
	mu.Lock()
	before := n
	mu.Unlock()
	a.started.Store(true)
	a.GetRefs(id)
	mu.Lock()
	defer mu.Unlock()
	if before != 0 || n == 0 {
		t.Fatalf("before=%d after=%d", before, n)
	}
}

func TestAIToolContextCarriesOrigin(t *testing.T) {
	ctx := aiToolContext(context.Background())
	if o, ok := cmdlog.OriginFrom(ctx); !ok || o != cmdlog.OriginAI {
		t.Fatalf("got %q %v", o, ok)
	}
}
```

(`App.CreateBranch(id, name, target string, checkout bool)` runs `git branch <name> <target>` through `a.write`; `GetRefs(id)` runs reads only.)

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/app/ -run 'CommandLog|AIExecution|CommandEvent|AIToolContext' -v`
Expected: FAIL — undefined `CommandLog`, `markAIWrite`, `cmdEmit`, `started`, `aiToolContext`.

- [ ] **Step 3: Implement**

`internal/app/cmdlog.go`:

```go
package app

import (
	"context"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"git-ui/internal/cmdlog"
	"git-ui/internal/gitcmd"
)

// EventCommand carries each finished git command to the Commands panel.
const EventCommand = "cmdlog:entry"

// recordGit is the gitcmd recorder: every command the app runs lands in
// the log, and once the window is up, in the panel.
func (a *App) recordGit(r gitcmd.Record) {
	if o, ok := cmdlog.OriginFrom(r.Ctx); !ok || o != cmdlog.OriginAI {
		if a.aiWriting(cmdlog.RepoKey(r.Dir)) && cmdlog.Classify(r.Args) == cmdlog.KindWrite {
			r.Ctx = cmdlog.WithOrigin(r.Ctx, cmdlog.OriginAI)
		}
	}
	e := a.cmds.Add(r)
	// Not through a.emit: the AI deps' Emit is what tests watch for chat
	// events, and every git command would flood it. Before Startup (and in
	// tests) there is no Wails runtime to emit to.
	if a.started.Load() && a.cmdEmit != nil {
		a.cmdEmit(e)
	}
}

// markAIWrite marks repository id as running an approved AI write until
// the returned func is called; writes run there meanwhile are logged as AI.
func (a *App) markAIWrite(id string) func() {
	dir, err := a.dir(id)
	if err != nil {
		return func() {}
	}
	key := cmdlog.RepoKey(dir)
	a.aiWrites.Store(key, struct{}{})
	return func() { a.aiWrites.Delete(key) }
}

func (a *App) aiWriting(repoKey string) bool {
	_, ok := a.aiWrites.Load(repoKey)
	return ok
}

// aiToolContext marks every git command an AI tool call runs as AI.
func aiToolContext(ctx context.Context) context.Context {
	return cmdlog.WithOrigin(ctx, cmdlog.OriginAI)
}

// CommandLog is repository id's git commands, newest first.
func (a *App) CommandLog(id string) ([]cmdlog.Entry, error) {
	dir, err := a.dir(id)
	if err != nil {
		return nil, err
	}
	return a.cmds.List(dir), nil
}

// CommandLogOutput is what command entryID printed.
func (a *App) CommandLogOutput(id string, entryID int64) (cmdlog.Output, error) {
	dir, err := a.dir(id)
	if err != nil {
		return cmdlog.Output{}, err
	}
	return a.cmds.Output(dir, entryID)
}

// ClearCommandLog forgets repository id's commands.
func (a *App) ClearCommandLog(id string) error {
	dir, err := a.dir(id)
	if err != nil {
		return err
	}
	a.cmds.Clear(dir)
	return nil
}

func wailsCommandEmitter(ctx context.Context) func(cmdlog.Entry) {
	return func(e cmdlog.Entry) { runtime.EventsEmit(ctx, EventCommand, e) }
}
```

Design note: the AI mark is read in `recordGit` itself, keyed by repository path, and applies only to writes. That covers `write`, `writeAll` and `writeWorktree` without touching them, and a concurrent refresh read in that window stays Auto (spec, "Origin").

In `internal/app/app.go`:
- add `"sync/atomic"` and `"git-ui/internal/cmdlog"`, `"git-ui/internal/gitcmd"` to imports;
- add to `App` (after `writes sync.Map`):

```go
	// cmds is the log behind the Commands panel; aiWrites marks
	// repositories (by cmdlog.RepoKey) running an approved AI write.
	cmds     *cmdlog.Log
	aiWrites sync.Map
	// started is set by Startup; cmdEmit sends a logged command to the
	// frontend and is nil until then.
	started atomic.Bool
	cmdEmit func(cmdlog.Entry)
```

- in `New`, after building `a` and before `return a`:

```go
	a.cmds = cmdlog.New()
	// One recorder per process: the most recently created App owns it —
	// there is one App in the real application.
	gitcmd.SetRecorder(a.recordGit)
```

- replace `Startup`:

```go
// Startup receives the Wails runtime context.
func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	a.cmdEmit = wailsCommandEmitter(ctx)
	a.started.Store(true)
}
```

In `internal/app/ai.go`, first line inside `RunTool: func(ctx context.Context, call ai.ToolCall, step int) string {`:

```go
				ctx = aiToolContext(ctx)
```

In `internal/app/chatwrite.go` `runWriteTool`, replace `done, err := a.executeWrite(repoID, call.Name, p)` with:

```go
	unmark := a.markAIWrite(repoID)
	done, err := a.executeWrite(repoID, call.Name, p)
	unmark()
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/... -race`
Expected: PASS — the new tests and every existing test (Review Focus 4: no `log.Fatalf` from Wails, AI event tests unaffected).

- [ ] **Step 5: Regenerate bindings**

Run: `source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && ~/go/bin/wails generate module`
Expected: `frontend/wailsjs/go/app/App.d.ts` now declares `CommandLog`, `CommandLogOutput`, `ClearCommandLog`; `models.ts` has `cmdlog.Entry` and `cmdlog.Output`. Then `git checkout -- frontend/wailsjs/runtime 2>/dev/null || true` (as `make build` does).

- [ ] **Step 6: Commit**

```bash
git add internal/app frontend/wailsjs/go
git commit -m "feat(app): log every git command with its origin; Commands bindings and event"
```

---

### Task 6: Commands panel (frontend)

**Files:**
- Create: `frontend/src/lib/cmdlog.ts`, `frontend/src/lib/cmdlog.test.ts`
- Create: `frontend/src/components/CommandLogPanel.svelte`
- Modify: `frontend/src/lib/types.ts`, `frontend/src/lib/api.ts`, `frontend/src/lib/stores.ts`, `frontend/src/components/Icon.svelte`

**Interfaces:**
- Consumes: bindings `CommandLog`, `CommandLogOutput`, `ClearCommandLog`; event `cmdlog:entry` (Task 5).
- Produces:
  ```ts
  // types.ts
  export type CommandOrigin = 'you' | 'ai' | 'auto'
  export interface CommandEntry { id: number; repo: string; args: string[]; origin: CommandOrigin; kind: 'read' | 'write'; start: string; durationMs: number; exitCode: number; outcome: 'ok' | 'failed' | 'timeout'; outputTruncated: boolean; outputDropped: boolean }
  export interface CommandOutput { stdout: string; stderr: string }
  // stores.ts
  export const commandsOpen: Writable<boolean>        // persisted 'commandsOpen', false
  export const commandsShowReads: Writable<boolean>   // persisted 'commandsShowReads', false
  // cmdlog.ts
  export const MAX_ENTRIES = 500
  export const ORIGIN_LABEL: Record<CommandOrigin, string>
  export function mergeEntries(list: CommandEntry[], more: CommandEntry[], repoPath: string): CommandEntry[]
  export function visibleEntries(list: CommandEntry[], showReads: boolean): CommandEntry[]
  export function emptyMessage(list: CommandEntry[], showReads: boolean): '' | 'none' | 'onlyReads'
  export function formatDuration(ms: number): string
  export function formatClock(iso: string): string
  export function commandLine(args: string[]): string
  export function outcomeText(e: CommandEntry): string
  export function isCommandsToggle(e: ToggleKey, platform: string, inTerminal: boolean): boolean
  export function commandsShortcutLabel(platform: string): string
  ```

- [ ] **Step 1: Write the failing tests** — `frontend/src/lib/cmdlog.test.ts`:

```ts
import { describe, expect, it } from 'vitest'
import { commandLine, commandsShortcutLabel, emptyMessage, formatClock, formatDuration, isCommandsToggle, MAX_ENTRIES, mergeEntries, outcomeText, visibleEntries } from './cmdlog'
import type { CommandEntry } from './types'

const entry = (id: number, over: Partial<CommandEntry> = {}): CommandEntry => ({
  id, repo: '/r', args: ['status'], origin: 'auto', kind: 'read', start: '2026-09-27T10:04:05Z',
  durationMs: 12, exitCode: 0, outcome: 'ok', outputTruncated: false, outputDropped: false, ...over,
})
const key = (over: Partial<KeyboardEvent>) => ({ code: 'KeyJ', ctrlKey: false, metaKey: false, altKey: false, shiftKey: false, ...over })

describe('mergeEntries', () => {
  it('keeps newest first and drops duplicates and other repos', () => {
    const list = mergeEntries([entry(2)], [entry(1), entry(3), entry(2), entry(4, { repo: '/other' })], '/r')
    expect(list.map((e) => e.id)).toEqual([3, 2, 1])
  })
  it('returns the same array when nothing is added', () => {
    const list = [entry(1)]
    expect(mergeEntries(list, [entry(1)], '/r')).toBe(list)
  })
  it('caps the list', () => {
    const many = Array.from({ length: MAX_ENTRIES + 20 }, (_, i) => entry(i + 1))
    const list = mergeEntries([], many, '/r')
    expect(list).toHaveLength(MAX_ENTRIES)
    expect(list[0].id).toBe(MAX_ENTRIES + 20)
  })
})

describe('visibleEntries and emptyMessage', () => {
  const list = [entry(1), entry(2, { kind: 'write', origin: 'you', args: ['commit'] })]
  it('hides reads unless asked', () => {
    expect(visibleEntries(list, false).map((e) => e.id)).toEqual([2])
    expect(visibleEntries(list, true)).toHaveLength(2)
  })
  it('explains an empty panel', () => {
    expect(emptyMessage([], false)).toBe('none')
    expect(emptyMessage([entry(1)], false)).toBe('onlyReads')
    expect(emptyMessage([entry(1)], true)).toBe('')
    expect(emptyMessage(list, false)).toBe('')
  })
})

describe('formatting', () => {
  it('formats durations', () => {
    expect(formatDuration(42)).toBe('42 ms')
    expect(formatDuration(999)).toBe('999 ms')
    expect(formatDuration(1300)).toBe('1.3 s')
  })
  it('formats the clock as HH:MM:SS local time', () => {
    const d = new Date('2026-09-27T10:04:05Z')
    const pad = (n: number) => String(n).padStart(2, '0')
    expect(formatClock(d.toISOString())).toBe(`${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`)
  })
  it('quotes arguments only when needed', () => {
    expect(commandLine(['log', '--format=%H', '-n', '10'])).toBe('git log --format=%H -n 10')
    expect(commandLine(['commit', '-m', "it's done"])).toBe(`git commit -m 'it'\\''s done'`)
    expect(commandLine(['diff', ''])).toBe("git diff ''")
  })
  it('describes the outcome', () => {
    expect(outcomeText(entry(1))).toBe('Exit code 0')
    expect(outcomeText(entry(1, { outcome: 'failed', exitCode: 128 }))).toBe('Failed · exit code 128')
    expect(outcomeText(entry(1, { outcome: 'timeout', exitCode: -1 }))).toBe('Timed out')
  })
})

describe('shortcut', () => {
  it('is Cmd+Shift+J on macOS', () => {
    expect(isCommandsToggle(key({ metaKey: true, shiftKey: true }), 'darwin', false)).toBe(true)
    expect(isCommandsToggle(key({ metaKey: true }), 'darwin', false)).toBe(false)
    expect(commandsShortcutLabel('darwin')).toBe('⌘⇧J')
  })
  it('is Ctrl+Shift+J elsewhere, left to the shell inside the terminal', () => {
    expect(isCommandsToggle(key({ ctrlKey: true, shiftKey: true }), 'linux', false)).toBe(true)
    expect(isCommandsToggle(key({ ctrlKey: true, shiftKey: true }), 'linux', true)).toBe(false)
    expect(commandsShortcutLabel('linux')).toBe('Ctrl+Shift+J')
  })
})
```

- [ ] **Step 2: Run to verify failure**

Run: `cd frontend && npm test -- cmdlog`
Expected: FAIL — cannot resolve `./cmdlog`.

- [ ] **Step 3: Implement the pure helpers**

Append to `frontend/src/lib/types.ts`:

```ts
export type CommandOrigin = 'you' | 'ai' | 'auto'
/** One git command the app ran, as the Commands panel lists it. */
export interface CommandEntry {
  id: number
  /** The repository's path, cleaned — compared with Repo.path. */
  repo: string
  args: string[]
  origin: CommandOrigin
  kind: 'read' | 'write'
  /** ISO timestamp. */
  start: string
  durationMs: number
  exitCode: number
  outcome: 'ok' | 'failed' | 'timeout'
  outputTruncated: boolean
  outputDropped: boolean
}
export interface CommandOutput { stdout: string; stderr: string }
```

In `frontend/src/lib/api.ts`, add `CommandEntry, CommandOutput` to the type import and these to `api`:

```ts
  commandLog: (id: string) => call<CommandEntry[]>(Go.CommandLog(id)),
  commandLogOutput: (id: string, entryId: number) => call<CommandOutput>(Go.CommandLogOutput(id, entryId)),
  clearCommandLog: (id: string) => call<void>(Go.ClearCommandLog(id)),
```

In `frontend/src/lib/stores.ts`, next to `terminalOpen`:

```ts
export const commandsOpen = persisted('commandsOpen', false)
/** Whether the Commands panel lists reads too (off: only writes). */
export const commandsShowReads = persisted('commandsShowReads', false)
```

`frontend/src/lib/cmdlog.ts`:

```ts
import type { ToggleKey } from './terminal'
import type { CommandEntry, CommandOrigin } from './types'

/** Same cap as the backend's per-repository log. */
export const MAX_ENTRIES = 500

export const ORIGIN_LABEL: Record<CommandOrigin, string> = { you: 'You', ai: 'AI', auto: 'Auto' }

/** mergeEntries adds more to list — only repoPath's, without duplicates
 *  (the initial load and live events overlap) — newest first, capped. */
export function mergeEntries(list: CommandEntry[], more: CommandEntry[], repoPath: string): CommandEntry[] {
  const seen = new Set(list.map((e) => e.id))
  const add = more.filter((e) => e.repo === repoPath && !seen.has(e.id) && seen.add(e.id))
  if (add.length === 0) return list
  return [...list, ...add].sort((a, b) => b.id - a.id).slice(0, MAX_ENTRIES)
}

export function visibleEntries(list: CommandEntry[], showReads: boolean): CommandEntry[] {
  return showReads ? list : list.filter((e) => e.kind === 'write')
}

/** Why the panel shows no rows: nothing yet, or only hidden reads. */
export function emptyMessage(list: CommandEntry[], showReads: boolean): '' | 'none' | 'onlyReads' {
  if (list.length === 0) return 'none'
  return visibleEntries(list, showReads).length === 0 ? 'onlyReads' : ''
}

export function formatDuration(ms: number): string {
  return ms < 1000 ? `${ms} ms` : `${(ms / 1000).toFixed(1)} s`
}

export function formatClock(iso: string): string {
  const d = new Date(iso)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

const PLAIN = /^[A-Za-z0-9_\-.,/:=@%+^~{}]+$/

/** commandLine is args as a shell command someone could paste. */
export function commandLine(args: string[]): string {
  const quote = (a: string) => (PLAIN.test(a) ? a : `'${a.replace(/'/g, `'\\''`)}'`)
  return ['git', ...args.map(quote)].join(' ')
}

export function outcomeText(e: CommandEntry): string {
  if (e.outcome === 'timeout') return 'Timed out'
  if (e.outcome === 'failed') return `Failed · exit code ${e.exitCode}`
  return `Exit code ${e.exitCode}`
}

/** ⌘⇧J on macOS, Ctrl+Shift+J elsewhere — left to the shell while focus
 *  is in the terminal, like Ctrl+J. Matched on the physical key. */
export function isCommandsToggle(e: ToggleKey, platform: string, inTerminal: boolean): boolean {
  if (e.code !== 'KeyJ' || !e.shiftKey || e.altKey) return false
  if (platform === 'darwin') return e.metaKey && !e.ctrlKey
  return e.ctrlKey && !e.metaKey && !inTerminal
}

export function commandsShortcutLabel(platform: string): string {
  return platform === 'darwin' ? '⌘⇧J' : 'Ctrl+Shift+J'
}
```

(`seen.add` returns the Set — truthy — so duplicates inside `more` are dropped too.)

In `frontend/src/components/Icon.svelte` add to `paths`:

```ts
    list: 'M5.5 4h8M5.5 8h8M5.5 12h8M2.5 4h.01M2.5 8h.01M2.5 12h.01',
```

- [ ] **Step 4: Run tests**

Run: `cd frontend && npm test -- cmdlog`
Expected: PASS.

- [ ] **Step 5: Write the panel** — `frontend/src/components/CommandLogPanel.svelte`:

```svelte
<script lang="ts">
  import { onDestroy } from 'svelte'
  import Icon from './Icon.svelte'
  import { api } from '../lib/api'
  import { commandLine, emptyMessage, formatClock, formatDuration, mergeEntries, ORIGIN_LABEL, outcomeText, visibleEntries } from '../lib/cmdlog'
  import { commandsOpen, commandsShowReads, selectedRepo } from '../lib/stores'
  import type { CommandEntry, CommandOutput } from '../lib/types'
  import { copyText } from '../lib/ui'
  import { EventsOn } from '../../wailsjs/runtime/runtime'

  let entries: CommandEntry[] = []
  let error = ''
  let expanded: number | null = null
  let outputs: Record<number, CommandOutput> = {}
  let outputErrors: Record<number, string> = {}
  let loadedId = ''
  let loadedPath = ''
  let list: HTMLElement

  $: repoId = $selectedRepo && !$selectedRepo.missing ? $selectedRepo.id : ''
  $: if (repoId !== loadedId) load(repoId, $selectedRepo?.path ?? '')
  $: shown = visibleEntries(entries, $commandsShowReads)
  $: empty = emptyMessage(entries, $commandsShowReads)

  async function load(id: string, path: string) {
    loadedId = id
    loadedPath = path
    entries = []
    outputs = {}
    outputErrors = {}
    expanded = null
    error = ''
    if (!id) return
    try {
      const loaded = await api.commandLog(id)
      if (loadedId === id) entries = mergeEntries(entries, loaded, path)
    } catch (e) {
      if (loadedId === id) error = String(e)
    }
  }

  const off = EventsOn('cmdlog:entry', (e: CommandEntry) => {
    if (loadedId) entries = mergeEntries(entries, [e], loadedPath)
  })
  onDestroy(off)

  async function toggle(e: CommandEntry) {
    expanded = expanded === e.id ? null : e.id
    if (expanded !== e.id || e.outputDropped || outputs[e.id] || outputErrors[e.id]) return
    try {
      outputs = { ...outputs, [e.id]: await api.commandLogOutput(loadedId, e.id) }
    } catch (err) {
      outputErrors = { ...outputErrors, [e.id]: String(err) }
    }
  }

  async function clear() {
    try {
      await api.clearCommandLog(loadedId)
      entries = []
      outputs = {}
      outputErrors = {}
      expanded = null
    } catch (e) {
      error = String(e)
    }
  }

  // ↑/↓ move between rows; Enter/Space on a row is the button's own click.
  function onKeydown(ev: KeyboardEvent) {
    if (ev.key !== 'ArrowDown' && ev.key !== 'ArrowUp') return
    const rows = Array.from(list.querySelectorAll<HTMLButtonElement>('button.main'))
    const at = rows.indexOf(document.activeElement as HTMLButtonElement)
    const next = rows[ev.key === 'ArrowDown' ? at + 1 : Math.max(0, at - 1)]
    if (next) {
      ev.preventDefault()
      next.focus()
    }
  }
</script>

<div class="panel">
  <header>
    <span class="title">Commands</span>
    <label class="reads"><input type="checkbox" bind:checked={$commandsShowReads} /> Show reads</label>
    <span class="spacer" />
    <button class="btn small" disabled={!entries.length} on:click={clear}>Clear</button>
    <button class="icon-btn" title="Hide commands" on:click={() => commandsOpen.set(false)}><Icon name="list" /></button>
  </header>
  {#if error}<div class="error ellipsis" title={error}>{error}</div>{/if}
  <!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
  <div class="body" role="list" bind:this={list} on:keydown={onKeydown}>
    {#each shown as e (e.id)}
      <div class="entry" role="listitem" class:failed={e.outcome !== 'ok'}>
        <div class="line">
          <button class="main" aria-expanded={expanded === e.id} title={commandLine(e.args)} on:click={() => toggle(e)}>
            <span class="mark" aria-label={e.outcome === 'ok' ? 'Succeeded' : outcomeText(e)}>{e.outcome === 'ok' ? '✓' : '✗'}</span>
            <span class="cmd ellipsis">{commandLine(e.args)}</span>
            <span class="badge {e.origin}">{ORIGIN_LABEL[e.origin]}</span>
            <span class="time">{formatClock(e.start)}</span>
            <span class="dur">{formatDuration(e.durationMs)}</span>
          </button>
          <button class="icon-btn" title="Copy command" on:click={() => copyText(commandLine(e.args))}><Icon name="copy" size={14} /></button>
        </div>
        {#if expanded === e.id}
          <div class="detail">
            <div class="meta">{outcomeText(e)}</div>
            {#if e.outputDropped}
              <div class="muted">Output no longer kept</div>
            {:else if outputErrors[e.id]}
              <div class="error">{outputErrors[e.id]}</div>
            {:else if outputs[e.id]}
              {#if outputs[e.id].stdout}<pre>{outputs[e.id].stdout}</pre>{/if}
              {#if outputs[e.id].stderr}<pre class="stderr">{outputs[e.id].stderr}</pre>{/if}
              {#if !outputs[e.id].stdout && !outputs[e.id].stderr}<div class="muted">No output</div>{/if}
            {/if}
            {#if e.outputTruncated}<div class="muted">Output truncated</div>{/if}
          </div>
        {/if}
      </div>
    {/each}
    {#if empty === 'none'}
      <div class="empty">No git commands yet</div>
    {:else if empty === 'onlyReads'}
      <div class="empty">Only reads so far — <button class="link" on:click={() => commandsShowReads.set(true)}>Show reads</button></div>
    {/if}
  </div>
</div>

<style>
  .panel { display: flex; flex-direction: column; height: 100%; min-height: 0; background: var(--surface); }
  header { display: flex; align-items: center; gap: 8px; padding: 4px 8px; border-bottom: 1px solid var(--border); flex: none; font-size: 12px; }
  .title { font-weight: 600; }
  .reads { display: flex; align-items: center; gap: 4px; color: var(--muted); }
  .spacer { flex: 1; }
  .error { padding: 4px 8px; font-size: 12px; color: var(--danger); }
  .body { flex: 1; min-height: 0; overflow-y: auto; font-size: 12px; }
  .line { display: flex; align-items: center; }
  .main { flex: 1; min-width: 0; display: flex; align-items: center; gap: 8px; padding: 3px 8px; background: none; border: 0; color: inherit; font: inherit; text-align: left; cursor: pointer; }
  .main:hover, .main:focus-visible { background: var(--hover); }
  .mark { flex: none; width: 1em; color: var(--ok); }
  .failed .mark { color: var(--danger); }
  .cmd { flex: 1; min-width: 0; font-family: var(--mono); }
  .badge { flex: none; padding: 0 6px; border: 1px solid var(--border); border-radius: 8px; font-size: 11px; color: var(--muted); }
  .badge.ai { color: var(--accent); border-color: var(--accent); }
  .time, .dur { flex: none; color: var(--muted); font-variant-numeric: tabular-nums; }
  .dur { width: 5em; text-align: right; }
  .detail { padding: 4px 8px 8px 28px; display: flex; flex-direction: column; gap: 4px; }
  .meta, .muted { color: var(--muted); }
  pre { margin: 0; max-height: 200px; overflow: auto; padding: 6px; background: var(--bg); border: 1px solid var(--border); border-radius: 6px; font-family: var(--mono); font-size: 11px; white-space: pre-wrap; word-break: break-word; }
  pre.stderr { color: var(--danger); }
  .empty { padding: 16px; text-align: center; color: var(--muted); }
  .link { background: none; border: 0; padding: 0; color: var(--accent); font: inherit; cursor: pointer; text-decoration: underline; }
</style>
```

If `btn small` does not exist in `theme.css` (`grep -n "\.btn\.small\|\.small" frontend/src/theme.css`), use plain `class="btn"`.

- [ ] **Step 6: Type-check and test**

Run: `cd frontend && npx svelte-check --fail-on-warnings=false && npm test`
Expected: no type errors in the new files; all tests PASS. (If the project has no `svelte-check`, run `npm run build` instead — check `frontend/package.json` scripts.)

- [ ] **Step 7: Commit**

```bash
git add frontend/src
git commit -m "feat(ui): Commands panel listing the repository's git commands"
```

---

### Task 7: Bottom dock, terminal move, toolbar toggle, shortcut, spec

**Files:**
- Create: `frontend/src/components/BottomDock.svelte`
- Modify: `frontend/src/App.svelte`, `frontend/src/lib/stores.ts`, `frontend/src/lib/toolbar.ts`, `frontend/src/lib/toolbar.test.ts`, `frontend/src/components/Toolbar.svelte`
- Docs: create `docs/spec/09-command-log.md`; modify `docs/spec/README.md`, `docs/spec/08-terminal.md`, `docs/spec/05-remote-and-stash.md`

**Interfaces:**
- Consumes: `CommandLogPanel` (Task 6), `commandsOpen`, `isCommandsToggle`, `commandsShortcutLabel` (Task 6), `TerminalPanel`, `terminalOpen`.
- Produces: `dockHeight` (replaces `terminalHeight`, same storage key), `dockSplit` stores; `ToolbarInput.commandsOpen`; toolbar item id `'commands'`.

- [ ] **Step 1: Write the failing toolbar tests** — in `frontend/src/lib/toolbar.test.ts`:
  - add `commandsOpen: false,` to the base input object (line ~10, next to `terminalOpen: false`);
  - in the test at line ~43 change `['terminal', 'folder', 'chat']` to `['terminal', 'commands', 'folder', 'chat']`;
  - add:

```ts
  it('has a Commands toggle after Terminal', () => {
    const ids = toolbarItems(input()).map((i) => i.id)
    expect(ids.indexOf('commands')).toBe(ids.indexOf('terminal') + 1)
    expect(item({ commandsOpen: true }, 'commands')).toMatchObject({ active: true, label: 'Commands', title: 'Hide git commands (⌘⇧J)' })
    expect(item({ commandsOpen: false, platform: 'linux' }, 'commands')).toMatchObject({ active: false, title: 'Show git commands (Ctrl+Shift+J)' })
  })
```

  (Use the file's existing helper names for building the input and picking an item — read the top of `toolbar.test.ts` first; `input`/`item` above stand for them.)

- [ ] **Step 2: Run to verify failure**

Run: `cd frontend && npm test -- toolbar`
Expected: FAIL — no `commands` item.

- [ ] **Step 3: Implement the toolbar item** — in `frontend/src/lib/toolbar.ts`:
  - `import { commandsShortcutLabel } from './cmdlog'`;
  - `ToolbarId` gains `| 'commands'`;
  - `ToolbarInput` gains `commandsOpen: boolean` after `terminalOpen`;
  - after the `terminal` item:

```ts
    item('commands', 'Commands', 'list', 'tools', '', `${i.commandsOpen ? 'Hide' : 'Show'} git commands (${commandsShortcutLabel(i.platform)})`, { active: i.commandsOpen }),
```

In `frontend/src/components/Toolbar.svelte`: import `commandsOpen` from `../lib/stores`, pass `commandsOpen: $commandsOpen` into `toolbarItems({...})`, and next to `case 'terminal'` add:

```ts
      case 'commands': return commandsOpen.update((open) => !open)
```

Run: `cd frontend && npm test -- toolbar` → PASS.

- [ ] **Step 4: Stores** — in `frontend/src/lib/stores.ts` replace

```ts
/** Height of the terminal under the chat, in pixels. */
export const terminalHeight = persisted('terminalHeight', 260)
```

with

```ts
/** Height of the bottom dock (Terminal and Commands), in pixels. Stored
 *  under the old terminal-height key so an existing setting carries over. */
export const dockHeight = persisted('terminalHeight', 260)
/** Width of the Terminal when Commands shares the dock with it, in pixels. */
export const dockSplit = persisted('dockSplit', 480)
```

Run `grep -rn "terminalHeight" frontend/src` — only `App.svelte` should use it; it is replaced in Step 6.

- [ ] **Step 5: BottomDock** — `frontend/src/components/BottomDock.svelte`:

```svelte
<script lang="ts">
  import CommandLogPanel from './CommandLogPanel.svelte'
  import Splitter from './Splitter.svelte'
  import TerminalPanel from './TerminalPanel.svelte'
  import { commandsOpen, dockSplit, terminalOpen } from '../lib/stores'

  const MIN = 240
  const clamp = (v: number, min: number, max: number) => Math.min(max, Math.max(min, v))
  let width = 0
</script>

<!-- TerminalPanel stays mounted for the app's lifetime even while hidden,
     so its xterm instances and scrollback survive; only its layout is
     toggled with CSS. The Commands panel is mounted only while open: it
     reloads its log from the backend. -->
<div class="dock" bind:clientWidth={width}>
  <div class="terminal" class:hidden={!$terminalOpen} style={$commandsOpen ? `flex: none; width: ${clamp($dockSplit, MIN, Math.max(MIN, width - MIN))}px` : 'flex: 1'}>
    <TerminalPanel />
  </div>
  {#if $terminalOpen && $commandsOpen}
    <Splitter on:drag={(e) => dockSplit.set(clamp($dockSplit + e.detail, MIN, Math.max(MIN, width - MIN)))} />
  {/if}
  {#if $commandsOpen}<div class="commands"><CommandLogPanel /></div>{/if}
</div>

<style>
  .dock { display: flex; height: 100%; min-height: 0; }
  .terminal { min-width: 0; min-height: 0; }
  .terminal.hidden { display: none; }
  .commands { flex: 1; min-width: 0; min-height: 0; }
</style>
```

- [ ] **Step 6: App layout** — in `frontend/src/App.svelte`:
  - imports: remove `TerminalPanel`; add `import BottomDock from './components/BottomDock.svelte'` and `import { isCommandsToggle } from './lib/cmdlog'`; in the stores import replace `terminalHeight` with `commandsOpen, dockHeight`.
  - replace `let sideHeight = 0` with `let mainHeight = 0` and add `$: dockOpen = $terminalOpen || $commandsOpen`.
  - after `toggleTerminal`, add:

```ts
  function toggleCommands(e: KeyboardEvent) {
    const inTerminal = e.target instanceof Element && !!e.target.closest('.xterm')
    if (isCommandsToggle(e, $platform, inTerminal)) {
      e.preventDefault()
      // Same rule as the terminal: opening needs a selected, present repository.
      commandsOpen.update((v) => (v ? false : !!$selectedRepo && !$selectedRepo.missing))
    }
  }
```

    and call `toggleCommands(e)` in `onKeydown` after `toggleTerminal(e)`.
  - replace everything from `<main>` to the closing `</section>` with:

```svelte
  <main bind:clientHeight={mainHeight}>
    <div class="view">
      {#if showChanges && $selectedRepo}
        <ChangesView repoId={$selectedRepo.id} />
      {:else if showStash && $selectedRepo && selectedStashEntry}
        <StashView repoId={$selectedRepo.id} entry={selectedStashEntry} />
      {:else if showBlame && $selectedRepo}
        <BlameView repoId={$selectedRepo.id} />
      {:else}
        <LogView />
      {/if}
    </div>
    {#if dockOpen}
      <Splitter direction="horizontal" on:drag={(e) => dockHeight.set(clamp($dockHeight - e.detail, 120, Math.max(120, mainHeight - 200)))} />
    {/if}
    <!-- The dock stays mounted while closed so the terminal's shells and
         scrollback survive; only its layout is toggled with CSS. -->
    <div class="dock" class:hidden={!dockOpen} style="height: {clamp($dockHeight, 120, Math.max(120, mainHeight - 200))}px"><BottomDock /></div>
  </main>
  {#if $chatOpen}
    <Splitter on:drag={(e) => chatWidth.set(clamp($chatWidth - e.detail, 260, 560))} />
    <section class="side" style="width: {$chatWidth}px"><ChatPanel /></section>
  {/if}
```

  - styles: replace the `main`, `.side`, `.side.hidden`, `.chat`, `.terminal`, `.terminal.hidden` rules with:

```css
  main { flex: 1; min-width: 0; display: flex; flex-direction: column; background: var(--surface); }
  .view { flex: 1; min-height: 0; }
  .dock { flex: none; min-height: 0; border-top: 1px solid var(--border); }
  .dock.hidden { display: none; }
  .side { flex: none; min-width: 0; display: flex; flex-direction: column; background: var(--bg); }
```

  Note: `ChatPanel` was previously kept mounted only while `$chatOpen` (`{#if $chatOpen}`), so moving it under the same `{#if}` changes nothing for it.

- [ ] **Step 7: Check the views still fill their pane** — `LogView`, `ChangesView`, `StashView`, `BlameView` used `main` as their parent; they now sit in `.view`. Run `grep -n "height: 100%" frontend/src/components/{LogView,ChangesView,StashView,BlameView}.svelte` — a root with `height: 100%` works inside the flex item `.view`. If one relied on `main` being `display: block`, give `.view` `display: flex; flex-direction: column` and the root `flex: 1`. Verify visually in Task 8.

- [ ] **Step 8: Tests and build**

Run: `cd frontend && npm test && npm run build`
Expected: all tests PASS; build succeeds with no new warnings.

- [ ] **Step 9: Spec docs**

Create `docs/spec/09-command-log.md`:

```markdown
# Command log

Every git command the application runs for a repository — because the user
clicked something, because the AI chat ran a tool, or because a view
refreshed — is recorded, with how it ended, and can be read in the Commands
panel. Commands typed in the embedded terminal are not recorded: the
terminal already shows them and their output.

## Opening it

The Commands panel lives in the bottom dock, under the repository view,
next to the terminal (see `08-terminal.md`). It opens and closes with the
repository toolbar's "Commands" toggle, the button in its own header, or
**Cmd+Shift+J** on macOS / **Ctrl+Shift+J** elsewhere (left to the shell
while focus is in the terminal, like Ctrl+J). It cannot open with no
repository selected or a missing one. When both the terminal and Commands
are open they share the dock's width with a draggable divider (each at
least 240 px); alone, either takes the whole width. The dock's height is
shared, draggable, and remembered.

## What it shows

The selected repository's commands, newest first. Each row shows:

- ✓ or ✗ (a timed-out command is a ✗ that says "Timed out" when expanded);
- the command line, `git …`, in monospace, cut with an ellipsis (the full
  line is in the tooltip);
- who asked for it: **You** (a change made from the interface), **AI** (a
  tool call of the chat, including the changes the user approved), or
  **Auto** (a read the application made on its own, such as a refresh —
  reads made because of a click count as Auto too);
- the time it started (HH:MM:SS) and how long it took.

Clicking a row (or Enter on it) expands it: the exit code and what the
command printed, standard output and standard error apart. Each stream is
kept up to 64 KB ("Output truncated" when cut); a repository keeps at most
8 MB of output, beyond which the oldest commands lose their output ("Output
no longer kept") but stay listed. A copy button copies the command line.
↑ and ↓ move between rows.

Reads are hidden unless **Show reads** is ticked (remembered). With nothing
to show the panel says "No git commands yet", or "Only reads so far" with a
Show reads button.

## Retention and privacy

The log is in memory only: the last 500 commands per repository, gone when
the application closes. **Clear** empties the selected repository's log.
Credentials never reach the log: a URL's password or bare token
(`https://user:***@host`, `https://***@host`), the value of a `-c` setting
for an HTTP extra header or any key containing "token", "password" or
"secret", and those same secrets wherever they appear in the output, are
replaced by `***`.
```

In `docs/spec/README.md`, after the Terminal row add:

```markdown
| [Command log](09-command-log.md) | Every git command the application ran, who asked for it and how it ended |
```

In `docs/spec/08-terminal.md`, replace the first paragraph of "Layout and visibility" (from "The terminal lives in the same column…" up to "…rather than the character it produces:") with:

```markdown
The terminal lives in the bottom dock, under the repository view, separated
from it by a draggable divider; the dock's height is remembered between
sessions (and carries over the height the terminal had when it lived under
the chat). The chat panel has the right-hand column to itself. The dock also
holds the Commands panel (see `09-command-log.md`): when both are open they
sit side by side with a draggable divider between them, and either one alone
takes the whole width. Two toggles open the terminal: the repository
toolbar's "Terminal" button (see `05-remote-and-stash.md`), highlighted while
the terminal is open, whose tooltip names the shortcuts and which is not
shown while the selected repository is missing; and the button in the
terminal panel's own header to hide it again. Two shortcuts toggle it from
anywhere in the window, both matched on the physical key rather than the
character it produces:
```

and in its last paragraph change "Resizing the panel (dragging either divider)" to "Resizing the panel (dragging any of its dividers)".

In `docs/spec/05-remote-and-stash.md`:
- line ~36: "· Terminal, Finder" → "· Terminal, Commands, Finder";
- after the `| Terminal | … |` table row add:

```markdown
| Commands | A toggle, like Terminal, for the Commands panel (see `09-command-log.md`) | never |
```

- [ ] **Step 10: Commit**

```bash
git add frontend/src docs/spec
git commit -m "feat(ui): bottom dock under the repository view with Terminal and Commands; Commands toggle and ⌘⇧J"
```

---

### Task 8: Build, run, and check by hand

**Files:** none (fixes found here go in their own commit)

- [ ] **Step 1: Full test run**

Run: `go test ./... -race && (cd frontend && npm test)`
Expected: PASS.

- [ ] **Step 2: Run the app** — `make dev` (Node 22), select `/tmp/git-ui-demo` (recreate it with a few commits if missing).

- [ ] **Step 3: Check, in order**
  1. Toolbar shows Terminal · Commands · Finder · Chat; Commands tooltip "Show git commands (⌘⇧J)".
  2. ⌘J opens the terminal under the log, full width; ⌘⇧J opens Commands beside it; drag the vertical divider; drag the horizontal divider; quit and relaunch — both sizes remembered; the old terminal height carried over.
  3. Chat opens alone in the right column; closing the terminal keeps its shell running (reopen: scrollback intact).
  4. Stage a file and commit from the UI → rows `git add …`/`git commit …` with **You**, ✓.
  5. Push to a non-existent remote (e.g. `git remote add bad https://user:secret1234@example.invalid/r.git` in the terminal, then push to it from the UI or AI) → ✗ row; expanded shows stderr; `secret1234` appears nowhere.
  6. Ask the chat to stage a file and approve → rows with **AI**.
  7. Tick Show reads → Auto reads appear; untick → gone. Clear → "No git commands yet".
  8. Light, dark and high contrast themes: badges, ✓/✗ and `<pre>` readable.
  9. Select another repository → its own log; the previous one's rows do not appear (Review Focus 5).

- [ ] **Step 4: Commit any fixes**, each with a message naming what it fixes.
