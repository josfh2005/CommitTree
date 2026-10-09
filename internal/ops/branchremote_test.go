package ops_test

import (
	"errors"
	"strings"
	"testing"

	"git-ui/internal/ops"
	"git-ui/internal/testrepo"
)

func TestFetchRemoteFetchesOnlyThatRemote(t *testing.T) {
	a, b := clones(t)
	h := b.Commit("from b")
	b.Git("push", "-q", "origin", "main")
	other := testrepo.NewBareFrom(t, b)
	a.Git("remote", "add", "other", other)

	if err := ops.FetchRemote(ctx, a.Dir, "origin"); err != nil {
		t.Fatal(err)
	}
	if got := a.Git("rev-parse", "origin/main"); got != h {
		t.Errorf("origin/main = %s, want %s", got, h)
	}
	if out := a.Git("for-each-ref", "refs/remotes/other"); out != "" {
		t.Errorf("the other remote was fetched too: %s", out)
	}
	if err := ops.FetchRemote(ctx, a.Dir, "-x"); !errors.Is(err, ops.ErrInvalidRef) {
		t.Errorf("err = %v, want ErrInvalidRef", err)
	}
}

// behindClone is a clone whose develop branch (not checked out) is behind
// origin/develop by one commit that the clone has already fetched.
func behindClone(t *testing.T) (*testrepo.Repo, string) {
	t.Helper()
	a, b := clones(t)
	b.Git("switch", "-q", "-c", "develop", "origin/main")
	h := b.Commit("remote develop")
	b.Git("push", "-q", "origin", "develop")
	a.Git("branch", "develop", "origin/main")
	a.Git("fetch", "-q", "origin")
	a.Git("branch", "-q", "--set-upstream-to=origin/develop", "develop")
	return a, h
}

func TestFastForwardBranchMovesANonCurrentBranch(t *testing.T) {
	a, h := behindClone(t)
	mainBefore := a.Git("rev-parse", "main")
	moved, err := ops.FastForwardBranch(ctx, a.Dir, "develop")
	if err != nil || !moved {
		t.Fatalf("moved = %v, err = %v, want moved", moved, err)
	}
	if got := a.Git("rev-parse", "develop"); got != h {
		t.Errorf("develop = %s, want %s", got, h)
	}
	if a.Git("rev-parse", "main") != mainBefore || a.Git("branch", "--show-current") != "main" {
		t.Error("the current branch moved")
	}
}

func TestFastForwardBranchFetchesNewCommitsItself(t *testing.T) {
	a, b := clones(t)
	a.Git("branch", "--track", "feature2", "origin/feature")
	b.Git("switch", "-q", "feature")
	h := b.Commit("more")
	b.Git("push", "-q", "origin", "feature")
	// a never fetched: the branch still reaches the commit.
	if moved, err := ops.FastForwardBranch(ctx, a.Dir, "feature2"); err != nil || !moved {
		t.Fatalf("moved = %v, err = %v, want moved", moved, err)
	}
	if got := a.Git("rev-parse", "feature2"); got != h {
		t.Errorf("feature2 = %s, want %s", got, h)
	}
}

func TestFastForwardBranchRefusesADivergedBranch(t *testing.T) {
	a, _ := behindClone(t)
	a.Git("switch", "-q", "develop")
	local := a.Commit("local develop")
	a.Git("switch", "-q", "main")
	moved, err := ops.FastForwardBranch(ctx, a.Dir, "develop")
	if moved || err == nil || err.Error() != "develop has diverged — check it out to pull" {
		t.Fatalf("err = %v, want the diverged message", err)
	}
	if a.Git("rev-parse", "develop") != local {
		t.Error("develop moved")
	}
}

func TestFastForwardBranchOnlyAheadOfItsUpstreamIsUpToDate(t *testing.T) {
	a, _ := aheadClone(t) // develop (not checked out) has a commit origin/develop lacks
	before := a.Git("rev-parse", "develop")
	moved, err := ops.FastForwardBranch(ctx, a.Dir, "develop")
	if err != nil || moved {
		t.Fatalf("moved = %v, err = %v, want no change and no error", moved, err)
	}
	if a.Git("rev-parse", "develop") != before {
		t.Error("develop moved")
	}
}

func TestFastForwardBranchAlreadyAtItsUpstreamDoesNotMove(t *testing.T) {
	a, _ := clones(t)
	a.Git("branch", "--track", "feature2", "origin/feature")
	if moved, err := ops.FastForwardBranch(ctx, a.Dir, "feature2"); err != nil || moved {
		t.Fatalf("moved = %v, err = %v, want no change", moved, err)
	}
}

func TestFastForwardBranchRefusesWithoutAnUpstreamOrOnALocalOne(t *testing.T) {
	a, _ := clones(t)
	a.Git("branch", "loose")
	a.Git("branch", "--track", "child", "main")
	if _, err := ops.FastForwardBranch(ctx, a.Dir, "loose"); !errors.Is(err, ops.ErrNoUpstream) {
		t.Errorf("no upstream: err = %v, want ErrNoUpstream", err)
	}
	if _, err := ops.FastForwardBranch(ctx, a.Dir, "child"); !errors.Is(err, ops.ErrUpstreamLocal) {
		t.Errorf("local upstream: err = %v, want ErrUpstreamLocal", err)
	}
	if _, err := ops.FastForwardBranch(ctx, a.Dir, "main"); err == nil {
		t.Error("the current branch must go through pull")
	}
}

func TestFastForwardBranchSurfacesGitsMessageForAWorktreeBranch(t *testing.T) {
	a, _ := behindClone(t)
	a.Git("worktree", "add", "-q", t.TempDir()+"/wt", "develop")
	_, err := ops.FastForwardBranch(ctx, a.Dir, "develop")
	if err == nil || !strings.Contains(err.Error(), "checked out") {
		t.Fatalf("err = %v, want git's checked-out message", err)
	}
}

func TestPushBranchPushesOnlyThatNonCurrentBranch(t *testing.T) {
	a, _ := aheadClone(t)
	mainRemote := remoteHash(a, "origin", "main")
	got, err := ops.PushBranch(ctx, a.Dir, "feature/x")
	if err != nil {
		t.Fatal(err)
	}
	want := ops.BranchPushResult{Branch: "feature/x", Target: "origin/feature/x", Status: ops.PushPushed}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if remoteHash(a, "origin", "feature/x") != a.Git("rev-parse", "feature/x") {
		t.Error("feature/x did not reach the remote")
	}
	if remoteHash(a, "origin", "main") != mainRemote || remoteHash(a, "origin", "develop") == a.Git("rev-parse", "develop") {
		t.Error("another branch was pushed")
	}
}

func TestPushBranchReportsUpToDate(t *testing.T) {
	a, _ := aheadClone(t)
	a.Git("push", "-q", "origin", "main")
	got, err := ops.PushBranch(ctx, a.Dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != ops.PushUpToDate {
		t.Errorf("status = %s, want upToDate", got.Status)
	}
}

func TestPushBranchPublishesABranchWithNoUpstream(t *testing.T) {
	a, _ := aheadClone(t)
	a.Git("branch", "topic")
	got, err := ops.PushBranch(ctx, a.Dir, "topic")
	if err != nil {
		t.Fatal(err)
	}
	want := ops.BranchPushResult{Branch: "topic", Target: "origin/topic", Status: ops.PushPushed}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if up := a.Git("rev-parse", "--abbrev-ref", "topic@{upstream}"); up != "origin/topic" {
		t.Errorf("upstream = %q, want origin/topic", up)
	}
}

func TestPushBranchRejectedGivesThePullFirstReason(t *testing.T) {
	a, bare := aheadClone(t)
	b := testrepo.Clone(t, bare)
	b.Commit("remote main")
	b.Git("push", "-q", "origin", "main")
	got, err := ops.PushBranch(ctx, a.Dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	want := ops.BranchPushResult{Branch: "main", Target: "origin/main", Status: ops.PushRejected, Reason: "The remote has commits you don't have — pull main first"}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestPushBranchRefusesABranchTrackingALocalOne(t *testing.T) {
	a, _ := aheadClone(t)
	a.Git("branch", "--track", "child", "main")
	if _, err := ops.PushBranch(ctx, a.Dir, "child"); !errors.Is(err, ops.ErrUpstreamLocal) {
		t.Errorf("err = %v, want ErrUpstreamLocal", err)
	}
	if _, err := ops.PushBranch(ctx, a.Dir, "missing"); err == nil {
		t.Error("an unknown branch must be an error")
	}
}

func TestPushBranchPublishesToTheOnlyRemoteWhenThereIsNoOrigin(t *testing.T) {
	a, _ := aheadClone(t)
	a.Git("remote", "rename", "origin", "backup")
	a.Git("branch", "topic")
	got, err := ops.PushBranch(ctx, a.Dir, "topic")
	if err != nil {
		t.Fatal(err)
	}
	want := ops.BranchPushResult{Branch: "topic", Target: "backup/topic", Status: ops.PushPushed}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestPushBranchWithNoOriginAndSeveralRemotesHasNowhereToPublish(t *testing.T) {
	a, _ := aheadClone(t)
	a.Git("remote", "rename", "origin", "backup")
	a.Git("remote", "add", "other", testrepo.NewBareFrom(t, a))
	a.Git("branch", "topic")
	if _, err := ops.PushBranch(ctx, a.Dir, "topic"); !errors.Is(err, ops.ErrNoRemote) {
		t.Errorf("err = %v, want ErrNoRemote", err)
	}
}

func TestPushBranchWithAGoneUpstreamRecreatesTheRemoteBranch(t *testing.T) {
	a, bare := aheadClone(t)
	b := testrepo.Clone(t, bare)
	b.Git("push", "-q", "origin", "--delete", "develop")
	a.Git("fetch", "-q", "--prune")
	got, err := ops.PushBranch(ctx, a.Dir, "develop")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != ops.PushPushed || got.Target != "origin/develop" {
		t.Fatalf("got %+v, want pushed to origin/develop", got)
	}
	if remoteHash(a, "origin", "develop") != a.Git("rev-parse", "develop") {
		t.Error("develop was not recreated on the remote")
	}
}
