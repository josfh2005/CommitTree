package cmdlog

import (
	"context"
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
	// OutcomeRunning is a write that has started and not ended yet.
	OutcomeRunning   Outcome = "running"
	OutcomeOK        Outcome = "ok"
	OutcomeFailed    Outcome = "failed"
	OutcomeTimeout   Outcome = "timeout"
	OutcomeCancelled Outcome = "cancelled"
)

var ErrNotFound = errors.New("command is no longer in the log")

// ErrNotRunning is Cancel's error for a command that is not running (it
// ended, was never listed, or belongs to another repository).
var ErrNotRunning = errors.New("command is not running")

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

// RepoKey is the key a command run in dir is logged under.
func RepoKey(dir string) string { return filepath.Clean(dir) }

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

// capStream cuts s to MaxStream bytes without splitting a UTF-8 character.
func capStream(s string) (string, bool) {
	if len(s) <= MaxStream {
		return s, false
	}
	return strings.ToValidUTF8(s[:MaxStream], ""), true
}

// maskMargin is how far past MaxStream maskAndCap still scans for a secret,
// so one straddling the eventual cut point is still masked: the margin only
// needs to be at least as long as the longest secret RedactArgs can produce
// (a URL token or password), which is always far under 4 KB.
const maskMargin = 4 << 10

// maskAndCap masks every secret in s and caps it to MaxStream. Masking runs
// on at most MaxStream+maskMargin bytes, not all of s: for a large output
// (RunEnv's caller may hand back megabytes) that avoids scanning what
// capStream would throw away anyway. The returned bool is whether s itself
// (before masking or capping) exceeded MaxStream — masking can shrink a
// string, so that must be decided before it runs.
func maskAndCap(s string, secrets []string) (string, bool) {
	truncated := len(s) > MaxStream
	scan := s
	if len(scan) > MaxStream+maskMargin {
		scan = scan[:MaxStream+maskMargin]
	}
	out, _ := capStream(MaskOutput(scan, secrets))
	return out, truncated
}
