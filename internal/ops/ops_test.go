package ops_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"git-ui/internal/gitcmd"
	"git-ui/internal/ops"
	"git-ui/internal/testrepo"
)

var ctx = context.Background()

// clones returns two clones of a remote that has branches main and feature.
func clones(t *testing.T) (*testrepo.Repo, *testrepo.Repo) {
	t.Helper()
	src := testrepo.New(t)
	src.Commit("base")
	src.Git("branch", "feature")
	bare := testrepo.NewBareFrom(t, src)
	return testrepo.Clone(t, bare), testrepo.Clone(t, bare)
}

func TestFetchAndPullFastForward(t *testing.T) {
	a, b := clones(t)
	h := b.Commit("from b")
	b.Git("push", "-q", "origin", "main")

	if err := ops.Fetch(ctx, a.Dir); err != nil {
		t.Fatal(err)
	}
	if got := a.Git("rev-parse", "origin/main"); got != h {
		t.Fatalf("origin/main = %s, want %s", got, h)
	}
	if err := ops.Pull(ctx, a.Dir); err != nil {
		t.Fatal(err)
	}
	if got := a.Git("rev-parse", "HEAD"); got != h {
		t.Fatalf("HEAD = %s, want %s", got, h)
	}
}

func TestPullRefusesDivergedHistory(t *testing.T) {
	a, b := clones(t)
	b.Commit("from b")
	b.Git("push", "-q", "origin", "main")
	a.Commit("local only")

	err := ops.Pull(ctx, a.Dir)
	if !errors.Is(err, ops.ErrNotFastForward) {
		t.Fatalf("want ErrNotFastForward, got %v", err)
	}
}

func TestCheckoutRemoteCreatesTrackingBranch(t *testing.T) {
	a, _ := clones(t)

	if err := ops.CheckoutRemote(ctx, a.Dir, "origin", "feature"); err != nil {
		t.Fatal(err)
	}
	if name := a.Git("symbolic-ref", "--short", "HEAD"); name != "feature" {
		t.Fatalf("branch = %s", name)
	}
	if up := a.Git("rev-parse", "--abbrev-ref", "feature@{upstream}"); up != "origin/feature" {
		t.Fatalf("upstream = %s", up)
	}

	a.Git("switch", "-q", "main")
	if err := ops.CheckoutRemote(ctx, a.Dir, "origin", "feature"); err != nil {
		t.Fatalf("second checkout of existing local branch: %v", err)
	}
}

func TestCheckoutAndDetached(t *testing.T) {
	r := testrepo.New(t)
	first := r.Commit("first")
	r.Git("branch", "other")
	r.Commit("second")

	if err := ops.Checkout(ctx, r.Dir, "other"); err != nil {
		t.Fatal(err)
	}
	if head := r.Git("rev-parse", "HEAD"); head != first {
		t.Fatalf("HEAD = %s", head)
	}
	if err := ops.CheckoutDetached(ctx, r.Dir, first); err != nil {
		t.Fatal(err)
	}
	if _, err := gitcmd.Run(ctx, r.Dir, gitcmd.ReadTimeout, "symbolic-ref", "-q", "HEAD"); err == nil {
		t.Fatal("HEAD is not detached")
	}
	if err := ops.Checkout(ctx, r.Dir, "-f"); !errors.Is(err, ops.ErrInvalidRef) {
		t.Fatalf("want ErrInvalidRef, got %v", err)
	}
}

func TestCheckoutConflictReturnsGitError(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	r.Git("switch", "-q", "-c", "other")
	r.WriteFile("file-1.txt", "other\n")
	r.Git("commit", "-q", "-am", "change on other")
	r.Git("switch", "-q", "main")
	r.WriteFile("file-1.txt", "dirty\n")

	err := ops.Checkout(ctx, r.Dir, "other")
	var gerr *gitcmd.Error
	if !errors.As(err, &gerr) || !strings.Contains(gerr.Stderr, "would be overwritten") {
		t.Fatalf("want git overwrite error, got %v", err)
	}
}
