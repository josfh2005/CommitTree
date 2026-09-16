package gitcmd_test

import (
	"context"
	"errors"
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
