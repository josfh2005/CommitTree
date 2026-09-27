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

// Error is returned when git exits unsuccessfully.
type Error struct {
	Args     []string
	Stderr   string
	ExitCode int
	Err      error
}

func (e *Error) Error() string {
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
	out, errOut := stdout.String(), stderr.String()
	rec := Record{Ctx: caller, Dir: dir, Args: args, Start: start, Duration: time.Since(start), Stdout: out, Stderr: errOut}
	if runErr != nil {
		gerr := &Error{Args: args, Stderr: errOut, ExitCode: -1, Err: runErr}
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			gerr.ExitCode = exitErr.ExitCode()
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			gerr.Err = ErrTimeout
		}
		rec.ExitCode, rec.Err = gerr.ExitCode, gerr
		record(rec)
		return out, gerr
	}
	record(rec)
	return out, nil
}
