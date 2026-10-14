# In-app credential prompts Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When git needs a credential no helper has, CommitTree asks for it in its own dialog (HTTPS username + token, SSH passphrase / password / unknown host key) for the network commands the user starts, with an optional "Remember" through git's own helpers.

**Architecture:** A new package `internal/credprompt` runs a Unix-socket server inside the app. A marked `context` makes `gitcmd` start git with an environment that appends CommitTree's own binary as the **last** credential helper (via `GIT_CONFIG_COUNT`) and as `GIT_ASKPASS`/`SSH_ASKPASS`; that binary, relaunched in helper mode (`main()` dispatches before Wails), forwards each request over the socket with a per-command nonce. The app turns requests into `credential:request` events, a Svelte dialog answers through `App.AnswerCredential`.

**Tech Stack:** Go 1.x (stdlib `net`, `crypto/rand`, `net/http/cgi` for tests), Wails v2.16, Svelte + TypeScript, vitest.

**Spec:** `docs/superpowers/specs/2026-10-13-credential-prompts-design.md`

## Global Constraints

- Run git as `/usr/bin/git` in your own shell commands (the `rtk` hook rewrites bare `git`). Product code keeps calling `git` through `gitcmd`.
- Commits: conventional messages, **no `Co-Authored-By` line, ever**.
- Work only in the worktree `/Users/josfh/playground/git-ui/.claude/worktrees/commitree-credential-prompts-ce1953`; prefix every Bash with `cd <that path> &&`. Never merge; the owner says when (`merge --no-ff`).
- Dialog timeout: **60 s**, countdown text shown in the **last 20 s**: `Closes in N s`.
- Outcome messages, verbatim:
  - `No credentials entered.`
  - `No credentials entered (timed out).`
  - `The SSH host was not trusted.`
  - `The username or token was not accepted.`
  - `The saved credential was not accepted and was removed. Try again to enter a new one.`
- Env names: `COMMITREE_PROMPT_SOCK`, `COMMITREE_PROMPT_NONCE`; helper argument `credential-helper`.
- Events: `credential:request`, `credential:closed`, `credential:notice`. Bindings: `AnswerCredential(id, answer)`, `CancelCredential(id)`.
- The background fetch (`ops.AutoFetch`) is never marked and keeps `NoPromptEnv`.
- A secret (token, passphrase, password) must never reach: the command log, any emitted event, a toast or notification, the AI chat, a Go log line.
- Unix only: `internal/credprompt` server/helper files carry `//go:build !windows`; a `windows` stub keeps the package compiling with `Allow` returning ctx unchanged.
- Go tests: `go test ./internal/...`; frontend: `cd frontend && npx vitest run`; types: `cd frontend && npx svelte-check` (node 22: `source ~/.nvm/nvm.sh && nvm use 22`).
- Behaviour changes update `docs/spec/*` in the same commit as the code they describe (Task 9 owns the doc pages; earlier tasks update the design spec only if they deviate from it).

## Review Focus

1. **The app lives in a path with spaces or quotes** (`/Applications/My Apps/Commit'Tree.app`) — the helper must still run. Pinned in Task 2 (`shellQuote`) and Task 5 (`TestHelperPathWithSpaceAndQuote`).
2. **The user cancels the HTTPS dialog** — git then falls back to askpass (`Username for '…':`); no second dialog may appear and the message is `No credentials entered.` Pinned in Task 4 (`TestGitOwnPromptIsRefused`) and Task 5 (`TestCancelShowsOneDialog`).
3. **A stale token sits in the user's helper** — the first attempt fails with the "saved credential … removed" message and the helper no longer holds it; the second attempt asks. Pinned in Task 5 (`TestStaleSavedCredential`).
4. **The command ends while its dialog is open** (status-bar Cancel, the command's own timeout) — the dialog closes, no credential outcome replaces the command's own error; `TestRemote`'s timeout must exceed the 60 s dialog. Pinned in Task 4 (`TestEndClosesPending`) and Task 6 (`RemoteTestTimeout` = 90 s, `TestRemoteTimeoutOutlastsDialog`).
5. **Two prompting commands at once** (a clone and a push) — one dialog at a time, the second queued with `waiting` updated. Pinned in Task 4 (`TestQueueShowsOneAtATime`).

---

## File map

| File | Responsibility |
|---|---|
| `internal/gitcmd/prompt.go` (new) | `PromptSession`, `WithPrompts`, `PromptError` — the hook gitcmd calls |
| `internal/gitcmd/gitcmd.go` | `run` uses the hook; `RunInput` (stdin) |
| `internal/credprompt/wire.go` (new) | git credential protocol parse/format, credential URL |
| `internal/credprompt/sshprompt.go` (new) | classify askpass prompt texts |
| `internal/credprompt/env.go` (new) | env of a prompting command, shell quoting, helper label |
| `internal/credprompt/remember.go` (new) | which helper "Remember" names; approve/reject |
| `internal/credprompt/server.go` (new) | socket server, sessions, queue, timeout, outcomes |
| `internal/credprompt/helper.go` (new) | `RunHelper`: the helper-mode program |
| `internal/credprompt/stub_windows.go` (new) | Windows: no prompts |
| `internal/testrepo/authserver.go` (new) | smart-HTTP git server with basic auth, for tests |
| `internal/app/credentials.go` (new) | bindings, UI events, `prompting` marks |
| `internal/app/{remote,app,submodules,remotes,clone,terminal}.go` | call sites marked; Shutdown |
| `internal/ops/remotes.go` | `TestRemote` may prompt; timeout 90 s |
| `internal/clone/clone.go` | `Explain` passes a prompt outcome through |
| `main.go` | helper-mode dispatch; server wiring |
| `frontend/src/lib/credential.ts` (new) | pure dialog logic + store |
| `frontend/src/components/CredentialDialog.svelte` (new) | the modal |
| `frontend/src/{App.svelte,components/StatusBar.svelte,components/CloneDialog.svelte,lib/api.ts}` | mount, waiting text, binding wrappers |
| `docs/spec/*.md` | behaviour docs |

---

### Task 1: gitcmd prompt hook and stdin

**Files:**
- Create: `internal/gitcmd/prompt.go`, `internal/gitcmd/prompt_test.go`
- Modify: `internal/gitcmd/gitcmd.go` (`Error.Error`, `runOpts`, `run`, new `RunInput`)

**Interfaces:**
- Produces:
  - `type PromptSession interface { Env() []string; End(failed bool) error }`
  - `func WithPrompts(ctx context.Context, begin func() PromptSession) context.Context`
  - `type PromptError struct{ Msg string }` (implements `error`; `(*Error).Error()` returns `Msg` alone when `Err` wraps one)
  - `func RunInput(ctx context.Context, dir string, timeout time.Duration, env []string, stdin string, args ...string) (string, error)`

- [ ] **Step 1: Write the failing tests** — `internal/gitcmd/prompt_test.go`:

```go
package gitcmd

import (
	"context"
	"crypto/sha1"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

type fakeSession struct {
	env     []string
	outcome error
	ended   []bool
}

func (f *fakeSession) Env() []string { return f.env }
func (f *fakeSession) End(failed bool) error {
	f.ended = append(f.ended, failed)
	return f.outcome
}

func marked(f *fakeSession) context.Context {
	return WithPrompts(context.Background(), func() PromptSession { return f })
}

func TestPromptSessionEnvReachesGit(t *testing.T) {
	f := &fakeSession{env: []string{"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=test.x", "GIT_CONFIG_VALUE_0=hello"}}
	out, err := Run(marked(f), t.TempDir(), ReadTimeout, "config", "--get", "test.x")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "hello" {
		t.Fatalf("got %q", out)
	}
	if len(f.ended) != 1 || f.ended[0] {
		t.Fatalf("End calls = %v, want [false]", f.ended)
	}
}

func TestCallerEnvBeatsPromptEnv(t *testing.T) {
	f := &fakeSession{env: []string{"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=test.x", "GIT_CONFIG_VALUE_0=session"}}
	out, err := RunEnv(marked(f), t.TempDir(), ReadTimeout, []string{"GIT_CONFIG_VALUE_0=caller"}, "config", "--get", "test.x")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "caller" {
		t.Fatalf("got %q", out)
	}
}

func TestPromptOutcomeReplacesGitMessage(t *testing.T) {
	f := &fakeSession{outcome: &PromptError{Msg: "No credentials entered."}}
	_, err := Run(marked(f), t.TempDir(), ReadTimeout, "config", "--get", "nope.missing")
	if err == nil {
		t.Fatal("want an error")
	}
	if err.Error() != "No credentials entered." {
		t.Fatalf("message %q", err.Error())
	}
	var pe *PromptError
	if !errors.As(err, &pe) {
		t.Fatal("want a *PromptError in the chain")
	}
	var gerr *Error
	if !errors.As(err, &gerr) {
		t.Fatalf("want *Error kept, got %#v", err)
	}
	if len(f.ended) != 1 || !f.ended[0] {
		t.Fatalf("End calls = %v, want [true]", f.ended)
	}
}

func TestCancelBeatsPromptOutcome(t *testing.T) {
	f := &fakeSession{outcome: &PromptError{Msg: "No credentials entered."}}
	ctx := marked(f)
	rec := &Recorder{Begin: func(s Start) int64 { s.Cancel(); return 1 }}
	SetRecorder(rec)
	defer SetRecorder(nil)
	_, err := Run(ctx, t.TempDir(), ReadTimeout, "hash-object", "--stdin")
	if err == nil || !errors.Is(err, ErrCancelled) {
		t.Fatalf("want ErrCancelled, got %v", err)
	}
}

func TestUnmarkedContextHasNoSession(t *testing.T) {
	if promptsFrom(context.Background()) != nil {
		t.Fatal("unmarked ctx returned a begin func")
	}
}

func TestNilSessionIsIgnored(t *testing.T) {
	ctx := WithPrompts(context.Background(), func() PromptSession { return nil })
	if _, err := Run(ctx, t.TempDir(), ReadTimeout, "--version"); err != nil {
		t.Fatal(err)
	}
}

func TestRunInputFeedsStdin(t *testing.T) {
	out, err := RunInput(context.Background(), t.TempDir(), 5*time.Second, nil, "x", "hash-object", "--stdin")
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("%x", sha1.Sum([]byte("blob 1\x00x")))
	if strings.TrimSpace(out) != want {
		t.Fatalf("got %q want %q", out, want)
	}
}
```

Note for `TestCancelBeatsPromptOutcome`: the recorder's `Begin` cancels before git starts, so `cmd.Run` fails at once with the cancel cause set; the test pins that a cancelled command keeps `ErrCancelled` even when the session reports an outcome.

- [ ] **Step 2: Run to verify they fail**

Run: `cd <worktree> && go test ./internal/gitcmd/ -run 'Prompt|Caller|Cancel|Unmarked|NilSession|RunInput' -v`
Expected: build failure — `WithPrompts`, `PromptSession`, `PromptError`, `RunInput`, `promptsFrom` undefined.

- [ ] **Step 3: Implement** — `internal/gitcmd/prompt.go`:

```go
package gitcmd

import "context"

// PromptSession is what a context marked by WithPrompts gives one git
// command: environment that lets git and ssh ask the user through
// CommitTree, and an End called once git has exited.
type PromptSession interface {
	// Env is appended to git's environment before the caller's own env, so
	// a caller's env (NoPromptEnv above all) still wins.
	Env() []string
	// End closes the session; failed says whether git exited unsuccessfully.
	// A non-nil result is why it failed — the user cancelled or let the
	// dialog time out, or the credential was refused — and replaces git's
	// own message.
	End(failed bool) error
}

type promptKey struct{}

// WithPrompts marks ctx: every git command run with it calls begin just
// before starting and, when begin returns a session, runs with its Env.
// Unmarked contexts never prompt.
func WithPrompts(ctx context.Context, begin func() PromptSession) context.Context {
	return context.WithValue(ctx, promptKey{}, begin)
}

func promptsFrom(ctx context.Context) func() PromptSession {
	if ctx == nil {
		return nil
	}
	b, _ := ctx.Value(promptKey{}).(func() PromptSession)
	return b
}

// PromptError is a prompt session's reason a command failed. Its message is
// the whole error the user sees: it says what happened in their words, where
// git's own stderr ("could not read Username…") would not.
type PromptError struct{ Msg string }

func (e *PromptError) Error() string { return e.Msg }
```

In `internal/gitcmd/gitcmd.go`:

1. `(*Error).Error()` — first lines of the method:

```go
	var pe *PromptError
	if errors.As(e.Err, &pe) {
		return pe.Msg
	}
```

2. `runOpts` gains `stdin string` (comment: `// fed to git's stdin; never recorded`), and add after `RunStream`:

```go
// RunInput is RunEnv with stdin fed to git — for `git credential approve`
// and `reject`, whose fields (a password among them) must never be args.
// The command log records args, stdout and stderr only, never stdin.
func RunInput(ctx context.Context, dir string, timeout time.Duration, env []string, stdin string, args ...string) (string, error) {
	return run(ctx, dir, runOpts{timeout: timeout, env: env, stdin: stdin}, args)
}
```

3. In `run`, replace the two `cmd.Env = …` lines with:

```go
	var ps PromptSession
	if b := promptsFrom(caller); b != nil {
		ps = b()
	}
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	if ps != nil {
		cmd.Env = append(cmd.Env, ps.Env()...)
	}
	cmd.Env = append(cmd.Env, o.env...)
	if o.stdin != "" {
		cmd.Stdin = strings.NewReader(o.stdin)
	}
```

4. Right after `runErr := cmd.Run()` and `lines.flush()`:

```go
	var outcome error
	if ps != nil {
		outcome = ps.End(runErr != nil)
	}
```

and in the error `switch`, add a last case:

```go
		case outcome != nil:
			gerr.Err = outcome
```

- [ ] **Step 4: Run tests**

Run: `cd <worktree> && go test ./internal/gitcmd/ -v`
Expected: all PASS (old tests too).

- [ ] **Step 5: Commit**

```bash
cd <worktree> && /usr/bin/git add internal/gitcmd && /usr/bin/git commit -m "feat(gitcmd): prompt sessions from a marked context, RunInput with stdin"
```

---

### Task 2: credprompt pure helpers (protocol, prompt classification, env)

**Files:**
- Create: `internal/credprompt/wire.go`, `internal/credprompt/sshprompt.go`, `internal/credprompt/env.go`, `internal/credprompt/pure_test.go`

**Interfaces:**
- Produces:
  - `func ParseCredential(r io.Reader) (map[string]string, error)`
  - `func FormatCredential(f map[string]string) string`
  - `func CredentialURL(f map[string]string) string`
  - `type Kind string` with `KindHTTPS="https"`, `KindPassphrase="ssh-passphrase"`, `KindPassword="ssh-password"`, `KindHostKey="ssh-hostkey"`, `KindOther="ssh-other"`, `KindGitPrompt="git-prompt"`
  - `type Prompt struct{ Kind Kind; Host, User, KeyPath, KeyType, Fingerprint, Text string }`
  - `func ClassifyPrompt(text string) Prompt`
  - `const EnvSock = "COMMITREE_PROMPT_SOCK"`, `const EnvNonce = "COMMITREE_PROMPT_NONCE"`
  - `func shellQuote(s string) string`
  - `func sessionEnv(exe, sock, nonce, inheritedCount string) []string`
  - `func helperLabel(value string) string`

- [ ] **Step 1: Write the failing tests** — `internal/credprompt/pure_test.go`:

```go
package credprompt

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

func TestParseCredential(t *testing.T) {
	f, err := ParseCredential(strings.NewReader("protocol=https\nhost=github.com\nusername=me\n\nignored=1\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"protocol": "https", "host": "github.com", "username": "me"}
	if !reflect.DeepEqual(f, want) {
		t.Fatalf("got %v", f)
	}
}

func TestParseCredentialEOFWithoutBlankLine(t *testing.T) {
	f, _ := ParseCredential(strings.NewReader("host=a"))
	if f["host"] != "a" {
		t.Fatalf("got %v", f)
	}
}

func TestFormatCredentialSortedAndRoundTrips(t *testing.T) {
	in := map[string]string{"username": "me", "password": "p=w", "host": "h", "protocol": "https"}
	s := FormatCredential(in)
	if s != "host=h\npassword=p=w\nprotocol=https\nusername=me\n" {
		t.Fatalf("got %q", s)
	}
	back, _ := ParseCredential(strings.NewReader(s))
	if !reflect.DeepEqual(back, in) {
		t.Fatalf("round trip %v", back)
	}
}

func TestCredentialURL(t *testing.T) {
	cases := map[string]map[string]string{
		"https://github.com":            {"protocol": "https", "host": "github.com"},
		"https://github.com/org/r.git":  {"protocol": "https", "host": "github.com", "path": "org/r.git"},
		"http://127.0.0.1:8080/x":       {"protocol": "http", "host": "127.0.0.1:8080", "path": "x"},
	}
	for want, f := range cases {
		if got := CredentialURL(f); got != want {
			t.Errorf("%v → %q, want %q", f, got, want)
		}
	}
}

func TestClassifyPrompt(t *testing.T) {
	hostKey := "The authenticity of host 'github.com (140.82.121.4)' can't be established.\n" +
		"ED25519 key fingerprint is SHA256:+DiY3wvvV6TuJJhbpZisF/zLDA0zPMSvHdkr4UvCOqU.\n" +
		"This key is not known by any other names.\n" +
		"Are you sure you want to continue connecting (yes/no/[fingerprint])? "
	rsaPort := "The authenticity of host '[git.example.com]:2222 ([10.0.0.1]:2222)' can't be established.\n" +
		"RSA key fingerprint is SHA256:abcDEF123.\n" +
		"Are you sure you want to continue connecting (yes/no/[fingerprint])? "
	cases := []struct {
		in   string
		want Prompt
	}{
		{"Enter passphrase for key '/Users/me/.ssh/id_ed25519': ", Prompt{Kind: KindPassphrase, KeyPath: "/Users/me/.ssh/id_ed25519"}},
		{"git@example.com's password: ", Prompt{Kind: KindPassword, User: "git", Host: "example.com"}},
		{hostKey, Prompt{Kind: KindHostKey, Host: "github.com", KeyType: "ED25519", Fingerprint: "SHA256:+DiY3wvvV6TuJJhbpZisF/zLDA0zPMSvHdkr4UvCOqU"}},
		{rsaPort, Prompt{Kind: KindHostKey, Host: "[git.example.com]:2222", KeyType: "RSA", Fingerprint: "SHA256:abcDEF123"}},
		{"Username for 'https://github.com': ", Prompt{Kind: KindGitPrompt}},
		{"Password for 'https://me@github.com': ", Prompt{Kind: KindGitPrompt}},
		{"Verification code: ", Prompt{Kind: KindOther, Text: "Verification code:"}},
	}
	for _, c := range cases {
		got := ClassifyPrompt(c.in)
		if c.want.Text == "" && c.want.Kind != KindOther {
			got.Text = ""
		}
		if got != c.want {
			t.Errorf("%q\n got  %+v\n want %+v", c.in, got, c.want)
		}
	}
}

func TestShellQuoteSurvivesSh(t *testing.T) {
	for _, s := range []string{"/Applications/CommitTree.app/Contents/MacOS/CommitTree", "/Apps/My Apps/Commit'Tree", `/a/"b"/$c`} {
		out, err := exec.Command("sh", "-c", "printf %s "+shellQuote(s)).Output()
		if err != nil {
			t.Fatal(err)
		}
		if string(out) != s {
			t.Errorf("%q came back as %q", s, out)
		}
	}
}

func TestSessionEnv(t *testing.T) {
	got := sessionEnv("/x/Commit Tree", "/tmp/ct-1/s", "abc", "")
	want := []string{
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=credential.helper",
		"GIT_CONFIG_VALUE_0=!'/x/Commit Tree' credential-helper",
		"GIT_ASKPASS=/x/Commit Tree",
		"SSH_ASKPASS=/x/Commit Tree",
		"SSH_ASKPASS_REQUIRE=force",
		"COMMITREE_PROMPT_SOCK=/tmp/ct-1/s",
		"COMMITREE_PROMPT_NONCE=abc",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q", got)
	}
}

func TestSessionEnvAfterInheritedCount(t *testing.T) {
	got := sessionEnv("/x", "/s", "n", "2")
	if got[0] != "GIT_CONFIG_COUNT=3" || got[1] != "GIT_CONFIG_KEY_2=credential.helper" || !strings.HasPrefix(got[2], "GIT_CONFIG_VALUE_2=") {
		t.Fatalf("got %q", got[:3])
	}
	if bad := sessionEnv("/x", "/s", "n", "junk"); bad[0] != "GIT_CONFIG_COUNT=1" {
		t.Fatalf("junk count: %q", bad[0])
	}
}

func TestHelperLabel(t *testing.T) {
	cases := map[string]string{
		"osxkeychain":                                   "osxkeychain",
		"store --file ~/.creds":                         "store",
		"/usr/local/share/gcm-core/git-credential-manager": "manager",
		"!f() { echo; }; f":                             "your credential helper",
		"  cache --timeout=60 ":                         "cache",
	}
	for in, want := range cases {
		if got := helperLabel(in); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `cd <worktree> && go test ./internal/credprompt/ -v`
Expected: build failure (package has no non-test files / undefined symbols).

- [ ] **Step 3: Implement**

`internal/credprompt/wire.go`:

```go
// Package credprompt lets git and ssh ask the user for credentials through
// CommitTree: a socket server in the app, and the same binary relaunched as
// git's credential helper and as git's/ssh's askpass program.
package credprompt

import (
	"bufio"
	"io"
	"sort"
	"strings"
)

// ParseCredential reads git's credential protocol: key=value lines up to a
// blank line or EOF. A line without "=" is skipped; a repeated key keeps the
// last value.
func ParseCredential(r io.Reader) (map[string]string, error) {
	f := map[string]string{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 64*1024)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			break
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		f[k] = v
	}
	return f, sc.Err()
}

// FormatCredential writes f in git's credential protocol, keys sorted.
func FormatCredential(f map[string]string) string {
	keys := make([]string, 0, len(f))
	for k := range f {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k + "=" + f[k] + "\n")
	}
	return b.String()
}

// CredentialURL is the URL a credential request is about:
// protocol://host[/path].
func CredentialURL(f map[string]string) string {
	u := f["protocol"] + "://" + f["host"]
	if p := f["path"]; p != "" {
		u += "/" + strings.TrimPrefix(p, "/")
	}
	return u
}
```

`internal/credprompt/sshprompt.go`:

```go
package credprompt

import (
	"regexp"
	"strings"
)

// Kind is what a request asks for; the dialog has one shape per kind.
type Kind string

const (
	KindHTTPS      Kind = "https"
	KindPassphrase Kind = "ssh-passphrase"
	KindPassword   Kind = "ssh-password"
	KindHostKey    Kind = "ssh-hostkey"
	KindOther      Kind = "ssh-other"
	// KindGitPrompt is git's own "Username for …" / "Password for …"
	// askpass prompt. CommitTree's credential helper already asked (or the
	// user cancelled), so it is always refused: never a second dialog.
	KindGitPrompt Kind = "git-prompt"
)

// Prompt is an askpass prompt text, recognised.
type Prompt struct {
	Kind                           Kind
	Host, User                     string
	KeyPath, KeyType, Fingerprint  string
	// Text is the prompt trimmed, shown verbatim for KindOther.
	Text string
}

var (
	rePassphrase = regexp.MustCompile(`^Enter passphrase for key '(.+)':$`)
	rePassword   = regexp.MustCompile(`^(\S+)@(\S+)'s password:$`)
	reHostKey    = regexp.MustCompile(`The authenticity of host '([^' ]+)`)
	reKeyLine    = regexp.MustCompile(`(?m)^(\S+) key fingerprint is (\S+?)\.?$`)
	reGitPrompt  = regexp.MustCompile(`^(Username|Password) for '.*':$`)
)

// ClassifyPrompt recognises the prompts OpenSSH and git pass to askpass.
// Output is in English (gitcmd sets LC_ALL=C).
func ClassifyPrompt(text string) Prompt {
	t := strings.TrimSpace(text)
	p := Prompt{Kind: KindOther, Text: t}
	switch {
	case reGitPrompt.MatchString(t):
		p.Kind = KindGitPrompt
	case rePassphrase.MatchString(t):
		p.Kind = KindPassphrase
		p.KeyPath = rePassphrase.FindStringSubmatch(t)[1]
	case rePassword.MatchString(t):
		m := rePassword.FindStringSubmatch(t)
		p.Kind, p.User, p.Host = KindPassword, m[1], m[2]
	case strings.Contains(t, "Are you sure you want to continue connecting") && reHostKey.MatchString(t):
		p.Kind = KindHostKey
		p.Host = reHostKey.FindStringSubmatch(t)[1]
		if m := reKeyLine.FindStringSubmatch(t); m != nil {
			p.KeyType, p.Fingerprint = m[1], m[2]
		}
	}
	return p
}
```

`internal/credprompt/env.go`:

```go
package credprompt

import (
	"path/filepath"
	"strconv"
	"strings"
)

// The helper finds the app through these; their presence is also what makes
// main() run as the helper.
const (
	EnvSock  = "COMMITREE_PROMPT_SOCK"
	EnvNonce = "COMMITREE_PROMPT_NONCE"
)

// shellQuote quotes s for sh: git runs a "!" helper through the shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// sessionEnv is the environment of one prompting git command. CommitTree is
// appended as the last credential helper through GIT_CONFIG_COUNT, after any
// count the user's environment already has (inheritedCount; junk counts as
// 0), so the user's helpers are asked first and args stay as they are.
func sessionEnv(exe, sock, nonce, inheritedCount string) []string {
	n, err := strconv.Atoi(strings.TrimSpace(inheritedCount))
	if err != nil || n < 0 {
		n = 0
	}
	i := strconv.Itoa(n)
	return []string{
		"GIT_CONFIG_COUNT=" + strconv.Itoa(n+1),
		"GIT_CONFIG_KEY_" + i + "=credential.helper",
		"GIT_CONFIG_VALUE_" + i + "=!" + shellQuote(exe) + " credential-helper",
		"GIT_ASKPASS=" + exe,
		"SSH_ASKPASS=" + exe,
		"SSH_ASKPASS_REQUIRE=force",
		EnvSock + "=" + sock,
		EnvNonce + "=" + nonce,
	}
}

// helperLabel names a credential.helper value for "Remember in <name>".
func helperLabel(value string) string {
	v := strings.TrimSpace(value)
	if v == "" || strings.HasPrefix(v, "!") {
		return "your credential helper"
	}
	name := filepath.Base(strings.Fields(v)[0])
	return strings.TrimPrefix(name, "git-credential-")
}
```

- [ ] **Step 4: Run tests**

Run: `cd <worktree> && go test ./internal/credprompt/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd <worktree> && /usr/bin/git add internal/credprompt && /usr/bin/git commit -m "feat(credprompt): credential protocol, askpass prompt classification, session env"
```

---

### Task 3: Remember — helper discovery, approve and reject

**Files:**
- Create: `internal/credprompt/remember.go`, `internal/credprompt/remember_test.go`

**Interfaces:**
- Consumes: `gitcmd.Run`, `gitcmd.RunInput` (Task 1); `FormatCredential`, `helperLabel` (Task 2).
- Produces:
  - `type RememberTarget struct { Name string \`json:"name"\`; User bool \`json:"-"\` }`
  - `func findRemember(ctx context.Context, dir, url, platform string) *RememberTarget`
  - `func DetectPlatformHelper(ctx context.Context) string` — `"osxkeychain"` / `"libsecret"` / `""`
  - `func approve(ctx context.Context, dir, platform string, f map[string]string) error`
  - `func reject(ctx context.Context, dir string, f map[string]string) error`
  - `func urlMatches(pattern, url string) bool`

- [ ] **Step 1: Write the failing tests** — `internal/credprompt/remember_test.go`:

```go
package credprompt

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git-ui/internal/testrepo"
)

func hermetic(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("HOME", t.TempDir())
}

func TestFindRememberUserHelper(t *testing.T) {
	hermetic(t)
	r := testrepo.New(t)
	r.Git("config", "credential.helper", "store --file "+filepath.Join(t.TempDir(), "c"))
	got := findRemember(context.Background(), r.Dir, "https://h.test/x", "osxkeychain")
	if got == nil || got.Name != "store" || !got.User {
		t.Fatalf("got %+v", got)
	}
}

func TestFindRememberEmptyValueResets(t *testing.T) {
	hermetic(t)
	r := testrepo.New(t)
	r.Git("config", "--add", "credential.helper", "store")
	r.Git("config", "--add", "credential.helper", "")
	got := findRemember(context.Background(), r.Dir, "https://h.test/x", "osxkeychain")
	if got == nil || got.Name != "osxkeychain" || got.User {
		t.Fatalf("want platform fallback, got %+v", got)
	}
}

func TestFindRememberURLScoped(t *testing.T) {
	hermetic(t)
	r := testrepo.New(t)
	r.Git("config", "credential.https://other.test.helper", "cache")
	r.Git("config", "credential.https://h.test.helper", "store")
	got := findRemember(context.Background(), r.Dir, "https://h.test/org/repo.git", "")
	if got == nil || got.Name != "store" {
		t.Fatalf("got %+v", got)
	}
}

func TestFindRememberNone(t *testing.T) {
	hermetic(t)
	r := testrepo.New(t)
	if got := findRemember(context.Background(), r.Dir, "https://h.test", ""); got != nil {
		t.Fatalf("got %+v", got)
	}
}

func TestFindRememberOutsideARepo(t *testing.T) {
	hermetic(t)
	if got := findRemember(context.Background(), "", "https://h.test", "osxkeychain"); got == nil || got.User {
		t.Fatalf("got %+v", got)
	}
}

func TestURLMatches(t *testing.T) {
	cases := []struct {
		pattern, url string
		want         bool
	}{
		{"https://github.com", "https://github.com/org/r.git", true},
		{"https://github.com/org", "https://github.com/org/r.git", true},
		{"https://github.com/other", "https://github.com/org/r.git", false},
		{"http://github.com", "https://github.com/org", false},
		{"https://*.example.com", "https://git.example.com/x", true},
		{"https://*.example.com", "https://example.com/x", false},
		{"https://me@github.com", "https://github.com/x", false},
		{"not a url", "https://github.com", false},
	}
	for _, c := range cases {
		if got := urlMatches(c.pattern, c.url); got != c.want {
			t.Errorf("urlMatches(%q, %q) = %v", c.pattern, c.url, got)
		}
	}
}

func TestApproveAndReject(t *testing.T) {
	hermetic(t)
	r := testrepo.New(t)
	file := filepath.Join(t.TempDir(), "creds")
	f := map[string]string{"protocol": "https", "host": "h.test", "username": "me", "password": "tok"}
	if err := approve(context.Background(), r.Dir, "store --file "+file, f); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(file)
	if !strings.Contains(string(b), "https://me:tok@h.test") {
		t.Fatalf("approve wrote %q", b)
	}
	r.Git("config", "credential.helper", "store --file "+file)
	if err := reject(context.Background(), r.Dir, f); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(file)
	if strings.Contains(string(b), "tok") {
		t.Fatalf("reject left %q", b)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `cd <worktree> && go test ./internal/credprompt/ -run 'Remember|URLMatches|Approve' -v`
Expected: build failure — `findRemember`, `urlMatches`, `approve`, `reject` undefined.

- [ ] **Step 3: Implement** — `internal/credprompt/remember.go`:

```go
package credprompt

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"git-ui/internal/gitcmd"
)

// RememberTarget is where a credential typed in the dialog is kept when
// "Remember" is checked.
type RememberTarget struct {
	// Name labels the checkbox: "Remember in <Name>".
	Name string `json:"name"`
	// User is true for the user's own helper, which git stores into by
	// itself on success; false for the platform helper CommitTree approves
	// into.
	User bool `json:"-"`
}

// findRemember is the RememberTarget for url as git sees it in dir ("" for
// a clone: config outside any repository), or nil when nothing could keep
// it. platform is DetectPlatformHelper's answer.
func findRemember(ctx context.Context, dir, rawURL, platform string) *RememberTarget {
	if dir == "" {
		dir = os.TempDir()
	}
	helpers := userHelpers(ctx, dir, rawURL)
	if n := len(helpers); n > 0 {
		return &RememberTarget{Name: helperLabel(helpers[n-1]), User: true}
	}
	if platform != "" {
		return &RememberTarget{Name: helperLabel(platform)}
	}
	return nil
}

// userHelpers lists, in git's order, the credential helpers that apply to
// rawURL: credential.helper and every matching credential.<url>.helper; an
// empty value resets the list, as in git. The context is never marked, so
// CommitTree's own helper is not among them.
func userHelpers(ctx context.Context, dir, rawURL string) []string {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "config", "-z", "--get-regexp", `^credential\..*helper$`)
	if err != nil {
		return nil // exit 1: none set
	}
	var list []string
	for _, entry := range strings.Split(out, "\x00") {
		key, value, _ := strings.Cut(entry, "\n")
		if key == "" {
			continue
		}
		if key != "credential.helper" {
			sub := strings.TrimSuffix(strings.TrimPrefix(key, "credential."), ".helper")
			if !urlMatches(sub, rawURL) {
				continue
			}
		}
		if strings.TrimSpace(value) == "" {
			list = nil
			continue
		}
		list = append(list, value)
	}
	return list
}

// urlMatches approximates git's credential.<url> matching: same scheme,
// host equal or matching a "*." wildcard label, path a prefix by segments,
// and a user only when the pattern names the same one (a request without a
// user never matches a pattern with one).
func urlMatches(pattern, rawURL string) bool {
	p, err := url.Parse(pattern)
	if err != nil || p.Scheme == "" || p.Host == "" {
		return false
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if !strings.EqualFold(p.Scheme, u.Scheme) {
		return false
	}
	if p.User != nil && (u.User == nil || p.User.Username() != u.User.Username()) {
		return false
	}
	if !hostMatches(strings.ToLower(p.Host), strings.ToLower(u.Host)) {
		return false
	}
	pp := strings.Trim(p.Path, "/")
	up := strings.Trim(u.Path, "/")
	return pp == "" || up == pp || strings.HasPrefix(up, pp+"/")
}

func hostMatches(pattern, host string) bool {
	pl, hl := strings.Split(pattern, "."), strings.Split(host, ".")
	if len(pl) != len(hl) {
		return false
	}
	for i := range pl {
		if pl[i] != "*" && pl[i] != hl[i] {
			return false
		}
	}
	return true
}

// DetectPlatformHelper is the helper "Remember" uses when the user has none:
// osxkeychain on macOS, libsecret on Linux, when git ships it.
func DetectPlatformHelper(ctx context.Context) string {
	out, err := gitcmd.Run(ctx, os.TempDir(), gitcmd.ReadTimeout, "--exec-path")
	if err != nil {
		return ""
	}
	name := map[string]string{"darwin": "osxkeychain", "linux": "libsecret"}[runtime.GOOS]
	if name == "" {
		return ""
	}
	if _, err := os.Stat(filepath.Join(strings.TrimSpace(out), "git-credential-"+name)); err != nil {
		return ""
	}
	return name
}

// approve keeps f in the platform helper only: the user's helpers are
// switched off for this one command by the empty credential.helper.
func approve(ctx context.Context, dir, platform string, f map[string]string) error {
	if dir == "" {
		dir = os.TempDir()
	}
	_, err := gitcmd.RunInput(ctx, dir, gitcmd.ReadTimeout, nil, FormatCredential(f),
		"-c", "credential.helper=", "-c", "credential.helper="+platform, "credential", "approve")
	return err
}

// reject removes f from the user's helpers — what git stored there on its
// own when "Remember" was unchecked.
func reject(ctx context.Context, dir string, f map[string]string) error {
	if dir == "" {
		dir = os.TempDir()
	}
	_, err := gitcmd.RunInput(ctx, dir, gitcmd.ReadTimeout, nil, FormatCredential(f), "credential", "reject")
	return err
}
```

- [ ] **Step 4: Run tests**

Run: `cd <worktree> && go test ./internal/credprompt/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd <worktree> && /usr/bin/git add internal/credprompt && /usr/bin/git commit -m "feat(credprompt): Remember target discovery, approve into the platform helper, reject"
```

---

### Task 4: Server, sessions and the helper program

**Files:**
- Create: `internal/credprompt/server.go`, `internal/credprompt/helper.go`, `internal/credprompt/stub_windows.go`, `internal/credprompt/server_test.go`

**Interfaces:**
- Consumes: Tasks 1–3.
- Produces:
  - `type Info struct { RepoID, Dir, Context string; FromAI bool }`
  - `type Request struct` — JSON: `id, kind, repoId, context, fromAI, host, path, username, prompt, keyPath, keyType, fingerprint, remember ({name}|null), retry, waiting, deadline` (ms since epoch)
  - `type Answer struct { Username string \`json:"username"\`; Secret string \`json:"secret"\`; Remember bool \`json:"remember"\`; Trust bool \`json:"trust"\` }`
  - `type UI struct { Show func(Request); Closed func(id string); Notice func(text string) }`
  - `type Options struct { Exe string; Timeout time.Duration; Platform string }`
  - `func NewServer(o Options, ui UI) (*Server, error)`
  - `func (s *Server) Allow(ctx context.Context, info Info) context.Context` (nil `*Server` → ctx unchanged)
  - `func (s *Server) Answer(id string, a Answer) error`; `func (s *Server) Cancel(id string) error`; `func (s *Server) Shutdown()`
  - `func RunHelper(args []string, getenv func(string) string, stdin io.Reader, stdout io.Writer) int`
  - `var ErrUnknownRequest`, `var ErrLineBreak`

Design notes the implementer must keep:
- Socket directory `os.MkdirTemp("", "ct-")` (mode 0700 by default), socket `<dir>/s` chmod `0600`. macOS limits a socket path to 104 bytes — keep these short names.
- One JSON line in, one JSON line out per connection.
- Nonces: 32 bytes from `crypto/rand`, hex; looked up by comparing every live session's nonce with `subtle.ConstantTimeCompare`.
- Exactly one request is shown at a time (`queue[0]`); its timer starts when shown. Adding to the queue re-sends the head through `ui.Show` with the new `waiting` and the same `deadline`.
- UI callbacks are never called with the server lock held, and must reach the frontend **in order** (a late re-send of an answered head would show a dead dialog). They go through one buffered channel drained by a single goroutine (`s.out`).
- `ui.Closed(id)` is sent whenever a shown request leaves the queue for any reason (answer, cancel, timeout, session end, shutdown).
- Outcomes (set on the session, returned by `End(true)`): cancel → `No credentials entered.` (`The SSH host was not trusted.` for a host key); timeout → `No credentials entered (timed out).`; `erase` after CommitTree answered → `The username or token was not accepted.`; `erase` without → `The saved credential was not accepted and was removed. Try again to enter a new one.`. A request closed because its session ended sets no outcome.
- A second `get`, or an askpass, after the session already has an outcome is refused without showing anything.
- `End(false)` with a stored credential runs Remember: checked + platform target → `approve`; unchecked + user target → `reject`. Then zero the stored password bytes.

- [ ] **Step 1: Write the failing tests** — `internal/credprompt/server_test.go`:

```go
//go:build !windows

package credprompt

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"git-ui/internal/gitcmd"
)

// fakeUI records what the server sent. Read it only through the accessor
// methods: the server calls the UI from its own goroutine.
type fakeUI struct {
	mu       sync.Mutex
	shownL   []Request
	closedL  []string
	noticesL []string
	showCh   chan Request
	closedCh chan string
}

func newFakeUI() *fakeUI {
	return &fakeUI{showCh: make(chan Request, 20), closedCh: make(chan string, 20)}
}

func (f *fakeUI) ui() UI {
	return UI{
		Show: func(r Request) {
			f.mu.Lock()
			f.shownL = append(f.shownL, r)
			f.mu.Unlock()
			f.showCh <- r
		},
		Closed: func(id string) {
			f.mu.Lock()
			f.closedL = append(f.closedL, id)
			f.mu.Unlock()
			f.closedCh <- id
		},
		Notice: func(t string) { f.mu.Lock(); f.noticesL = append(f.noticesL, t); f.mu.Unlock() },
	}
}

func (f *fakeUI) shown() []Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Request(nil), f.shownL...)
}

func (f *fakeUI) notices() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.noticesL...)
}

func (f *fakeUI) next(t *testing.T) Request {
	t.Helper()
	select {
	case r := <-f.showCh:
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("no request shown")
	}
	return Request{}
}

// nextClosed waits for the next credential:closed id.
func (f *fakeUI) nextClosed(t *testing.T) string {
	t.Helper()
	select {
	case id := <-f.closedCh:
		return id
	case <-time.After(5 * time.Second):
		t.Fatal("nothing closed")
	}
	return ""
}

func newTestServer(t *testing.T, timeout time.Duration) (*Server, *fakeUI) {
	t.Helper()
	ui := newFakeUI()
	s, err := NewServer(Options{Exe: "/unused", Timeout: timeout}, ui.ui())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Shutdown)
	return s, ui
}

// helperRun runs RunHelper in-process with the session's environment.
func helperRun(sess gitcmd.PromptSession, args []string, stdin string) (int, string) {
	env := map[string]string{}
	for _, kv := range sess.Env() {
		k, v, _ := strings.Cut(kv, "=")
		env[k] = v
	}
	var out bytes.Buffer
	code := RunHelper(args, func(k string) string { return env[k] }, strings.NewReader(stdin), &out)
	return code, out.String()
}

type result struct {
	code int
	out  string
}

func goHelper(sess gitcmd.PromptSession, args []string, stdin string) chan result {
	ch := make(chan result, 1)
	go func() { c, o := helperRun(sess, args, stdin); ch <- result{c, o} }()
	return ch
}

const getHTTPS = "protocol=https\nhost=h.test\n\n"

func TestGetIsAnswered(t *testing.T) {
	s, ui := newTestServer(t, time.Minute)
	sess := s.begin(Info{RepoID: "r1", Context: "Push · repo"})
	done := goHelper(sess, []string{"credential-helper", "get"}, getHTTPS)
	req := ui.next(t)
	if req.Kind != KindHTTPS || req.Host != "h.test" || req.Context != "Push · repo" || req.RepoID != "r1" {
		t.Fatalf("request %+v", req)
	}
	if err := s.Answer(req.ID, Answer{Username: "me", Secret: "tok"}); err != nil {
		t.Fatal(err)
	}
	r := <-done
	if r.code != 0 || r.out != "password=tok\nusername=me\n" {
		t.Fatalf("helper %d %q", r.code, r.out)
	}
	if err := sess.End(false); err != nil {
		t.Fatal(err)
	}
}

func TestUnknownNonceIsRefused(t *testing.T) {
	s, ui := newTestServer(t, time.Minute)
	sess := s.begin(Info{})
	_ = sess.End(false) // nonce no longer valid
	code, out := helperRun(sess, []string{"credential-helper", "get"}, getHTTPS)
	if code != 0 || out != "" {
		t.Fatalf("get: %d %q", code, out)
	}
	code, _ = helperRun(sess, []string{"Enter passphrase for key '/k': "}, "")
	if code == 0 {
		t.Fatal("askpass with a dead nonce succeeded")
	}
	if len(ui.shown()) != 0 {
		t.Fatal("something was shown")
	}
}

func TestQueueShowsOneAtATime(t *testing.T) {
	s, ui := newTestServer(t, time.Minute)
	a, b := s.begin(Info{RepoID: "a"}), s.begin(Info{RepoID: "b"})
	doneA := goHelper(a, []string{"credential-helper", "get"}, getHTTPS)
	first := ui.next(t)
	doneB := goHelper(b, []string{"credential-helper", "get"}, getHTTPS)
	again := ui.next(t) // head re-sent with waiting=1
	if again.ID != first.ID || again.Waiting != 1 || again.Deadline != first.Deadline {
		t.Fatalf("re-show %+v (first %+v)", again, first)
	}
	_ = s.Answer(first.ID, Answer{Username: "u", Secret: "p"})
	<-doneA
	second := ui.next(t)
	if second.RepoID != "b" || second.Waiting != 0 {
		t.Fatalf("second %+v", second)
	}
	_ = s.Answer(second.ID, Answer{Username: "u", Secret: "p"})
	<-doneB
}

func TestTimeoutOutcome(t *testing.T) {
	s, ui := newTestServer(t, 50*time.Millisecond)
	sess := s.begin(Info{})
	done := goHelper(sess, []string{"credential-helper", "get"}, getHTTPS)
	req := ui.next(t)
	r := <-done
	if r.out != "" {
		t.Fatalf("answered %q", r.out)
	}
	err := sess.End(true)
	if err == nil || err.Error() != "No credentials entered (timed out)." {
		t.Fatalf("outcome %v", err)
	}
	if id := ui.nextClosed(t); id != req.ID {
		t.Fatalf("closed %q want %q", id, req.ID)
	}
}

func TestCancelOutcomes(t *testing.T) {
	s, ui := newTestServer(t, time.Minute)
	sess := s.begin(Info{})
	done := goHelper(sess, []string{"credential-helper", "get"}, getHTTPS)
	_ = s.Cancel(ui.next(t).ID)
	<-done
	if err := sess.End(true); err == nil || err.Error() != "No credentials entered." {
		t.Fatalf("https cancel %v", err)
	}

	ssh := s.begin(Info{})
	hk := "The authenticity of host 'h (1.2.3.4)' can't be established.\nED25519 key fingerprint is SHA256:x.\nAre you sure you want to continue connecting (yes/no/[fingerprint])? "
	done = goHelper(ssh, []string{hk}, "")
	req := ui.next(t)
	if req.Kind != KindHostKey || req.Fingerprint != "SHA256:x" || req.Host != "h" {
		t.Fatalf("hostkey %+v", req)
	}
	_ = s.Cancel(req.ID)
	if r := <-done; r.code == 0 {
		t.Fatal("cancelled host key exited 0")
	}
	if err := ssh.End(true); err == nil || err.Error() != "The SSH host was not trusted." {
		t.Fatalf("hostkey cancel %v", err)
	}
}

func TestHostKeyTrustAnswersYes(t *testing.T) {
	s, ui := newTestServer(t, time.Minute)
	sess := s.begin(Info{})
	done := goHelper(sess, []string{"The authenticity of host 'h (1.2.3.4)' can't be established.\nRSA key fingerprint is SHA256:y.\nAre you sure you want to continue connecting (yes/no/[fingerprint])? "}, "")
	_ = s.Answer(ui.next(t).ID, Answer{Trust: true})
	if r := <-done; r.code != 0 || r.out != "yes\n" {
		t.Fatalf("got %d %q", r.code, r.out)
	}
}

func TestEndClosesPending(t *testing.T) {
	s, ui := newTestServer(t, time.Minute)
	sess := s.begin(Info{})
	done := goHelper(sess, []string{"credential-helper", "get"}, getHTTPS)
	req := ui.next(t)
	if err := sess.End(true); err != nil {
		t.Fatalf("ending a session is not a credential outcome: %v", err)
	}
	if r := <-done; r.out != "" {
		t.Fatalf("answered %q", r.out)
	}
	if id := ui.nextClosed(t); id != req.ID {
		t.Fatalf("closed %q want %q", id, req.ID)
	}
}

func TestEraseOutcomes(t *testing.T) {
	s, ui := newTestServer(t, time.Minute)
	sess := s.begin(Info{})
	done := goHelper(sess, []string{"credential-helper", "get"}, getHTTPS)
	_ = s.Answer(ui.next(t).ID, Answer{Username: "me", Secret: "bad"})
	<-done
	helperRun(sess, []string{"credential-helper", "erase"}, getHTTPS+"username=me\npassword=bad\n")
	if err := sess.End(true); err == nil || err.Error() != "The username or token was not accepted." {
		t.Fatalf("typed then erased: %v", err)
	}

	stale := s.begin(Info{})
	helperRun(stale, []string{"credential-helper", "erase"}, getHTTPS)
	if err := stale.End(true); err == nil || !strings.HasPrefix(err.Error(), "The saved credential was not accepted") {
		t.Fatalf("stale: %v", err)
	}
}

func TestPassphraseRetry(t *testing.T) {
	s, ui := newTestServer(t, time.Minute)
	sess := s.begin(Info{})
	p := []string{"Enter passphrase for key '/k': "}
	done := goHelper(sess, p, "")
	r1 := ui.next(t)
	_ = s.Answer(r1.ID, Answer{Secret: "wrong"})
	<-done
	done = goHelper(sess, p, "")
	r2 := ui.next(t)
	if r1.Retry || !r2.Retry || r2.KeyPath != "/k" {
		t.Fatalf("retry flags %v %v", r1.Retry, r2)
	}
	_ = s.Answer(r2.ID, Answer{Secret: "right"})
	if r := <-done; r.out != "right\n" {
		t.Fatalf("got %q", r.out)
	}
}

func TestGitOwnPromptIsRefused(t *testing.T) {
	s, ui := newTestServer(t, time.Minute)
	sess := s.begin(Info{})
	code, _ := helperRun(sess, []string{"Username for 'https://h.test': "}, "")
	if code == 0 || len(ui.shown()) != 0 {
		t.Fatalf("code %d shown %d", code, len(ui.shown()))
	}
}

func TestAnswerRejectsLineBreaks(t *testing.T) {
	s, ui := newTestServer(t, time.Minute)
	sess := s.begin(Info{})
	done := goHelper(sess, []string{"credential-helper", "get"}, getHTTPS)
	req := ui.next(t)
	if err := s.Answer(req.ID, Answer{Username: "me\nhost=evil", Secret: "x"}); !errors.Is(err, ErrLineBreak) {
		t.Fatalf("got %v", err)
	}
	_ = s.Answer(req.ID, Answer{Username: "me", Secret: "x"})
	<-done
	if err := s.Answer("nope", Answer{}); !errors.Is(err, ErrUnknownRequest) {
		t.Fatalf("unknown id: %v", err)
	}
}

func TestNoticeIsNotADialog(t *testing.T) {
	s, ui := newTestServer(t, time.Minute)
	sess := s.begin(Info{})
	env := map[string]string{"SSH_ASKPASS_PROMPT": "none"}
	for _, kv := range sess.Env() {
		k, v, _ := strings.Cut(kv, "=")
		env[k] = v
	}
	code := RunHelper([]string{"Confirm user presence for key ED25519-SK"}, func(k string) string { return env[k] }, strings.NewReader(""), &bytes.Buffer{})
	if code != 0 || len(ui.shown()) != 0 || len(ui.notices()) != 1 {
		t.Fatalf("code %d shown %d notices %v", code, len(ui.shown()), ui.notices())
	}
}

func TestShutdownRefusesAndRemovesSocket(t *testing.T) {
	s, ui := newTestServer(t, time.Minute)
	sess := s.begin(Info{})
	done := goHelper(sess, []string{"credential-helper", "get"}, getHTTPS)
	ui.next(t)
	dir := s.dir
	s.Shutdown()
	if r := <-done; r.out != "" {
		t.Fatalf("answered %q", r.out)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("socket dir still there: %v", err)
	}
	if s.Allow(context.Background(), Info{}) == nil {
		t.Fatal("Allow returned nil ctx")
	}
}

func TestNilServerAllowIsIdentity(t *testing.T) {
	var s *Server
	ctx := context.Background()
	if s.Allow(ctx, Info{}) != ctx {
		t.Fatal("nil server changed ctx")
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `cd <worktree> && go test ./internal/credprompt/ -v`
Expected: build failure — `NewServer`, `RunHelper`, etc. undefined.

- [ ] **Step 3: Implement**

`internal/credprompt/helper.go`:

```go
//go:build !windows

package credprompt

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"time"
)

// wireReq / wireResp are the helper↔app messages: one JSON line each way.
type wireReq struct {
	Nonce  string            `json:"nonce"`
	Op     string            `json:"op"` // get, store, erase, askpass, notice
	Fields map[string]string `json:"fields,omitempty"`
	Prompt string            `json:"prompt,omitempty"`
}

type wireResp struct {
	OK     bool              `json:"ok"`
	Fields map[string]string `json:"fields,omitempty"`
	Answer string            `json:"answer,omitempty"`
}

// RunHelper is the whole program when CommitTree runs as git's credential
// helper (`credential-helper get|store|erase`) or as git's/ssh's askpass
// (one argument: the prompt). It returns the exit code. It never writes
// anything it got from the app to stderr.
func RunHelper(args []string, getenv func(string) string, stdin io.Reader, stdout io.Writer) int {
	sock, nonce := getenv(EnvSock), getenv(EnvNonce)
	if len(args) >= 2 && args[0] == "credential-helper" {
		fields, err := ParseCredential(stdin)
		if err != nil {
			return 0
		}
		switch args[1] {
		case "get":
			resp, err := call(sock, wireReq{Nonce: nonce, Op: "get", Fields: fields})
			if err == nil && resp.OK {
				io.WriteString(stdout, FormatCredential(resp.Fields))
			}
		case "store", "erase":
			_, _ = call(sock, wireReq{Nonce: nonce, Op: args[1], Fields: fields})
		}
		// git ignores a helper's exit code; "nothing printed" means no credential.
		return 0
	}
	prompt := ""
	if len(args) > 0 {
		prompt = args[0]
	}
	if getenv("SSH_ASKPASS_PROMPT") == "none" {
		_, _ = call(sock, wireReq{Nonce: nonce, Op: "notice", Prompt: prompt})
		return 0
	}
	resp, err := call(sock, wireReq{Nonce: nonce, Op: "askpass", Prompt: prompt})
	if err != nil || !resp.OK {
		return 1
	}
	fmt.Fprintln(stdout, resp.Answer)
	return 0
}

func call(sock string, req wireReq) (wireResp, error) {
	var resp wireResp
	conn, err := net.DialTimeout("unix", sock, 5*time.Second)
	if err != nil {
		return resp, err
	}
	defer conn.Close()
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return resp, err
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return resp, err
	}
	err = json.Unmarshal(line, &resp)
	return resp, err
}
```

`internal/credprompt/server.go`:

```go
//go:build !windows

package credprompt

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"git-ui/internal/gitcmd"
)

// DefaultTimeout is how long a dialog waits for the user.
const DefaultTimeout = time.Minute

var (
	ErrUnknownRequest = errors.New("credprompt: no such request")
	ErrLineBreak      = errors.New("Line breaks are not allowed.")

	errTimedOut = errors.New("timed out")
	errCancel   = errors.New("cancelled")
	errEnded    = errors.New("session ended")
)

// Info says which command a session belongs to.
type Info struct {
	RepoID string
	// Dir is where "Remember" discovery runs; "" for a clone.
	Dir string
	// Context is the dialog's context line, e.g. "Push · my-repo".
	Context string
	FromAI  bool
}

// Request is one dialog, sent to the frontend as credential:request. It
// never carries an answer.
type Request struct {
	ID          string          `json:"id"`
	Kind        Kind            `json:"kind"`
	RepoID      string          `json:"repoId"`
	Context     string          `json:"context"`
	FromAI      bool            `json:"fromAI"`
	Host        string          `json:"host"`
	Path        string          `json:"path,omitempty"`
	Username    string          `json:"username,omitempty"`
	Prompt      string          `json:"prompt,omitempty"`
	KeyPath     string          `json:"keyPath,omitempty"`
	KeyType     string          `json:"keyType,omitempty"`
	Fingerprint string          `json:"fingerprint,omitempty"`
	Remember    *RememberTarget `json:"remember"`
	Retry       bool            `json:"retry"`
	Waiting     int             `json:"waiting"`
	Deadline    int64           `json:"deadline"`
}

// Answer is what the dialog sends back.
type Answer struct {
	Username string `json:"username"`
	Secret   string `json:"secret"`
	Remember bool   `json:"remember"`
	Trust    bool   `json:"trust"`
}

// UI is how the server reaches the frontend.
type UI struct {
	Show   func(Request)
	Closed func(id string)
	Notice func(text string)
}

type Options struct {
	// Exe is the program git and ssh run: CommitTree's own binary.
	Exe string
	// Timeout per shown dialog; 0 means DefaultTimeout.
	Timeout time.Duration
	// Platform is DetectPlatformHelper's answer ("" for none).
	Platform string
}

type reply struct {
	ok  bool
	a   Answer
	why error
}

type pending struct {
	req   Request
	s     *session
	reply chan reply
	timer *time.Timer
}

// Server is the app side: sessions, the queue of requests, the socket.
type Server struct {
	opts Options
	ui   UI
	dir  string
	sock string
	ln   net.Listener

	mu       sync.Mutex
	sessions map[string]*session
	queue    []*pending
	nextID   int
	closed   bool

	// out carries UI calls, in order, to the one goroutine (pump) that
	// makes them — never with mu held.
	out chan func()
}

func NewServer(o Options, ui UI) (*Server, error) {
	if o.Timeout == 0 {
		o.Timeout = DefaultTimeout
	}
	dir, err := os.MkdirTemp("", "ct-")
	if err != nil {
		return nil, err
	}
	sock := filepath.Join(dir, "s")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	if err := os.Chmod(sock, 0o600); err != nil {
		ln.Close()
		os.RemoveAll(dir)
		return nil, err
	}
	s := &Server{opts: o, ui: ui, dir: dir, sock: sock, ln: ln, sessions: map[string]*session{}, out: make(chan func(), 256)}
	go s.pump()
	go s.accept()
	return s, nil
}

// pump makes the UI calls in the order they were queued. out is never
// closed: a late timer may still queue a call after Shutdown.
func (s *Server) pump() {
	for f := range s.out {
		f()
	}
}

// Allow marks ctx so the git commands run with it may ask the user.
func (s *Server) Allow(ctx context.Context, info Info) context.Context {
	if s == nil {
		return ctx
	}
	return gitcmd.WithPrompts(ctx, func() gitcmd.PromptSession {
		if ss := s.begin(info); ss != nil {
			return ss
		}
		return nil
	})
}

func (s *Server) begin(info Info) *session {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil
	}
	ss := &session{srv: s, nonce: hex.EncodeToString(b), info: info, asked: map[string]int{}}
	s.sessions[ss.nonce] = ss
	return ss
}

func (s *Server) lookup(nonce string) *session {
	for n, ss := range s.sessions {
		if subtle.ConstantTimeCompare([]byte(n), []byte(nonce)) == 1 {
			return ss
		}
	}
	return nil
}

func (s *Server) accept() {
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(c)
	}
}

func (s *Server) handle(c net.Conn) {
	defer c.Close()
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	line, err := bufio.NewReader(&limitReader{c, 64 * 1024}).ReadBytes('\n')
	if err != nil {
		return
	}
	_ = c.SetReadDeadline(time.Time{})
	var req wireReq
	if json.Unmarshal(line, &req) != nil {
		return
	}
	resp := s.serve(req)
	_ = json.NewEncoder(c).Encode(resp)
}

// serve answers one helper message.
func (s *Server) serve(req wireReq) wireResp {
	s.mu.Lock()
	ss := s.lookup(req.Nonce)
	s.mu.Unlock()
	if ss == nil {
		return wireResp{}
	}
	switch req.Op {
	case "get":
		return ss.get(req.Fields)
	case "store":
		ss.store(req.Fields)
		return wireResp{OK: true}
	case "erase":
		ss.mu.Lock()
		ss.erased = true
		ss.mu.Unlock()
		return wireResp{OK: true}
	case "notice":
		if s.ui.Notice != nil {
			s.ui.Notice(strings.TrimSpace(req.Prompt))
		}
		return wireResp{OK: true}
	case "askpass":
		return ss.askpass(req.Prompt)
	}
	return wireResp{}
}

// ask queues r for ss and waits for its reply.
func (s *Server) ask(ss *session, r Request) reply {
	p := &pending{req: r, s: ss, reply: make(chan reply, 1)}
	s.mu.Lock()
	if s.closed || ss.isEnded() {
		s.mu.Unlock()
		return reply{why: errEnded}
	}
	s.nextID++
	p.req.ID = strconv.Itoa(s.nextID)
	s.queue = append(s.queue, p)
	if len(s.queue) == 1 {
		s.showHeadLocked()
	} else {
		head := s.queue[0]
		head.req.Waiting = len(s.queue) - 1
		s.showLocked(head.req)
	}
	s.mu.Unlock()
	return <-p.reply
}

func (s *Server) showHeadLocked() {
	if len(s.queue) == 0 {
		return
	}
	p := s.queue[0]
	p.req.Waiting = len(s.queue) - 1
	p.req.Deadline = time.Now().Add(s.opts.Timeout).UnixMilli()
	id := p.req.ID
	p.timer = time.AfterFunc(s.opts.Timeout, func() { s.finish(id, reply{why: errTimedOut}) })
	s.showLocked(p.req)
}

func (s *Server) showLocked(r Request) {
	if s.ui.Show != nil {
		s.out <- func() { s.ui.Show(r) }
	}
}

// finish removes request id from the queue with r; it reports whether id
// was there.
func (s *Server) finish(id string, r reply) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.finishLocked(id, r)
}

func (s *Server) finishLocked(id string, r reply) bool {
	for i, p := range s.queue {
		if p.req.ID != id {
			continue
		}
		if p.timer != nil {
			p.timer.Stop()
		}
		s.queue = append(s.queue[:i], s.queue[i+1:]...)
		p.reply <- r
		if i == 0 {
			if s.ui.Closed != nil {
				s.out <- func() { s.ui.Closed(id) }
			}
			s.showHeadLocked()
		}
		return true
	}
	return false
}

// Answer is the dialog's Continue.
func (s *Server) Answer(id string, a Answer) error {
	if strings.ContainsAny(a.Username, "\r\n\x00") || strings.ContainsAny(a.Secret, "\r\n\x00") {
		return ErrLineBreak
	}
	if !s.finish(id, reply{ok: true, a: a}) {
		return ErrUnknownRequest
	}
	return nil
}

// Cancel is the dialog's Cancel (or Esc).
func (s *Server) Cancel(id string) error {
	if !s.finish(id, reply{why: errCancel}) {
		return ErrUnknownRequest
	}
	return nil
}

// Shutdown answers "no credential" to everything pending, stops the socket
// and removes its directory. Later sessions are not created.
func (s *Server) Shutdown() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	for len(s.queue) > 0 {
		s.finishLocked(s.queue[0].req.ID, reply{why: errEnded})
	}
	s.mu.Unlock()
	s.ln.Close()
	os.RemoveAll(s.dir)
}

// session is one prompting git command.
type session struct {
	srv   *Server
	nonce string
	info  Info

	mu       sync.Mutex
	ended    bool
	answered bool // CommitTree supplied the HTTPS credential
	erased   bool
	outcome  error
	target   *RememberTarget
	remember bool
	stored   map[string]string
	asked    map[string]int
}

func (ss *session) isEnded() bool {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return ss.ended
}

func (ss *session) Env() []string {
	return sessionEnv(ss.srv.opts.Exe, ss.srv.sock, ss.nonce, os.Getenv("GIT_CONFIG_COUNT"))
}

func (ss *session) hasOutcome() bool {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return ss.outcome != nil
}

func (ss *session) setOutcome(kind Kind, why error) {
	var msg string
	switch {
	case errors.Is(why, errTimedOut):
		msg = "No credentials entered (timed out)."
	case errors.Is(why, errCancel) && kind == KindHostKey:
		msg = "The SSH host was not trusted."
	case errors.Is(why, errCancel):
		msg = "No credentials entered."
	default:
		return // session ended: the command's own error stands
	}
	ss.mu.Lock()
	ss.outcome = &gitcmd.PromptError{Msg: msg}
	ss.mu.Unlock()
}

func (ss *session) base(kind Kind) Request {
	return Request{Kind: kind, RepoID: ss.info.RepoID, Context: ss.info.Context, FromAI: ss.info.FromAI}
}

func (ss *session) get(f map[string]string) wireResp {
	if ss.hasOutcome() {
		return wireResp{}
	}
	r := ss.base(KindHTTPS)
	r.Host, r.Path, r.Username = f["host"], f["path"], f["username"]
	r.Remember = findRemember(context.Background(), ss.info.Dir, CredentialURL(f), ss.srv.opts.Platform)
	rep := ss.srv.ask(ss, r)
	if !rep.ok {
		ss.setOutcome(KindHTTPS, rep.why)
		return wireResp{}
	}
	ss.mu.Lock()
	ss.answered, ss.target, ss.remember = true, r.Remember, rep.a.Remember
	ss.mu.Unlock()
	return wireResp{OK: true, Fields: map[string]string{"username": rep.a.Username, "password": rep.a.Secret}}
}

func (ss *session) store(f map[string]string) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if ss.answered {
		ss.stored = f
	}
}

func (ss *session) askpass(text string) wireResp {
	p := ClassifyPrompt(text)
	if p.Kind == KindGitPrompt || ss.hasOutcome() {
		return wireResp{}
	}
	r := ss.base(p.Kind)
	r.Host, r.Username, r.KeyPath, r.KeyType, r.Fingerprint = p.Host, p.User, p.KeyPath, p.KeyType, p.Fingerprint
	if p.Kind == KindOther {
		r.Prompt = p.Text
	}
	key := string(p.Kind) + "\x00" + p.KeyPath + "\x00" + p.User + "@" + p.Host
	ss.mu.Lock()
	ss.asked[key]++
	r.Retry = ss.asked[key] > 1 && (p.Kind == KindPassphrase || p.Kind == KindPassword)
	ss.mu.Unlock()
	rep := ss.srv.ask(ss, r)
	if !rep.ok || (p.Kind == KindHostKey && !rep.a.Trust) {
		why := rep.why
		if rep.ok { // answered without Trust: same as Cancel
			why = errCancel
		}
		ss.setOutcome(p.Kind, why)
		return wireResp{}
	}
	if p.Kind == KindHostKey {
		return wireResp{OK: true, Answer: "yes"}
	}
	return wireResp{OK: true, Answer: rep.a.Secret}
}

// End closes the session (gitcmd calls it once git exited): pending
// requests close, Remember runs after a success, and a failure's credential
// outcome comes back.
func (ss *session) End(failed bool) error {
	s := ss.srv
	s.mu.Lock()
	ss.mu.Lock()
	ss.ended = true
	ss.mu.Unlock()
	delete(s.sessions, ss.nonce)
	for i := 0; i < len(s.queue); {
		if s.queue[i].s == ss {
			s.finishLocked(s.queue[i].req.ID, reply{why: errEnded})
			continue
		}
		i++
	}
	s.mu.Unlock()

	ss.mu.Lock()
	defer ss.mu.Unlock()
	defer func() {
		for k := range ss.stored {
			ss.stored[k] = ""
		}
		ss.stored = nil
	}()
	if !failed {
		if ss.stored != nil && ss.target != nil {
			ctx := context.Background()
			switch {
			case ss.remember && !ss.target.User:
				_ = approve(ctx, ss.info.Dir, s.opts.Platform, ss.stored)
			case !ss.remember && ss.target.User:
				_ = reject(ctx, ss.info.Dir, ss.stored)
			}
		}
		return nil
	}
	if ss.outcome != nil {
		return ss.outcome
	}
	if ss.erased {
		if ss.answered {
			return &gitcmd.PromptError{Msg: "The username or token was not accepted."}
		}
		return &gitcmd.PromptError{Msg: "The saved credential was not accepted and was removed. Try again to enter a new one."}
	}
	return nil
}

// limitReader stops a request line at n bytes.
type limitReader struct {
	c net.Conn
	n int
}

func (l *limitReader) Read(p []byte) (int, error) {
	if l.n <= 0 {
		return 0, errors.New("request too long")
	}
	if len(p) > l.n {
		p = p[:l.n]
	}
	n, err := l.c.Read(p)
	l.n -= n
	return n, err
}
```

Note on secrets in memory: Go strings cannot be wiped; zeroing the map values drops the references early. The spec documents this as best effort — do not add more machinery.

Note on the `End` signature: `End` must return a plain `nil` interface when there is no outcome (never a typed nil `*gitcmd.PromptError`), as written above.

`internal/credprompt/stub_windows.go`:

```go
//go:build windows

package credprompt

import (
	"context"
	"errors"
	"io"
	"time"
)

// On Windows CommitTree does not prompt: commands behave as before.

type Info struct {
	RepoID, Dir, Context string
	FromAI               bool
}
type Request struct{}
type Answer struct {
	Username string `json:"username"`
	Secret   string `json:"secret"`
	Remember bool   `json:"remember"`
	Trust    bool   `json:"trust"`
}
type UI struct {
	Show   func(Request)
	Closed func(id string)
	Notice func(text string)
}
type Options struct {
	Exe      string
	Timeout  time.Duration
	Platform string
}
type Server struct{}

var ErrUnknownRequest = errors.New("credprompt: no such request")

func NewServer(Options, UI) (*Server, error)                       { return nil, errors.New("credential prompts: not on Windows") }
func (s *Server) Allow(ctx context.Context, _ Info) context.Context { return ctx }
func (s *Server) Answer(string, Answer) error                       { return ErrUnknownRequest }
func (s *Server) Cancel(string) error                               { return ErrUnknownRequest }
func (s *Server) Shutdown()                                         {}
func RunHelper([]string, func(string) string, io.Reader, io.Writer) int { return 1 }
```

`remember.go` uses only portable code and stays without a build tag (`DetectPlatformHelper` is called from `main.go` on every platform).

- [ ] **Step 4: Run tests** (race detector on: the server is concurrent)

Run: `cd <worktree> && go test -race ./internal/credprompt/ -v`
Expected: PASS, no race reports. Also `GOOS=windows go vet ./internal/credprompt/` must succeed.

- [ ] **Step 5: Commit**

```bash
cd <worktree> && /usr/bin/git add internal/credprompt && /usr/bin/git commit -m "feat(credprompt): socket server with sessions, queue, timeout and outcomes; helper program"
```

---

### Task 5: End-to-end with real git (HTTPS, Remember, ssh) 

**Files:**
- Create: `internal/testrepo/authserver.go`, `internal/credprompt/main_test.go`, `internal/credprompt/integration_test.go`

**Interfaces:**
- Consumes: Tasks 1–4.
- Produces: `func testrepo.AuthServer(t testing.TB, bare, user, token string) (url string, authed *atomic.Int32)` — smart-HTTP server over `git http-backend` requiring basic auth; `authed` counts requests that carried the right credentials.

- [ ] **Step 1: Write the test server** — `internal/testrepo/authserver.go`:

```go
package testrepo

import (
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// AuthServer serves the bare repository bare over git's smart HTTP
// protocol, requiring basic auth user:token. The URL ends in the bare
// repository's base name. authed counts requests with the right credentials.
func AuthServer(t testing.TB, bare, user, token string) (string, *atomic.Int32) {
	t.Helper()
	execPath, err := exec.Command("git", "--exec-path").Output()
	if err != nil {
		t.Fatal(err)
	}
	run(t, bare, nil, "config", "http.receivepack", "true")
	backend := &cgi.Handler{
		Path: filepath.Join(strings.TrimSpace(string(execPath)), "git-http-backend"),
		Env:  []string{"GIT_PROJECT_ROOT=" + filepath.Dir(bare), "GIT_HTTP_EXPORT_ALL=1"},
	}
	authed := &atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != user || p != token {
			w.Header().Set("WWW-Authenticate", `Basic realm="test"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		authed.Add(1)
		backend.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/" + filepath.Base(bare), authed
}
```

(`run` is testrepo's existing helper; `run(t, bare, …)` applies `-C bare`.)

- [ ] **Step 2: Helper dispatch for the test binary** — `internal/credprompt/main_test.go`:

```go
package credprompt

import (
	"os"
	"testing"
)

// The test binary is also the helper git and ssh run in the integration
// tests, exactly as CommitTree's own binary is in the app.
func TestMain(m *testing.M) {
	if os.Getenv(EnvSock) != "" {
		os.Exit(RunHelper(os.Args[1:], os.Getenv, os.Stdin, os.Stdout))
	}
	os.Exit(m.Run())
}
```

- [ ] **Step 3: Write the integration tests** — `internal/credprompt/integration_test.go`:

```go
//go:build !windows

package credprompt

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git-ui/internal/gitcmd"
	"git-ui/internal/testrepo"
)

// newServerWith starts a server whose UI records requests and calls onShow
// for each (nil: nobody answers). The server reaches onShow through an
// atomic pointer: the UI runs on the server's own goroutine.
func newServerWith(t *testing.T, o Options, onShow func(s *Server, r Request)) (*Server, *fakeUI) {
	t.Helper()
	if o.Exe == "" {
		exe, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		o.Exe = exe
	}
	if o.Timeout == 0 {
		o.Timeout = 10 * time.Second
	}
	ui := newFakeUI()
	var sp atomic.Pointer[Server]
	u := ui.ui()
	show := u.Show
	u.Show = func(r Request) {
		show(r)
		if onShow != nil {
			onShow(sp.Load(), r)
		}
	}
	s, err := NewServer(o, u)
	if err != nil {
		t.Fatal(err)
	}
	sp.Store(s)
	t.Cleanup(s.Shutdown)
	return s, ui
}

// answering answers the request on screen (not re-sends) with a.
func answering(t *testing.T, o Options, a Answer) (*Server, *fakeUI) {
	return newServerWith(t, o, func(s *Server, r Request) {
		if r.Waiting == 0 {
			go s.Answer(r.ID, a)
		}
	})
}

// pushSetup is a repository with one commit and an "origin" behind basic auth.
func pushSetup(t *testing.T) *testrepo.Repo {
	t.Helper()
	hermetic(t)
	r := testrepo.New(t)
	r.Commit("base")
	bare := testrepo.NewBareFrom(t, r)
	url, _ := testrepo.AuthServer(t, bare, "me", "tok")
	r.Git("remote", "add", "origin", url)
	return r
}

func push(ctx context.Context, r *testrepo.Repo) error {
	_, err := gitcmd.Run(ctx, r.Dir, gitcmd.NetworkTimeout, "push", "origin", "HEAD:refs/heads/pushed")
	return err
}

func TestPushAsksAndSucceeds(t *testing.T) {
	r := pushSetup(t)
	s, ui := answering(t, Options{}, Answer{Username: "me", Secret: "tok"})
	if err := push(s.Allow(context.Background(), Info{Dir: r.Dir, Context: "Push · r"}), r); err != nil {
		t.Fatal(err)
	}
	shown := ui.shown()
	if len(shown) != 1 || shown[0].Kind != KindHTTPS || !strings.HasPrefix(shown[0].Host, "127.0.0.1:") {
		t.Fatalf("shown %+v", shown)
	}
}

func TestRememberUncheckedUndoesUserHelper(t *testing.T) {
	r := pushSetup(t)
	file := filepath.Join(t.TempDir(), "creds")
	r.Git("config", "credential.helper", "store --file "+file)
	s, ui := answering(t, Options{}, Answer{Username: "me", Secret: "tok", Remember: false})
	if err := push(s.Allow(context.Background(), Info{Dir: r.Dir}), r); err != nil {
		t.Fatal(err)
	}
	if rem := ui.shown()[0].Remember; rem == nil || rem.Name != "store" {
		t.Fatalf("remember %+v", rem)
	}
	if b, _ := os.ReadFile(file); strings.Contains(string(b), "tok") {
		t.Fatalf("still stored: %q", b)
	}
}

func TestRememberCheckedKeepsUserHelper(t *testing.T) {
	r := pushSetup(t)
	file := filepath.Join(t.TempDir(), "creds")
	r.Git("config", "credential.helper", "store --file "+file)
	s, _ := answering(t, Options{}, Answer{Username: "me", Secret: "tok", Remember: true})
	if err := push(s.Allow(context.Background(), Info{Dir: r.Dir}), r); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(file); !strings.Contains(string(b), "me:tok@") {
		t.Fatalf("not stored: %q", b)
	}
}

func TestRememberCheckedWithoutHelperApprovesIntoPlatform(t *testing.T) {
	r := pushSetup(t)
	file := filepath.Join(t.TempDir(), "platform")
	s, _ := answering(t, Options{Platform: "store --file " + file}, Answer{Username: "me", Secret: "tok", Remember: true})
	if err := push(s.Allow(context.Background(), Info{Dir: r.Dir}), r); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(file); !strings.Contains(string(b), "me:tok@") {
		t.Fatalf("platform helper got %q", b)
	}
}

func TestWrongTokenMessage(t *testing.T) {
	r := pushSetup(t)
	s, _ := answering(t, Options{}, Answer{Username: "me", Secret: "nope"})
	err := push(s.Allow(context.Background(), Info{Dir: r.Dir}), r)
	if err == nil || err.Error() != "The username or token was not accepted." {
		t.Fatalf("got %v", err)
	}
}

func TestStaleSavedCredential(t *testing.T) {
	r := pushSetup(t)
	file := filepath.Join(t.TempDir(), "creds")
	r.Git("config", "credential.helper", "store --file "+file)
	url := r.Git("remote", "get-url", "origin")
	stale := strings.Replace(url, "http://", "http://me:old@", 1)
	if err := os.WriteFile(file, []byte(strings.TrimSuffix(stale, "/"+filepath.Base(url))+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, ui := answering(t, Options{}, Answer{Username: "me", Secret: "tok", Remember: true})
	err := push(s.Allow(context.Background(), Info{Dir: r.Dir}), r)
	if err == nil || !strings.HasPrefix(err.Error(), "The saved credential was not accepted") {
		t.Fatalf("first attempt: %v", err)
	}
	if n := len(ui.shown()); n != 0 {
		t.Fatalf("the stale credential should have been used without asking, shown %d", n)
	}
	if err := push(s.Allow(context.Background(), Info{Dir: r.Dir}), r); err != nil {
		t.Fatalf("second attempt: %v", err)
	}
	if n := len(ui.shown()); n != 1 {
		t.Fatalf("second attempt should ask once, shown %d", n)
	}
}

func TestCancelShowsOneDialog(t *testing.T) {
	r := pushSetup(t)
	s, ui := newServerWith(t, Options{}, func(s *Server, req Request) { go s.Cancel(req.ID) })
	err := push(s.Allow(context.Background(), Info{Dir: r.Dir}), r)
	if err == nil || err.Error() != "No credentials entered." {
		t.Fatalf("got %v", err)
	}
	if n := len(ui.shown()); n != 1 {
		t.Fatalf("shown %d dialogs, want 1", n)
	}
}

func TestDialogTimeout(t *testing.T) {
	r := pushSetup(t)
	s, _ := newServerWith(t, Options{Timeout: 200 * time.Millisecond}, nil)
	err := push(s.Allow(context.Background(), Info{Dir: r.Dir}), r)
	if err == nil || err.Error() != "No credentials entered (timed out)." {
		t.Fatalf("got %v", err)
	}
}

func TestUnmarkedCommandNeverAsks(t *testing.T) {
	r := pushSetup(t)
	_, ui := answering(t, Options{}, Answer{Username: "me", Secret: "tok"})
	if err := push(context.Background(), r); err == nil {
		t.Fatal("an unmarked push to a locked remote succeeded")
	}
	if n := len(ui.shown()); n != 0 {
		t.Fatal("an unmarked command asked")
	}
}

func TestHelperPathWithSpaceAndQuote(t *testing.T) {
	r := pushSetup(t)
	exe, _ := os.Executable()
	dir := filepath.Join(t.TempDir(), "My Apps", "Commit'Tree")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "CommitTree")
	if err := os.Symlink(exe, link); err != nil {
		t.Fatal(err)
	}
	s, _ := answering(t, Options{Exe: link}, Answer{Username: "me", Secret: "tok"})
	if err := push(s.Allow(context.Background(), Info{Dir: r.Dir}), r); err != nil {
		t.Fatal(err)
	}
}

func TestFakeSSHAsksPassphraseThenHostKey(t *testing.T) {
	hermetic(t)
	r := testrepo.New(t)
	marker := filepath.Join(t.TempDir(), "ok")
	script := filepath.Join(t.TempDir(), "fake-ssh")
	body := `#!/bin/sh
pp=$("$SSH_ASKPASS" "Enter passphrase for key '/k/id_test': ") || exit 255
[ "$pp" = "secret-pp" ] || exit 255
hk=$("$SSH_ASKPASS" "The authenticity of host 'example.com (1.2.3.4)' can't be established.
ED25519 key fingerprint is SHA256:fake.
Are you sure you want to continue connecting (yes/no/[fingerprint])? ") || exit 255
[ "$hk" = "yes" ] || exit 255
echo ok > "` + marker + `"
exit 255
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	s, ui := newServerWith(t, Options{}, func(s *Server, req Request) {
		a := Answer{Secret: "secret-pp"}
		if req.Kind == KindHostKey {
			a = Answer{Trust: true}
		}
		go s.Answer(req.ID, a)
	})
	ctx := s.Allow(context.Background(), Info{Dir: r.Dir})
	_, _ = gitcmd.RunEnv(ctx, r.Dir, gitcmd.NetworkTimeout, []string{"GIT_SSH_COMMAND=" + script}, "ls-remote", "ssh://example.com/repo.git")
	if b, _ := os.ReadFile(marker); strings.TrimSpace(string(b)) != "ok" {
		t.Fatal("fake ssh did not get both answers")
	}
	shown := ui.shown()
	if len(shown) != 2 || shown[0].Kind != KindPassphrase || shown[1].Kind != KindHostKey || shown[1].Fingerprint != "SHA256:fake" {
		t.Fatalf("shown %+v", shown)
	}
}
```

Notes for the implementer:
- `TestStaleSavedCredential` — git's `store` file holds one URL per line, `http://me:old@127.0.0.1:PORT` (no path, `useHttpPath` off). Build it from the remote URL as shown; if the test fails because the line is wrong, print the file and fix the line, not the product code.
- Add `"sync/atomic"` to the imports.

- [ ] **Step 4: Run tests**

Run: `cd <worktree> && go test -race ./internal/credprompt/ ./internal/testrepo/ -v`
Expected: PASS. If a test fails, investigate with superpowers:systematic-debugging before touching product code.

- [ ] **Step 5: Commit**

```bash
cd <worktree> && /usr/bin/git add internal/credprompt internal/testrepo && /usr/bin/git commit -m "test(credprompt): end-to-end with git over HTTP basic auth and a fake ssh"
```

---

### Task 6: App wiring — bindings, marked call sites, helper dispatch

**Files:**
- Create: `internal/app/credentials.go`, `internal/app/credentials_test.go`, `internal/app/main_test.go`
- Modify: `internal/app/app.go` (`App` struct field `creds *credprompt.Server`; `DeleteRemoteBranch`), `internal/app/remote.go`, `internal/app/submodules.go`, `internal/app/remotes.go`, `internal/app/clone.go`, `internal/app/terminal.go` (`Shutdown`), `internal/ops/remotes.go`, `internal/clone/clone.go` (`Explain`), `main.go`

**Interfaces:**
- Consumes: `credprompt.Server`, `Info`, `UI`, `Answer`, `Options`, `DetectPlatformHelper`, `RunHelper`, `EnvSock` (Tasks 2–4); `gitcmd.PromptError` (Task 1).
- Produces:
  - `func WithCredentials(a *App, s *credprompt.Server)`
  - `func (a *App) CredentialUI() credprompt.UI`
  - `func (a *App) AnswerCredential(id string, ans credprompt.Answer) error` (Wails binding)
  - `func (a *App) CancelCredential(id string) error` (Wails binding)
  - `func (a *App) prompting(ctx context.Context, id, dir, op string) context.Context`
  - Events: `credential:request` (payload `credprompt.Request`), `credential:closed` (`CredentialClosed{ID string \`json:"id"\`}`), `credential:notice` (`CredentialNotice{Text string \`json:"text"\`}`)

- [ ] **Step 1: Write the failing tests**

`internal/app/main_test.go`:

```go
package app

import (
	"os"
	"testing"

	"git-ui/internal/credprompt"
)

// The test binary doubles as the credential helper (see credprompt).
func TestMain(m *testing.M) {
	if os.Getenv(credprompt.EnvSock) != "" {
		os.Exit(credprompt.RunHelper(os.Args[1:], os.Getenv, os.Stdin, os.Stdout))
	}
	os.Exit(m.Run())
}
```

`internal/app/credentials_test.go`:

```go
package app

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"git-ui/internal/cmdlog"
	"git-ui/internal/credprompt"
	"git-ui/internal/ops"
	"git-ui/internal/testrepo"
)

// credApp is newPlainApp plus a credential server whose UI goes through the
// app's own events, answered by answer (nil: fail the test on any request).
func credApp(t *testing.T, answer *credprompt.Answer) (*App, *testrepo.Repo, string, *events) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	a, r, id := newPlainApp(t)
	ev := newEvents()
	a.ai.deps.Emit = ev.emit
	exe, _ := os.Executable()
	srv, err := credprompt.NewServer(credprompt.Options{Exe: exe, Timeout: 10 * time.Second}, a.CredentialUI())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Shutdown)
	WithCredentials(a, srv)
	go func() {
		for e := range ev.ch {
			if e.name != "credential:request" {
				continue
			}
			req := e.data.(credprompt.Request)
			if answer == nil {
				t.Errorf("unexpected credential request %+v", req)
				_ = a.CancelCredential(req.ID)
				continue
			}
			_ = a.AnswerCredential(req.ID, *answer)
		}
	}()
	return a, r, id, ev
}

func lockedOrigin(t *testing.T, r *testrepo.Repo, token string) {
	t.Helper()
	bare := testrepo.NewBareFrom(t, r)
	url, _ := testrepo.AuthServer(t, bare, "me", token)
	r.Git("remote", "add", "origin", url)
}

func TestFetchAsksForCredentials(t *testing.T) {
	a, r, id, ev := credApp(t, &credprompt.Answer{Username: "me", Secret: "tok"})
	lockedOrigin(t, r, "tok")
	if err := a.Fetch(id); err != nil {
		t.Fatal(err)
	}
	req := ev.find(t, "credential:request").data.(credprompt.Request)
	if req.Context != "Fetch · "+filepath.Base(r.Dir) || req.RepoID != id || req.FromAI {
		t.Fatalf("request %+v", req)
	}
}

func TestAutoFetchNeverAsks(t *testing.T) {
	a, r, id, _ := credApp(t, nil)
	lockedOrigin(t, r, "tok")
	res, err := a.AutoFetch(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.AuthFailed) != 1 || res.AuthFailed[0] != "origin" {
		t.Fatalf("AuthFailed %v", res.AuthFailed)
	}
}

func TestRemoteTimeoutOutlastsDialog(t *testing.T) {
	if ops.RemoteTestTimeout <= credprompt.DefaultTimeout {
		t.Fatalf("Test's %v must outlast the %v dialog", ops.RemoteTestTimeout, credprompt.DefaultTimeout)
	}
}

func TestSecretNeverLeaves(t *testing.T) {
	sentinel := fmt.Sprintf("SENTINEL-%d", time.Now().UnixNano())
	a, r, id, ev := credApp(t, &credprompt.Answer{Username: "me", Secret: sentinel})
	lockedOrigin(t, r, sentinel)
	r.Commit("more")
	branch := r.Git("rev-parse", "--abbrev-ref", "HEAD")
	if res, err := a.PushBranch(id, branch); err != nil {
		t.Fatal(err)
	} else {
		t.Logf("push result %+v", res) // must say pushed; check its fields in ops.BranchPushResult
	}
	dir, _ := a.dir(id)
	for _, e := range a.cmds.List(cmdlog.RepoKey(dir)) {
		b, _ := json.Marshal(e)
		if strings.Contains(string(b), sentinel) {
			t.Fatalf("command log entry holds the secret: %s", b)
		}
		out, _ := a.cmds.Output(cmdlog.RepoKey(dir), e.ID)
		if b, _ := json.Marshal(out); strings.Contains(string(b), sentinel) {
			t.Fatalf("command output holds the secret: %s", b)
		}
	}
	ev.mu.Lock()
	defer ev.mu.Unlock()
	for _, e := range ev.list {
		b, _ := json.Marshal(e.data)
		if strings.Contains(string(b), sentinel) {
			t.Fatalf("event %s holds the secret: %s", e.name, b)
		}
	}
}
```

Adapt to the real helpers before running: `events` (in `ai_test.go`) has `mu`, `list`, `ch`, `wait`; add a `find(t, name) event` that scans `list` under the lock (or use `wait` if the event can still be on `ch` — it cannot here, since the goroutine above drains `ch`; so add `find`). The name check uses `filepath.Base(r.Dir)` (add `"path/filepath"` to the imports; check `repos.Store.Add` names a repo by its folder) `cmdlog.Entry` has an `ID` field (check `internal/cmdlog/log.go:52` area for the exact name).

- [ ] **Step 2: Run to verify they fail**

Run: `cd <worktree> && go test ./internal/app/ -run 'Credentials|AutoFetchNeverAsks|RemoteTimeout|SecretNeverLeaves' -v`
Expected: build failure — `WithCredentials`, `CredentialUI`, `AnswerCredential`, `CancelCredential` undefined.

- [ ] **Step 3: Implement** — `internal/app/credentials.go`:

```go
package app

import (
	"context"

	"git-ui/internal/cmdlog"
	"git-ui/internal/credprompt"
)

// CredentialClosed is credential:closed: the dialog with this id is gone
// (answered, cancelled, timed out, or its command ended).
type CredentialClosed struct {
	ID string `json:"id"`
}

// CredentialNotice is credential:notice: ssh telling the user something
// that needs no answer (e.g. "touch your security key").
type CredentialNotice struct {
	Text string `json:"text"`
}

// WithCredentials lets the network commands the user starts ask for
// credentials through s (docs/spec/05-remote-and-stash.md). Without it
// nothing prompts, as before.
func WithCredentials(a *App, s *credprompt.Server) { a.creds = s }

// CredentialUI is how the credential server reaches the frontend. Payloads
// never carry an answer.
func (a *App) CredentialUI() credprompt.UI {
	return credprompt.UI{
		Show:   func(r credprompt.Request) { a.emitUnlessShutdown("credential:request", r) },
		Closed: func(id string) { a.emitUnlessShutdown("credential:closed", CredentialClosed{ID: id}) },
		Notice: func(text string) { a.emitUnlessShutdown("credential:notice", CredentialNotice{Text: text}) },
	}
}

// AnswerCredential is the dialog's Continue (Trust and connect for a host
// key). The answer is handed to git and never logged.
func (a *App) AnswerCredential(id string, ans credprompt.Answer) error {
	if a.creds == nil {
		return credprompt.ErrUnknownRequest
	}
	return a.creds.Answer(id, ans)
}

// CancelCredential is the dialog's Cancel.
func (a *App) CancelCredential(id string) error {
	if a.creds == nil {
		return credprompt.ErrUnknownRequest
	}
	return a.creds.Cancel(id)
}

// prompting marks ctx so the network command run with it may ask; op is the
// context line's verb, e.g. "Push" or "Fetch origin".
func (a *App) prompting(ctx context.Context, id, dir, op string) context.Context {
	if a.creds == nil {
		return ctx
	}
	name := id
	if r, ok := a.repo(id); ok {
		name = r.Name
	}
	_, fromAI := a.aiWrites.Load(cmdlog.RepoKey(dir))
	return a.creds.Allow(ctx, credprompt.Info{RepoID: id, Dir: dir, Context: op + " · " + name, FromAI: fromAI})
}
```

Add to `App` struct (in `app.go`, after `notes notifyState`):

```go
	// creds lets marked network commands ask for credentials; nil when
	// prompts are off (tests, Windows).
	creds *credprompt.Server
```

Mark the call sites (exact edits):

- `remote.go`:
  - `Fetch`: `return ops.Fetch(a.prompting(ctx, id, dir, "Fetch"), dir)`
  - `FetchRemote`: `return ops.FetchRemote(a.prompting(ctx, id, dir, "Fetch "+remote), dir, remote)`
  - `FastForwardBranch`: `moved, ffErr = ops.FastForwardBranch(a.prompting(ctx, id, dir, "Pull "+branch), dir, branch)`
  - `PushBranch`: `result, pushErr = ops.PushBranch(a.prompting(ctx, id, dir, "Push "+branch), dir, branch)`
  - `Push`: `return ops.Push(a.prompting(ctx, id, dir, "Push"), dir)`
  - `PushAll`: `results, pushErr = ops.PushAll(a.prompting(ctx, id, dir, "Push all"), dir)`
  - `Pull`: `result, pullErr = ops.Pull(a.prompting(ctx, id, dir, "Pull"), dir, cfg.PullStrategy)`
- `app.go` `DeleteRemoteBranch`: `return refs.DeleteRemoteBranch(a.prompting(ctx, id, dir, "Delete "+remote+"/"+name), dir, remote, name)`
- `submodules.go`: in `InitSubmodule`, `UpdateSubmodule` wrap `ctx` with `a.prompting(ctx, id, dir, "Submodule update")`; in `InitAllSubmodules`, `UpdateAllSubmodules` likewise. `SyncSubmodule` stays unmarked (local only).
- `remotes.go` `TestRemote`: `return ops.TestRemote(a.prompting(a.ctx, id, dir, "Test "+name), dir, name)`
- `clone.go` `CloneRepo`, just before `go a.runClone(...)`:

```go
	if a.creds != nil {
		ctx = a.creds.Allow(ctx, credprompt.Info{Context: "Clone · " + shown[0]})
	}
```

- `ops/remotes.go`: `RemoteTestTimeout = 90 * time.Second` with the comment `// longer than a credential dialog (credprompt.DefaultTimeout), which Test may open`; `TestRemote` drops `NoPromptEnv` (call `gitcmd.Run(ctx, dir, RemoteTestTimeout, "ls-remote", …)`) and its doc comment becomes `// TestRemote asks the remote for its branches; a marked ctx may prompt. A failure to reach it is a result, not an error.` Also map a prompt outcome to its message: before the `IsAuthError` case add

```go
	case errors.As(err, new(*gitcmd.PromptError)):
		return RemoteTest{Message: err.Error()}, nil
```

- `clone/clone.go` `Explain`: first lines,

```go
	var pe *gitcmd.PromptError
	if errors.As(err, &pe) {
		return pe.Msg
	}
```

- `terminal.go` `Shutdown`: first line `if a.creds != nil { a.creds.Shutdown() }` and update its comment to `// Shutdown is Wails' OnShutdown hook: pending credential dialogs answer "no credential", and no shell or clone outlives the app.`

`main.go` — first statement of `main()`:

```go
	// Relaunched by git or ssh as the credential helper / askpass program:
	// answer through the running app and exit, before any GUI start.
	if os.Getenv(credprompt.EnvSock) != "" {
		os.Exit(credprompt.RunHelper(os.Args[1:], os.Getenv, os.Stdin, os.Stdout))
	}
```

and after `app.WithAI(...)`:

```go
	if exe, err := os.Executable(); err == nil {
		if exe, err = filepath.EvalSymlinks(exe); err == nil {
			srv, err := credprompt.NewServer(credprompt.Options{
				Exe:      exe,
				Platform: credprompt.DetectPlatformHelper(context.Background()),
			}, api.CredentialUI())
			if err != nil {
				log.Printf("credential prompts off: %v", err)
			} else {
				app.WithCredentials(api, srv)
			}
		}
	}
```

(add imports `context`, `os`, `path/filepath`, `git-ui/internal/credprompt`).

- [ ] **Step 4: Run all Go tests**

Run: `cd <worktree> && go build ./... && go test -race ./internal/...`
Expected: PASS everywhere (existing remote/clone/submodule tests must still pass: unmarked when `creds` is nil).

- [ ] **Step 5: Regenerate bindings**

Run: `cd <worktree> && ~/go/bin/wails generate module && /usr/bin/git checkout -- frontend/wailsjs/runtime 2>/dev/null; /usr/bin/git status --short frontend/wailsjs`
Expected: `frontend/wailsjs/go/app/App.{d.ts,js}` gain `AnswerCredential`/`CancelCredential`, `models.ts` gains `credprompt.Answer`.

- [ ] **Step 6: Commit**

```bash
cd <worktree> && /usr/bin/git add internal main.go frontend/wailsjs && /usr/bin/git commit -m "feat(app): network commands the user starts may ask for credentials; helper-mode dispatch in main"
```

---

### Task 7: Frontend logic — `lib/credential.ts`

**Files:**
- Create: `frontend/src/lib/credential.ts`, `frontend/src/lib/credential.test.ts`
- Modify: `frontend/src/lib/api.ts` (two wrappers)

**Interfaces:**
- Consumes: bindings `Go.AnswerCredential`, `Go.CancelCredential` (Task 6).
- Produces (exports of `lib/credential.ts`):
  - types `CredentialKind`, `CredentialRequest`, `CredentialAnswer`
  - `credentialRequest: Writable<CredentialRequest | null>`
  - `onRequest(r)`, `onClosed(id)`
  - `title(r)`, `secretLabel(kind)`, `tokenHint(host)`, `countdown(deadline, now)`, `canSubmit(r, a)`, `waitingText(task, r)`, `notifyBody(r)`
  - `COUNTDOWN_FROM_MS = 20_000`

- [ ] **Step 1: Write the failing tests** — `frontend/src/lib/credential.test.ts`:

```ts
import { describe, expect, it, beforeEach } from 'vitest'
import { get } from 'svelte/store'
import {
  canSubmit, countdown, credentialRequest, notifyBody, onClosed, onRequest, secretLabel, title, tokenHint, waitingText,
  type CredentialRequest,
} from './credential'

const base: CredentialRequest = {
  id: '1', kind: 'https', repoId: 'r1', context: 'Push · repo', fromAI: false, host: 'github.com',
  remember: { name: 'osxkeychain' }, retry: false, waiting: 0, deadline: 60_000,
}

describe('credential', () => {
  beforeEach(() => credentialRequest.set(null))

  it('titles each kind', () => {
    expect(title(base)).toBe('Sign in to github.com')
    expect(title({ ...base, kind: 'ssh-passphrase' })).toBe('SSH key passphrase')
    expect(title({ ...base, kind: 'ssh-password', username: 'git', host: 'h' })).toBe('SSH password for git@h')
    expect(title({ ...base, kind: 'ssh-hostkey' })).toBe('Unknown SSH host')
    expect(title({ ...base, kind: 'ssh-other', host: '' })).toBe('The SSH server asks')
  })

  it('labels the secret field', () => {
    expect(secretLabel('https')).toBe('Token')
    expect(secretLabel('ssh-passphrase')).toBe('Passphrase')
    expect(secretLabel('ssh-password')).toBe('Password')
    expect(secretLabel('ssh-other')).toBe('Answer')
  })

  it('hints a token on the big forges only', () => {
    expect(tokenHint('github.com')).toBe('Use a personal access token, not your account password.')
    expect(tokenHint('gitlab.com')).not.toBeNull()
    expect(tokenHint('bitbucket.org')).not.toBeNull()
    expect(tokenHint('git.example.com')).toBeNull()
  })

  it('counts down only in the last 20 s', () => {
    expect(countdown(60_000, 0)).toBeNull()
    expect(countdown(60_000, 39_999)).toBeNull()
    expect(countdown(60_000, 40_500)).toBe('Closes in 20 s')
    expect(countdown(60_000, 59_100)).toBe('Closes in 1 s')
    expect(countdown(60_000, 61_000)).toBe('Closes in 0 s')
  })

  it('needs a username and a token for https, a secret otherwise, nothing for a host key', () => {
    const a = { username: 'me', secret: 't', remember: true, trust: false }
    expect(canSubmit(base, a)).toBe(true)
    expect(canSubmit(base, { ...a, username: '' })).toBe(false)
    expect(canSubmit(base, { ...a, secret: '' })).toBe(false)
    expect(canSubmit({ ...base, kind: 'ssh-passphrase' }, { ...a, username: '' })).toBe(true)
    expect(canSubmit({ ...base, kind: 'ssh-hostkey' }, { ...a, secret: '' })).toBe(true)
  })

  it('replaces and closes the shown request', () => {
    onRequest(base)
    onRequest({ ...base, waiting: 1 })
    expect(get(credentialRequest)?.waiting).toBe(1)
    onClosed('other')
    expect(get(credentialRequest)).not.toBeNull()
    onClosed('1')
    expect(get(credentialRequest)).toBeNull()
  })

  it('marks the waiting task', () => {
    expect(waitingText({ kind: 'op', repoId: 'r1' }, base)).toBe('Waiting for credentials…')
    expect(waitingText({ kind: 'op', repoId: 'r2' }, base)).toBe('')
    expect(waitingText({ kind: 'clone', repoId: '' }, { ...base, repoId: '', context: 'Clone · github.com/o/r' })).toBe('Waiting for credentials…')
    expect(waitingText({ kind: 'model', repoId: '' }, { ...base, repoId: '' })).toBe('')
    expect(waitingText({ kind: 'op', repoId: 'r1' }, null)).toBe('')
  })

  it('writes a notification body with no username', () => {
    expect(notifyBody({ ...base, username: 'me' })).toBe('Credentials needed for github.com · Push · repo')
    expect(notifyBody({ ...base, kind: 'ssh-passphrase', host: '' })).toBe('Credentials needed for SSH · Push · repo')
  })
})
```

- [ ] **Step 2: Run to verify they fail**

Run: `cd <worktree>/frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npx vitest run src/lib/credential.test.ts`
Expected: FAIL — cannot resolve `./credential`.

- [ ] **Step 3: Implement** — `frontend/src/lib/credential.ts`:

```ts
import { writable } from 'svelte/store'

/** The credential dialog's pure logic (docs/spec/05-remote-and-stash.md). */

export type CredentialKind = 'https' | 'ssh-passphrase' | 'ssh-password' | 'ssh-hostkey' | 'ssh-other'

/** credential:request — never carries an answer. */
export interface CredentialRequest {
  id: string
  kind: CredentialKind
  /** '' for a clone. */
  repoId: string
  /** e.g. 'Push · my-repo'. */
  context: string
  fromAI: boolean
  host: string
  path?: string
  username?: string
  /** ssh's own text, for 'ssh-other'. */
  prompt?: string
  keyPath?: string
  keyType?: string
  fingerprint?: string
  remember: { name: string } | null
  /** A passphrase or password asked again: the last one was wrong. */
  retry: boolean
  /** Requests queued behind this one. */
  waiting: number
  /** ms since epoch. */
  deadline: number
}

export interface CredentialAnswer {
  username: string
  secret: string
  remember: boolean
  trust: boolean
}

export const COUNTDOWN_FROM_MS = 20_000

/** The request on screen; null when none. */
export const credentialRequest = writable<CredentialRequest | null>(null)

export function onRequest(r: CredentialRequest) {
  credentialRequest.set(r)
}

export function onClosed(id: string) {
  credentialRequest.update((cur) => (cur?.id === id ? null : cur))
}

export function title(r: CredentialRequest): string {
  switch (r.kind) {
    case 'https': return `Sign in to ${r.host}`
    case 'ssh-passphrase': return 'SSH key passphrase'
    case 'ssh-password': return `SSH password for ${r.username}@${r.host}`
    case 'ssh-hostkey': return 'Unknown SSH host'
    default: return 'The SSH server asks'
  }
}

export function secretLabel(kind: CredentialKind): string {
  return { https: 'Token', 'ssh-passphrase': 'Passphrase', 'ssh-password': 'Password', 'ssh-hostkey': '', 'ssh-other': 'Answer' }[kind]
}

const TOKEN_HOSTS = new Set(['github.com', 'gitlab.com', 'bitbucket.org'])

export function tokenHint(host: string): string | null {
  return TOKEN_HOSTS.has(host.toLowerCase()) ? 'Use a personal access token, not your account password.' : null
}

export function countdown(deadline: number, now: number): string | null {
  const left = deadline - now
  if (left > COUNTDOWN_FROM_MS) return null
  return `Closes in ${Math.max(0, Math.ceil(left / 1000))} s`
}

export function canSubmit(r: CredentialRequest, a: CredentialAnswer): boolean {
  if (r.kind === 'ssh-hostkey') return true
  if (r.kind === 'https') return a.username.trim() !== '' && a.secret !== ''
  return a.secret !== ''
}

/** The status bar's text for a task waiting on the dialog, else ''. */
export function waitingText(t: { kind: string; repoId: string }, r: CredentialRequest | null): string {
  if (!r) return ''
  const mine = t.kind === 'clone' ? r.repoId === '' && r.context.startsWith('Clone') : t.repoId !== '' && t.repoId === r.repoId
  return mine ? 'Waiting for credentials…' : ''
}

/** The notification body when the window is not focused: host and context only. */
export function notifyBody(r: CredentialRequest): string {
  return `Credentials needed for ${r.host || 'SSH'} · ${r.context}`
}
```

Note: the `countdown` test `countdown(60_000, 40_500)` → left 19 500 → ceil 20 → "Closes in 20 s"; `countdown(60_000, 39_999)` → left 20 001 > 20 000 → null. Keep the `>` comparison.

In `frontend/src/lib/api.ts`, add next to `cancelClone`:

```ts
  answerCredential: (id: string, a: CredentialAnswer) => call<void>(Go.AnswerCredential(id, a as any)),
  cancelCredential: (id: string) => call<void>(Go.CancelCredential(id)),
```

with `import type { CredentialAnswer } from './credential'`.

- [ ] **Step 4: Run tests and types**

Run: `cd <worktree>/frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npx vitest run && npx svelte-check`
Expected: PASS, 0 errors.

- [ ] **Step 5: Commit**

```bash
cd <worktree> && /usr/bin/git add frontend/src/lib && /usr/bin/git commit -m "feat(ui): credential dialog logic"
```

---

### Task 8: The dialog, status bar, clone view, notification

**Files:**
- Create: `frontend/src/components/CredentialDialog.svelte`
- Modify: `frontend/src/App.svelte` (mount + events), `frontend/src/components/StatusBar.svelte` (waiting text), `frontend/src/components/CloneDialog.svelte` (waiting line)

**Interfaces:**
- Consumes: everything exported by `lib/credential.ts` (Task 7); `api.answerCredential`, `api.cancelCredential`; `notify`, `windowFocused` from `lib/notify.ts`; `toast` from `lib/ui.ts`; `EventsOn` from `wailsjs/runtime/runtime`.

- [ ] **Step 1: Write the component** — `frontend/src/components/CredentialDialog.svelte`:

```svelte
<script lang="ts">
  import { onDestroy, tick } from 'svelte'
  import { api } from '../lib/api'
  import {
    canSubmit, countdown, credentialRequest, onClosed, secretLabel, title, tokenHint, type CredentialAnswer,
  } from '../lib/credential'
  import { errorMessage } from '../lib/ui'

  let username = ''
  let secret = ''
  let remember = true
  let reveal = false
  let error = ''
  let shownId = ''
  let now = Date.now()
  let first: HTMLInputElement | undefined
  let cancelBtn: HTMLButtonElement | undefined
  const timer = setInterval(() => (now = Date.now()), 250)
  onDestroy(() => clearInterval(timer))

  $: r = $credentialRequest
  // A new request (not a re-send of the same one with a new "waiting") resets the form.
  $: if (r && r.id !== shownId) {
    shownId = r.id
    username = r.username ?? ''
    secret = ''
    remember = true
    reveal = false
    error = ''
    void focusFirst()
  }
  $: if (!r) shownId = ''
  $: answer = { username, secret, remember: remember && !!r?.remember, trust: false } as CredentialAnswer
  $: ok = r ? canSubmit(r, answer) : false
  $: left = r ? countdown(r.deadline, now) : null

  async function focusFirst() {
    await tick()
    if (r?.kind === 'ssh-hostkey') cancelBtn?.focus()
    else first?.focus()
  }

  async function send(a: CredentialAnswer) {
    if (!r) return
    const id = r.id
    try {
      await api.answerCredential(id, a)
      secret = '' // never keep the secret in component state
      onClosed(id)
    } catch (e) {
      error = errorMessage(e)
    }
  }

  function submit() {
    if (ok) void send(answer)
  }

  async function cancel() {
    if (!r) return
    const id = r.id
    secret = ''
    onClosed(id)
    try { await api.cancelCredential(id) } catch { /* already gone */ }
  }

  function onKey(e: KeyboardEvent) {
    if (e.key === 'Escape') { e.preventDefault(); void cancel() }
  }
</script>

{#if r}
  <div class="backdrop" role="presentation" on:keydown={onKey}>
    <div class="dialog" role="dialog" aria-modal="true" aria-label={title(r)}>
      <h3>{title(r)}</h3>
      <p class="context ellipsis" title={r.context}>{r.context}{#if r.fromAI} · Requested by the AI chat{/if}</p>

      {#if r.kind === 'ssh-hostkey'}
        <p>CommitTree has never connected to <strong>{r.host}</strong>. Check the fingerprint with the server's owner before trusting it.</p>
        <p class="mono">{r.keyType} {r.fingerprint}</p>
        <div class="buttons">
          {#if left}<span class="left" aria-live="polite">{left}</span>{/if}
          <button type="button" class="btn" bind:this={cancelBtn} on:click={cancel}>Cancel</button>
          <button type="button" class="btn primary" on:click={() => send({ username: '', secret: '', remember: false, trust: true })}>Trust and connect</button>
        </div>
      {:else}
        <form on:submit|preventDefault={submit}>
          {#if r.kind === 'ssh-passphrase'}<p class="mono ellipsis" title={r.keyPath}>{r.keyPath}</p>{/if}
          {#if r.kind === 'ssh-other'}<p class="prompt">{r.prompt}</p>{/if}
          {#if r.retry}<p class="problem" role="alert">That was not accepted. Try again.</p>{/if}
          {#if r.kind === 'https'}
            <label>Username
              <input bind:this={first} spellcheck="false" autocomplete="off" bind:value={username} />
            </label>
          {/if}
          <label>{secretLabel(r.kind)}
            <span class="secret">
              {#if reveal}
                <input spellcheck="false" autocomplete="off" bind:value={secret} bind:this={first} />
              {:else}
                <input type="password" autocomplete="off" bind:value={secret} bind:this={first} />
              {/if}
              <button type="button" class="btn icon" aria-label={reveal ? 'Hide' : 'Show'} aria-pressed={reveal} on:click={() => (reveal = !reveal)}>👁</button>
            </span>
          </label>
          {#if r.kind === 'https' && tokenHint(r.host)}<p class="hint">{tokenHint(r.host)}</p>{/if}
          {#if r.kind === 'ssh-passphrase'}<p class="hint">To stop being asked, add the key to ssh-agent (on macOS <code>UseKeychain yes</code> in ~/.ssh/config).</p>{/if}
          {#if r.kind === 'https'}
            {#if r.remember}
              <label class="check"><input type="checkbox" bind:checked={remember} /> Remember in {r.remember.name}</label>
            {:else}
              <p class="hint">Not saved: no credential helper is set up.</p>
            {/if}
          {/if}
          {#if error}<p class="problem" role="alert">{error}</p>{/if}
          <div class="buttons">
            {#if r.waiting > 0}<span class="left">{r.waiting} more waiting</span>{/if}
            {#if left}<span class="left" aria-live="polite">{left}</span>{/if}
            <button type="button" class="btn" on:click={cancel}>Cancel</button>
            <button type="submit" class="btn primary" disabled={!ok}>Continue</button>
          </div>
        </form>
      {/if}
    </div>
  </div>
{/if}

<style>
  .backdrop { position: fixed; inset: 0; z-index: 50; display: grid; place-items: center; background: rgba(0, 0, 0, 0.25); }
  .dialog {
    width: min(420px, 92vw); padding: 18px 20px 16px; background: var(--surface);
    border: 1px solid var(--border); border-radius: 12px; box-shadow: var(--shadow);
    display: flex; flex-direction: column; gap: 12px;
  }
  form { display: flex; flex-direction: column; gap: 12px; }
  h3 { margin: 0; font-size: 14px; font-weight: 600; }
  p { margin: 0; line-height: 1.5; }
  .context { color: var(--muted); font-size: 12px; }
  label { display: flex; flex-direction: column; gap: 5px; color: var(--muted); font-size: 12px; }
  label input { font-size: 13px; }
  label.check { flex-direction: row; align-items: center; gap: 6px; color: var(--text); }
  .secret { display: flex; gap: 6px; }
  .secret input { flex: 1; min-width: 0; }
  .mono { font-family: var(--mono); font-size: 12px; overflow-wrap: anywhere; }
  .prompt { white-space: pre-line; }
  .hint { color: var(--muted); font-size: 12px; }
  .problem { color: var(--danger); font-size: 12px; }
  .buttons { display: flex; justify-content: flex-end; align-items: center; gap: 8px; margin-top: 4px; }
  .left { margin-right: auto; color: var(--muted); font-size: 12px; }
</style>
```

Notes: if `lib/ui.ts` has no `errorMessage` export, take it from where `notify.ts` imports it (`./ui`). If Svelte complains about `bind:this={first}` on two exclusive branches, keep it — only one exists at a time. Match the project's button classes (`btn`, `btn primary`; check whether an `icon` button class exists in the global CSS and reuse the closest one).

- [ ] **Step 2: Mount and wire events** — in `frontend/src/App.svelte`:
  - import `CredentialDialog` and mount it **after** `<DialogHost />` and before `<Toasts />` (it stacks above the clone dialog and confirms).
  - in the `onMount` block where the other `EventsOn` live, add:

```ts
    const offCredReq = EventsOn('credential:request', (r: CredentialRequest) => {
      onRequest(r)
      if (!get(windowFocused)) void notify({ category: 'problem', repoID: r.repoId, target: 'repo', body: notifyBody(r) })
    })
    const offCredClosed = EventsOn('credential:closed', (p: { id: string }) => onClosed(p.id))
    const offCredNotice = EventsOn('credential:notice', (p: { text: string }) => toast(p.text, 'info'))
```

  and call the three `off…()` in the same cleanup as the others. Imports: `onRequest, onClosed, notifyBody, type CredentialRequest` from `./lib/credential`; `notify, windowFocused` from `./lib/notify`; `get` from `svelte/store` (if not already); `toast` from `./lib/ui`.

  Note: a re-sent request with the same id (only `waiting` changed) must not raise a second notification. Keep a `let notifiedId = ''` beside the handler and only notify when `r.id !== notifiedId`.

- [ ] **Step 3: Status bar and clone view**
  - `StatusBar.svelte`: import `credentialRequest, waitingText` from `../lib/credential`; both places that render `{#if shown.progress}…{/if}` / `{#if t.progress}…{/if}` become:

```svelte
{#if waitingText(shown, $credentialRequest) || shown.progress}<span class="muted">· {waitingText(shown, $credentialRequest) || shown.progress}</span>{/if}
```

  (and the same with `t` in the list). If the live region at line ~106 reads `shown.progress`, give it the same expression.
  - `CloneDialog.svelte`: import the same two; the phase line becomes

```svelte
<p class="phase">{waitingText({ kind: 'clone', repoId: '' }, $credentialRequest) || (p ? p.phase : 'Starting…')}</p>
```

- [ ] **Step 4: Types, tests, build**

Run: `cd <worktree>/frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npx svelte-check && npx vitest run && npm run build`
Expected: 0 errors, all PASS, build OK.

- [ ] **Step 5: Commit**

```bash
cd <worktree> && /usr/bin/git add frontend/src && /usr/bin/git commit -m "feat(ui): credential dialog, waiting text in the status bar and clone view, notification when unfocused"
```

---

### Task 9: Docs, the dev-log check, whole-feature verification

**Files:**
- Modify: `docs/spec/05-remote-and-stash.md`, `docs/spec/01-repositories-and-sidebar.md`, `docs/spec/06-ai.md`, `docs/spec/07-conventions-and-constraints.md`, `docs/spec/11-notifications.md`, `docs/spec/12-repository-settings.md`, `docs/spec/13-status-bar.md`, `docs/superpowers/specs/2026-10-13-credential-prompts-design.md`

- [ ] **Step 1: Behaviour docs** — write, in each page's existing style:
  - `05`: a **Credentials** section — who answers first (user helpers, GCM, ssh-agent), the HTTPS dialog and Remember table, the stale-credential message, SSH kinds incl. host key and retry, 60 s timeout with countdown, queue, which commands may ask (list from the design spec), background fetch unchanged (never asks, pauses).
  - `01`: clone — credentials are asked for in the app; remove any "set up a credential helper first" wording; the progress view shows "Waiting for credentials…".
  - `06`: the chat's `fetch`/`push`/`pull` may open the dialog (context line says "Requested by the AI chat"); the AI never sees the dialog or the answer.
  - `07`: the "git never prompts" rule becomes "only commands the user starts on the network may prompt, through CommitTree's own helper; reads, local writes, signing and the background fetch never do"; secrets never in logs/events.
  - `11`: a `problem` notification "Credentials needed for <host> · <context>" when the window is not focused.
  - `12`: Test may ask for credentials; it waits up to 90 s.
  - `13`: "Waiting for credentials…" replaces a task's progress while its dialog is open.

- [ ] **Step 2: Design-spec deviations** — update `docs/superpowers/specs/2026-10-13-credential-prompts-design.md` where the plan settled details:
  - the stale-credential message (`The saved credential was not accepted and was removed. Try again to enter a new one.`);
  - a passphrase/password asked again shows "That was not accepted. Try again.";
  - `ssh-other` title is "The SSH server asks" (ssh does not pass the host);
  - the notification body is "Credentials needed for <host> · <context>" with the repository name as title;
  - Test's timeout is 90 s;
  - a cancelled host key says "The SSH host was not trusted.".

- [ ] **Step 3: Dev-log check (secrets in `wails dev` output)**

Run the app with `make dev` from the worktree (`cd <worktree> && make dev`, background), add a repository whose `origin` is an `AuthServer`-style locked remote (or any private HTTPS remote), click Fetch, type the token `SENTINEL-DEVLOG-12345`, then:

```bash
cd <worktree> && grep -c "SENTINEL-DEVLOG-12345" <the make dev output file>
```

Expected: `0`. If the binding call is logged with its arguments, set the dev log level so binding calls are not printed (`options.App{LogLevel: logger.INFO}` in `main.go`, or `wails dev -loglevel Info` in the Makefile's `dev` target) and re-check. Record the result in the commit message. Restore `frontend/wailsjs/runtime` afterwards (`/usr/bin/git checkout -- frontend/wailsjs/runtime`).

- [ ] **Step 4: Whole-feature check against the specs** — go through every bullet of the design spec's Behaviour and Security sections and of the doc pages touched in Step 1; for each, name the test or the code line that satisfies it. Fix gaps before committing.

- [ ] **Step 5: Full verification**

Run: `cd <worktree> && go vet ./... && go test -race ./... && cd frontend && source ~/.nvm/nvm.sh && nvm use 22 >/dev/null && npx svelte-check && npx vitest run`
Expected: all green.

- [ ] **Step 6: Commit**

```bash
cd <worktree> && /usr/bin/git add docs main.go Makefile && /usr/bin/git commit -m "docs: in-app credential prompts in the behaviour spec; dev log checked for secrets"
```

- [ ] **Step 7: Hand over** — build with `make build`, open `build/bin/CommitTree.app`, and give the owner the manual list from the design spec's Testing section (GitHub PAT push with Remember on/off + Keychain Access check, private clone, ssh key with passphrase, new ssh host, a dialog left for a minute, a chat `push` that asks). Do not merge.
