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
	"time"
)

const (
	ReadTimeout    = 10 * time.Second
	NetworkTimeout = 5 * time.Minute
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

// Run executes git with args in dir and returns stdout. Prompts are disabled
// so missing credentials fail instead of hanging, and output is in English so
// callers can match messages.
func Run(ctx context.Context, dir string, timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	if err := cmd.Run(); err != nil {
		gerr := &Error{Args: args, Stderr: stderr.String(), ExitCode: -1, Err: err}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			gerr.ExitCode = exitErr.ExitCode()
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			gerr.Err = ErrTimeout
		}
		return stdout.String(), gerr
	}
	return stdout.String(), nil
}
