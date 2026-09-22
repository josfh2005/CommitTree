package ops_test

import (
	"context"
	"errors"
	"slices"
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
	result, err := ops.Pull(ctx, a.Dir, ops.StrategyAuto)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != ops.Merged {
		t.Errorf("outcome = %v, want Merged", result.Outcome)
	}
	if got := a.Git("rev-parse", "HEAD"); got != h {
		t.Fatalf("HEAD = %s, want %s", got, h)
	}
}

func TestPullReportsUpToDate(t *testing.T) {
	a, _ := clones(t)
	result, err := ops.Pull(ctx, a.Dir, ops.StrategyAuto)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != ops.UpToDate {
		t.Errorf("outcome = %v, want UpToDate", result.Outcome)
	}
}

func TestPullMergesDivergedHistoryWithMergeStrategy(t *testing.T) {
	a, b := clones(t)
	b.Commit("from b")
	b.Git("push", "-q", "origin", "main")
	a.Commit("local only")

	result, err := ops.Pull(ctx, a.Dir, ops.StrategyMerge)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != ops.Merged {
		t.Fatalf("outcome = %v, want Merged", result.Outcome)
	}
	if got := a.Git("log", "-1", "--format=%P"); !strings.Contains(got, " ") {
		t.Errorf("HEAD parents = %q, want two — a merge commit", got)
	}
}

func TestPullRebasesDivergedHistoryWithRebaseStrategy(t *testing.T) {
	a, b := clones(t)
	b.Commit("from b")
	b.Git("push", "-q", "origin", "main")
	a.Commit("local only")

	result, err := ops.Pull(ctx, a.Dir, ops.StrategyRebase)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != ops.Rebased {
		t.Fatalf("outcome = %v, want Rebased", result.Outcome)
	}
	if got := a.Git("log", "-1", "--format=%P"); strings.Contains(got, " ") {
		t.Errorf("HEAD parents = %q, want one — a rebase, not a merge commit", got)
	}
}

func TestPullConflictLeavesTheRepositoryForTheConflictViewToShow(t *testing.T) {
	a, b := clones(t)
	a.WriteFile("file-1.txt", "a's change\n")
	a.Git("commit", "-q", "-am", "a's change")
	b.WriteFile("file-1.txt", "b's change\n")
	b.Git("commit", "-q", "-am", "b's change")
	b.Git("push", "-q", "origin", "main")

	result, err := ops.Pull(ctx, a.Dir, ops.StrategyMerge)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != ops.Conflicted || !slices.Contains(result.Conflicts, "file-1.txt") {
		t.Fatalf("result = %+v, want Conflicted on file-1.txt", result)
	}
}

func TestPullRefusesWhileAConflictIsUnresolved(t *testing.T) {
	a, b := clones(t)
	a.WriteFile("file-1.txt", "a's change\n")
	a.Git("commit", "-q", "-am", "a's change")
	b.WriteFile("file-1.txt", "b's change\n")
	b.Git("commit", "-q", "-am", "b's change")
	b.Git("push", "-q", "origin", "main")
	if _, err := ops.Pull(ctx, a.Dir, ops.StrategyMerge); err != nil {
		t.Fatal(err)
	}

	// The first pull left a conflict; a second one must refuse rather than
	// report the conflict it did not cause.
	if _, err := ops.Pull(ctx, a.Dir, ops.StrategyMerge); !errors.Is(err, ops.ErrResolutionInProgress) {
		t.Errorf("err = %v, want ErrResolutionInProgress", err)
	}
}

func TestPullRefusesAnUnknownStrategy(t *testing.T) {
	a, _ := clones(t)
	if _, err := ops.Pull(ctx, a.Dir, "sometimes"); !errors.Is(err, ops.ErrInvalidStrategy) {
		t.Errorf("err = %v, want ErrInvalidStrategy", err)
	}
}

func TestPushSetsUpstreamOnFirstPush(t *testing.T) {
	a, _ := clones(t)
	a.Git("switch", "-q", "-c", "topic")
	a.Commit("on topic")

	if err := ops.Push(ctx, a.Dir); err != nil {
		t.Fatal(err)
	}
	if up := a.Git("rev-parse", "--abbrev-ref", "topic@{upstream}"); up != "origin/topic" {
		t.Errorf("upstream = %q, want origin/topic", up)
	}
}

func TestPushToAnExistingUpstream(t *testing.T) {
	a, b := clones(t)
	h := a.Commit("from a")
	if err := ops.Push(ctx, a.Dir); err != nil {
		t.Fatal(err)
	}
	if err := ops.Fetch(ctx, b.Dir); err != nil {
		t.Fatal(err)
	}
	if got := b.Git("rev-parse", "origin/main"); got != h {
		t.Fatalf("origin/main on b = %s, want %s", got, h)
	}
}

func TestCountsAheadAndBehind(t *testing.T) {
	a, b := clones(t)
	a.Commit("local")
	b.Commit("remote")
	b.Git("push", "-q", "origin", "main")
	if err := ops.Fetch(ctx, a.Dir); err != nil {
		t.Fatal(err)
	}

	counts, err := ops.Counts(ctx, a.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if counts.Ahead != 1 || counts.Behind != 1 {
		t.Errorf("counts = %+v, want 1 ahead, 1 behind", counts)
	}
}

func TestCountsWithNoUpstream(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	counts, err := ops.Counts(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if counts.Ahead != 0 || counts.Behind != 0 {
		t.Errorf("counts = %+v, want zero with no upstream", counts)
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
