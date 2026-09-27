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
