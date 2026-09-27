// Package gitcmd runs the git CLI.
package gitcmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"
)

const (
	ReadTimeout    = 10 * time.Second
	NetworkTimeout = 5 * time.Minute
	// HookTimeout covers a git command that may run the user's hooks: a
	// pre-commit lint or test run, or a signing passphrase prompt, takes far
	// longer than an ordinary read.
	HookTimeout = 2 * time.Minute
	// BlameTimeout covers git blame, which walks a file's whole history and
	// can take far longer than an ordinary read on a long-lived file.
	BlameTimeout = 30 * time.Second
)

var ErrTimeout = errors.New("git command timed out")

// ErrCancelled is wrapped by the error of a command stopped through its
// Start.Cancel — the Commands panel's Cancel button.
var ErrCancelled = errors.New("git command cancelled")

// Error is returned when git exits unsuccessfully.
type Error struct {
	Args     []string
	Stderr   string
	ExitCode int
	Err      error
}

func (e *Error) Error() string {
	// A cancel's stderr is whatever git or a hook printed before the signal
	// reached it — hook output, or a shell's "Killed by signal 2." — which
	// says nothing useful about why the command stopped; ErrCancelled's own
	// message is what the user needs to see.
	if errors.Is(e.Err, ErrCancelled) {
		return fmt.Sprintf("git %s: %s", strings.Join(e.Args, " "), ErrCancelled.Error())
	}
	msg := strings.TrimSpace(e.Stderr)
	if msg == "" {
		msg = e.Err.Error()
	}
	return fmt.Sprintf("git %s: %s", strings.Join(e.Args, " "), msg)
}

func (e *Error) Unwrap() error { return e.Err }

// Record is one finished git command, handed to the recorder set with
// SetRecorder — the app's command log.
type Record struct {
	// ID is the ID Recorder.Begin returned for this command; 0 without Begin.
	ID int64
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

// Run executes git with args in dir and returns stdout. Prompts are disabled
// so missing credentials fail instead of hanging, and output is in English so
// callers can match messages.
func Run(ctx context.Context, dir string, timeout time.Duration, args ...string) (string, error) {
	return RunEnv(ctx, dir, timeout, nil, args...)
}

// RunEnv is Run with extra environment entries appended last, so they beat
// anything inherited from the user's shell — GIT_EDITOR=true for a
// --continue that must never open an editor, above all.
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
