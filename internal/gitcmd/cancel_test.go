//go:build !windows

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

// sleepyCommit prepares r so that committing runs a pre-commit hook that
// sleeps for 30 s, and returns the -c argument that enables that hook.
func sleepyCommit(t *testing.T, r *testrepo.Repo) string {
	t.Helper()
	hooks := t.TempDir()
	if err := os.WriteFile(filepath.Join(hooks, "pre-commit"), []byte("#!/bin/sh\nsleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	r.WriteFile("new.txt", "x\n")
	r.Git("add", "new.txt")
	return "core.hooksPath=" + hooks
}

func TestCancelStopsCommitAndItsHook(t *testing.T) {
	r := testrepo.New(t)
	head := strings.TrimSpace(r.Commit("first"))
	hooksPath := sleepyCommit(t, r)
	gitcmd.SetRecorder(&gitcmd.Recorder{Begin: func(s gitcmd.Start) int64 {
		time.AfterFunc(300*time.Millisecond, s.Cancel)
		return 1
	}})
	t.Cleanup(func() { gitcmd.SetRecorder(nil) })

	started := time.Now()
	_, err := gitcmd.Run(context.Background(), r.Dir, gitcmd.HookTimeout, "-c", hooksPath, "commit", "-m", "x")
	if !errors.Is(err, gitcmd.ErrCancelled) {
		t.Fatalf("got %v, want ErrCancelled", err)
	}
	if d := time.Since(started); d > 4*time.Second {
		t.Fatalf("took %v: the hook was not interrupted", d)
	}
	if got := strings.TrimSpace(r.Git("rev-parse", "HEAD")); got != head {
		t.Fatal("a cancelled commit must not be made")
	}
	if _, err := os.Stat(filepath.Join(r.Dir, ".git", "index.lock")); !os.IsNotExist(err) {
		t.Fatalf("index.lock left behind: %v", err)
	}
}

func TestTimeoutIsNotCancel(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("first")
	hooksPath := sleepyCommit(t, r)

	started := time.Now()
	_, err := gitcmd.Run(context.Background(), r.Dir, 300*time.Millisecond, "-c", hooksPath, "commit", "-m", "x")
	if !errors.Is(err, gitcmd.ErrTimeout) || errors.Is(err, gitcmd.ErrCancelled) {
		t.Fatalf("got %v, want ErrTimeout only", err)
	}
	if d := time.Since(started); d > 4*time.Second {
		t.Fatalf("took %v: the hook was not interrupted on timeout", d)
	}
}
