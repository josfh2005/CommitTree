//go:build !windows

package gitcmd_test

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"git-ui/internal/gitcmd"
)

// TestSessionHelper is not a test: git runs it through an alias to print
// the session it lives in.
func TestSessionHelper(t *testing.T) {
	if os.Getenv("GITCMD_SESSION_HELPER") != "1" {
		t.Skip("helper")
	}
	sid, _ := syscall.Getsid(0)
	fmt.Print(sid)
	os.Exit(0)
}

// git runs in a session of its own, so neither it nor ssh can open the
// terminal the app was started from to ask for a passphrase.
func TestGitRunsInASessionOfItsOwn(t *testing.T) {
	alias := "alias.sid=!" + os.Args[0] + " -test.run=^TestSessionHelper$"
	out, err := gitcmd.RunEnv(context.Background(), t.TempDir(), 30*time.Second,
		[]string{"GITCMD_SESSION_HELPER=1"}, "-c", alias, "sid")
	if err != nil {
		t.Fatal(err)
	}
	child, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		t.Fatalf("helper printed %q", out)
	}
	own, _ := syscall.Getsid(0)
	if child == own {
		t.Errorf("git shares the test's session %d", own)
	}
}
