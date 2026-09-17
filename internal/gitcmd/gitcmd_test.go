package gitcmd_test

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

func TestRunReturnsStdout(t *testing.T) {
	r := testrepo.New(t)
	hash := r.Commit("first")

	out, err := gitcmd.Run(context.Background(), r.Dir, gitcmd.ReadTimeout, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != hash {
		t.Fatalf("got %q, want %q", out, hash)
	}
}

func TestRunReturnsTypedErrorWithStderr(t *testing.T) {
	r := testrepo.New(t)

	_, err := gitcmd.Run(context.Background(), r.Dir, gitcmd.ReadTimeout, "rev-parse", "--verify", "nope")

	var gerr *gitcmd.Error
	if !errors.As(err, &gerr) {
		t.Fatalf("want *gitcmd.Error, got %T: %v", err, err)
	}
	if gerr.ExitCode == 0 {
		t.Fatal("want non-zero exit code")
	}
	if !strings.Contains(gerr.Stderr, "Needed a single revision") {
		t.Fatalf("stderr = %q", gerr.Stderr)
	}
	if !strings.Contains(gerr.Error(), "Needed a single revision") {
		t.Fatalf("Error() = %q", gerr.Error())
	}
}

func TestRunTimeout(t *testing.T) {
	r := testrepo.New(t)

	_, err := gitcmd.Run(context.Background(), r.Dir, time.Nanosecond, "status")

	if !errors.Is(err, gitcmd.ErrTimeout) {
		t.Fatalf("want ErrTimeout, got %v", err)
	}
}

// TestRunBoundedByWaitDelayWhenChildOrphansHoldPipesOpen simulates a killed
// git whose child process holds the stdout pipe open (e.g. a credential
// helper or pager left running). Without cmd.WaitDelay, Run would block
// until that orphan closes the pipe on its own, well past the timeout.
func TestRunBoundedByWaitDelayWhenChildOrphansHoldPipesOpen(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "git")
	// The direct child dies almost immediately when killed; the background
	// subshell it spawns inherits the stdout/stderr pipe and keeps it open
	// for far longer than the WaitDelay under test.
	content := "#!/bin/sh\nsh -c 'sleep 10' &\nsleep 5\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}

	oldPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Setenv("PATH", oldPath) })

	start := time.Now()
	_, err := gitcmd.Run(context.Background(), t.TempDir(), 200*time.Millisecond, "status")
	elapsed := time.Since(start)

	if !errors.Is(err, gitcmd.ErrTimeout) {
		t.Fatalf("want ErrTimeout, got %v", err)
	}
	if elapsed > 7*time.Second {
		t.Fatalf("Run took %s, want it bounded by WaitDelay well under the orphan's 10s hold", elapsed)
	}
}
