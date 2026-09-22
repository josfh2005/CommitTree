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

// TestCloseAfterExitReturnsPromptly ensures Close on an already-exited tab
// never signals a reaped (and possibly recycled) pid/pgid: it must return
// well under killAfter, before any SIGKILL fallback could fire.
func TestCloseAfterExitReturnsPromptly(t *testing.T) {
	m, r := newTestManager(t)
	tab, _ := m.Open("repo1", t.TempDir(), 80, 24)
	m.Write(tab, "exit 0\r")
	select {
	case <-r.exited:
	case <-time.After(5 * time.Second):
		t.Fatal("no exit")
	}
	start := time.Now()
	if err := m.Close(tab); err != nil {
		t.Fatalf("close after exit: %v", err)
	}
	if elapsed := time.Since(start); elapsed >= killAfter {
		t.Fatalf("close took %v, wanted well under killAfter (%v)", elapsed, killAfter)
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

func TestShellEnvAddsLangWhenLocaleAbsent(t *testing.T) {
	base := []string{"PATH=/usr/bin", "HOME=/home/x"}
	env := shellEnv(base)
	if !containsEnv(env, "LANG=en_US.UTF-8") {
		t.Fatalf("LANG not added: %v", env)
	}
	if !containsEnv(env, "TERM=xterm-256color") || !containsEnv(env, "COLORTERM=truecolor") {
		t.Fatalf("TERM/COLORTERM missing: %v", env)
	}
}

func TestShellEnvLeavesLangUnchanged(t *testing.T) {
	base := []string{"PATH=/usr/bin", "LANG=fr_FR.UTF-8"}
	env := shellEnv(base)
	if containsEnv(env, "LANG=en_US.UTF-8") {
		t.Fatalf("LANG overridden: %v", env)
	}
	if !containsEnv(env, "LANG=fr_FR.UTF-8") {
		t.Fatalf("original LANG dropped: %v", env)
	}
}

func TestShellEnvLeavesLcAllUnchanged(t *testing.T) {
	base := []string{"PATH=/usr/bin", "LC_ALL=C"}
	env := shellEnv(base)
	if containsEnv(env, "LANG=en_US.UTF-8") {
		t.Fatalf("LANG added despite LC_ALL: %v", env)
	}
}

func TestShellEnvLeavesLcCtypeUnchanged(t *testing.T) {
	base := []string{"PATH=/usr/bin", "LC_CTYPE=C"}
	env := shellEnv(base)
	if containsEnv(env, "LANG=en_US.UTF-8") {
		t.Fatalf("LANG added despite LC_CTYPE: %v", env)
	}
}

func containsEnv(env []string, kv string) bool {
	for _, e := range env {
		if e == kv {
			return true
		}
	}
	return false
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
