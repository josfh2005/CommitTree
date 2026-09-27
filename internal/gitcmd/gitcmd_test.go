package gitcmd_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

// RunEnv's entries win over the inherited environment, which is what makes
// GIT_EDITOR=true reliable for the --continue calls in internal/merge.
func TestRunEnvOverridesTheInheritedEnvironment(t *testing.T) {
	t.Setenv("GIT_EDITOR", "false")
	r := testrepo.New(t)
	r.Commit("base")

	out, err := gitcmd.RunEnv(context.Background(), r.Dir, gitcmd.ReadTimeout,
		[]string{"GIT_EDITOR=true"}, "var", "GIT_EDITOR")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "true" {
		t.Errorf("GIT_EDITOR = %q, want true — the override did not win", strings.TrimSpace(out))
	}
}

type ctxKey struct{}

func TestRecorderSeesSuccessAndFailure(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("first")
	var mu sync.Mutex
	var got []gitcmd.Record
	gitcmd.SetRecorder(&gitcmd.Recorder{End: func(rec gitcmd.Record) {
		mu.Lock()
		got = append(got, rec)
		mu.Unlock()
	}})
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
	gitcmd.SetRecorder(&gitcmd.Recorder{End: func(gitcmd.Record) { panic("boom") }})
	t.Cleanup(func() { gitcmd.SetRecorder(nil) })

	out, err := gitcmd.Run(context.Background(), r.Dir, gitcmd.ReadTimeout, "rev-parse", "HEAD")
	if err != nil || strings.TrimSpace(out) != hash {
		t.Fatalf("got %q, %v", out, err)
	}
}

func TestRecorderBeginAndEndShareID(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("first")
	var mu sync.Mutex
	var begun []gitcmd.Start
	var ended []gitcmd.Record
	gitcmd.SetRecorder(&gitcmd.Recorder{
		Begin: func(s gitcmd.Start) int64 {
			mu.Lock()
			defer mu.Unlock()
			begun = append(begun, s)
			return 42
		},
		End: func(rec gitcmd.Record) {
			mu.Lock()
			defer mu.Unlock()
			ended = append(ended, rec)
		},
	})
	t.Cleanup(func() { gitcmd.SetRecorder(nil) })

	if _, err := gitcmd.Run(context.Background(), r.Dir, gitcmd.ReadTimeout, "rev-parse", "HEAD"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(begun) != 1 || len(ended) != 1 {
		t.Fatalf("begun %d, ended %d", len(begun), len(ended))
	}
	b := begun[0]
	if b.Dir != r.Dir || strings.Join(b.Args, " ") != "rev-parse HEAD" || b.Cancel == nil || b.Start.IsZero() {
		t.Fatalf("start wrong: %+v", b)
	}
	if ended[0].ID != 42 || !ended[0].Start.Equal(b.Start) {
		t.Fatalf("end must carry Begin's ID and start: %+v", ended[0])
	}
}

func TestPanickingBeginDoesNotBreakRun(t *testing.T) {
	r := testrepo.New(t)
	hash := r.Commit("first")
	var gotID int64 = -1
	gitcmd.SetRecorder(&gitcmd.Recorder{
		Begin: func(gitcmd.Start) int64 { panic("boom") },
		End:   func(rec gitcmd.Record) { gotID = rec.ID },
	})
	t.Cleanup(func() { gitcmd.SetRecorder(nil) })

	out, err := gitcmd.Run(context.Background(), r.Dir, gitcmd.ReadTimeout, "rev-parse", "HEAD")
	if err != nil || strings.TrimSpace(out) != strings.TrimSpace(hash) {
		t.Fatalf("got %q, %v", out, err)
	}
	if gotID != 0 {
		t.Fatalf("a panicking Begin must yield ID 0, got %d", gotID)
	}
}
